// Package ui serves the embedded certificate dashboard at /ui and the
// JSON data API at /api/certificates.
package ui

import (
	_ "embed"
	"encoding/json"
	"net/http"
	"sync"

	"github.com/sirajudheenam/kcert-checker/internal/result"
)

//go:embed dashboard.html
var dashboardHTML []byte

// certRow is the JSON shape consumed by the dashboard JavaScript.
type certRow struct {
	Namespace   string `json:"namespace"`
	SourceType  string `json:"source_type"`
	SourceName  string `json:"source_name"`
	Container   string `json:"container,omitempty"`
	Subject     string `json:"subject"`
	Issuer      string `json:"issuer"`
	NotAfter    string `json:"not_after"`
	DaysLeft    int    `json:"days_left"`
	Status      string `json:"status"`
	IsCA        bool   `json:"is_ca"`
	Fingerprint string `json:"fingerprint"`
}

// Handler serves the dashboard HTML and the /api/certificates JSON endpoint.
// Call SetResults after each scan to refresh the data the dashboard shows.
type Handler struct {
	mu         sync.RWMutex
	certs      []certRow
	thresholds result.Thresholds
}

// New creates a Handler with the given status thresholds.
func New(t result.Thresholds) *Handler {
	return &Handler{thresholds: t}
}

// SetResults replaces the certificate data served by /api/certificates.
// Safe to call from any goroutine.
func (h *Handler) SetResults(results []result.CertificateResult) {
	rows := make([]certRow, 0, len(results))
	for _, r := range results {
		rows = append(rows, certRow{
			Namespace:   r.Namespace,
			SourceType:  string(r.SourceType),
			SourceName:  r.SourceName,
			Container:   r.Container,
			Subject:     r.Subject,
			Issuer:      r.Issuer,
			NotAfter:    r.NotAfter.Format("2006-01-02"),
			DaysLeft:    r.DaysLeft,
			Status:      string(h.thresholds.Classify(r.DaysLeft)),
			IsCA:        r.IsCA,
			Fingerprint: r.Fingerprint,
		})
	}
	h.mu.Lock()
	h.certs = rows
	h.mu.Unlock()
}

// Dashboard serves the embedded HTML dashboard.
func (h *Handler) Dashboard(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(dashboardHTML)
}

// Certificates serves the JSON data array consumed by the dashboard.
func (h *Handler) Certificates(w http.ResponseWriter, _ *http.Request) {
	h.mu.RLock()
	rows := h.certs
	h.mu.RUnlock()

	if rows == nil {
		rows = []certRow{}
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	enc := json.NewEncoder(w)
	_ = enc.Encode(rows)
}
