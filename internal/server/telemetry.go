package server

import (
	"context"
	"crypto/tls"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	sdk "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

type counters struct {
	Received, Success, Invalid, Limited, WriteError     atomic.Uint64
	LogDropped, SpanDropped, ExportFailed, HTTPRejected atomic.Uint64
	Connections, Buckets, LogPeak, SpanPeak             atomic.Int64
}

type asyncHandler struct {
	queue chan string
	drops *atomic.Uint64
	peak  *atomic.Int64
}

func (h *asyncHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h *asyncHandler) Handle(_ context.Context, r slog.Record) error {
	// Callers supply only fixed event names; arbitrary error/attribute values never leave this boundary.
	event := "packet"
	if r.Message == "started" || r.Message == "stopped" || r.Message == "fatal" {
		event = r.Message
	}
	correlation := ""
	r.Attrs(func(a slog.Attr) bool {
		if a.Key == "trace_id" && a.Value.Kind() == slog.KindString {
			v := a.Value.String()
			if len(v) == 32 {
				if _, err := hex.DecodeString(v); err == nil {
					correlation = v
				}
			}
		}
		return true
	})
	line := fmt.Sprintf("{\"level\":\"INFO\",\"service\":\"stun\",\"component\":\"server\",\"event\":\"%s\",\"trace_id\":\"%s\"}\n", event, correlation)
	select {
	case h.queue <- line:
		updatePeak(h.peak, int64(len(h.queue)))
	default:
		h.drops.Add(1)
	}
	return nil
}
func (h *asyncHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *asyncHandler) WithGroup(string) slog.Handler      { return h }

func updatePeak(p *atomic.Int64, n int64) {
	for old := p.Load(); n > old; old = p.Load() {
		if p.CompareAndSwap(old, n) {
			return
		}
	}
}

type processor struct {
	queue    chan sdk.ReadOnlySpan
	exporter sdk.SpanExporter
	stats    *counters
	stop     chan struct{}
	done     chan struct{}
	once     sync.Once
	mu       sync.Mutex
	second   time.Time
	count    int
}

func (p *processor) OnStart(context.Context, sdk.ReadWriteSpan) {}
func (p *processor) OnEnd(s sdk.ReadOnlySpan) {
	p.mu.Lock()
	now := time.Now()
	if now.Sub(p.second) >= time.Second {
		p.second = now
		p.count = 0
	}
	if p.count >= 100 {
		p.mu.Unlock()
		p.stats.SpanDropped.Add(1)
		return
	}
	p.count++
	p.mu.Unlock()
	select {
	case <-p.stop:
		return
	default:
	}
	select {
	case p.queue <- s:
		updatePeak(&p.stats.SpanPeak, int64(len(p.queue)))
	default:
		p.stats.SpanDropped.Add(1)
	}
}
func (p *processor) ForceFlush(context.Context) error { return nil }
func (p *processor) Shutdown(ctx context.Context) error {
	p.once.Do(func() { close(p.stop) })
	select {
	case <-p.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (p *processor) run() {
	defer close(p.done)
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-p.stop:
			if p.exporter != nil {
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				_ = p.exporter.Shutdown(ctx)
				cancel()
			}
			return
		case <-ticker.C:
			batch := make([]sdk.ReadOnlySpan, 0, 128)
			for len(batch) < 128 {
				select {
				case s := <-p.queue:
					batch = append(batch, s)
				default:
					goto collected
				}
			}
		collected:
			if len(batch) > 0 && p.exporter != nil {
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				err := p.exporter.ExportSpans(ctx, batch)
				cancel()
				if err != nil {
					p.stats.ExportFailed.Add(1)
				}
			}
		}
	}
}

type telemetry struct {
	stats     *counters
	logger    *slog.Logger
	provider  *sdk.TracerProvider
	tracer    trace.Tracer
	processor *processor
	logs      chan string
	stop      chan struct{}
	done      chan struct{}
	sink      io.WriteCloser
	logMu     sync.Mutex
	logTime   time.Time
	logTokens float64
}

func newTelemetry(ctx context.Context, endpoint string, sink io.WriteCloser, stats *counters) (*telemetry, error) {
	var exporter sdk.SpanExporter
	if endpoint != "" {
		var err error
		client := &http.Client{Timeout: 2 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }, Transport: &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS13}, MaxConnsPerHost: 1, MaxIdleConns: 1, IdleConnTimeout: 5 * time.Second, DisableKeepAlives: true}}
		exporter, err = otlptracehttp.New(ctx, otlptracehttp.WithEndpointURL(endpoint), otlptracehttp.WithHTTPClient(client), otlptracehttp.WithHeaders(map[string]string{}), otlptracehttp.WithEncoding(otlptracehttp.EncodingProtobuf), otlptracehttp.WithMaxRequestSize(256<<10), otlptracehttp.WithMaxResponseSize(64<<10), otlptracehttp.WithTimeout(2*time.Second), otlptracehttp.WithRetry(otlptracehttp.RetryConfig{Enabled: false}), otlptracehttp.WithCompression(otlptracehttp.NoCompression))
		if err != nil {
			return nil, fmt.Errorf("telemetry_init")
		}
	}
	return buildTelemetry(exporter, sink, stats, sdk.TraceIDRatioBased(0.01)), nil
}

