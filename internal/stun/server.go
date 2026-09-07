package stun

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"time"

	"github.com/endless-net/stun/internal/metrics"
	"github.com/endless-net/stun/internal/ratelimit"
)

type Server struct {
	Addr    string
	Metrics *metrics.Registry
	Limiter *ratelimit.Limiter
	Logger  *slog.Logger
}

func (s Server) ListenAndServe(ctx context.Context) error {
	if s.Addr == "" {
		return errors.New("STUN listen address is required")
	}
	if s.Limiter == nil {
		return errors.New("STUN rate limiter is required")
	}
	if s.Metrics == nil {
		return errors.New("STUN metrics registry is required")
	}
	conn, err := ListenPacket(s.Addr)
	if err != nil {
		return fmt.Errorf("listen for STUN on %s: %w", s.Addr, err)
	}
	return s.Serve(ctx, conn)
}

// Serve takes ownership of an already bound socket, including on failure.
// This lets the service bind all listeners before starting any component.
func (s Server) Serve(ctx context.Context, conn net.PacketConn) error {
	if conn == nil {
		return errors.New("STUN socket is required")
	}
	defer conn.Close()
	if s.Limiter == nil || s.Metrics == nil {
		return errors.New("STUN rate limiter and metrics registry are required")
	}
	listener := conn.LocalAddr().String()
	s.Metrics.SetListener(listener, true)
	defer s.Metrics.SetListener(listener, false)
	logger := s.Logger
	if logger == nil {
		logger = slog.Default()
	}
	logger.Info("STUN listener started", "listener", listener)
	defer logger.Info("STUN listener stopped", "listener", listener)

	closed := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = conn.Close()
		case <-closed:
		}
	}()
	defer close(closed)

	buffer := make([]byte, MaxDatagramSize+1)
	for {
		n, remoteAddr, readErr := conn.ReadFrom(buffer)
		if readErr != nil && !isDatagramTruncated(readErr) {
			if ctx.Err() != nil || errors.Is(readErr, net.ErrClosed) {
				return nil
			}
			s.Metrics.RecordError(listener, "read_error")
			return fmt.Errorf("read STUN datagram on %s: read_error", listener)
		}
		started := time.Now()
		outcome := s.handleDatagram(conn, buffer[:n], remoteAddr, isDatagramTruncated(readErr), started)
		s.recordOutcome(logger, listener, remoteAddr, outcome, time.Since(started))
	}
}

// handleDatagram owns packet policy; the read loop only manages transport lifetime.
func (s Server) handleDatagram(conn net.PacketConn, packet []byte, source net.Addr, truncated bool, now time.Time) packetOutcome {
	remote, ok := source.(*net.UDPAddr)
	if !ok || remote == nil {
		return packetOutcome{result: "invalid", code: "non_udp_source"}
	}
	if len(packet) > MaxDatagramSize || truncated {
		return packetOutcome{result: "invalid", code: "datagram_too_large"}
	}
	if !s.Limiter.Allow(remote, now) {
		return packetOutcome{result: "rate_limited", code: "source_rate_limit"}
	}
	response, err := BuildBindingResponse(packet, remote)
	if err != nil {
		return packetOutcome{result: "invalid", code: "invalid_request"}
	}
	if _, err := conn.WriteTo(response, remote); err != nil {
		return packetOutcome{result: "error", code: "write_error"}
	}
	return packetOutcome{result: "success"}
}

type packetOutcome struct{ result, code string }

// Keep outcome accounting and redaction in one place, without transport errors
// or packet contents ever becoming log fields.
func (s Server) recordOutcome(logger *slog.Logger, listener string, source net.Addr, outcome packetOutcome, duration time.Duration) {
	remote, _ := source.(*net.UDPAddr)
	family := addressFamily(remote)
	s.Metrics.RecordRequest(listener, family)
	s.Metrics.ObserveDuration(listener, outcome.result, duration)
	level, message, result := slog.LevelDebug, "STUN datagram rejected", "rejected"
	switch outcome.result {
	case "success":
		s.Metrics.RecordResponse(listener, family)
		return
	case "invalid":
		s.Metrics.RecordInvalid(listener, family)
	case "rate_limited":
		s.Metrics.RecordRateLimited(listener, family)
		message, result = "STUN datagram rate limited", "rate_limited"
	case "error":
		s.Metrics.RecordError(listener, outcome.code)
		level, message, result = slog.LevelWarn, "STUN response failed", "error"
	}
	logger.Log(context.Background(), level, message, "listener", listener, "remote_address", "redacted", "result", result, "error_code", outcome.code)
}

func addressFamily(addr *net.UDPAddr) string {
	if addr == nil || addr.IP == nil {
		return "unknown"
	}
	if addr.IP.To4() != nil {
		return "ipv4"
	}
	return "ipv6"
}
