//go:build linux && acceptance

package acceptance

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"syscall"
	"testing"
	"time"
)

const transaction = "independent!"
const loopback = "127.0.0.1"
const cookie = 0x2112a442

func executable(t *testing.T) string {
	t.Helper()
	binary := os.Getenv("STUN_BINARY")
	if binary == "" {
		t.Fatal("set STUN_BINARY to the absolute path of the built STUN executable")
	}
	return binary
}

func stunCommand(ctx context.Context, binary string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Env = append(cmd.Environ(), "ENDLESSNET_STUN_OTEL_ENDPOINT=")
	return cmd
}

func listenUDP(t *testing.T, network, address string) net.PacketConn {
	t.Helper()
	listener, err := (&net.ListenConfig{}).ListenPacket(t.Context(), network, address)
	if err != nil {
		t.Fatal(err)
	}
	return listener
}

func closeSocket(t *testing.T, socket interface{ Close() error }) {
	t.Helper()
	if err := socket.Close(); err != nil {
		t.Error(err)
	}
}

func freeUDPPort(t *testing.T) string {
	t.Helper()
	listener := listenUDP(t, "udp4", loopback+":0")
	port := strconv.Itoa(listener.LocalAddr().(*net.UDPAddr).Port)
	closeSocket(t, listener)
	return port
}

func freeHTTPAddress(t *testing.T) string {
	t.Helper()
	listener, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp4", loopback+":0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	closeSocket(t, listener)
	return address
}

func TestConfigAndVersionDoNotBind(t *testing.T) {
	binary := executable(t)
	listener := listenUDP(t, "udp4", loopback+":0")
	t.Cleanup(func() { closeSocket(t, listener) })
	for _, mode := range []string{"--version", "--check-config"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
			defer cancel()
			output, err := stunCommand(ctx, binary, "--listen", listener.LocalAddr().String(), mode).CombinedOutput()
			if err != nil {
				t.Fatalf("%s: %v\n%s", mode, err, output)
			}
		})
	}
}

func TestBindingAndSIGTERM(t *testing.T) {
	binary := executable(t)
	port := freeUDPPort(t)
	httpAddress := freeHTTPAddress(t)
	udp4 := net.JoinHostPort("0.0.0.0", port)
	udp6 := net.JoinHostPort("::", port)
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	cmd := stunCommand(ctx, binary, "--listen", udp4+","+udp6, "--http-listen", httpAddress)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	var waitErr error
	go func() {
		waitErr = cmd.Wait()
		close(done)
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})

	waitForReady(t, httpAddress, done)
	for _, tc := range []struct{ network, host string }{{"udp4", loopback}, {"udp6", "::1"}} {
		t.Run(tc.network, func(t *testing.T) {
			checkBinding(t, tc.network, net.JoinHostPort(tc.host, port))
		})
	}

	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	select {
	case <-done:
		if waitErr != nil {
			t.Fatalf("shutdown failed: %v\n%s", waitErr, stderr.Bytes())
		}
	case <-timer.C:
		t.Fatal("shutdown did not finish within 5 seconds")
	}
	if bytes.Contains(stderr.Bytes(), []byte(loopback)) ||
		bytes.Contains(stdout.Bytes(), []byte(transaction)) || bytes.Contains(stderr.Bytes(), []byte(transaction)) {
		t.Fatal("diagnostics contain an IP address or transaction ID")
	}
	ipv4 := listenUDP(t, "udp4", udp4)
	t.Cleanup(func() { closeSocket(t, ipv4) })
	ipv6 := listenUDP(t, "udp6", udp6)
	closeSocket(t, ipv6)
}

func waitForReady(t *testing.T, address string, done <-chan struct{}) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	transport := &http.Transport{}
	defer transport.CloseIdleConnections()
	client := &http.Client{Timeout: 200 * time.Millisecond, Transport: transport}
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+address+"/readyz", nil)
		if err != nil {
			t.Fatal(err)
		}
		response, err := client.Do(req)
		if err == nil {
			if closeErr := response.Body.Close(); closeErr != nil {
				t.Fatal(closeErr)
			}
			if response.StatusCode == http.StatusOK {
				return
			}
		}
		select {
		case <-done:
			t.Fatal("STUN process exited during startup")
		case <-ctx.Done():
			t.Fatal("STUN process did not become ready within 5 seconds")
		case <-ticker.C:
		}
	}
}

func checkBinding(t *testing.T, network, address string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	conn, err := (&net.Dialer{}).DialContext(ctx, network, address)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { closeSocket(t, conn) })
	deadline, _ := ctx.Deadline()
	if err := conn.SetDeadline(deadline); err != nil {
		t.Fatal(err)
	}
	request := make([]byte, 20)
	binary.BigEndian.PutUint16(request, 1)
	binary.BigEndian.PutUint32(request[4:], cookie)
	copy(request[8:], transaction)
	if _, err := conn.Write(request); err != nil {
		t.Fatal(err)
	}
	var buffer [1501]byte
	n, err := conn.Read(buffer[:])
	if err != nil {
		t.Fatal(err)
	}
	response := buffer[:n]
	source := conn.LocalAddr().(*net.UDPAddr).AddrPort()
	ip := source.Addr().Unmap().AsSlice()
	if n != 28+len(ip) || binary.BigEndian.Uint16(response) != 0x0101 ||
		string(response[8:20]) != transaction || binary.BigEndian.Uint16(response[26:])^0x2112 != source.Port() {
		t.Fatalf("unexpected Binding response: %x", response)
	}
	for i, octet := range ip {
		if response[28+i]^request[4+i] != octet {
			t.Fatal("XOR mapping does not contain the client IP")
		}
	}
}

func TestFailedSecondListenerReleasesFirst(t *testing.T) {
	binary := executable(t)
	occupied := listenUDP(t, "udp4", loopback+":0")
	t.Cleanup(func() { closeSocket(t, occupied) })
	first := net.JoinHostPort(loopback, freeUDPPort(t))
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	cmd := stunCommand(ctx, binary, "--listen", first+","+occupied.LocalAddr().String())
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err := cmd.Run()
	var exit *exec.ExitError
	if ctx.Err() != nil || !errors.As(err, &exit) {
		t.Fatalf("expected startup failure, got %v (context: %v)", err, ctx.Err())
	}
	if bytes.Contains(stderr.Bytes(), []byte(loopback)) {
		t.Fatal("diagnostics contain an IP address")
	}
	reused := listenUDP(t, "udp4", first)
	closeSocket(t, reused)
}
