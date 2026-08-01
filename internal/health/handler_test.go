package health

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/endless-net/stun/internal/metrics"
)

func TestHealthReadinessAndMetrics(t *testing.T) {
	r := metrics.New("test", "test")
	h := Handler(r)
	assertStatus(t, h, "/healthz", http.StatusOK)
	assertStatus(t, h, "/readyz", http.StatusServiceUnavailable)
	r.SetListener("127.0.0.1:3478", true)
	assertStatus(t, h, "/readyz", http.StatusOK)

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	res := httptest.NewRecorder()
	h.ServeHTTP(res, req)
	if res.Code != http.StatusOK || !strings.Contains(res.Body.String(), "stun_build_info") {
		t.Fatalf("metrics response: status=%d body=%q", res.Code, res.Body.String())
	}
}

func assertStatus(t *testing.T, h http.Handler, path string, want int) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	res := httptest.NewRecorder()
	h.ServeHTTP(res, req)
	if res.Code != want {
		t.Fatalf("GET %s status = %d, want %d; body=%q", path, res.Code, want, res.Body.String())
	}
}
