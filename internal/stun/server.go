package stun

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"time"

	"github.com/unng-lab/endlessnet-stun/internal/metrics"
	"github.com/unng-lab/endlessnet-stun/internal/ratelimit"
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
	conn, err := net.ListenPacket("udp", s.Addr)
	if err != nil {
		return fmt.Errorf("listen for STUN on %s: %w", s.Addr, err)
	}
	defer conn.Close()
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
		if readErr != nil {
			if ctx.Err() != nil || errors.Is(readErr, net.ErrClosed) {
				return nil
			}
			s.Metrics.RecordError(listener, "read_error")
			return fmt.Errorf("read STUN datagram on %s: %w", listener, readErr)
		}
		started := time.Now()
		remote, ok := remoteAddr.(*net.UDPAddr)
		family := addressFamily(remote)
		s.Metrics.RecordRequest(listener, family)
		if !ok {
			s.Metrics.RecordInvalid(listener, family)
			s.Metrics.ObserveDuration(listener, "invalid", time.Since(started))
			logger.Debug("STUN datagram rejected", "listener", listener, "remote_address", "redacted", "result", "rejected", "error_code", "non_udp_source")
			continue
		}
		if n > MaxDatagramSize {
			s.Metrics.RecordInvalid(listener, family)
			s.Metrics.ObserveDuration(listener, "invalid", time.Since(started))
			logger.Debug("STUN datagram rejected", "listener", listener, "remote_address", "redacted", "result", "rejected", "error_code", "datagram_too_large")
			continue
		}
		if !s.Limiter.Allow(remote, started) {
			s.Metrics.RecordRateLimited(listener, family)
			s.Metrics.ObserveDuration(listener, "rate_limited", time.Since(started))
			logger.Debug("STUN datagram rate limited", "listener", listener, "remote_address", "redacted", "result", "rate_limited", "error_code", "source_rate_limit")
			continue
		}
		response, buildErr := BuildBindingResponse(buffer[:n], remote)
		if buildErr != nil {
			s.Metrics.RecordInvalid(listener, family)
			s.Metrics.ObserveDuration(listener, "invalid", time.Since(started))
			logger.Debug("STUN datagram rejected", "listener", listener, "remote_address", "redacted", "result", "rejected", "error_code", "invalid_request")
			continue
		}
		if _, writeErr := conn.WriteTo(response, remote); writeErr != nil {
			s.Metrics.RecordError(listener, "write_error")
			s.Metrics.ObserveDuration(listener, "error", time.Since(started))
			logger.Warn("STUN response failed", "listener", listener, "remote_address", "redacted", "result", "error", "error_code", "write_error", "error", writeErr)
			continue
		}
		s.Metrics.RecordResponse(listener, family)
		s.Metrics.ObserveDuration(listener, "success", time.Since(started))
	}
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
