package health

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHealthzAlwaysOK(t *testing.T) {
	h := &Handler{}

	rr := httptest.NewRecorder()
	h.Healthz(rr, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	if rr.Code != http.StatusOK {
		t.Errorf("healthz = %d, want 200", rr.Code)
	}
}

func TestReadyzNotReadyReturns503(t *testing.T) {
	h := &Handler{}

	rr := httptest.NewRecorder()
	h.Readyz(rr, httptest.NewRequest(http.MethodGet, "/readyz", nil))

	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("readyz before MarkReady = %d, want 503", rr.Code)
	}
}

func TestReadyzAfterMarkReadyReturns200(t *testing.T) {
	h := &Handler{}
	h.MarkReady()

	rr := httptest.NewRecorder()
	h.Readyz(rr, httptest.NewRequest(http.MethodGet, "/readyz", nil))

	if rr.Code != http.StatusOK {
		t.Errorf("readyz after MarkReady = %d, want 200", rr.Code)
	}
}

func TestHealthzUnaffectedByMarkReady(t *testing.T) {
	h := &Handler{}

	rr := httptest.NewRecorder()
	h.Healthz(rr, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rr.Code != http.StatusOK {
		t.Errorf("healthz before MarkReady = %d, want 200", rr.Code)
	}

	h.MarkReady()
	rr = httptest.NewRecorder()
	h.Healthz(rr, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rr.Code != http.StatusOK {
		t.Errorf("healthz after MarkReady = %d, want 200", rr.Code)
	}
}
