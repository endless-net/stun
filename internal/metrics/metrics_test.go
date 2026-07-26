package metrics

import (
	"strings"
	"testing"
	"time"
)

func TestRegistryRendersRequiredMetricsWithoutSourceIPLabels(t *testing.T) {
	r := New("v1.2.3", "abc123")
	r.SetListener("127.0.0.1:3478", true)
	r.RecordRequest("127.0.0.1:3478", "ipv4")
	r.RecordResponse("127.0.0.1:3478", "ipv4")
	r.RecordInvalid("127.0.0.1:3478", "ipv4")
	r.RecordRateLimited("127.0.0.1:3478", "ipv4")
	r.RecordError("127.0.0.1:3478", "write_error")
	r.ObserveDuration("127.0.0.1:3478", "success", 5*time.Millisecond)

	rendered := r.Render()
	for _, want := range []string{
		"stun_requests_total",
		"stun_responses_total",
		"stun_invalid_requests_total",
		"stun_rate_limited_total",
		"stun_errors_total",
		"stun_active_listeners",
		"stun_request_duration_seconds_sum",
		"stun_request_duration_seconds_count",
		`stun_build_info{commit="abc123",version="v1.2.3"} 1`,
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("metrics missing %q:\n%s", want, rendered)
		}
	}
	if strings.Contains(rendered, "source_ip") || strings.Contains(rendered, "remote_address") {
		t.Fatalf("metrics contain a high-cardinality source label:\n%s", rendered)
	}
	if !r.Ready() {
		t.Fatal("registry is not ready with active listener")
	}
	r.SetListener("127.0.0.1:3478", false)
	if r.Ready() {
		t.Fatal("registry remained ready without active listeners")
	}
}
