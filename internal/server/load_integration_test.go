//go:build integration

package server

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	sdk "go.opentelemetry.io/otel/sdk/trace"
)

type faultExporter struct{ blocked atomic.Bool }

func (e *faultExporter) ExportSpans(ctx context.Context, _ []sdk.ReadOnlySpan) error {
	if !e.blocked.Load() {
		return nil
	}
	<-ctx.Done()
	return ctx.Err()
}
func (*faultExporter) Shutdown(context.Context) error { return nil }

type switchSink struct {
	blocked atomic.Bool
	stop    chan struct{}
	once    sync.Once
}

func (s *switchSink) Write(b []byte) (int, error) {
	for s.blocked.Load() {
		select {
		case <-s.stop:
			return 0, io.ErrClosedPipe
		case <-time.After(10 * time.Millisecond):
		}
	}
	return len(b), nil
}
func (s *switchSink) Close() error { s.once.Do(func() { close(s.stop) }); return nil }

type loadSnapshot struct {
	Received, Success, Limited, LogDrop, SpanDrop, ExportFailed, HTTPRejected uint64
	Buckets, LogPeak, SpanPeak, LogQueue, SpanQueue, Connections              int64
	Goroutines                                                                int
	RSS                                                                       int64
	Ready, Failed                                                             bool
}

// TestLoadChild hosts the real server in a separate process. Control stays on
// inherited stdin/stdout; no test control endpoint exists in the shipped binary.
func TestLoadChild(t *testing.T) {
	if os.Getenv("STUN_LOAD_CHILD") != "1" {
		t.Skip("private acceptance fixture")
	}
	runtime.GOMAXPROCS(2)
	exporter := &faultExporter{}
	sink := &switchSink{stop: make(chan struct{})}
	s := New(Config{Listen: os.Getenv("STUN_TEST_UDP"), HTTP: os.Getenv("STUN_TEST_HTTP")}, sink, func() {})
	s.telemetryNew = func(_ context.Context, _ string, w io.WriteCloser, c *counters) (*telemetry, error) {
		return buildTelemetry(exporter, w, c, sdk.AlwaysSample()), nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	if err := s.Start(ctx); err != nil {
		cancel()
		t.Fatal(err)
	}
	cancel()
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := s.Stop(ctx); err != nil {
			t.Error(err)
		}
	}()
	decoder, encoder := json.NewDecoder(os.Stdin), json.NewEncoder(os.Stdout)
	for {
		var command string
		if err := decoder.Decode(&command); err != nil {
			if err == io.EOF {
				return
			}
			t.Fatal(err)
		}
		switch command {
		case "fault":
			sink.blocked.Store(true)
			exporter.blocked.Store(true)
		case "recover":
			sink.blocked.Store(false)
			exporter.blocked.Store(false)
		case "stop":
			return
		case "sample":
		default:
			t.Fatal("unknown fixture command")
		}
		snap := loadSnapshot{Received: s.stats.Received.Load(), Success: s.stats.Success.Load(), Limited: s.stats.Limited.Load(), LogDrop: s.stats.LogDropped.Load(), SpanDrop: s.stats.SpanDropped.Load(), ExportFailed: s.stats.ExportFailed.Load(), HTTPRejected: s.stats.HTTPRejected.Load(), Buckets: s.stats.Buckets.Load(), LogPeak: s.stats.LogPeak.Load(), SpanPeak: s.stats.SpanPeak.Load(), LogQueue: int64(len(s.telemetry.logs)), SpanQueue: int64(len(s.telemetry.processor.queue)), Connections: s.stats.Connections.Load(), Goroutines: runtime.NumGoroutine(), Ready: s.ready.Load(), Failed: s.failed.Load()}
		data, err := os.ReadFile("/proc/self/status")
		if err != nil {
			t.Fatal(err)
		}
		for _, line := range strings.Split(string(data), "\n") {
			if strings.HasPrefix(line, "VmHWM:") {
				n, err := strconv.ParseInt(strings.Fields(line)[1], 10, 64)
				if err != nil {
					t.Fatal(err)
				}
				snap.RSS = n
			}
		}
		if err := encoder.Encode(snap); err != nil {
			t.Fatal(err)
		}
	}
}

type loadController struct {
	cmd     *exec.Cmd
	input   io.WriteCloser
	encoder *json.Encoder
	decoder *json.Decoder
	mu      sync.Mutex
}

