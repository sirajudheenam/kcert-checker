package output

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/sirajudheenam/kcert-checker/internal/result"
)

var testThresholds = result.Thresholds{WarningDays: 30, CriticalDays: 7}

// sampleResults returns one secret cert (CRITICAL, 2d) and one container cert (OK, 60d).
func sampleResults() []result.CertificateResult {
	return []result.CertificateResult{
		{
			Namespace:   "monitoring",
			SourceType:  result.SourceSecret,
			SourceName:  "my-secret/tls.crt",
			Subject:     "example.com",
			Issuer:      "My CA",
			NotAfter:    time.Now().Add(2 * 24 * time.Hour),
			DaysLeft:    2,
			Fingerprint: "AABBCCDDEEFF",
		},
		{
			Namespace:  "default",
			SourceType: result.SourceContainer,
			SourceName: "mypod:/etc/tls/tls.crt",
			Container:  "app",
			Subject:    "internal.svc",
			Issuer:     "Internal CA",
			NotAfter:   time.Now().Add(60 * 24 * time.Hour),
			DaysLeft:   60,
			Fingerprint: "112233445566",
		},
	}
}

// TestWriteTable verifies the happy path: both sections appear with the correct
// rows, the summary line shows the right total, and section headers are present.
// TestWriteTable is the main happy-path: both a secret result and a container
// result are present. Verifies the summary line, both section headers, and
// that all key fields appear in the correct section.
func TestWriteTable(t *testing.T) {
	var buf bytes.Buffer
	err := Write(&buf, sampleResults(), testThresholds, FormatTable)
	if err != nil {
		t.Fatalf("Write table: %v", err)
	}
	out := buf.String()

	// Summary line present.
	if !strings.Contains(out, "Total: 2") {
		t.Errorf("table missing summary line\ngot:\n%s", out)
	}

	// Both section headers must be present.
	for _, want := range []string{
		"=== Kubernetes Secrets ===",
		"=== Pod Containers ===",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("table missing section header %q\ngot:\n%s", want, out)
		}
	}

	// Secret row data.
	for _, want := range []string{"monitoring", "CRITICAL", "example.com", "My CA", "my-secret/tls.crt"} {
		if !strings.Contains(out, want) {
			t.Errorf("table missing %q in secrets section\ngot:\n%s", want, out)
		}
	}

	// Container row data.
	for _, want := range []string{"default", "OK", "internal.svc", "Internal CA", "app", "mypod"} {
		if !strings.Contains(out, want) {
			t.Errorf("table missing %q in containers section\ngot:\n%s", want, out)
		}
	}
}

// TestWriteTableOnlySecrets verifies the empty-state message shown when no container
// certificates are found. The secrets section should still show its row.
// TestWriteTableOnlySecrets verifies that when there are no container results
// the Containers section shows the empty-state message instead of a blank table.
func TestWriteTableOnlySecrets(t *testing.T) {
	results := []result.CertificateResult{
		{
			Namespace:  "prod",
			SourceType: result.SourceSecret,
			SourceName: "prod-tls/tls.crt",
			Subject:    "prod.example.com",
			Issuer:     "CA",
			NotAfter:   time.Now().Add(90 * 24 * time.Hour),
			DaysLeft:   90,
		},
	}
	var buf bytes.Buffer
	if err := Write(&buf, results, testThresholds, FormatTable); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "prod.example.com") {
		t.Errorf("secret cert not in output:\n%s", out)
	}
	if !strings.Contains(out, "no certificates found in scanned container paths") {
		t.Errorf("containers section should show empty message:\n%s", out)
	}
}

// TestWriteTableOnlyContainers verifies the empty-state message shown when no
// secret certificates are found. The container section should still show its row.
// TestWriteTableOnlyContainers verifies the symmetric case: only container
// results present, Secrets section shows the empty-state message.
func TestWriteTableOnlyContainers(t *testing.T) {
	results := []result.CertificateResult{
		{
			Namespace:  "default",
			SourceType: result.SourceContainer,
			SourceName: "mypod:/etc/tls/tls.crt",
			Container:  "app",
			Subject:    "app.internal",
			Issuer:     "CA",
			NotAfter:   time.Now().Add(15 * 24 * time.Hour),
			DaysLeft:   15,
		},
	}
	var buf bytes.Buffer
	if err := Write(&buf, results, testThresholds, FormatTable); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "app.internal") {
		t.Errorf("container cert not in output:\n%s", out)
	}
	if !strings.Contains(out, "no TLS/certificate-bearing secrets found") {
		t.Errorf("secrets section should show empty message:\n%s", out)
	}
}

