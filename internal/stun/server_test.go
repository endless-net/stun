package stun

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/unng-lab/endlessnet-stun/internal/metrics"
	"github.com/unng-lab/endlessnet-stun/internal/ratelimit"
)

func TestServerAnswersBindingAndSurvivesMalformedDatagram(t *testing.T) {
	addr := reserveUDPAddr(t)
	registry := metrics.New("test", "test")
	cancel, errCh := startTestServer(t, addr, registry, ratelimit.New(100, 100))
	defer stopTestServer(t, cancel, errCh)

	conn, err := net.Dial("udp", addr)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Write([]byte("not-stun")); err != nil {
		t.Fatal(err)
	}
	_ = conn.Close()

	queryUntilReady(t, addr)
	rendered := registry.Render()
	for _, want := range []string{"stun_invalid_requests_total", "stun_responses_total", `address_family="ipv4"`} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("metrics missing %q:\n%s", want, rendered)
		}
	}
}

func TestServerRateLimitsBySourceIP(t *testing.T) {
	addr := reserveUDPAddr(t)
	registry := metrics.New("test", "test")
	cancel, errCh := startTestServer(t, addr, registry, ratelimit.New(1, 1))
	defer stopTestServer(t, cancel, errCh)

	conn, err := net.Dial("udp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	queryOnConnUntilReady(t, conn)
	if answered, err := queryOnConn(conn, 100*time.Millisecond); err != nil || answered {
		t.Fatalf("second query = answered %v, error %v; want rate-limited timeout", answered, err)
	}
	if !strings.Contains(registry.Render(), "stun_rate_limited_total") {
		t.Fatalf("rate limit metric missing:\n%s", registry.Render())
	}
}

func TestServerRejectsListenerError(t *testing.T) {
	err := (Server{
		Addr:    "not-an-address",
		Metrics: metrics.New("test", "test"),
		Limiter: ratelimit.New(1, 1),
		Logger:  slog.New(slog.NewTextHandler(io.Discard, nil)),
	}).ListenAndServe(context.Background())
	if err == nil || !strings.Contains(err.Error(), "listen for STUN") {
		t.Fatalf("error = %v", err)
	}
}

func startTestServer(t *testing.T, addr string, registry *metrics.Registry, limiter *ratelimit.Limiter) (context.CancelFunc, <-chan error) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- (Server{
			Addr:    addr,
			Metrics: registry,
			Limiter: limiter,
			Logger:  slog.New(slog.NewTextHandler(io.Discard, nil)),
		}).ListenAndServe(ctx)
	}()
	return cancel, errCh
}

func stopTestServer(t *testing.T, cancel context.CancelFunc, errCh <-chan error) {
	t.Helper()
	cancel()
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("server stopped with error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("server did not stop after context cancellation")
	}
}

func queryUntilReady(t *testing.T, addr string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		_, err := Query(ctx, addr)
		cancel()
		if err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("STUN listener did not become ready: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func queryOnConnUntilReady(t *testing.T, conn net.Conn) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		answered, err := queryOnConn(conn, 100*time.Millisecond)
		if err == nil && answered {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("STUN listener did not become ready: answered=%v err=%v", answered, err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func queryOnConn(conn net.Conn, timeout time.Duration) (bool, error) {
	request, txID, err := BuildBindingRequest()
	if err != nil {
		return false, err
	}
	if err := conn.SetDeadline(time.Now().Add(timeout)); err != nil {
		return false, err
	}
	if _, err := conn.Write(request); err != nil {
		return false, err
	}
	response := make([]byte, MaxDatagramSize)
	n, err := conn.Read(response)
	if err != nil {
		var netErr net.Error
		if errors.As(err, &netErr) && netErr.Timeout() {
			return false, nil
		}
		return false, err
	}
	_, err = ParseBindingResponse(response[:n], txID)
	return err == nil, err
}

func reserveUDPAddr(t *testing.T) string {
	t.Helper()
	conn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := conn.LocalAddr().String()
	if err := conn.Close(); err != nil {
		t.Fatal(err)
	}
	return addr
}
