// Package app contains the full kcert-checker runtime. Call Run() from main.
package app

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/sirajudheenam/kcert-checker/internal/config"
	"github.com/sirajudheenam/kcert-checker/internal/health"
	kubeclient "github.com/sirajudheenam/kcert-checker/internal/kubernetes"
	"github.com/sirajudheenam/kcert-checker/internal/metrics"
	"github.com/sirajudheenam/kcert-checker/internal/output"
	"github.com/sirajudheenam/kcert-checker/internal/result"
	"github.com/sirajudheenam/kcert-checker/internal/scanner"
	"github.com/sirajudheenam/kcert-checker/internal/ui"
)

// Exit codes
const (
	ExitOK       = 0
	ExitWarning  = 1
	ExitCritical = 2
	ExitExpired  = 3
	ExitFailure  = 10
)

// Options holds the parsed CLI flags passed in from main.
type Options struct {
	ConfigPath   string
	OutputFormat string
	ListenAddr   string
	FailOn       string
	Once         bool
}

// Run is the full kcert-checker entrypoint. Call this from main() after
// parsing flags. Exits the process on fatal errors.
func Run(opts Options) {
	cfg, err := config.Load(opts.ConfigPath)
	if err != nil {
		log.Printf("failed to load config: %v", err)
		os.Exit(ExitFailure)
	}

	if opts.ListenAddr != "" {
		cfg.Metrics.ListenAddress = opts.ListenAddr
	}

	client, err := kubeclient.NewClient(cfg.Kubernetes)
	if err != nil {
		log.Printf("failed to create Kubernetes client: %v", err)
		os.Exit(ExitFailure)
	}
	log.Println("Kubernetes client created successfully")

	containerReader := scanner.NewContainerReader(client.Clientset, client.Config)
	metricsClient := metrics.New()

	kcertScanner := scanner.New(
		client.Clientset,
		containerReader,
		metricsClient,
		cfg.Scanner,
	)

	thresholds := result.Thresholds{
		WarningDays:  cfg.Scan.WarningDays,
		CriticalDays: cfg.Scan.CriticalDays,
	}

	healthHandler := &health.Handler{}
	uiHandler := ui.New(thresholds)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	mux := http.NewServeMux()
	mux.Handle(cfg.Metrics.Path, promhttp.Handler())
	mux.HandleFunc("/healthz", healthHandler.Healthz)
	mux.HandleFunc("/readyz", healthHandler.Readyz)
	mux.HandleFunc("/ui", uiHandler.Dashboard)
	mux.HandleFunc("/api/certificates", uiHandler.Certificates)

	srv := &http.Server{
		Addr:    cfg.Metrics.ListenAddress,
		Handler: mux,
	}

	if cfg.Metrics.Enabled && !opts.Once {
		go func() {
			log.Printf("HTTP server listening on %s (metrics=%s, /healthz, /readyz, /ui)",
				cfg.Metrics.ListenAddress, cfg.Metrics.Path)
			if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				log.Fatalf("HTTP server failed to start on %s: %v\n"+
					"  → Free the port with: lsof -ti :%s | xargs kill\n"+
					"  → Or use a different port: --listen :9090",
					cfg.Metrics.ListenAddress, err,
					portFromAddr(cfg.Metrics.ListenAddress))
			}
		}()

		go func() {
			<-ctx.Done()
			shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer shutdownCancel()
			if err := srv.Shutdown(shutdownCtx); err != nil {
				log.Printf("HTTP server shutdown error: %v", err)
			}
		}()
	}

	runScan := func() int {
		results := kcertScanner.Scan(ctx)
		metricsClient.SetScanTimestamp(time.Now())

		if err := output.Write(os.Stdout, results, thresholds, output.Format(opts.OutputFormat)); err != nil {
			log.Printf("output error: %v", err)
		}

		uiHandler.SetResults(results)
		return worstExitCode(results, thresholds, opts.FailOn)
	}

	log.Println("starting initial certificate scan")
	code := runScan()
	healthHandler.MarkReady()

	if opts.Once || opts.FailOn != "" {
		os.Exit(code)
	}

	ticker := time.NewTicker(time.Duration(cfg.Scan.IntervalSeconds) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Println("shutting down")
			return
		case <-ticker.C:
			log.Println("starting certificate scan")
			runScan()
		}
	}
}

func worstExitCode(results []result.CertificateResult, t result.Thresholds, failOn string) int {
	if failOn == "" {
		return ExitOK
	}

	worst := result.StatusOK
	for _, r := range results {
		s := t.Classify(r.DaysLeft)
		if statusSeverity(s) > statusSeverity(worst) {
			worst = s
		}
	}

	switch failOn {
	case "warning":
		switch worst {
		case result.StatusWarning:
			return ExitWarning
		case result.StatusCritical:
			return ExitCritical
		case result.StatusExpired:
			return ExitExpired
		}
	case "critical":
		switch worst {
		case result.StatusCritical:
			return ExitCritical
		case result.StatusExpired:
			return ExitExpired
		}
	case "expired":
		if worst == result.StatusExpired {
			return ExitExpired
		}
	}
	return ExitOK
}

func statusSeverity(s result.Status) int {
	switch s {
	case result.StatusExpired:
		return 3
	case result.StatusCritical:
		return 2
	case result.StatusWarning:
		return 1
	default:
		return 0
	}
}

func portFromAddr(addr string) string {
	if i := strings.LastIndex(addr, ":"); i >= 0 {
		return addr[i+1:]
	}
	return addr
}
