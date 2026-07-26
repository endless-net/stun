package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/unng-lab/endlessnet-stun/internal/config"
	"github.com/unng-lab/endlessnet-stun/internal/health"
	"github.com/unng-lab/endlessnet-stun/internal/metrics"
	"github.com/unng-lab/endlessnet-stun/internal/ratelimit"
	"github.com/unng-lab/endlessnet-stun/internal/stun"
)

var (
	version   = "dev"
	commit    = "unknown"
	buildDate = "unknown"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "endlessnet-stun:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	cfg, err := config.Parse(args, os.LookupEnv, os.Stderr)
	if err != nil {
		return err
	}
	if cfg.ShowVersion {
		fmt.Printf("endlessnet-stun %s (commit %s, built %s)\n", version, commit, buildDate)
		return nil
	}
	if cfg.CheckConfig {
		fmt.Println("configuration ok")
		return nil
	}

	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: cfg.LogLevel})).With("component", "stun")
	slog.SetDefault(logger)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	logger.Info("STUN service starting", "version", version, "commit", commit, "metrics_address", cfg.MetricsAddr)
	if err := serve(ctx, cfg, logger); err != nil {
		return err
	}
	logger.Info("STUN service stopped")
	return nil
}

func serve(parent context.Context, cfg config.Config, logger *slog.Logger) error {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	registry := metrics.New(version, commit)
	httpServer := &http.Server{
		Addr:              cfg.MetricsAddr,
		Handler:           health.Handler(registry),
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1 << 16,
	}

	errCh := make(chan error, len(cfg.ListenAddrs)+1)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		err := httpServer.ListenAndServe()
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		errCh <- err
	}()
	for _, addr := range cfg.ListenAddrs {
		server := stun.Server{
			Addr:    addr,
			Metrics: registry,
			Limiter: ratelimit.New(cfg.RateLimitPerSecond, cfg.RateLimitBurst),
			Logger:  logger,
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			errCh <- server.ListenAndServe(ctx)
		}()
	}

	var result error
	select {
	case <-parent.Done():
	case err := <-errCh:
		if err != nil {
			result = err
		} else if parent.Err() == nil {
			result = errors.New("service component stopped unexpectedly")
		}
	}
	cancel()
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil && result == nil {
		result = fmt.Errorf("shut down health server: %w", err)
	}
	wg.Wait()
	return result
}
