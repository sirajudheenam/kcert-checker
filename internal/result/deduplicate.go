package result

// Location records one place where a certificate was found.
type Location struct {
	Namespace  string
	SourceType SourceType
	SourceName string
	Container  string
	Path       string
}

// DeduplicatedCert groups all locations that share the same certificate
// (identified by fingerprint) into a single entry.
type DeduplicatedCert struct {
	CertificateResult        // representative result (first seen)
	Locations         []Location
}

// Deduplicate groups a flat list of CertificateResults by fingerprint.
// Each unique certificate appears once; its Locations list contains every
// place it was discovered.
// The order slice is kept separate from the index map so the returned slice
// preserves first-seen order — pure map iteration would produce random output
// and make the table / JSON output non-deterministic.
func Deduplicate(results []CertificateResult) []DeduplicatedCert {
	index := make(map[string]*DeduplicatedCert)
	order := make([]string, 0, len(results))

	for _, r := range results {
		fp := r.Fingerprint

		if _, exists := index[fp]; !exists {
			index[fp] = &DeduplicatedCert{CertificateResult: r}
			order = append(order, fp)
		}

		index[fp].Locations = append(index[fp].Locations, Location{
			Namespace:  r.Namespace,
			SourceType: r.SourceType,
			SourceName: r.SourceName,
			Container:  r.Container,
			Path:       r.Path,
		})
	}

	out := make([]DeduplicatedCert, 0, len(order))
	for _, fp := range order {
		out = append(out, *index[fp])
	}

	return out
}
