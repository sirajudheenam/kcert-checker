// Package health provides /healthz and /readyz HTTP handlers.
package health

import (
	"net/http"
	"sync/atomic"
)

// Handler exposes /healthz (always 200) and /readyz (200 only after the first
// scan completes). Call MarkReady() once the initial scan is done.
type Handler struct {
	ready atomic.Bool
}

// MarkReady signals that the scanner has completed at least one scan.
func (h *Handler) MarkReady() {
	h.ready.Store(true)
}

// Healthz always returns 200 OK – the process is alive.
func (h *Handler) Healthz(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

// Readyz returns 200 after the first scan, 503 before it.
func (h *Handler) Readyz(w http.ResponseWriter, _ *http.Request) {
	if h.ready.Load() {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ready"))
		return
	}
	w.WriteHeader(http.StatusServiceUnavailable)
	_, _ = w.Write([]byte("not ready"))
}
