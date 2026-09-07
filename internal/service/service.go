// Package service manages the lifetime of the standalone HTTP and UDP listeners.
package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/endless-net/stun/internal/config"
	"github.com/endless-net/stun/internal/health"
	"github.com/endless-net/stun/internal/metrics"
	"github.com/endless-net/stun/internal/ratelimit"
	"github.com/endless-net/stun/internal/stun"
)

// Run binds every socket before serving traffic. Any bind or component failure
// stops the entire service and releases all resources acquired so far.
func Run(parent context.Context, cfg config.Config, logger *slog.Logger, info metrics.BuildInfo) error {
	if parent.Err() != nil {
		return nil
	}
	if err := cfg.Validate(); err != nil {
		return err
	}
	registry := metrics.New(info)
	sockets := make([]net.PacketConn, 0, len(cfg.ListenAddrs))
	defer func() {
		for _, socket := range sockets {
			_ = socket.Close()
		}
	}()
	for _, addr := range cfg.ListenAddrs {
		socket, err := net.ListenPacket("udp", addr)
		if err != nil {
			return fmt.Errorf("listen for STUN on %s: %w", addr, err)
		}
		sockets = append(sockets, socket)
		registry.SetListener(socket.LocalAddr().String(), false)
	}
	httpListener, err := net.Listen("tcp", cfg.MetricsAddr)
	if err != nil {
		return fmt.Errorf("listen for health: %w", err)
	}
	defer httpListener.Close()
	httpServer := &http.Server{
		Handler:           health.Handler(registry),
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1 << 16,
	}
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	errCh := make(chan error, len(sockets)+1)
	var wg sync.WaitGroup
	start := func(run func() error) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errCh <- run()
		}()
	}
	start(func() error { return httpServer.Serve(httpListener) })
	for _, socket := range sockets {
		server := stun.Server{
			Metrics: registry,
			Limiter: ratelimit.New(cfg.RateLimitPerSecond, cfg.RateLimitBurst),
			Logger:  logger,
		}
		start(func() error { return server.Serve(ctx, socket) })
	}
	var result error
	select {
	case <-parent.Done():
	case result = <-errCh:
		if parent.Err() != nil {
			result = nil
		} else if result == nil || errors.Is(result, http.ErrServerClosed) {
			result = errors.New("service component stopped unexpectedly")
		}
	}
	cancel()
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		// Shutdown does not close active connections after its deadline.
		_ = httpServer.Close()
		result = errors.Join(result, fmt.Errorf("shut down health server: %w", err))
	}
	wg.Wait()
	return result
}
