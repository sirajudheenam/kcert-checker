package ui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/sirajudheenam/kcert-checker/internal/result"
)

var testThresholds = result.Thresholds{WarningDays: 30, CriticalDays: 7}

func certResult(subject string, daysLeft int) result.CertificateResult {
	return result.CertificateResult{
		Namespace:   "default",
		SourceType:  result.SourceSecret,
		SourceName:  subject + "/tls.crt",
		Subject:     subject,
		Issuer:      "test-ca",
		NotAfter:    time.Now().Add(time.Duration(daysLeft) * 24 * time.Hour),
		DaysLeft:    daysLeft,
		Fingerprint: "DEADBEEF" + subject,
	}
}

// TestDashboardServesHTML verifies that the embedded dashboard HTML is served
// with the correct Content-Type and a non-trivially-small body.
func TestDashboardServesHTML(t *testing.T) {
	h := New(testThresholds)
	rr := httptest.NewRecorder()
	h.Dashboard(rr, httptest.NewRequest(http.MethodGet, "/ui", nil))

	if rr.Code != http.StatusOK {
		t.Errorf("Dashboard status = %d, want 200", rr.Code)
	}
	ct := rr.Header().Get("Content-Type")
	if ct == "" {
		t.Error("expected Content-Type header")
	}
	body := rr.Body.String()
	if len(body) < 100 {
		t.Errorf("Dashboard body too short (%d bytes)", len(body))
	}
	if !contains(body, "<html") {
		t.Error("expected HTML in dashboard response")
	}
}

// TestCertificatesEmptyBeforeSet verifies that /api/certificates returns an
// empty JSON array (not null/404) before any scan results are loaded.
func TestCertificatesEmptyBeforeSet(t *testing.T) {
	h := New(testThresholds)
	rr := httptest.NewRecorder()
	h.Certificates(rr, httptest.NewRequest(http.MethodGet, "/api/certificates", nil))

	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rr.Code)
	}
	var rows []certRow
	if err := json.NewDecoder(rr.Body).Decode(&rows); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(rows) != 0 {
		t.Errorf("expected empty array, got %d rows", len(rows))
	}
}

// TestCertificatesAfterSetResults verifies that status classification
// (OK/WARNING/CRITICAL/EXPIRED) is applied correctly when results are set.
func TestCertificatesAfterSetResults(t *testing.T) {
	h := New(testThresholds)
	h.SetResults([]result.CertificateResult{
		certResult("ok-cert", 90),
		certResult("warning-cert", 20),
		certResult("critical-cert", 3),
		certResult("expired-cert", -5),
	})

	rr := httptest.NewRecorder()
	h.Certificates(rr, httptest.NewRequest(http.MethodGet, "/api/certificates", nil))

	var rows []certRow
	if err := json.NewDecoder(rr.Body).Decode(&rows); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(rows) != 4 {
		t.Fatalf("expected 4 rows, got %d", len(rows))
	}

	want := map[string]string{
		"ok-cert":       "OK",
		"warning-cert":  "WARNING",
		"critical-cert": "CRITICAL",
		"expired-cert":  "EXPIRED",
	}
	for _, r := range rows {
		if expected, ok := want[r.Subject]; ok {
			if r.Status != expected {
				t.Errorf("cert %q: status = %q, want %q", r.Subject, r.Status, expected)
			}
		}
	}
}

// TestSetResultsPopulatesFields verifies that all certRow fields are populated
// from the CertificateResult, including the formatted NotAfter date.
func TestSetResultsPopulatesFields(t *testing.T) {
	h := New(testThresholds)
	h.SetResults([]result.CertificateResult{certResult("my-cert", 60)})

	rr := httptest.NewRecorder()
	h.Certificates(rr, httptest.NewRequest(http.MethodGet, "/api/certificates", nil))

	var rows []certRow
	json.NewDecoder(rr.Body).Decode(&rows)
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	r := rows[0]
	if r.Subject != "my-cert" {
		t.Errorf("Subject = %q", r.Subject)
	}
	if r.Namespace != "default" {
		t.Errorf("Namespace = %q", r.Namespace)
	}
	if r.SourceType != "secret" {
		t.Errorf("SourceType = %q", r.SourceType)
	}
	if r.DaysLeft != 60 {
		t.Errorf("DaysLeft = %d, want 60", r.DaysLeft)
	}
	if r.Status != "OK" {
		t.Errorf("Status = %q, want OK", r.Status)
	}
	if r.NotAfter == "" {
		t.Error("NotAfter should not be empty")
	}
}

// TestCertificatesContentType confirms the JSON endpoint always sets
// Content-Type: application/json.
func TestCertificatesContentType(t *testing.T) {
	h := New(testThresholds)
	rr := httptest.NewRecorder()
	h.Certificates(rr, httptest.NewRequest(http.MethodGet, "/api/certificates", nil))

	ct := rr.Header().Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
}

// TestSetResultsThreadSafe exercises the RWMutex by running concurrent
// SetResults writes alongside /api/certificates reads. The race detector
// will flag any data race if the locking is wrong.
func TestSetResultsThreadSafe(t *testing.T) {
	h := New(testThresholds)
	done := make(chan struct{})
	go func() {
		for i := 0; i < 50; i++ {
			h.SetResults([]result.CertificateResult{certResult("cert", 30)})
		}
		close(done)
	}()
	for i := 0; i < 50; i++ {
		rr := httptest.NewRecorder()
		h.Certificates(rr, httptest.NewRequest(http.MethodGet, "/api/certificates", nil))
	}
	<-done
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}
func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