// TestWriteJSON verifies that JSON output is valid, has the correct number of
// rows, and that status classification and field mapping are correct.
// TestWriteJSON verifies that JSON output is valid, contains the correct number
// of rows, and that status classification (CRITICAL vs OK) is applied.
func TestWriteJSON(t *testing.T) {
	var buf bytes.Buffer
	err := Write(&buf, sampleResults(), testThresholds, FormatJSON)
	if err != nil {
		t.Fatalf("Write JSON: %v", err)
	}

	var rows []jsonRow
	if err := json.Unmarshal(buf.Bytes(), &rows); err != nil {
		t.Fatalf("invalid JSON output: %v\noutput:\n%s", err, buf.String())
	}
	if len(rows) != 2 {
		t.Errorf("expected 2 rows, got %d", len(rows))
	}
	if rows[0].Status != "CRITICAL" {
		t.Errorf("first row status = %q, want CRITICAL", rows[0].Status)
	}
	if rows[1].Status != "OK" {
		t.Errorf("second row status = %q, want OK", rows[1].Status)
	}
	if rows[0].Namespace != "monitoring" {
		t.Errorf("namespace = %q, want monitoring", rows[0].Namespace)
	}
	if rows[0].Fingerprint != "AABBCCDDEEFF" {
		t.Errorf("fingerprint = %q", rows[0].Fingerprint)
	}
}

// TestWriteTableEmpty verifies that an empty result list still produces the
// section headers and a "Total: 0" summary line — useful for monitoring pipelines
// that check for the presence of section headers even when no certs are found.
// TestWriteTableEmpty confirms that an empty result slice still produces
// both section headers and a "Total: 0" summary rather than panicking.
func TestWriteTableEmpty(t *testing.T) {
	var buf bytes.Buffer
	if err := Write(&buf, nil, testThresholds, FormatTable); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "Total: 0") {
		t.Error("expected summary line with Total: 0")
	}
	if !strings.Contains(out, "=== Kubernetes Secrets ===") {
		t.Error("expected secrets header even for empty results")
	}
	if !strings.Contains(out, "=== Pod Containers ===") {
		t.Error("expected containers header even for empty results")
	}
}

// TestWriteJSONEmpty verifies that an empty result list produces a valid "[]"
// JSON array rather than null or an error.
// TestWriteJSONEmpty confirms that an empty result slice produces a JSON
// array "[]" rather than null or an error.
func TestWriteJSONEmpty(t *testing.T) {
	var buf bytes.Buffer
	if err := Write(&buf, nil, testThresholds, FormatJSON); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var rows []jsonRow
	if err := json.Unmarshal(buf.Bytes(), &rows); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(rows) != 0 {
		t.Errorf("expected 0 rows, got %d", len(rows))
	}
}

// TestWriteUnknownFormatFallsBackToTable verifies that an unrecognised format string
// silently falls back to table output rather than returning an error or empty output.
// TestWriteUnknownFormatFallsBackToTable verifies that an unrecognised format
// string silently falls back to table output rather than returning an error.
func TestWriteUnknownFormatFallsBackToTable(t *testing.T) {
	var buf bytes.Buffer
	if err := Write(&buf, sampleResults(), testThresholds, "yaml"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(buf.String(), "=== Kubernetes Secrets ===") {
		t.Error("expected table header for unknown format")
	}
}

// TestPodFromSourceName verifies the colon-split logic that extracts pod names
// from "podname:path" source names used in the containers table column.
// TestPodFromSourceName verifies the colon-separator parse used to render
// the POD column in the Containers section.
func TestPodFromSourceName(t *testing.T) {
	cases := []struct{ in, want string }{
		{"mypod:/etc/tls/tls.crt", "mypod"},
		{"pod-name:/path/to/cert.pem", "pod-name"},
		// No colon: return the whole string (graceful fallback)
		{"nocolon", "nocolon"},
	}
	for _, c := range cases {
		if got := podFromSourceName(c.in); got != c.want {
			t.Errorf("podFromSourceName(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
