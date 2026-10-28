package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDiagnosticHandlers(t *testing.T) {
	s := &Server{}
	h := s.handler()
	for _, tc := range []struct {
		method, path, body string
		ready              bool
		code               int
	}{
		{"GET", "/healthz", "", false, 200}, {"GET", "/readyz", "", false, 503}, {"GET", "/readyz", "", true, 200},
		{"POST", "/readyz", "", true, 405}, {"GET", "/unknown", "", true, 404}, {"GET", "/metrics", "x", true, 413}, {"GET", "/metrics", "", true, 200},
	} {
		s.ready.Store(tc.ready)
		req := httptest.NewRequestWithContext(context.Background(), tc.method, tc.path, strings.NewReader(tc.body))
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		if w.Code != tc.code {
			t.Fatal(tc, w.Code)
		}
		if w.Body.Len() > 512<<10 {
			t.Fatal("response cap")
		}
	}
	r := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/metrics", nil)
	r.TransferEncoding = []string{"chunked"}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 413 {
		t.Fatal("transfer encoding")
	}
	s.ready.Store(true)
	s.failed.Store(true)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequestWithContext(context.Background(), "GET", "/readyz", nil))
	if w.Code != 503 {
		t.Fatal("fatal readiness")
	}
}
