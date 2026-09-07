//go:build e2e

// Product E2E intentionally does not import the product wire codec.
package e2e_test

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

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
	cmd, name := productCommand(context.Background(), args)
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
		if t.Failed() {
			t.Logf("product output:\n%s", p.logs())
		}
		log.Close()
	})
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case err := <-p.done:
			p.stopped = true
			t.Fatalf("process exited before ready: %v\n%s", err, p.logs())
		default:
		}
		if status, _ := httpGet(health, "/readyz"); status == 200 {
			return p
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("process did not become ready\n%s", p.logs())
	return nil
}

func productCommand(ctx context.Context, args []string) (*exec.Cmd, string) {
	if image := os.Getenv("STUN_E2E_IMAGE"); image != "" {
		name := fmt.Sprintf("stun-e2e-%d", time.Now().UnixNano())
		return exec.CommandContext(ctx, "docker", append([]string{"run", "--rm", "--name", name, "--network", "host", "--read-only", "--cap-drop", "ALL", image}, args...)...), name
	}
	return exec.CommandContext(ctx, serverBinary, args...), ""
}

func (p *process) logs() string {
	data, err := os.ReadFile(p.log.Name())
	if err != nil {
		return fmt.Sprintf("read process log: %v", err)
	}
	if len(data) > 8192 {
		data = data[len(data)-8192:]
	}
	return string(data)
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