func buildTelemetry(exporter sdk.SpanExporter, sink io.WriteCloser, stats *counters, sampler sdk.Sampler) *telemetry {
	p := &processor{queue: make(chan sdk.ReadOnlySpan, 1024), stats: stats, exporter: exporter, stop: make(chan struct{}), done: make(chan struct{})}
	t := &telemetry{stats: stats, processor: p, logs: make(chan string, 256), stop: make(chan struct{}), done: make(chan struct{}), sink: sink, logTokens: 20, logTime: time.Now()}
	t.logger = slog.New(&asyncHandler{queue: t.logs, drops: &stats.LogDropped, peak: &stats.LogPeak})
	t.provider = sdk.NewTracerProvider(sdk.WithSampler(sampler), sdk.WithSpanProcessor(p), sdk.WithRawSpanLimits(sdk.SpanLimits{AttributeCountLimit: 4, AttributeValueLengthLimit: 64, EventCountLimit: 0, LinkCountLimit: 0, AttributePerEventCountLimit: 0, AttributePerLinkCountLimit: 0}))
	t.tracer = t.provider.Tracer("stun")
	go p.run()
	go func() {
		defer close(t.done)
		for {
			select {
			case <-t.stop:
				return
			case line := <-t.logs:
				_, _ = io.WriteString(t.sink, line)
			}
		}
	}()
	return t
}

func (t *telemetry) packet(code string) {
	_, span := t.tracer.Start(context.Background(), "stun.binding", trace.WithAttributes(attribute.String("result", code)))
	t.logMu.Lock()
	now := time.Now()
	t.logTokens = min(20, t.logTokens+now.Sub(t.logTime).Seconds()*10)
	t.logTime = now
	log := t.logTokens >= 1
	if log {
		t.logTokens--
	}
	t.logMu.Unlock()
	if log {
		t.logger.Info("packet", slog.String("trace_id", span.SpanContext().TraceID().String()))
	}
	span.End()
}

func (t *telemetry) close(ctx context.Context) error {
	close(t.stop)
	// Closing the owned sink releases a writer blocked by a pipe/slow reader.
	_ = t.sink.Close()
	err := t.provider.Shutdown(ctx)
	select {
	case <-t.done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (c *counters) metrics() string {
	var b strings.Builder
	for _, m := range []struct {
		name  string
		value uint64
	}{
		{"received_total", c.Received.Load()}, {"success_total", c.Success.Load()}, {"invalid_total", c.Invalid.Load()},
		{"limited_total", c.Limited.Load()}, {"write_error_total", c.WriteError.Load()}, {"log_dropped_total", c.LogDropped.Load()},
		{"span_dropped_total", c.SpanDropped.Load()}, {"export_failed_total", c.ExportFailed.Load()}, {"http_rejected_total", c.HTTPRejected.Load()},
	} {
		fmt.Fprintf(&b, "stun_%s %d\n", m.name, m.value)
	}
	fmt.Fprintf(&b, "stun_connections %d\nstun_buckets %d\nstun_log_peak %d\nstun_span_peak %d\n", c.Connections.Load(), c.Buckets.Load(), c.LogPeak.Load(), c.SpanPeak.Load())
	return b.String()
}
