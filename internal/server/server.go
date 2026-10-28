package server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"go.uber.org/fx"
	"golang.org/x/net/ipv4"
	"golang.org/x/net/ipv6"
)

type udpListener struct {
	conn    *net.UDPConn
	v4      *ipv4.PacketConn
	v6      *ipv6.PacketConn
	limit   *limiter
	buckets int
}

// Server owns its sockets, workers and telemetry for one lifecycle.
type Server struct {
	config       Config
	stats        counters
	ready        atomic.Bool
	stopping     atomic.Bool
	failed       atomic.Bool
	udp          []*udpListener
	http         *http.Server
	listener     net.Listener
	telemetry    *telemetry
	workers      sync.WaitGroup
	fatal        func()
	sink         io.WriteCloser
	telemetryNew func(context.Context, string, io.WriteCloser, *counters) (*telemetry, error)
}

// New returns an unstarted server; failures never log configuration values.
func New(c Config, sink io.WriteCloser, fatal func()) *Server {
	return &Server{config: c, sink: sink, fatal: fatal, telemetryNew: newTelemetry}
}

// App wires bounded startup/shutdown to Fx without exposing Fx's raw errors.
func App(c Config, sink io.WriteCloser) *fx.App {
	return fx.New(fx.NopLogger, fx.StartTimeout(5*time.Second), fx.StopTimeout(5*time.Second), fx.Invoke(func(lc fx.Lifecycle, stop fx.Shutdowner) {
		s := New(c, sink, func() { _ = stop.Shutdown(fx.ExitCode(1)) })
		lc.Append(fx.Hook{OnStart: s.Start, OnStop: s.Stop})
	}))
}

// Start acquires every mandatory socket before starting workers/readiness.
func (s *Server) Start(ctx context.Context) error {
	if err := s.config.validate(); err != nil {
		return err
	}
	lc := net.ListenConfig{}
	rollback := func() {
		for _, u := range s.udp {
			_ = u.conn.Close()
		}
		if s.listener != nil {
			_ = s.listener.Close()
		}
	}
	for _, text := range strings.Split(s.config.Listen, ",") {
		a := netip.MustParseAddrPort(text)
		network := "udp4"
		if a.Addr().Is6() {
			network = "udp6"
		}
		conn, err := lc.ListenPacket(ctx, network, text)
		if err != nil {
			rollback()
			return errors.New("udp_bind")
		}
		udp, ok := conn.(*net.UDPConn)
		if !ok {
			_ = conn.Close()
			rollback()
			return errors.New("udp_socket")
		}
		u := &udpListener{conn: udp, limit: newLimiter()}
		if a.Addr().Is6() {
			u.v6 = ipv6.NewPacketConn(udp)
			err = u.v6.SetControlMessage(ipv6.FlagDst, true)
		} else {
			u.v4 = ipv4.NewPacketConn(udp)
			err = u.v4.SetControlMessage(ipv4.FlagDst, true)
		}
		if err != nil {
			_ = udp.Close()
			rollback()
			return errors.New("udp_packet_info")
		}
		s.udp = append(s.udp, u)
	}
	network := "tcp4"
	if netip.MustParseAddrPort(s.config.HTTP).Addr().Is6() {
		network = "tcp6"
	}
	ln, err := lc.Listen(ctx, network, s.config.HTTP)
	if err != nil {
		rollback()
		return errors.New("http_bind")
	}
	s.listener = &cappedListener{Listener: ln, stats: &s.stats}
	t, err := s.telemetryNew(ctx, s.config.Endpoint, s.sink, &s.stats)
	if err != nil {
		rollback()
		return err
	}
	s.telemetry = t
	s.http = &http.Server{Handler: s.handler(), ErrorLog: log.New(io.Discard, "", 0), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 5 * time.Second, WriteTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 65536}
	for _, u := range s.udp {
		s.workers.Add(1)
		go func() { defer s.workers.Done(); s.serveUDP(u) }()
	}
	s.workers.Add(1)
	go func() {
		defer s.workers.Done()
		err := s.http.Serve(s.listener)
		if err != nil && !errors.Is(err, http.ErrServerClosed) && !s.stopping.Load() {
			s.fail()
		}
	}()
	s.ready.Store(true)
	t.logger.Info("started")
	return nil
}

func (s *Server) fail() {
	s.failed.Store(true)
	s.ready.Store(false)
	s.telemetry.logger.Info("fatal")
	if s.fatal != nil {
		s.fatal()
	}
}

