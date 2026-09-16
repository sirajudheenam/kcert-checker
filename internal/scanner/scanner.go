package scanner

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/sirajudheenam/kcert-checker/internal/config"
	"github.com/sirajudheenam/kcert-checker/internal/filter"
	"github.com/sirajudheenam/kcert-checker/internal/metrics"
	"github.com/sirajudheenam/kcert-checker/internal/result"
)

// namespaceScanWorkers caps parallel namespace goroutines to avoid exhausting
// the Kubernetes API server with too many concurrent list/exec requests.
const namespaceScanWorkers = 5

// Scanner scans Kubernetes Secrets and certificates inside running Pod containers.
type Scanner struct {
	Clientset             kubernetes.Interface
	ContainerReader       FileReader
	Metrics               *metrics.Metrics
	CertificatePaths      []string
	IncludeInitContainers bool
	ContainerScanMode     string // "exec" or "disabled"
	ExcludeNamespaces     []string
	ExcludePods           []string
	ExcludeContainers     []string
	ExcludeSecrets        []string
}

// New creates a new certificate Scanner.
func New(
	clientset kubernetes.Interface,
	containerReader FileReader,
	metricsClient *metrics.Metrics,
	cfg config.ScannerConfig,
) *Scanner {
	return &Scanner{
		Clientset:             clientset,
		ContainerReader:       containerReader,
		Metrics:               metricsClient,
		CertificatePaths:      cfg.Certificates.Paths,
		IncludeInitContainers: cfg.Containers.IncludeInitContainers,
		ContainerScanMode:     cfg.Containers.Mode,
		ExcludeNamespaces:     cfg.Namespaces.Exclude,
		ExcludePods:           cfg.Pods.Exclude,
		ExcludeContainers:     cfg.Containers.Exclude,
		ExcludeSecrets:        cfg.Secrets.Exclude,
	}
}

// Scan scans all Kubernetes namespaces concurrently, returns all discovered
// certificates, and updates Prometheus metrics.
func (s *Scanner) Scan(ctx context.Context) []result.CertificateResult {
	log.Println("starting Kubernetes certificate scan")
	start := time.Now()

	namespaces, err := s.Clientset.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	if err != nil {
		if ctx.Err() != nil {
			log.Println("scan cancelled")
			return nil
		}
		log.Printf("ERROR listing namespaces: %v", err)
		s.Metrics.IncScanErrors()
		return nil
	}

	s.Metrics.ResetExpiry()

	var (
		mu  sync.Mutex
		all []result.CertificateResult
		sem = make(chan struct{}, namespaceScanWorkers)
		wg  sync.WaitGroup
	)

	for _, ns := range namespaces.Items {
		if ctx.Err() != nil {
			log.Println("scan cancelled")
			break
		}
		if filter.Namespace(ns.Name, s.ExcludeNamespaces) {
			log.Printf("skipping excluded namespace: %s", ns.Name)
			continue
		}

		wg.Add(1)
		sem <- struct{}{}

		go func(namespace string) {
			defer wg.Done()
			defer func() { <-sem }()

			log.Printf("Scanning namespace: %s", namespace)

			results, err := s.scanNamespace(ctx, namespace)
			if err != nil {
				if ctx.Err() != nil {
					return
				}
				log.Printf("ERROR scanning namespace %s: %v", namespace, err)
				s.Metrics.IncScanErrors()
				return
			}

			mu.Lock()
			all = append(all, results...)
			mu.Unlock()
		}(ns.Name)
	}

	wg.Wait()

	if ctx.Err() != nil {
		log.Println("scan cancelled")
		return nil
	}

	expired := 0
	for _, r := range all {
		s.Metrics.SetCertificateExpiry(
			r.Namespace, string(r.SourceType), r.SourceName,
			r.Container, r.Path, r.Subject, r.Issuer, r.NotAfter,
		)
		if r.Expired {
			expired++
		}
	}

	duration := time.Since(start)
	s.Metrics.ObserveScanDuration(duration)
	s.Metrics.SetCertificateCounts(len(all), expired)

	log.Printf("Kubernetes certificate scan completed: %d certificates found (%d expired) in %s",
		len(all), expired, duration.Round(time.Millisecond))
	return all
}

