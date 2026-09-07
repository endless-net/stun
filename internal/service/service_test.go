package service

import (
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/endless-net/stun/internal/config"
	"github.com/endless-net/stun/internal/metrics"
	"github.com/endless-net/stun/internal/stun"
)

func freeAddress(t *testing.T, network string) string {
	t.Helper()
	if network == "udp" {
		c, err := net.ListenPacket(network, "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close()
		return c.LocalAddr().String()
	}
	c, err := net.Listen(network, "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	return c.Addr().String()
}

func testConfig(t *testing.T) config.Config {
	return config.Config{ListenAddrs: []string{freeAddress(t, "udp")}, MetricsAddr: freeAddress(t, "tcp"), RateLimitPerSecond: 100, RateLimitBurst: 100}
}

func TestRunReleasesSocketsAfterPartialStartupFailure(t *testing.T) {
	for _, network := range []string{"udp", "tcp"} {
		t.Run(network, func(t *testing.T) {
			cfg := testConfig(t)
			if network == "udp" {
				busy, err := net.ListenPacket("udp", "127.0.0.1:0")
				if err != nil {
					t.Fatal(err)
				}
				defer busy.Close()
				cfg.ListenAddrs = append(cfg.ListenAddrs, busy.LocalAddr().String())
			} else {
				busy, err := net.Listen("tcp", cfg.MetricsAddr)
				if err != nil {
					t.Fatal(err)
				}
				defer busy.Close()
			}
			if err := Run(context.Background(), cfg, nil, metrics.BuildInfo{}); err == nil {
				t.Fatal("startup unexpectedly succeeded")
			}
			reopened, err := net.ListenPacket("udp", cfg.ListenAddrs[0])
			if err != nil {
				t.Fatalf("startup failure leaked first socket: %v", err)
			}
			reopened.Close()
		})
	}
}

func TestRunServesAllListenersAndStopsOnCancellation(t *testing.T) {
	cfg := testConfig(t)
	cfg.ListenAddrs = append(cfg.ListenAddrs, freeAddress(t, "udp"))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Run(ctx, cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), metrics.BuildInfo{}) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("shutdown: %v", err)
			}
		case <-time.After(7 * time.Second):
			t.Error("service did not stop")
		}
		for _, addr := range cfg.ListenAddrs {
			socket, err := net.ListenPacket("udp", addr)
			if err != nil {
				t.Errorf("shutdown leaked %s: %v", addr, err)
				continue
			}
			socket.Close()
		}
		socket, err := net.Listen("tcp", cfg.MetricsAddr)
		if err != nil {
			t.Errorf("shutdown leaked HTTP listener: %v", err)
		} else {
			socket.Close()
		}
	})
	client := &http.Client{Timeout: 100 * time.Millisecond}
	deadline := time.Now().Add(3 * time.Second)
	for {
		res, err := client.Get("http://" + cfg.MetricsAddr + "/readyz")
		if err == nil {
			res.Body.Close()
			if res.StatusCode == http.StatusOK {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("service did not become ready")
		}
		time.Sleep(10 * time.Millisecond)
	}
	for _, addr := range cfg.ListenAddrs {
		queryCtx, stop := context.WithTimeout(ctx, time.Second)
		_, err := stun.Query(queryCtx, addr)
		stop()
		if err != nil {
			t.Fatalf("ready service does not answer on %s: %v", addr, err)
		}
	}
}
