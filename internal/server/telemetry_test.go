package server

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	sdk "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

type testSink struct{ io.Writer }

func (testSink) Close() error { return nil }

func TestResourcePrivacyAndLogLevel(t *testing.T) {
	t.Setenv("OTEL_RESOURCE_ATTRIBUTES", "secret=canary,client.ip=192.0.2.1")
	t.Setenv("OTEL_SERVICE_NAME", "canary")
	stats := &counters{}
	tel := buildTelemetry(nil, testSink{Writer: io.Discard}, stats, sdk.AlwaysSample())
	recorder := tracetest.NewSpanRecorder()
	tel.provider.RegisterSpanProcessor(recorder)
	_, span := tel.tracer.Start(context.Background(), "stun.binding")
	span.End()
	ended := recorder.Ended()
	if len(ended) != 1 {
		t.Fatal("missing span")
	}
	attrs := ended[0].Resource().Attributes()
	if len(attrs) != 1 || string(attrs[0].Key) != "service.name" || attrs[0].Value.AsString() != "stun" {
		t.Fatal("ambient resource", attrs)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := tel.close(ctx); err != nil {
		t.Fatal(err)
	}
	q := make(chan string, 256)
	h := &asyncHandler{queue: q, drops: &stats.LogDropped, peak: &stats.LogPeak, level: slog.LevelError}
	logger := slog.New(h)
	logger.Info("started")
	logger.Error("fatal")
	if len(q) != 1 {
		t.Fatal("level filter")
	}
	if !bytes.Contains([]byte(<-q), []byte(`"level":"ERROR"`)) {
		t.Fatal("wrong event level")
	}
}

type blockedSink struct {
	start chan struct{}
	done  chan struct{}
	once  sync.Once
	began sync.Once
}

func (s *blockedSink) Write(b []byte) (int, error) {
	s.began.Do(func() { close(s.start) })
	<-s.done
	return 0, io.ErrClosedPipe
}
func (s *blockedSink) Close() error { s.once.Do(func() { close(s.done) }); return nil }

func TestBoundedLogPrivacy(t *testing.T) {
	c := &counters{}
	q := make(chan string, 256)
	h := &asyncHandler{queue: q, drops: &c.LogDropped, peak: &c.LogPeak}
	logger := slog.New(h).With("client", "192.0.2.1", "secret", "password")
	for range 300 {
		logger.Error("192.0.2.1 secret payload", "error", "net.OpError 192.0.2.1")
	}
	if len(q) != 256 || c.LogDropped.Load() != 44 || c.LogPeak.Load() != 256 {
		t.Fatal("cap")
	}
	for range 256 {
		if line := <-q; strings.Contains(line, "192.0.2.1") || strings.Contains(line, "password") || len(line) > 2048 {
			t.Fatal(line)
		}
	}
}

func TestBlockedLogStop(t *testing.T) {
	sink := &blockedSink{start: make(chan struct{}), done: make(chan struct{})}
	stats := &counters{}
	tel, err := newTelemetry(context.Background(), "", sink, stats)
	if err != nil {
		t.Fatal(err)
	}
	tel.logger.Info("started")
	select {
	case <-sink.start:
	case <-time.After(time.Second):
		t.Fatal("writer")
	}
	for range 300 {
		tel.logger.Info("packet")
	}
	if stats.LogDropped.Load() == 0 {
		t.Fatal("no pressure")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := tel.close(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestSpanAdmission(t *testing.T) {
	c := &counters{}
	p := &processor{queue: make(chan sdk.ReadOnlySpan, 1024), stats: c, stop: make(chan struct{}), done: make(chan struct{})}
	span := tracetest.SpanStub{Name: "stun.binding"}.Snapshot()
	for range 1100 {
		p.mu.Lock()
		p.second = time.Time{}
		p.mu.Unlock()
		p.OnEnd(span)
	}
	if len(p.queue) != 1024 || c.SpanDropped.Load() != 76 || c.SpanPeak.Load() != 1024 {
		t.Fatal("span queue cap")
	}
	q := &processor{queue: make(chan sdk.ReadOnlySpan, 1024), stats: c, stop: make(chan struct{}), done: make(chan struct{})}
	for range 101 {
		q.OnEnd(span)
	}
	if len(q.queue) != 100 {
		t.Fatal("span rate")
	}
}