func (s *Scanner) scanNamespace(ctx context.Context, namespace string) ([]result.CertificateResult, error) {
	var all []result.CertificateResult

	secretResults, err := s.scanSecrets(ctx, namespace)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		log.Printf("ERROR scanning Secrets in namespace %s: %v", namespace, err)
		s.Metrics.IncScanErrors()
	}
	all = append(all, secretResults...)

	if s.ContainerScanMode == "disabled" {
		return all, nil
	}

	pods, err := s.Clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		log.Printf("ERROR listing Pods in namespace %s: %v", namespace, err)
		s.Metrics.IncScanErrors()
		return all, err
	}

	for _, pod := range pods.Items {
		if ctx.Err() != nil {
			return all, ctx.Err()
		}
		if pod.Status.Phase != corev1.PodRunning {
			continue
		}
		if filter.Pod(pod.Name, s.ExcludePods) {
			log.Printf("skipping excluded pod: %s/%s", namespace, pod.Name)
			continue
		}

		log.Printf("Scanning Pod: %s/%s", namespace, pod.Name)

		podResults, err := s.scanPod(ctx, &pod)
		if err != nil {
			if ctx.Err() != nil {
				return all, ctx.Err()
			}
			log.Printf("ERROR scanning Pod %s/%s: %v", namespace, pod.Name, err)
			s.Metrics.IncScanErrors()
			continue
		}
		all = append(all, podResults...)
	}

	return all, nil
}

func (s *Scanner) scanPod(ctx context.Context, pod *corev1.Pod) ([]result.CertificateResult, error) {
	containers := pod.Spec.Containers
	if s.IncludeInitContainers {
		containers = append(containers, pod.Spec.InitContainers...)
	}

	var all []result.CertificateResult
	for _, container := range containers {
		if ctx.Err() != nil {
			return all, ctx.Err()
		}
		if filter.Container(container.Name, s.ExcludeContainers) {
			log.Printf("skipping excluded container: %s/%s/%s", pod.Namespace, pod.Name, container.Name)
			continue
		}

		results, err := s.scanContainer(ctx, pod.Namespace, pod.Name, container.Name)
		if err != nil {
			if ctx.Err() != nil {
				return all, ctx.Err()
			}
			log.Printf("ERROR scanning container %s/%s/%s: %v", pod.Namespace, pod.Name, container.Name, err)
			s.Metrics.IncScanErrors()
			continue
		}
		all = append(all, results...)
	}
	return all, nil
}

// execTimeout is the per-path deadline for a single `cat` exec inside a container.
// 5 seconds is enough for a small file read; longer would let a single unresponsive
// container stall the whole scan for that pod.
const execTimeout = 5 * time.Second

func (s *Scanner) scanContainer(ctx context.Context, namespace, pod, container string) ([]result.CertificateResult, error) {
	log.Printf("Scanning container: %s/%s/%s", namespace, pod, container)

	type pathResult struct {
		certs []result.CertificateResult
		err   bool
	}

	// Buffered so goroutines never block sending even if we stop reading early.
	resultsCh := make(chan pathResult, len(s.CertificatePaths))

	// Fan out: probe all configured paths in parallel so a slow exec on one
	// path does not serialize the rest.
	for _, path := range s.CertificatePaths {
		if ctx.Err() != nil {
			break
		}
		path := path
		go func() {
			// Each exec gets its own short deadline so a hung container
			// cannot block the whole scan.
			execCtx, cancel := context.WithTimeout(ctx, execTimeout)
			defer cancel()

			data, err := s.ContainerReader.ReadFile(execCtx, namespace, pod, container, path)
			if err != nil {
				resultsCh <- pathResult{}
				return
			}

			certs, err := ParseCertificates(data)
			if err != nil {
				resultsCh <- pathResult{err: true}
				return
			}

			var rows []result.CertificateResult
			for _, cert := range certs {
				rows = append(rows, buildResult(namespace, pod, container, path, result.SourceContainer, cert))
			}
			resultsCh <- pathResult{certs: rows}
		}()
	}

	var all []result.CertificateResult
	for range s.CertificatePaths {
		r := <-resultsCh
		if r.err {
			s.Metrics.IncScanErrors()
			continue
		}
		all = append(all, r.certs...)
	}
	return all, nil
}

