package health

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/endless-net/stun/internal/metrics"
)

func TestHealthReadinessAndMetrics(t *testing.T) {
	r := metrics.New(metrics.BuildInfo{
		Version:          "test-version",
		Commit:           "test-commit",
		BuildDate:        "2026-08-02T00:00:00Z",
		ExecutableDigest: "sha256:test",
	})
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

	req = httptest.NewRequest(http.MethodGet, "/revisionz", nil)
	res = httptest.NewRecorder()
	h.ServeHTTP(res, req)
	var revision metrics.BuildInfo
	if err := json.Unmarshal(res.Body.Bytes(), &revision); err != nil {
		t.Fatal(err)
	}
	if res.Code != http.StatusOK || revision.Commit != "test-commit" || revision.ExecutableDigest != "sha256:test" {
		t.Fatalf("revision response: status=%d revision=%#v", res.Code, revision)
	}
}

func TestRevisionUnavailableWithoutRegistry(t *testing.T) {
	assertStatus(t, Handler(nil), "/revisionz", http.StatusServiceUnavailable)
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
