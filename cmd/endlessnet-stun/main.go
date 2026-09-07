package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/endless-net/stun/internal/config"
	"github.com/endless-net/stun/internal/metrics"
	"github.com/endless-net/stun/internal/service"
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
	executableDigest, err := currentExecutableDigest()
	if err != nil {
		return fmt.Errorf("calculate executable revision: %w", err)
	}

	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: cfg.LogLevel})).With("component", "stun")
	slog.SetDefault(logger)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	logger.Info("STUN service starting", "version", version, "commit", commit, "metrics_address", cfg.MetricsAddr)
	if err := service.Run(ctx, cfg, logger, metrics.BuildInfo{Version: version, Commit: commit, BuildDate: buildDate, ExecutableDigest: executableDigest}); err != nil {
		return err
	}
	logger.Info("STUN service stopped")
	return nil
}
