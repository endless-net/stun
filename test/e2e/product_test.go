//go:build e2e

// Product E2E intentionally does not import the product's wire codec.
package e2e_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

var serverBinary, smokeBinary, evidence string

func TestMain(m *testing.M) {
	work, err := os.MkdirTemp("", "stun-product-e2e-")
	if err != nil {
		panic(err)
	}
	evidence = os.Getenv("STUN_E2E_EVIDENCE")
	if evidence == "" {
		evidence = filepath.Join(work, "evidence")
	}
	if err = os.MkdirAll(evidence, 0700); err != nil {
		panic(err)
	}
	serverBinary = filepath.Join(work, "stun")
	smokeBinary = filepath.Join(work, "smoke")
	if runtime.GOOS == "windows" {
		serverBinary += ".exe"
		smokeBinary += ".exe"
	}
	_, source, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(source), "../.."))
	metadata := map[string]string{"os": runtime.GOOS, "arch": runtime.GOARCH, "go": runtime.Version()}
	for _, item := range []struct{ path, command string }{{serverBinary, "endlessnet-stun"}, {smokeBinary, "endlessnet-stun-smoke"}} {
		cmd := exec.Command("go", "build", "-o", item.path, "./cmd/"+item.command)
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			fmt.Fprintf(os.Stderr, "build: %v\n%s", err, out)
			os.RemoveAll(work)
			os.Exit(1)
		}
		data, err := os.ReadFile(item.path)
		if err != nil {
			panic(err)
		}
		metadata[item.command+"_sha256"] = fmt.Sprintf("%x", sha256.Sum256(data))
	}
	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = root
	if out, err := cmd.Output(); err == nil {
		metadata["commit"] = strings.TrimSpace(string(out))
	}
	if image := os.Getenv("STUN_E2E_IMAGE"); image != "" {
		out, err := exec.Command("docker", "image", "inspect", "--format", "{{.Id}}", image).Output()
		if err != nil {
			panic(err)
		}
		metadata["image_id"] = strings.TrimSpace(string(out))
	}
	data, _ := json.MarshalIndent(metadata, "", "  ")
	if err := os.WriteFile(filepath.Join(evidence, "identity.json"), data, 0600); err != nil {
		panic(err)
	}
	code := m.Run()
	os.RemoveAll(work)
	os.Exit(code)
}

func freeAddress(t *testing.T, network, host string) string {
	t.Helper()
	if strings.HasPrefix(network, "udp") {
		c, err := net.ListenPacket(network, net.JoinHostPort(host, "0"))
		if err != nil {
			t.Fatalf("required %s socket unavailable: %v", network, err)
		}
		defer c.Close()
		return c.LocalAddr().String()
	}
	c, err := net.Listen(network, net.JoinHostPort(host, "0"))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	return c.Addr().String()
}

type process struct {
	cmd          *exec.Cmd
	done         chan error
	health, name string
	stopped      bool
	log          *os.File
}

