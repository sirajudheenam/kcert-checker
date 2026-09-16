// Package output formats CertificateResults as a human-readable table or JSON.
package output

import (
	"encoding/json"
	"fmt"
	"io"
	"text/tabwriter"

	"github.com/sirajudheenam/kcert-checker/internal/result"
)

// Format selects the output representation.
type Format string

const (
	FormatTable Format = "table"
	FormatJSON  Format = "json"
)

// jsonRow is the per-certificate shape written for JSON output.
type jsonRow struct {
	Namespace   string   `json:"namespace"`
	SourceType  string   `json:"source_type"`
	SourceName  string   `json:"source_name"`
	Container   string   `json:"container,omitempty"`
	Subject     string   `json:"subject"`
	Issuer      string   `json:"issuer"`
	DNSNames    []string `json:"dns_names,omitempty"`
	NotAfter    string   `json:"not_after"`
	DaysLeft    int      `json:"days_left"`
	Status      string   `json:"status"`
	IsCA        bool     `json:"is_ca"`
	Fingerprint string   `json:"fingerprint"`
}

// Write renders results to w in the requested format using the given thresholds.
func Write(w io.Writer, results []result.CertificateResult, t result.Thresholds, format Format) error {
	switch format {
	case FormatJSON:
		return writeJSON(w, results, t)
	default:
		return writeTable(w, results, t)
	}
}

func writeJSON(w io.Writer, results []result.CertificateResult, t result.Thresholds) error {
	rows := make([]jsonRow, 0, len(results))
	for _, r := range results {
		rows = append(rows, jsonRow{
			Namespace:   r.Namespace,
			SourceType:  string(r.SourceType),
			SourceName:  r.SourceName,
			Container:   r.Container,
			Subject:     r.Subject,
			Issuer:      r.Issuer,
			DNSNames:    r.DNSNames,
			NotAfter:    r.NotAfter.Format("2006-01-02"),
			DaysLeft:    r.DaysLeft,
			Status:      string(t.Classify(r.DaysLeft)),
			IsCA:        r.IsCA,
			Fingerprint: r.Fingerprint,
		})
	}

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(rows)
}

const (
	tableHeader = "STATUS\tNAMESPACE\tSOURCE NAME\tSUBJECT\tISSUER\tEXPIRES\tDAYS LEFT"
	tableSep    = "------\t---------\t-----------\t-------\t------\t-------\t---------"
)

func writeTable(w io.Writer, results []result.CertificateResult, t result.Thresholds) error {
	var secrets, containers []result.CertificateResult
	counts := map[result.Status]int{}
	for _, r := range results {
		s := t.Classify(r.DaysLeft)
		counts[s]++
		if r.SourceType == result.SourceSecret {
			secrets = append(secrets, r)
		} else {
			containers = append(containers, r)
		}
	}

	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)

	// Summary line
	fmt.Fprintf(tw, "Total: %d  |  OK: %d  WARNING: %d  CRITICAL: %d  EXPIRED: %d\n",
		len(results),
		counts[result.StatusOK],
		counts[result.StatusWarning],
		counts[result.StatusCritical],
		counts[result.StatusExpired],
	)
	fmt.Fprintln(tw, "")

	// Secrets section
	fmt.Fprintln(tw, "=== Kubernetes Secrets ===")
	fmt.Fprintln(tw, tableHeader)
	fmt.Fprintln(tw, tableSep)
	if len(secrets) == 0 {
		fmt.Fprintln(tw, "(no TLS/certificate-bearing secrets found in cluster)")
	}
	for _, r := range secrets {
		writeTableRow(tw, r, t)
	}

	fmt.Fprintln(tw, "")

	// Containers section
	fmt.Fprintln(tw, "=== Pod Containers ===")
	fmt.Fprintln(tw, tableHeader+"\tCONTAINER\tPOD")
	fmt.Fprintln(tw, tableSep+"\t---------\t---")
	if len(containers) == 0 {
		fmt.Fprintln(tw, "(no certificates found in scanned container paths)")
	}
	for _, r := range containers {
		status := t.Classify(r.DaysLeft)
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%d\t%s\t%s\n",
			status,
			r.Namespace,
			r.SourceName,
			r.Subject,
			r.Issuer,
			r.NotAfter.Format("2006-01-02"),
			r.DaysLeft,
			r.Container,
			podFromSourceName(r.SourceName),
		)
	}

	return tw.Flush()
}

func writeTableRow(tw io.Writer, r result.CertificateResult, t result.Thresholds) {
	status := t.Classify(r.DaysLeft)
	fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%d\n",
		status,
		r.Namespace,
		r.SourceName,
		r.Subject,
		r.Issuer,
		r.NotAfter.Format("2006-01-02"),
		r.DaysLeft,
	)
}

// podFromSourceName extracts the pod name from a container source name of
// the form "podname:path".
func podFromSourceName(sourceName string) string {
	for i, c := range sourceName {
		if c == ':' {
			return sourceName[:i]
		}
	}
	return sourceName
}