func (c *loadController) request(t *testing.T, command string) loadSnapshot {
	t.Helper()
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.encoder.Encode(command); err != nil {
		t.Fatal(err)
	}
	var snap loadSnapshot
	if err := c.decoder.Decode(&snap); err != nil {
		t.Fatal("child snapshot", err)
	}
	return snap
}
func startLoadChild(t *testing.T, port, httpPort int) *loadController {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 14*time.Minute+55*time.Second)
	t.Cleanup(cancel)
	cmd := exec.CommandContext(ctx, exe, "-test.run=^TestLoadChild$")
	cmd.Env = append(os.Environ(), "STUN_LOAD_CHILD=1", fmt.Sprintf("STUN_TEST_UDP=0.0.0.0:%d,[::]:%d", port, port), fmt.Sprintf("STUN_TEST_HTTP=127.0.0.1:%d", httpPort))
	cmd.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL}
	cmd.Stderr = os.Stderr
	input, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	output, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	c := &loadController{cmd: cmd, input: input, encoder: json.NewEncoder(input), decoder: json.NewDecoder(output)}
	t.Cleanup(func() {
		_ = input.Close()
		if err := cmd.Wait(); err != nil {
			t.Error("child process", err)
		}
	})
	if s := c.request(t, "sample"); !s.Ready || s.Failed {
		t.Fatal("child startup")
	}
	return c
}

type loadClient struct {
	conn                    net.Conn
	mu                      sync.Mutex
	pending                 map[uint64]time.Time
	sent, received, invalid int
	latency                 []time.Duration
	done                    chan struct{}
}

func newLoadClient(t *testing.T, network, target, source string) *loadClient {
	t.Helper()
	d := net.Dialer{LocalAddr: &net.UDPAddr{IP: net.ParseIP(source)}}
	conn, err := d.DialContext(context.Background(), network, target)
	if err != nil {
		t.Fatal(err)
	}
	c := &loadClient{conn: conn, pending: map[uint64]time.Time{}, done: make(chan struct{})}
	go func() {
		defer close(c.done)
		b := make([]byte, 1501)
		for {
			n, err := conn.Read(b)
			if err != nil {
				return
			}
			c.mu.Lock()
			if n < 32 || binary.BigEndian.Uint16(b) != 0x0101 {
				c.invalid++
				c.mu.Unlock()
				continue
			}
			id := binary.BigEndian.Uint64(b[12:20])
			sent, ok := c.pending[id]
			if ok {
				delete(c.pending, id)
				c.received++
				c.latency = append(c.latency, time.Since(sent))
			} else {
				c.invalid++
			}
			c.mu.Unlock()
		}
	}()
	return c
}
func (c *loadClient) send(t *testing.T, id uint64) {
	t.Helper()
	b := request()
	binary.BigEndian.PutUint64(b[12:], id)
	now := time.Now()
	c.mu.Lock()
	for key, at := range c.pending {
		if now.Sub(at) > time.Second {
			delete(c.pending, key)
		}
	}
	c.pending[id] = now
	c.sent++
	c.mu.Unlock()
	if _, err := c.conn.Write(b); err != nil {
		t.Error(err)
	}
}
func (c *loadClient) close() { _ = c.conn.Close(); <-c.done }

