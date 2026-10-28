//go:build integration

package server

import (
	"context"
	"encoding/binary"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

type discardSink struct{}

func (discardSink) Write(p []byte) (int, error) { return len(p), nil }
func (discardSink) Close() error                { return nil }

func freePort(t *testing.T, network, address string) int {
	t.Helper()
	lc := net.ListenConfig{}
	if network == "tcp4" {
		l, err := lc.Listen(context.Background(), network, address)
		if err != nil {
			t.Fatal(err)
		}
		port := l.Addr().(*net.TCPAddr).Port
		_ = l.Close()
		return port
	}
	l, err := lc.ListenPacket(context.Background(), network, address)
	if err != nil {
		t.Fatal(err)
	}
	port := l.LocalAddr().(*net.UDPAddr).Port
	_ = l.Close()
	return port
}
func startTestServer(t *testing.T) *Server {
	t.Helper()
	port := freePort(t, "udp4", "127.0.0.1:0")
	httpPort := freePort(t, "tcp4", "127.0.0.1:0")
	c := Config{Listen: "0.0.0.0:" + strconv.Itoa(port) + ",[::]:" + strconv.Itoa(port), HTTP: "127.0.0.1:" + strconv.Itoa(httpPort)}
	s := New(c, discardSink{}, func() {})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := s.Stop(ctx); err != nil {
			t.Error(err)
		}
	})
	return s
}
func exchange(t *testing.T, network, target, source string, b []byte, want bool) {
	t.Helper()
	d := net.Dialer{}
	if source != "" {
		d.LocalAddr = &net.UDPAddr{IP: net.ParseIP(source)}
	}
	c, err := d.DialContext(context.Background(), network, target)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()
	if err := c.SetDeadline(time.Now().Add(200 * time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Write(b); err != nil {
		t.Fatal(err)
	}
	out := make([]byte, 1501)
	n, err := c.Read(out)
	if !want {
		if err == nil {
			t.Fatal("unexpected response")
		}
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	a := c.LocalAddr().(*net.UDPAddr)
	size := 32
	if a.IP.To4() == nil {
		size = 44
	}
	if n != size || binary.BigEndian.Uint16(out) != 0x0101 || string(out[8:20]) != string(b[8:20]) || binary.BigEndian.Uint16(out[26:28])^0x2112 != uint16(a.Port) {
		t.Fatal("wire response")
	}
	ip := a.IP.To4()
	if ip == nil {
		ip = a.IP.To16()
	}
	mask := b[4:20]
	for i, v := range ip {
		if out[28+i]^mask[i] != v {
			t.Fatal("source mapping")
		}
	}
}
func TestRealDualStack(t *testing.T) {
	s := startTestServer(t)
	port := s.udp[0].conn.LocalAddr().(*net.UDPAddr).Port
	for _, tc := range []struct{ network, host string }{{"udp4", "127.0.0.1"}, {"udp6", "::1"}} {
		target := net.JoinHostPort(tc.host, strconv.Itoa(port))
		exchange(t, tc.network, target, "", request(), true)
		for _, b := range [][]byte{request()[:19], append(request(), 0), append(request(), make([]byte, 1481)...), append(request(), make([]byte, 1980)...), make([]byte, 65507)} {
			exchange(t, tc.network, target, "", b, false)
			exchange(t, tc.network, target, "", request(), true)
		}
	}
	client := &http.Client{Timeout: time.Second}
	req, err := http.NewRequestWithContext(context.Background(), "GET", "http://"+s.config.HTTP+"/readyz", nil)
	if err != nil {
		t.Fatal(err)
	}
	r, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = r.Body.Close()
	if r.StatusCode != 200 {
		t.Fatal(r.StatusCode)
	}
}
func TestStartupRollback(t *testing.T) {
	lc := net.ListenConfig{}
	occupied, err := lc.ListenPacket(context.Background(), "udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = occupied.Close() }()
	first := freePort(t, "udp4", "127.0.0.1:0")
	addr := "127.0.0.1:" + strconv.Itoa(first)
	s := New(Config{Listen: addr + "," + occupied.LocalAddr().String(), HTTP: "127.0.0.1:8080"}, discardSink{}, func() {})
	if err := s.Start(context.Background()); err == nil {
		t.Fatal("occupied accepted")
	}
	l, err := lc.ListenPacket(context.Background(), "udp4", addr)
	if err != nil {
		t.Fatal("not released", err)
	}
	_ = l.Close()
	if s.ready.Load() {
		t.Fatal("ready")
	}
}
func TestFatalAndHTTPAdmission(t *testing.T) {
	s := startTestServer(t)
	connections := []net.Conn{}
	defer func() {
		for _, c := range connections {
			_ = c.Close()
		}
	}()
	for range 32 {
		d := net.Dialer{Timeout: time.Second}
		c, err := d.DialContext(context.Background(), "tcp4", s.config.HTTP)
		if err != nil {
			t.Fatal(err)
		}
		connections = append(connections, c)
		_, _ = io.WriteString(c, "GET /healthz HTTP/1.1\r\n")
	}
	deadline := time.Now().Add(time.Second)
	for s.stats.Connections.Load() != 32 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	d := net.Dialer{Timeout: time.Second}
	extra, err := d.DialContext(context.Background(), "tcp4", s.config.HTTP)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = extra.Close() }()
	_ = extra.SetReadDeadline(time.Now().Add(time.Second))
	_, _ = io.WriteString(extra, "GET /healthz HTTP/1.1\r\n\r\n")
	var b [1]byte
	if _, err := extra.Read(b[:]); err == nil {
		t.Fatal("33rd accepted")
	}
	deadline = time.Now().Add(time.Second)
	for s.stats.HTTPRejected.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if s.stats.Connections.Load() > 32 || s.stats.HTTPRejected.Load() == 0 {
		t.Fatal("connection cap")
	}
	_ = s.udp[0].conn.Close()
	deadline = time.Now().Add(time.Second)
	for !s.failed.Load() && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if !s.failed.Load() || s.ready.Load() {
		t.Fatal("fatal readiness")
	}
}

func TestExplicitExporterEnvironment(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_TRACES_HEADERS", "authorization=canary")
	t.Setenv("OTEL_EXPORTER_OTLP_HEADERS", "x-private=canary")
	t.Setenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", "http://127.0.0.1:1/forbidden")
	for _, code := range []int{200, 307, 503} {
		t.Run(strconv.Itoa(code), func(t *testing.T) {
			var calls atomic.Int64
			collector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.Header.Get("Authorization") != "" || r.Header.Get("X-Private") != "" {
					t.Error("ambient headers leaked")
				}
				if r.URL.Path != "/v1/traces" {
					t.Error("wrong explicit path")
				}
				w.Header().Set("Location", "http://127.0.0.1:1/redirect")
				w.WriteHeader(code)
			}))
			defer collector.Close()
			stats := &counters{}
			tel, err := newTelemetry(context.Background(), collector.URL+"/v1/traces", discardSink{}, stats)
			if err != nil {
				t.Fatal(err)
			}
			tel.processor.OnEnd(tracetest.SpanStub{Name: "stun.binding"}.Snapshot())
			deadline := time.Now().Add(3 * time.Second)
			for calls.Load() == 0 && time.Now().Before(deadline) {
				time.Sleep(time.Millisecond)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := tel.close(ctx); err != nil {
				t.Fatal(err)
			}
			if calls.Load() != 1 {
				t.Fatal("unexpected retry/calls", calls.Load())
			}
			if code != 200 && stats.ExportFailed.Load() != 1 {
				t.Fatal("missing export failure")
			}
		})
	}
}
