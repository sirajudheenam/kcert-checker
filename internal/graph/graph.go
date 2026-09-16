// Package graph builds a resource relationship map from certificate scan results.
// It answers the question: "What breaks if this certificate expires?"
package graph

import "github.com/sirajudheenam/kcert-checker/internal/result"

// Usage records one Kubernetes resource that references a certificate.
type Usage struct {
	Namespace  string
	SourceType result.SourceType
	SourceName string
	Container  string
	Path       string
}

// Node is a single certificate with all the places it was found.
type Node struct {
	Subject     string
	Issuer      string
	// Fingerprint is the SHA-256 hex of the certificate's DER bytes, upper-case,
	// no colons. It is the stable key used to merge duplicate discoveries into
	// a single Node.
	Fingerprint string
	DaysLeft    int
	Expired     bool
	IsCA        bool
	UsedBy      []Usage
}

// Graph maps a SHA-256 fingerprint to its relationship node.
// All operations are O(1) via map lookup.
type Graph map[string]*Node

// Build constructs a Graph from a flat list of CertificateResults.
// Results with the same fingerprint are merged into one Node whose UsedBy
// list grows with each new location. This is how the graph reveals certs
// shared across multiple pods or secrets.
func Build(results []result.CertificateResult) Graph {
	g := make(Graph, len(results))
	for _, r := range results {
		fp := r.Fingerprint
		if fp == "" {
			continue
		}
		if _, exists := g[fp]; !exists {
			g[fp] = &Node{
				Subject:     r.Subject,
				Issuer:      r.Issuer,
				Fingerprint: fp,
				DaysLeft:    r.DaysLeft,
				Expired:     r.Expired,
				IsCA:        r.IsCA,
			}
		}
		g[fp].UsedBy = append(g[fp].UsedBy, Usage{
			Namespace:  r.Namespace,
			SourceType: r.SourceType,
			SourceName: r.SourceName,
			Container:  r.Container,
			Path:       r.Path,
		})
	}
	return g
}

// Lookup returns the Node for the given fingerprint, or nil if not found.
func (g Graph) Lookup(fingerprint string) *Node {
	return g[fingerprint]
}

// Certificates returns all nodes in the graph.
// Order is not guaranteed (map iteration).
func (g Graph) Certificates() []*Node {
	nodes := make([]*Node, 0, len(g))
	for _, n := range g {
		nodes = append(nodes, n)
	}
	return nodes
}

// SharedCertificates returns nodes referenced by more than one resource.
// Use this to identify certs whose expiry would affect multiple workloads.
func (g Graph) SharedCertificates() []*Node {
	var shared []*Node
	for _, n := range g {
		if len(n.UsedBy) > 1 {
			shared = append(shared, n)
		}
	}
	return shared
}