func (s *Server) serveUDP(u *udpListener) {
	buf := make([]byte, 1501)
	for {
		n, source, destination, err := u.read(buf)
		if err != nil {
			if !s.stopping.Load() {
				s.fail()
			}
			return
		}
		if s.stopping.Load() {
			return
		}
		s.stats.Received.Add(1)
		code := "limited"
		if u.limit.allow(source.Addr(), time.Now()) {
			var out []byte
			out, code = binding(buf[:n], source)
			if out != nil {
				if err := u.conn.SetWriteDeadline(time.Now().Add(100 * time.Millisecond)); err != nil {
					code = "write_error"
				} else if err := u.write(out, source, destination); err != nil {
					code = "write_error"
				}
			}
		}
		count := len(u.limit.sources)
		s.stats.Buckets.Add(int64(count - u.buckets))
		u.buckets = count
		switch code {
		case "limited":
			s.stats.Limited.Add(1)
		case "invalid":
			s.stats.Invalid.Add(1)
		case "write_error":
			s.stats.WriteError.Add(1)
		case "success":
			s.stats.Success.Add(1)
		}
		s.telemetry.packet(code)
	}
}

// Reply from the address that received the datagram, including wildcard sockets.
// Choosing a different local source would make connected STUN clients reject it.
func (u *udpListener) read(buf []byte) (int, netip.AddrPort, net.IP, error) {
	var n int
	var remote net.Addr
	var dst net.IP
	var err error
	if u.v4 != nil {
		var cm *ipv4.ControlMessage
		n, cm, remote, err = u.v4.ReadFrom(buf)
		if cm != nil {
			dst = cm.Dst
		}
	} else {
		var cm *ipv6.ControlMessage
		n, cm, remote, err = u.v6.ReadFrom(buf)
		if cm != nil {
			dst = cm.Dst
		}
	}
	if err != nil {
		return 0, netip.AddrPort{}, nil, err
	}
	a, ok := remote.(*net.UDPAddr)
	if !ok {
		return 0, netip.AddrPort{}, nil, errors.New("udp_source")
	}
	return n, a.AddrPort(), dst, nil
}
func (u *udpListener) write(b []byte, source netip.AddrPort, dst net.IP) error {
	addr := net.UDPAddrFromAddrPort(source)
	if u.v4 != nil {
		_, err := u.v4.WriteTo(b, &ipv4.ControlMessage{Src: dst}, addr)
		return err
	}
	_, err := u.v6.WriteTo(b, &ipv6.ControlMessage{Src: dst}, addr)
	return err
}

// Stop removes readiness and releases all owned resources within the caller budget.
func (s *Server) Stop(ctx context.Context) error {
	s.ready.Store(false)
	s.stopping.Store(true)
	for _, u := range s.udp {
		_ = u.conn.Close()
	}
	if s.http != nil {
		_ = s.http.Close()
	} else if s.listener != nil {
		_ = s.listener.Close()
	}
	done := make(chan struct{})
	go func() { s.workers.Wait(); close(done) }()
	select {
	case <-done:
	case <-ctx.Done():
		return ctx.Err()
	}
	if s.telemetry != nil {
		s.telemetry.logger.Info("stopped")
		return s.telemetry.close(ctx)
	}
	return nil
}

func (s *Server) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, _ *http.Request) {
		if !s.ready.Load() || s.failed.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/metrics", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		_, _ = io.WriteString(w, s.stats.metrics())
		if s.telemetry != nil {
			_, _ = fmt.Fprintf(w, "stun_log_queue %d\nstun_span_queue %d\n", len(s.telemetry.logs), len(s.telemetry.processor.queue))
		}
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", "GET")
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if r.ContentLength != 0 || len(r.TransferEncoding) > 0 {
			w.Header().Set("Connection", "close")
			w.WriteHeader(http.StatusRequestEntityTooLarge)
			return
		}
		if r.URL.Path != "/healthz" && r.URL.Path != "/readyz" && r.URL.Path != "/metrics" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		mux.ServeHTTP(w, r)
	})
}

type cappedListener struct {
	net.Listener
	stats *counters
}

func (l *cappedListener) Accept() (net.Conn, error) {
	for {
		c, err := l.Listener.Accept()
		if err != nil {
			return nil, err
		}
		if l.stats.Connections.Load() >= 32 {
			_ = c.Close()
			l.stats.HTTPRejected.Add(1)
			continue
		}
		l.stats.Connections.Add(1)
		return &countedConn{Conn: c, stats: l.stats}, nil
	}
}

type countedConn struct {
	net.Conn
	stats *counters
	once  sync.Once
}

func (c *countedConn) Close() error {
	err := c.Conn.Close()
	c.once.Do(func() { c.stats.Connections.Add(-1) })
	return err
}
