package graph

import (
	"testing"
	"time"

	"github.com/sirajudheenam/kcert-checker/internal/result"
)

func makeResult(fp, subject string, sourceType result.SourceType, sourceName string) result.CertificateResult {
	return result.CertificateResult{
		Fingerprint: fp,
		Subject:     subject,
		Issuer:      "test-ca",
		SourceType:  sourceType,
		SourceName:  sourceName,
		Namespace:   "default",
		NotAfter:    time.Now().Add(30 * 24 * time.Hour),
		DaysLeft:    30,
	}
}

// TestBuildEmpty verifies that Build on a nil slice returns an empty graph.
func TestBuildEmpty(t *testing.T) {
	g := Build(nil)
	if len(g) != 0 {
		t.Errorf("expected empty graph, got %d nodes", len(g))
	}
}

// TestBuildSingleResult verifies that a single result creates one node with
// the correct fields and a single UsedBy entry.
func TestBuildSingleResult(t *testing.T) {
	r := makeResult("ABCDEF", "my-cert", result.SourceSecret, "my-tls")
	g := Build([]result.CertificateResult{r})

	if len(g) != 1 {
		t.Fatalf("expected 1 node, got %d", len(g))
	}
	node := g.Lookup("ABCDEF")
	if node == nil {
		t.Fatal("node not found")
	}
	if node.Subject != "my-cert" {
		t.Errorf("subject = %q, want my-cert", node.Subject)
	}
	if len(node.UsedBy) != 1 {
		t.Errorf("UsedBy len = %d, want 1", len(node.UsedBy))
	}
	if node.UsedBy[0].SourceType != result.SourceSecret {
		t.Errorf("UsedBy[0].SourceType = %q", node.UsedBy[0].SourceType)
	}
}

// TestBuildDedupliatesSameCert verifies that multiple results with the same
// fingerprint are merged into a single node — the core purpose of this package.
func TestBuildDedupliatesSameCert(t *testing.T) {
	// Same fingerprint found in 3 places: secret, pod1, pod2
	results := []result.CertificateResult{
		makeResult("FINGERPRINT", "api.example.com", result.SourceSecret, "api-tls"),
		makeResult("FINGERPRINT", "api.example.com", result.SourceContainer, "pod1:/etc/tls/tls.crt"),
		makeResult("FINGERPRINT", "api.example.com", result.SourceContainer, "pod2:/etc/tls/tls.crt"),
	}
	g := Build(results)

	if len(g) != 1 {
		t.Fatalf("expected 1 node (same fingerprint), got %d", len(g))
	}
	node := g.Lookup("FINGERPRINT")
	if len(node.UsedBy) != 3 {
		t.Errorf("UsedBy len = %d, want 3", len(node.UsedBy))
	}
}

// TestBuildMultipleCerts verifies that two distinct fingerprints create two nodes,
// even when one fingerprint appears in multiple results.
func TestBuildMultipleCerts(t *testing.T) {
	results := []result.CertificateResult{
		makeResult("FP1", "cert-a", result.SourceSecret, "secret-a"),
		makeResult("FP2", "cert-b", result.SourceSecret, "secret-b"),
		makeResult("FP1", "cert-a", result.SourceContainer, "pod:/etc/tls/tls.crt"),
	}
	g := Build(results)

	if len(g) != 2 {
		t.Fatalf("expected 2 nodes (FP1 + FP2), got %d", len(g))
	}
	if g.Lookup("FP1") == nil || g.Lookup("FP2") == nil {
		t.Error("expected both fingerprints in graph")
	}
}

// TestBuildSkipsEmptyFingerprint verifies that results without a fingerprint
// are silently ignored rather than creating a node keyed on "".
func TestBuildSkipsEmptyFingerprint(t *testing.T) {
	r := makeResult("", "no-fingerprint", result.SourceSecret, "some-secret")
	g := Build([]result.CertificateResult{r})
	if len(g) != 0 {
		t.Errorf("expected 0 nodes for empty fingerprint, got %d", len(g))
	}
}

