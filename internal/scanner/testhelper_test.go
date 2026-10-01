package scanner

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"path/filepath"
	"sort"
	"time"
)

// selfSignedPEM generates a minimal ECDSA self-signed certificate in PEM format.
// It is the canonical way tests create cert bytes without depending on external tooling.
// notAfter can be in the past to produce an already-expired certificate.
func selfSignedPEM(t interface{ Helper(); Fatal(...interface{}) }, subject string, notAfter time.Time) []byte {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: subject},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     notAfter,
	}

	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	if err := pem.Encode(&buf, &pem.Block{Type: "CERTIFICATE", Bytes: der}); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// fakeContainerReader implements the FileReader interface used by scanContainer.
// files maps exact paths to PEM content; ListFiles returns any key that matches
// the given pattern using filepath.Match so tests can use wildcards naturally.
type fakeContainerReader struct {
	files map[string][]byte
}

func (f *fakeContainerReader) ListFiles(_ context.Context, _, _, _ string, pattern string) ([]string, error) {
	var matches []string
	for path := range f.files {
		ok, _ := filepath.Match(pattern, path)
		if ok {
			matches = append(matches, path)
		}
	}
	// Sort for deterministic test output.
	sort.Strings(matches)
	return matches, nil
}

func (f *fakeContainerReader) ReadFile(_ context.Context, _, _, _ string, path string) ([]byte, error) {
	if data, ok := f.files[path]; ok {
		return data, nil
	}
	return nil, &notFoundError{path: path}
}

type notFoundError struct{ path string }

func (e *notFoundError) Error() string { return "file not found: " + e.path }
