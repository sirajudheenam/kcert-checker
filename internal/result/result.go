// Package result defines the common certificate result model used across all
// discovery sources (Secrets, containers, Ingress, Gateway API, etc.).
package result

import "time"

// SourceType identifies where a certificate was discovered.
type SourceType string

const (
	SourceSecret    SourceType = "secret"
	SourceContainer SourceType = "container"
	// SourceIngress and SourceGateway are defined for future Ingress / Gateway API
	// scanning. They are not yet wired to any discoverer; reserved here so the
	// constants are stable when those discoverers land.
	SourceIngress SourceType = "ingress"
	SourceGateway SourceType = "gateway"
)

// CertificateResult is the single canonical representation of a discovered
// X.509 certificate, regardless of which discoverer found it. All output
// paths (Prometheus, JSON, table) consume this type.
type CertificateResult struct {
	// Location
	Cluster   string
	Namespace string

	// Source
	SourceType SourceType
	// SourceName is a human-readable identifier that encodes origin:
	// for secrets: "<secret-name>/<key>"; for containers: "<pod>:<path>".
	SourceName string
	Container  string
	Path       string

	// Identity
	Subject  string
	Issuer   string
	Serial   string
	DNSNames []string
	// Fingerprint is the SHA-256 hex of the raw DER bytes, upper-case, no colons.
	// Used as a stable deduplication key and for the graph package.
	Fingerprint string

	// Validity
	NotBefore time.Time
	NotAfter  time.Time
	// DaysLeft is positive for future expiry, zero on expiry day,
	// and negative for already-expired certificates.
	DaysLeft int

	// Classification
	IsCA    bool
	Expired bool
}
