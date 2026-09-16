package scanner

import (
	"crypto/x509"
	"fmt"
)

// Certificate is an alias for x509.Certificate so the rest of the scanner
// package can refer to it without importing crypto/x509 everywhere.
type Certificate = x509.Certificate

// ParseCertificate parses a single DER-encoded X.509 certificate.
func ParseCertificate(data []byte) (*Certificate, error) {
	cert, err := x509.ParseCertificate(data)
	if err != nil {
		return nil, fmt.Errorf(
			"invalid X.509 certificate: %w",
			err,
		)
	}

	return cert, nil
}
