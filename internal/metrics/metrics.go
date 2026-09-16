// Package metrics registers and exposes the Prometheus metrics for kcert-checker.
package metrics

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// Metrics holds all Prometheus collectors used by the scanner.
type Metrics struct {
	// CertificateExpiry is a GaugeVec: one series per discovered certificate.
	// Labels: namespace, source_type, source_name, container, path,
	//         subject_common_name, issuer_common_name.
	// Value: Unix timestamp of the certificate's NotAfter field.
	CertificateExpiry   *prometheus.GaugeVec
	CertificatesTotal   prometheus.Gauge
	CertificatesExpired prometheus.Gauge
	ScanTimestamp       prometheus.Gauge
	ScanDuration        prometheus.Histogram
	ScanErrors          prometheus.Counter
}

// New registers all metrics with the default Prometheus registry.
// Must be called once at startup; calling it more than once panics (duplicate registration).
func New() *Metrics {
	return NewWithRegistry(prometheus.DefaultRegisterer)
}

// NewWithRegistry registers all metrics on the provided registerer.
// Pass prometheus.NewRegistry() in tests to get an isolated registry and
// avoid duplicate-registration panics when tests share the same process.
func NewWithRegistry(reg prometheus.Registerer) *Metrics {
	certificateExpiry := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "kcert_certificate_expiry_timestamp_seconds",
			Help: "Unix timestamp when a discovered certificate expires.",
		},
		[]string{"namespace", "source_type", "source_name", "container", "path", "subject_common_name", "issuer_common_name"},
	)

	certificatesTotal := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "kcert_certificates_total",
		Help: "Total number of certificates discovered in the last scan.",
	})

	certificatesExpired := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "kcert_certificates_expired_total",
		Help: "Number of expired certificates discovered in the last scan.",
	})

	scanTimestamp := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "kcert_scan_timestamp_seconds",
		Help: "Unix timestamp of the last completed certificate scan.",
	})

	scanDuration := prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "kcert_scan_duration_seconds",
		Help:    "Duration of the last certificate scan in seconds.",
		Buckets: prometheus.DefBuckets,
	})

	scanErrors := prometheus.NewCounter(prometheus.CounterOpts{
		Name: "kcert_scan_errors_total",
		Help: "Total number of certificate scanning errors.",
	})

	reg.MustRegister(
		certificateExpiry,
		certificatesTotal,
		certificatesExpired,
		scanTimestamp,
		scanDuration,
		scanErrors,
	)

	return &Metrics{
		CertificateExpiry:   certificateExpiry,
		CertificatesTotal:   certificatesTotal,
		CertificatesExpired: certificatesExpired,
		ScanTimestamp:       scanTimestamp,
		ScanDuration:        scanDuration,
		ScanErrors:          scanErrors,
	}
}

// ResetExpiry clears all certificate expiry series so deleted certs do not
// leave stale Prometheus gauge series between scans.
func (m *Metrics) ResetExpiry() {
	m.CertificateExpiry.Reset()
}

// SetCertificateExpiry updates the expiry gauge for one certificate.
// Label values must exactly match the GaugeVec label names declared in NewWithRegistry.
func (m *Metrics) SetCertificateExpiry(
	namespace, sourceType, sourceName, container, path, subjectCN, issuerCN string,
	expiry time.Time,
) {
	m.CertificateExpiry.WithLabelValues(
		namespace, sourceType, sourceName, container, path, subjectCN, issuerCN,
	).Set(float64(expiry.Unix()))
}

// SetScanTimestamp records the time a scan completed.
func (m *Metrics) SetScanTimestamp(t time.Time) {
	m.ScanTimestamp.Set(float64(t.Unix()))
}

// ObserveScanDuration records how long a scan took.
func (m *Metrics) ObserveScanDuration(d time.Duration) {
	m.ScanDuration.Observe(d.Seconds())
}

// SetCertificateCounts updates the total and expired certificate gauges.
func (m *Metrics) SetCertificateCounts(total, expired int) {
	m.CertificatesTotal.Set(float64(total))
	m.CertificatesExpired.Set(float64(expired))
}

// IncScanErrors increments the scan error counter by one.
func (m *Metrics) IncScanErrors() {
	m.ScanErrors.Inc()
}
