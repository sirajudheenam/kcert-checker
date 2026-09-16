package result

import (
	"testing"
	"time"
)

func makeResult(fp, ns, sourceName string, sourceType SourceType) CertificateResult {
	return CertificateResult{
		Fingerprint: fp,
		Namespace:   ns,
		SourceType:  sourceType,
		SourceName:  sourceName,
		NotAfter:    time.Now().Add(24 * time.Hour),
	}
}

// TestDeduplicateEmpty verifies that a nil input produces an empty (not nil) slice.
func TestDeduplicateEmpty(t *testing.T) {
	got := Deduplicate(nil)
	if len(got) != 0 {
		t.Errorf("expected 0, got %d", len(got))
	}
}

// TestDeduplicateSingleCert verifies that a single result produces one entry
// with one location and the correct fingerprint.
func TestDeduplicateSingleCert(t *testing.T) {
	r := makeResult("AABBCC", "default", "mysecret/tls.crt", SourceSecret)
	got := Deduplicate([]CertificateResult{r})
	if len(got) != 1 {
		t.Fatalf("expected 1, got %d", len(got))
	}
	if len(got[0].Locations) != 1 {
		t.Errorf("expected 1 location, got %d", len(got[0].Locations))
	}
	if got[0].Fingerprint != "AABBCC" {
		t.Errorf("fingerprint mismatch")
	}
}

// TestDeduplicateMultipleLocations verifies that three results sharing the same
// fingerprint are collapsed into one entry with three locations.
func TestDeduplicateMultipleLocations(t *testing.T) {
	fp := "DEADBEEF"
	results := []CertificateResult{
		makeResult(fp, "ns1", "secret-a/tls.crt", SourceSecret),
		makeResult(fp, "ns2", "pod-b:/etc/tls/tls.crt", SourceContainer),
		makeResult(fp, "ns1", "secret-c/ca.crt", SourceSecret),
	}
	got := Deduplicate(results)
	if len(got) != 1 {
		t.Fatalf("expected 1 deduplicated cert, got %d", len(got))
	}
	if len(got[0].Locations) != 3 {
		t.Errorf("expected 3 locations, got %d", len(got[0].Locations))
	}
}

// TestDeduplicateDistinctFingerprints verifies that three different certs remain
// three separate entries with no location merging.
func TestDeduplicateDistinctFingerprints(t *testing.T) {
	results := []CertificateResult{
		makeResult("FP1", "ns1", "secret-a/tls.crt", SourceSecret),
		makeResult("FP2", "ns1", "secret-b/tls.crt", SourceSecret),
		makeResult("FP3", "ns2", "secret-c/tls.crt", SourceSecret),
	}
	got := Deduplicate(results)
	if len(got) != 3 {
		t.Fatalf("expected 3, got %d", len(got))
	}
	for _, d := range got {
		if len(d.Locations) != 1 {
			t.Errorf("expected 1 location each, got %d", len(d.Locations))
		}
	}
}

// TestDeduplicatePreservesOrder verifies that the output order matches the
// first-seen order of fingerprints in the input slice. This is important for
// deterministic table / JSON output even when the same cert appears in multiple rows.
func TestDeduplicatePreservesOrder(t *testing.T) {
	results := []CertificateResult{
		makeResult("C", "ns1", "a", SourceSecret),
		makeResult("A", "ns1", "b", SourceSecret),
		makeResult("B", "ns1", "c", SourceSecret),
	}
	got := Deduplicate(results)
	want := []string{"C", "A", "B"}
	for i, d := range got {
		if d.Fingerprint != want[i] {
			t.Errorf("position %d: got %q, want %q", i, d.Fingerprint, want[i])
		}
	}
}
