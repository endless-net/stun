//go:build e2e

// Product E2E intentionally does not import the product wire codec.
package e2e_test

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"net"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestProductWireAndRecovery(t *testing.T) {
	addrs := []string{freeAddress(t, "udp4", "127.0.0.1"), freeAddress(t, "udp6", "::1")}
	p := start(t, addrs, 10000, 10000)
	for _, addr := range addrs {
		t.Run(addr, func(t *testing.T) {
			c := socket(t, addr)
			exchange(t, c, request(), true)
			exchange(t, c, withAttrs(request(), []byte{0x80, 0x22, 0, 0}), true)
			large := make([]byte, 1480)
			large[0] = 0x80
			large[1] = 0x22
			binary.BigEndian.PutUint16(large[2:4], 1476)
			exchange(t, c, withAttrs(request(), large), true)
			badCookie := request()
			badCookie[4] ^= 0xff
			badType := request()
			badType[1] = 3
			badLength := request()
			badLength[3] = 4
			cases := [][]byte{[]byte("invalid"), badCookie, badType, badLength, withAttrs(request(), []byte{0x80, 0x22, 0, 8}), withAttrs(request(), []byte{0, 0x0f, 0, 0}), make([]byte, 1501), make([]byte, 2000), make([]byte, 65507)}
			for _, bad := range cases {
				exchange(t, c, bad, false)
				exchange(t, c, request(), true)
			}
		})
	}
	for _, path := range []string{"/healthz", "/readyz", "/metrics"} {
		status, body := httpGet(p.health, path)
		if status != 200 {
			t.Fatalf("%s status=%d", path, status)
		}
		if path == "/metrics" {
			for _, metric := range []string{"stun_requests_total", "stun_responses_total", "stun_invalid_requests_total", "stun_active_listeners", "stun_build_info"} {
				if !strings.Contains(body, metric) {
					t.Fatalf("missing %s", metric)
				}
			}
			var invalid float64
			for _, line := range strings.Split(body, "\n") {
				if strings.HasPrefix(line, `stun_invalid_requests_total{listener="`+addrs[0]+`",`) {
					fields := strings.Fields(line)
					value, err := strconv.ParseFloat(fields[len(fields)-1], 64)
					if err != nil {
						t.Fatal(err)
					}
					invalid += value
				}
			}
			if invalid != 9 {
				t.Fatalf("invalid counter did not increase by 9: %s", body)
			}
			if strings.Contains(body, "source_ip") || strings.Contains(body, "remote_address") {
				t.Fatal("client metric label")
			}
		}
	}
}

func TestProductRateLimitPerListener(t *testing.T) {
	addrs := []string{freeAddress(t, "udp4", "127.0.0.1"), freeAddress(t, "udp4", "127.0.0.1")}
	p := start(t, addrs, 1, 2)
	c1, c2 := socket(t, addrs[0]), socket(t, addrs[0])
	exchange(t, c1, request(), true)
	exchange(t, c2, request(), true)
	exchange(t, c1, request(), false)
	c3 := socket(t, addrs[1])
	exchange(t, c3, request(), true)
	time.Sleep(1100 * time.Millisecond)
	exchange(t, c2, request(), true)
	_, body := httpGet(p.health, "/metrics")
	if !strings.Contains(body, `stun_rate_limited_total{listener="`+addrs[0]+`",address_family="ipv4"} 1`) {
		t.Fatal("missing rate limit count")
	}
}

func TestProductRestart(t *testing.T) {
	addr := freeAddress(t, "udp4", "127.0.0.1")
	p := start(t, []string{addr}, 100, 100)
	exchange(t, socket(t, addr), request(), true)
	p.stop(t, runtime.GOOS != "windows")
	if status, _ := httpGet(p.health, "/readyz"); status != 0 {
		t.Fatal("HTTP listener survived shutdown")
	}
	p = start(t, []string{addr}, 100, 100)
	exchange(t, socket(t, addr), request(), true)
	p.stop(t, runtime.GOOS != "windows")
}

func TestProductRejectsStartupErrors(t *testing.T) {
	busy, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer busy.Close()
	for _, args := range [][]string{{"--addr", "127.0.0.1:0"}, {"--addr", busy.LocalAddr().String(), "--metrics-addr", freeAddress(t, "tcp4", "127.0.0.1")}, {"--addr", freeAddress(t, "udp4", "127.0.0.1"), "--metrics-addr", "0.0.0.0:9090"}} {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		cmd, container := productCommand(ctx, args)
		out, err := cmd.CombinedOutput()
		cancel()
		if container != "" {
			// A timeout kills the Docker CLI; explicitly remove its container too.
			cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
			_ = exec.CommandContext(cleanupCtx, "docker", "rm", "-f", container).Run()
			cleanupCancel()
		}
		if err == nil || ctx.Err() == context.DeadlineExceeded {
			t.Fatalf("expected prompt startup rejection: %s %v", out, err)
		}
	}
}

func TestProductSmoke(t *testing.T) {
	addr := freeAddress(t, "udp4", "127.0.0.1")
	start(t, []string{addr}, 100, 100)
	out, err := exec.Command(smokeBinary, "--stun-addr", addr, "--timeout", "1s").CombinedOutput()
	if err != nil {
		t.Fatalf("smoke failed: %s %v", out, err)
	}
	var result struct {
		Mapped string `json:"mapped_address"`
	}
	if err := json.Unmarshal(out, &result); err != nil || result.Mapped == "" {
		t.Fatal("invalid smoke output")
	}
	for _, reply := range []bool{false, true} {
		fake, err := net.ListenPacket("udp4", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		done := make(chan struct{})
		go func() {
			defer close(done)
			b := make([]byte, 100)
			_, remote, err := fake.ReadFrom(b)
			if err == nil && reply {
				_, _ = fake.WriteTo([]byte("malformed"), remote)
			}
		}()
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		cmd := exec.CommandContext(ctx, smokeBinary, "--stun-addr", fake.LocalAddr().String(), "--timeout", "150ms")
		out, err := cmd.CombinedOutput()
		cancel()
		fake.Close()
		<-done
		if err == nil || ctx.Err() == context.DeadlineExceeded {
			t.Fatalf("smoke should reject reply=%v: %s %v", reply, out, err)
		}
	}
}