func start(t *testing.T, addrs []string, rate, burst int) *process {
	t.Helper()
	health := freeAddress(t, "tcp4", "127.0.0.1")
	args := []string{"--addr", strings.Join(addrs, ","), "--metrics-addr", health, "--rate-limit-per-second", fmt.Sprint(rate), "--rate-limit-burst", fmt.Sprint(burst), "--log-level", "debug"}
	cmd := exec.Command(serverBinary, args...)
	name := ""
	if image := os.Getenv("STUN_E2E_IMAGE"); image != "" {
		name = fmt.Sprintf("stun-e2e-%d", time.Now().UnixNano())
		cmd = exec.Command("docker", append([]string{"run", "--rm", "--name", name, "--network", "host", "--read-only", "--cap-drop", "ALL", image}, args...)...)
	}
	log, err := os.CreateTemp(evidence, strings.ReplaceAll(t.Name(), "/", "-")+"-*.log")
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stdout = log
	cmd.Stderr = log
	if err = cmd.Start(); err != nil {
		log.Close()
		t.Fatal(err)
	}
	p := &process{cmd: cmd, done: make(chan error, 1), health: health, name: name, log: log}
	go func() { p.done <- cmd.Wait() }()
	t.Cleanup(func() {
		if !p.stopped {
			p.stop(t, false)
		}
		log.Close()
	})
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case err := <-p.done:
			p.stopped = true
			t.Fatalf("process exited before ready: %v", err)
		default:
		}
		if status, _ := httpGet(health, "/readyz"); status == 200 {
			return p
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatal("process did not become ready")
	return nil
}
func (p *process) stop(t *testing.T, graceful bool) {
	t.Helper()
	if p.stopped {
		return
	}
	if p.name != "" {
		op := []string{"rm", "-f", p.name}
		if graceful {
			op = []string{"stop", "--time", "5", p.name}
		}
		if out, err := exec.Command("docker", op...).CombinedOutput(); err != nil {
			t.Errorf("container stop: %v %s", err, out)
		}
	} else if graceful {
		if err := p.cmd.Process.Signal(os.Interrupt); err != nil {
			t.Error(err)
		}
	} else {
		_ = p.cmd.Process.Kill()
	}
	select {
	case err := <-p.done:
		if graceful && err != nil {
			t.Errorf("graceful exit: %v", err)
		}
	case <-time.After(10 * time.Second):
		_ = p.cmd.Process.Kill()
		t.Fatal("shutdown timed out")
	}
	p.stopped = true
}
func httpGet(addr, path string) (int, string) {
	client := http.Client{Timeout: time.Second}
	r, err := client.Get("http://" + addr + path)
	if err != nil {
		return 0, ""
	}
	defer r.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	return r.StatusCode, string(b)
}
func request() []byte {
	p := make([]byte, 20)
	p[1] = 1
	copy(p[4:8], []byte{0x21, 0x12, 0xa4, 0x42})
	if _, err := rand.Read(p[8:]); err != nil {
		panic(err)
	}
	return p
}
func withAttrs(p, a []byte) []byte {
	p = append(append([]byte(nil), p...), a...)
	binary.BigEndian.PutUint16(p[2:4], uint16(len(p)-20))
	return p
}
func socket(t *testing.T, addr string) net.Conn {
	t.Helper()
	c, err := net.Dial("udp", addr)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}
func exchange(t *testing.T, c net.Conn, p []byte, want bool) {
	t.Helper()
	_ = c.SetDeadline(time.Now().Add(time.Second))
	if _, err := c.Write(p); err != nil {
		t.Fatal(err)
	}
	if !want {
		_ = c.SetReadDeadline(time.Now().Add(80 * time.Millisecond))
	}
	r := make([]byte, 2048)
	n, err := c.Read(r)
	if !want {
		if e, ok := err.(net.Error); !ok || !e.Timeout() {
			t.Fatalf("expected dropped packet, n=%d err=%v", n, err)
		}
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	r = r[:n]
	if n < 32 || binary.BigEndian.Uint16(r[:2]) != 0x0101 || int(binary.BigEndian.Uint16(r[2:4])) != n-20 || !bytes.Equal(r[4:20], p[4:20]) {
		t.Fatal("invalid response header")
	}
	if binary.BigEndian.Uint16(r[20:22]) != 0x20 {
		t.Fatal("missing mapped attribute")
	}
	local := c.LocalAddr().(*net.UDPAddr)
	ip := local.IP.To4()
	family := byte(1)
	if ip == nil {
		ip = local.IP.To16()
		family = 2
	}
	if r[25] != family || len(r) != 28+len(ip) || int(binary.BigEndian.Uint16(r[22:24])) != 4+len(ip) {
		t.Fatal("invalid mapped family/length")
	}
	if int(binary.BigEndian.Uint16(r[26:28])^0x2112) != local.Port {
		t.Fatal("mapped port mismatch")
	}
	for i := range ip {
		if r[28+i]^p[4+i] != ip[i] {
			t.Fatal("mapped IP mismatch")
		}
	}
}

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
		cmd := exec.CommandContext(ctx, serverBinary, args...)
		out, err := cmd.CombinedOutput()
		cancel()
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