// TestFullLoad runs the complete 860-second profile, with generator outside
// the server process, in a private user/network namespace created by run.sh.
func TestFullLoad(t *testing.T) {
	if os.Getenv("STUN_FULL_LOAD") != "1" {
		t.Skip("separate acceptance: test/acceptance/run.sh")
	}
	port := freePort(t, "udp4", "127.0.0.1:0")
	httpPort := freePort(t, "tcp4", "127.0.0.1:0")
	control := startLoadChild(t, port, httpPort)
	types := []struct{ network, target, load, probe string }{{"udp4", fmt.Sprintf("127.0.0.1:%d", port), "127.0.0.2", "127.0.0.3"}, {"udp6", fmt.Sprintf("[fd00::1]:%d", port), "fd00::10", "fd00::11"}}
	for _, f := range types {
		exchange(t, f.network, f.target, f.load, request(), true)
	}
	phases := []struct {
		name          string
		seconds, rate int
		fault         bool
	}{{"warm", 10, 5, false}, {"normal", 60, 5, false}, {"saturationA", 60, 200, false}, {"recoveryA", 30, 5, false}, {"saturationB", 60, 200, true}, {"recoveryB", 30, 5, false}, {"idle", 610, 0, false}}
	var id uint64
	var peakRSS int64
	peakG := 0
	for _, phase := range phases {
		phaseStart := time.Now()
		t.Log("phase", phase.name)
		command := "recover"
		if phase.fault {
			command = "fault"
		}
		before := control.request(t, command)
		var slow []net.Conn
		if phase.fault {
			for range 32 {
				d := net.Dialer{Timeout: time.Second}
				c, err := d.DialContext(context.Background(), "tcp4", fmt.Sprintf("127.0.0.1:%d", httpPort))
				if err != nil {
					t.Fatal(err)
				}
				slow = append(slow, c)
				_, _ = io.WriteString(c, "GET /healthz HTTP/1.1\r\n")
			}
			time.Sleep(20 * time.Millisecond)
			d := net.Dialer{Timeout: time.Second}
			c, err := d.DialContext(context.Background(), "tcp4", fmt.Sprintf("127.0.0.1:%d", httpPort))
			if err != nil {
				t.Fatal(err)
			}
			_ = c.SetReadDeadline(time.Now().Add(time.Second))
			var b [1]byte
			_, _ = c.Read(b[:])
			_ = c.Close()
		}
		clients, probes := []*loadClient{}, []*loadClient{}
		for _, f := range types {
			clients = append(clients, newLoadClient(t, f.network, f.target, f.load))
			probes = append(probes, newLoadClient(t, f.network, f.target, f.probe))
		}
		nextLoad, nextProbe, nextSample := phaseStart, phaseStart, phaseStart
		end := phaseStart.Add(time.Duration(phase.seconds) * time.Second)
		for time.Now().Before(end) {
			now := time.Now()
			if phase.rate > 0 && !now.Before(nextLoad) {
				for _, c := range clients {
					id++
					c.send(t, id)
				}
				nextLoad = nextLoad.Add(time.Second / time.Duration(phase.rate))
			}
			if !now.Before(nextProbe) {
				for _, c := range probes {
					id++
					c.send(t, id)
				}
				nextProbe = nextProbe.Add(time.Second)
			}
			if !now.Before(nextSample) {
				snap := control.request(t, "sample")
				peakRSS = max(peakRSS, snap.RSS)
				peakG = max(peakG, snap.Goroutines)
				if snap.RSS > 256*1024 || snap.Goroutines > 128 || snap.Connections > 32 || snap.LogQueue > 256 || snap.SpanQueue > 1024 || snap.Buckets > 4*65536 || !snap.Ready || snap.Failed {
					t.Error("resource/ready invariant", snap)
				}
				nextSample = nextSample.Add(time.Second)
			}
			time.Sleep(time.Millisecond)
		}
		time.Sleep(50 * time.Millisecond)
		for _, c := range append(clients, probes...) {
			c.close()
		}
		for _, c := range probes {
			if c.sent != phase.seconds || c.received != c.sent || c.invalid != 0 {
				t.Errorf("%s probe sent=%d received=%d invalid=%d", phase.name, c.sent, c.received, c.invalid)
			}
			for _, d := range c.latency {
				if d > time.Second {
					t.Error("probe deadline")
				}
			}
		}
		for _, c := range clients {
			if c.invalid != 0 {
				t.Error("invalid response")
			}
			if phase.rate == 200 {
				if c.received > 40+20*phase.seconds+1 {
					t.Error("rate bound")
				}
				if c.sent < phase.seconds*phase.rate*99/100 {
					t.Error("generator pacing")
				}
			} else if phase.rate > 0 {
				if c.received*100 < c.sent*99 {
					t.Errorf("%s loss sent=%d received=%d", phase.name, c.sent, c.received)
				}
				sort.Slice(c.latency, func(i, j int) bool { return c.latency[i] < c.latency[j] })
				if len(c.latency) > 0 && c.latency[len(c.latency)*99/100] > 250*time.Millisecond {
					t.Error("p99")
				}
			}
		}
		after := control.request(t, "sample")
		if phase.rate == 200 {
			sent := 0
			for _, c := range clients {
				sent += c.sent
			}
			if (after.Limited-before.Limited)*100 < uint64(sent*80) {
				t.Error("rate pressure not demonstrated")
			}
		}
		if phase.fault {
			if after.LogDrop == 0 || after.SpanDrop == 0 || after.HTTPRejected == 0 || after.ExportFailed == 0 {
				t.Error("fault pressure not demonstrated")
			}
			if after.LogPeak != 256 || after.SpanPeak != 1024 {
				t.Error("queue caps not reached")
			}
		}
		if phase.name == "idle" && after.Buckets > 2 {
			t.Error("idle buckets retained", after.Buckets)
		}
		for _, c := range slow {
			_ = c.Close()
		}
		t.Logf("%s duration=%s success=%d limited=%d", phase.name, time.Since(phaseStart), after.Success-before.Success, after.Limited-before.Limited)
	}
	t.Logf("server peak RSS=%d KiB goroutines=%d", peakRSS, peakG)
}
