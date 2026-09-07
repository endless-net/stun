package stun

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/endless-net/stun/internal/metrics"
	"github.com/endless-net/stun/internal/ratelimit"
)

type failingPacketConn struct {
	request []byte
	read    bool
}

func (c *failingPacketConn) ReadFrom(p []byte) (int, net.Addr, error) {
	if c.read {
		return 0, nil, net.ErrClosed
	}
	c.read = true
	return copy(p, c.request), &net.UDPAddr{IP: net.ParseIP("192.0.2.99"), Port: 54321}, nil
}
func (*failingPacketConn) WriteTo([]byte, net.Addr) (int, error) {
	return 0, &net.OpError{Op: "write", Net: "udp", Addr: &net.UDPAddr{IP: net.ParseIP("192.0.2.99"), Port: 54321}, Err: errors.New("private-packet-body")}
}
func (*failingPacketConn) Close() error { return nil }
func (*failingPacketConn) LocalAddr() net.Addr {
	return &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 3478}
}
func (*failingPacketConn) SetDeadline(time.Time) error      { return nil }
func (*failingPacketConn) SetReadDeadline(time.Time) error  { return nil }
func (*failingPacketConn) SetWriteDeadline(time.Time) error { return nil }

func TestWriteErrorLogsDoNotExposeClient(t *testing.T) {
	req, _, err := BuildBindingRequest()
	if err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	r := metrics.New(metrics.BuildInfo{Version: "test", Commit: "test"})
	s := Server{Metrics: r, Limiter: ratelimit.New(100, 100), Logger: slog.New(slog.NewJSONHandler(&logs, nil))}
	if err := s.Serve(context.Background(), &failingPacketConn{request: req}); err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"192.0.2.99", "54321", "private-packet-body"} {
		if strings.Contains(logs.String(), secret) {
			t.Fatal("client material leaked")
		}
	}
	found := false
	for _, line := range strings.Split(strings.TrimSpace(logs.String()), "\n") {
		var event map[string]any
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatal(err)
		}
		if event["error_code"] == "write_error" {
			found = true
			if event["remote_address"] != "redacted" {
				t.Fatal("missing redaction")
			}
		}
	}
	if !found || !strings.Contains(r.Render(), `result="write_error"} 1`) {
		t.Fatal("missing error evidence")
	}
}