func (s *Scanner) scanSecrets(ctx context.Context, namespace string) ([]result.CertificateResult, error) {
	secrets, err := s.Clientset.CoreV1().Secrets(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, err
	}

	var all []result.CertificateResult
	for _, secret := range secrets.Items {
		if ctx.Err() != nil {
			return all, ctx.Err()
		}
		if secret.Type == corev1.SecretTypeServiceAccountToken {
			// Service account tokens embed a JWT, not a PEM certificate.
			// Attempting to parse them as certs always fails and adds noise.
			continue
		}
		if filter.Secret(secret.Name, s.ExcludeSecrets) {
			log.Printf("skipping excluded secret: %s/%s", namespace, secret.Name)
			continue
		}

		for key, data := range secret.Data {
			if !looksLikeCertificateKey(key) {
				continue
			}
			certs, err := ParseCertificates(data)
			if err != nil {
				continue
			}
			for _, cert := range certs {
				all = append(all, buildResult(namespace, "", "", secret.Name+"/"+key, result.SourceSecret, cert))
			}
		}
	}
	return all, nil
}

func buildResult(namespace, pod, container, path string, sourceType result.SourceType, cert *Certificate) result.CertificateResult {
	daysLeft := int(time.Until(cert.NotAfter).Hours() / 24)
	sum := sha256.Sum256(cert.Raw)
	fingerprint := strings.ToUpper(hex.EncodeToString(sum[:]))

	sourceName := path
	if pod != "" {
		sourceName = pod + ":" + path
	}

	r := result.CertificateResult{
		Namespace:   namespace,
		SourceType:  sourceType,
		SourceName:  sourceName,
		Container:   container,
		Path:        path,
		Subject:     cert.Subject.CommonName,
		Issuer:      cert.Issuer.CommonName,
		Serial:      cert.SerialNumber.String(),
		DNSNames:    cert.DNSNames,
		Fingerprint: fingerprint,
		NotBefore:   cert.NotBefore,
		NotAfter:    cert.NotAfter,
		DaysLeft:    daysLeft,
		IsCA:        cert.IsCA,
		Expired:     daysLeft < 0,
	}

	log.Printf(
		"Certificate found: source=%s namespace=%s source_name=%s subject=%s expires=%s days_remaining=%d",
		r.SourceType, r.Namespace, r.SourceName, r.Subject, r.NotAfter.Format("2006-01-02"), r.DaysLeft,
	)
	return r
}

// ParseCertificates extracts all X.509 certificates from PEM data.
func ParseCertificates(data []byte) ([]*Certificate, error) {
	var certificates []*Certificate
	for len(data) > 0 {
		block, rest := pem.Decode(data)
		if block == nil {
			break
		}
		data = rest
		if block.Type != "CERTIFICATE" {
			continue
		}
		cert, err := ParseCertificate(block.Bytes)
		if err != nil {
			return nil, err
		}
		certificates = append(certificates, cert)
	}
	if len(certificates) == 0 {
		return nil, fmt.Errorf("no X.509 certificate found")
	}
	return certificates, nil
}

// looksLikeCertificateKey reports whether a Secret key probably holds certificate data.
// This avoids calling ParseCertificates on private keys, passwords, or other binary
// blobs that would generate noisy parse errors.
func looksLikeCertificateKey(key string) bool {
	key = strings.ToLower(key)
	exactNames := map[string]struct{}{
		"tls.crt": {}, "ca.crt": {}, "cert": {}, "certificate": {},
		"certificate.crt": {}, "tls.pem": {}, "cert.pem": {}, "ca.pem": {},
		"chain.pem": {}, "fullchain.crt": {}, "fullchain.pem": {},
		"bundle.crt": {}, "bundle.pem": {},
	}
	if _, ok := exactNames[key]; ok {
		return true
	}
	return strings.HasSuffix(key, ".crt") || strings.HasSuffix(key, ".pem")
}