// TestLookupMissing verifies that Lookup returns nil for an unknown fingerprint.
func TestLookupMissing(t *testing.T) {
	g := Build(nil)
	if g.Lookup("NONEXISTENT") != nil {
		t.Error("expected nil for missing fingerprint")
	}
}

// TestCertificates verifies that Certificates() returns all nodes in the graph.
func TestCertificates(t *testing.T) {
	results := []result.CertificateResult{
		makeResult("FP1", "a", result.SourceSecret, "s1"),
		makeResult("FP2", "b", result.SourceSecret, "s2"),
	}
	g := Build(results)
	certs := g.Certificates()
	if len(certs) != 2 {
		t.Errorf("Certificates() = %d, want 2", len(certs))
	}
}

// TestCertificatesEmpty verifies that Certificates() on an empty graph returns an empty slice.
func TestCertificatesEmpty(t *testing.T) {
	g := Build(nil)
	if certs := g.Certificates(); len(certs) != 0 {
		t.Errorf("Certificates() on empty graph = %d, want 0", len(certs))
	}
}

// TestSharedCertificates verifies that only certs with more than one UsedBy entry
// are returned — these are the certificates where a single expiry event affects
// multiple workloads simultaneously.
func TestSharedCertificates(t *testing.T) {
	results := []result.CertificateResult{
		// FP1 used in 2 places → shared
		makeResult("FP1", "shared-cert", result.SourceSecret, "s1"),
		makeResult("FP1", "shared-cert", result.SourceContainer, "pod1:/etc/tls"),
		// FP2 used in 1 place → not shared
		makeResult("FP2", "solo-cert", result.SourceSecret, "s2"),
	}
	g := Build(results)
	shared := g.SharedCertificates()
	if len(shared) != 1 {
		t.Fatalf("SharedCertificates() = %d, want 1", len(shared))
	}
	if shared[0].Fingerprint != "FP1" {
		t.Errorf("shared cert fingerprint = %q, want FP1", shared[0].Fingerprint)
	}
}

// TestSharedCertificatesNone verifies that SharedCertificates returns empty when
// every cert appears in exactly one location.
func TestSharedCertificatesNone(t *testing.T) {
	results := []result.CertificateResult{
		makeResult("FP1", "a", result.SourceSecret, "s1"),
		makeResult("FP2", "b", result.SourceSecret, "s2"),
	}
	g := Build(results)
	if shared := g.SharedCertificates(); len(shared) != 0 {
		t.Errorf("SharedCertificates() = %d, want 0 (none shared)", len(shared))
	}
}

// TestNodeFieldsSetFromFirstResult verifies that node metadata (IsCA, DaysLeft, etc.)
// is taken from the first result seen for a fingerprint, and that all usages
// across different namespaces are recorded in UsedBy.
func TestNodeFieldsSetFromFirstResult(t *testing.T) {
	results := []result.CertificateResult{
		{
			Fingerprint: "FP_CA",
			Subject:     "Root CA",
			Issuer:      "Self",
			IsCA:        true,
			Expired:     false,
			DaysLeft:    3650,
			SourceType:  result.SourceSecret,
			SourceName:  "root-ca",
			Namespace:   "cert-manager",
		},
		{
			Fingerprint: "FP_CA",
			Subject:     "Root CA",
			Issuer:      "Self",
			IsCA:        true,
			DaysLeft:    3650,
			SourceType:  result.SourceContainer,
			SourceName:  "pod:/etc/certs/ca.crt",
			Namespace:   "default",
		},
	}
	g := Build(results)
	node := g.Lookup("FP_CA")
	if !node.IsCA {
		t.Error("IsCA should be true")
	}
	if node.DaysLeft != 3650 {
		t.Errorf("DaysLeft = %d, want 3650", node.DaysLeft)
	}
	if len(node.UsedBy) != 2 {
		t.Errorf("UsedBy = %d, want 2", len(node.UsedBy))
	}
	// Check both namespaces are recorded
	nsSet := map[string]bool{}
	for _, u := range node.UsedBy {
		nsSet[u.Namespace] = true
	}
	if !nsSet["cert-manager"] || !nsSet["default"] {
		t.Errorf("expected both namespaces, got %v", nsSet)
	}
}
