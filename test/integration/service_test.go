package integration_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/endless-net/stun/internal/stun"
)

func TestStandaloneBinaryEndToEnd(t *testing.T) {
	root := repositoryRoot(t)
	exe := filepath.Join(t.TempDir(), "endlessnet-stun")
	if runtime.GOOS == "windows" {
		exe += ".exe"
	}
	build := exec.Command("go", "build", "-o", exe, "./cmd/endlessnet-stun")
	build.Dir = root
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build standalone binary: %v\n%s", err, concise(out))
	}

	stunAddr := reserveAddr(t, "udp")
	metricsAddr := reserveAddr(t, "tcp")
	logs, err := os.CreateTemp(t.TempDir(), "service-*.log")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { logs.Close() })
	cmd := exec.Command(exe,
		"--addr="+stunAddr,
		"--metrics-addr="+metricsAddr,
		"--rate-limit-per-second=1",
		"--rate-limit-burst=3",
		"--log-level=debug",
	)
	cmd.Stdout = logs
	cmd.Stderr = logs
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	stopped := false
	t.Cleanup(func() {
		if !stopped {
			_ = cmd.Process.Kill()
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Error("process cleanup timed out")
			}
		}
	})

	baseURL := "http://" + metricsAddr
	waitHTTP(t, baseURL+"/readyz")
	mapped := query(t, stunAddr)
	if mapped.IP == nil || mapped.Port == 0 {
		t.Fatalf("mapped address = %v", mapped)
	}

	conn, err := net.Dial("udp", stunAddr)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Write([]byte("malformed")); err != nil {
		t.Fatal(err)
	}
	_ = conn.Close()
	query(t, stunAddr)

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	if _, err := stun.Query(ctx, stunAddr); err == nil {
		t.Fatal("request above burst was not rate limited")
	}

	for _, path := range []string{"/healthz", "/readyz", "/revisionz", "/metrics"} {
		body := get(t, baseURL+path)
		if path == "/metrics" {
			for _, metric := range []string{"stun_requests_total", "stun_responses_total", "stun_invalid_requests_total", "stun_rate_limited_total", "stun_active_listeners", "stun_request_duration_seconds", "stun_build_info"} {
				if !strings.Contains(body, metric) {
					t.Fatalf("metrics missing %s", metric)
				}
			}
		}
		if path == "/revisionz" {
			content, err := os.ReadFile(exe)
			if err != nil {
				t.Fatal(err)
			}
			var revision struct {
				Commit           string `json:"commit"`
				ExecutableDigest string `json:"executable_digest"`
			}
			if err := json.Unmarshal([]byte(body), &revision); err != nil {
				t.Fatal(err)
			}
			digest := sha256.Sum256(content)
			if revision.Commit != "unknown" || revision.ExecutableDigest != fmt.Sprintf("sha256:%x", digest) {
				t.Fatalf("revision = %#v", revision)
			}
		}
	}

	if runtime.GOOS != "windows" {
		if err := cmd.Process.Signal(os.Interrupt); err != nil {
			t.Fatal(err)
		}
		select {
		case err := <-done:
			stopped = true
			if err != nil {
				t.Fatalf("graceful shutdown: %v; logs: %s", err, readLogs(logs.Name()))
			}
		case <-time.After(5 * time.Second):
			t.Fatal("process did not stop after interrupt")
		}
	}
}

func repositoryRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

func reserveAddr(t *testing.T, network string) string {
	t.Helper()
	if network == "udp" {
		listener, err := net.ListenPacket("udp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		addr := listener.LocalAddr().String()
		_ = listener.Close()
		return addr
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := listener.Addr().String()
	_ = listener.Close()
	return addr
}

func waitHTTP(t *testing.T, url string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		res, err := (&http.Client{Timeout: time.Second}).Get(url)
		if err == nil {
			_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 1<<20))
			_ = res.Body.Close()
			if res.StatusCode == http.StatusOK {
				return
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s did not become ready: %v", url, err)
		}
		time.Sleep(25 * time.Millisecond)
	}
}

func query(t *testing.T, addr string) *net.UDPAddr {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	mapped, err := stun.Query(ctx, addr)
	if err != nil {
		t.Fatal(err)
	}
	return mapped
}

func get(t *testing.T, url string) string {
	t.Helper()
	res, err := (&http.Client{Timeout: time.Second}).Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET %s status = %d, body=%q", url, res.StatusCode, body)
	}
	return string(body)
}

func concise(out []byte) string {
	out = bytes.TrimSpace(out)
	if len(out) > 2000 {
		out = out[len(out)-2000:]
	}
	return string(out)
}

func readLogs(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return err.Error()
	}
	return concise(data)
}
