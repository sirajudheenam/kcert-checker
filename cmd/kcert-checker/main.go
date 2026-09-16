// kcert-checker scans every Pod container and Kubernetes Secret across all
// namespaces for X.509 certificates, then exposes their expiry timestamps
// as Prometheus metrics so alert rules can fire before certs expire.
package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/sirajudheenam/kcert-checker/internal/config"
	kubeclient "github.com/sirajudheenam/kcert-checker/internal/kubernetes"
	"github.com/sirajudheenam/kcert-checker/internal/metrics"
	"github.com/sirajudheenam/kcert-checker/internal/scanner"
)

func main() {
	configPath := flag.String(
		"config",
		"config.yaml",
		"Path to configuration file",
	)

	flag.Parse()

	// --------------------------------------------------------
	// Load configuration
	// --------------------------------------------------------

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf(
			"failed to load config: %v",
			err,
		)
	}

	// --------------------------------------------------------
	// Create Kubernetes client
	// --------------------------------------------------------

	client, err := kubeclient.NewClient(cfg.Kubernetes)
	if err != nil {
		log.Fatalf(
			"failed to create Kubernetes client: %v",
			err,
		)
	}

	log.Println("Kubernetes client created successfully")

	// --------------------------------------------------------
	// Create container reader
	// --------------------------------------------------------

	// ContainerReader uses the pod/exec subresource to run `cat <path>`
	// inside each container — requires the pods/exec ClusterRole verb.
	containerReader := scanner.NewContainerReader(
		client.Clientset,
		client.Config,
	)

	// --------------------------------------------------------
	// Create Prometheus metrics
	// --------------------------------------------------------

	metricsClient := metrics.New()

	// --------------------------------------------------------
	// Create scanner
	// --------------------------------------------------------

	kcertScanner := scanner.New(
		client.Clientset,
		containerReader,
		metricsClient,
		cfg.Scanner.Certificates.Paths,
		cfg.Scanner.Containers.IncludeInitContainers,
	)

	// --------------------------------------------------------
	// Context / shutdown
	// --------------------------------------------------------

	// Cancel propagates to all in-flight scans on SIGINT/SIGTERM so the
	// process exits cleanly without leaving orphaned exec sessions.
	ctx, cancel := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)

	defer cancel()

	// --------------------------------------------------------
	// Prometheus HTTP server
	// --------------------------------------------------------

	if cfg.Metrics.Enabled {
		http.Handle(
			cfg.Metrics.Path,
			promhttp.Handler(),
		)

		// Run in a goroutine so it doesn't block the scan loop.
		// A server error cancels the context to trigger a clean shutdown.
		go func() {
			log.Printf(
				"metrics server listening on %s",
				cfg.Metrics.ListenAddress,
			)

			if err := http.ListenAndServe(
				cfg.Metrics.ListenAddress,
				nil,
			); err != nil && err != http.ErrServerClosed {
				log.Printf(
					"metrics server failed: %v",
					err,
				)

				cancel()
			}
		}()
	}

	// --------------------------------------------------------
	// Initial scan
	// --------------------------------------------------------

	// Scan immediately on startup so metrics are available from the first
	// Prometheus scrape rather than waiting for the first ticker tick.
	log.Println("starting initial certificate scan")

	kcertScanner.Scan(ctx)

	metricsClient.SetScanTimestamp(time.Now())

	// --------------------------------------------------------
	// Periodic scan every 24 hours
	// --------------------------------------------------------

	// IntervalHours is validated to be > 0 in config.Validate(),
	// defaulting to 24 if unset or zero.
	ticker := time.NewTicker(
		time.Duration(cfg.Scan.IntervalHours) * time.Hour,
	)

	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Println("shutting down")
			return

		case <-ticker.C:
			log.Println("starting certificate scan")

			kcertScanner.Scan(ctx)

			metricsClient.SetScanTimestamp(time.Now())
		}
	}
}
