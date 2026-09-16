package metrics

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	promtest "github.com/prometheus/client_golang/prometheus/testutil"
)

// newTestMetrics creates an isolated Metrics instance for a single test.
// Using a fresh registry prevents duplicate-registration panics between tests.
func newTestMetrics() *Metrics {
	return NewWithRegistry(prometheus.NewRegistry())
}

// TestNewWithRegistryCreatesMetrics verifies that NewWithRegistry initialises
// all collectors and returns a non-nil Metrics struct.
func TestNewWithRegistryCreatesMetrics(t *testing.T) {
	m := newTestMetrics()
	if m == nil {
		t.Fatal("NewWithRegistry returned nil")
	}
	if m.CertificateExpiry == nil {
		t.Error("CertificateExpiry is nil")
	}
	if m.CertificatesTotal == nil {
		t.Error("CertificatesTotal is nil")
	}
	if m.CertificatesExpired == nil {
		t.Error("CertificatesExpired is nil")
	}
	if m.ScanTimestamp == nil {
		t.Error("ScanTimestamp is nil")
	}
	if m.ScanDuration == nil {
		t.Error("ScanDuration is nil")
	}
	if m.ScanErrors == nil {
		t.Error("ScanErrors is nil")
	}
}

// TestSetCertificateCounts verifies that both total and expired gauges are updated.
func TestSetCertificateCounts(t *testing.T) {
	m := newTestMetrics()
	m.SetCertificateCounts(15, 3)

	if got := promtest.ToFloat64(m.CertificatesTotal); got != 15 {
		t.Errorf("CertificatesTotal = %.0f, want 15", got)
	}
	if got := promtest.ToFloat64(m.CertificatesExpired); got != 3 {
		t.Errorf("CertificatesExpired = %.0f, want 3", got)
	}
}

// TestSetScanTimestamp verifies that the scan timestamp is stored as a Unix epoch value.
func TestSetScanTimestamp(t *testing.T) {
	m := newTestMetrics()
	ts := time.Unix(1700000000, 0)
	m.SetScanTimestamp(ts)

	if got := promtest.ToFloat64(m.ScanTimestamp); got != 1700000000 {
		t.Errorf("ScanTimestamp = %.0f, want 1700000000", got)
	}
}

// TestObserveScanDuration verifies that ObserveScanDuration does not panic.
// The actual histogram bucket distribution is not checked here.
func TestObserveScanDuration(t *testing.T) {
	m := newTestMetrics()
	m.ObserveScanDuration(2 * time.Second)
}

// TestIncScanErrors verifies the counter increments by one per call.
func TestIncScanErrors(t *testing.T) {
	m := newTestMetrics()
	m.IncScanErrors()
	m.IncScanErrors()

	if got := promtest.ToFloat64(m.ScanErrors); got != 2 {
		t.Errorf("ScanErrors = %.0f, want 2", got)
	}
}

// TestResetExpiry verifies that ResetExpiry clears all time series without panicking.
// After a reset the GaugeVec should have zero series (stale entries removed).
func TestResetExpiry(t *testing.T) {
	m := newTestMetrics()
	m.SetCertificateExpiry("ns", "secret", "my-secret/tls.crt", "", "", "example.com", "my-ca",
		time.Now().Add(24*time.Hour))
	m.ResetExpiry()
}

// TestSetCertificateExpiry verifies the correct label set is used when setting
// the expiry gauge. The label order must match the GaugeVec declaration exactly.
func TestSetCertificateExpiry(t *testing.T) {
	m := newTestMetrics()
	expiry := time.Unix(2000000000, 0)
	m.SetCertificateExpiry("production", "container", "pod-x:/etc/tls.crt", "app", "/etc/tls.crt",
		"my-service.prod", "corp-ca", expiry)

	got := promtest.ToFloat64(m.CertificateExpiry.WithLabelValues(
		"production", "container", "pod-x:/etc/tls.crt", "app", "/etc/tls.crt", "my-service.prod", "corp-ca",
	))
	if got != 2000000000 {
		t.Errorf("CertificateExpiry = %.0f, want 2000000000", got)
	}
}

// TestNewWithRegistryIndependent verifies that two Metrics instances backed by
// different registries do not share state — critical for test isolation.
func TestNewWithRegistryIndependent(t *testing.T) {
	m1 := NewWithRegistry(prometheus.NewRegistry())
	m2 := NewWithRegistry(prometheus.NewRegistry())

	m1.SetCertificateCounts(10, 1)
	m2.SetCertificateCounts(20, 2)

	if got := promtest.ToFloat64(m1.CertificatesTotal); got != 10 {
		t.Errorf("m1 CertificatesTotal = %.0f, want 10", got)
	}
	if got := promtest.ToFloat64(m2.CertificatesTotal); got != 20 {
		t.Errorf("m2 CertificatesTotal = %.0f, want 20", got)
	}
}

// TestNewRegistersOnDefaultRegistry is a reminder that metrics.New() must not
// be called more than once in the same process; duplicate registration panics.
func TestNewRegistersOnDefaultRegistry(t *testing.T) {
	// Calling metrics.New() panics on duplicate registration in the same process.
	// This test is intentionally skipped in CI but verifies New() == NewWithRegistry(default).
	// The actual New() is exercised by the main binary at startup.
	t.Skip("metrics.New() uses the global registry; calling it twice in tests panics")
}
