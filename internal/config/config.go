package config

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"strconv"
	"strings"
)

const (
	DefaultMetricsAddr        = "127.0.0.1:9090"
	DefaultRateLimitPerSecond = 20
	DefaultRateLimitBurst     = 40
	DefaultLogLevel           = "info"
)

type Config struct {
	ListenAddrs        []string
	MetricsAddr        string
	RateLimitPerSecond int
	RateLimitBurst     int
	LogLevel           slog.Level
	CheckConfig        bool
	ShowVersion        bool
}

type LookupEnv func(string) (string, bool)

func Parse(args []string, lookupEnv LookupEnv, stderr io.Writer) (Config, error) {
	if lookupEnv == nil {
		lookupEnv = func(string) (string, bool) { return "", false }
	}
	addrsDefault := env(lookupEnv, "ENDLESSNET_STUN_ADDRS", "")
	metricsDefault := env(lookupEnv, "ENDLESSNET_STUN_METRICS_ADDR", DefaultMetricsAddr)
	rateDefault := env(lookupEnv, "ENDLESSNET_STUN_RATE_LIMIT_PER_SECOND", strconv.Itoa(DefaultRateLimitPerSecond))
	burstDefault := env(lookupEnv, "ENDLESSNET_STUN_RATE_LIMIT_BURST", strconv.Itoa(DefaultRateLimitBurst))
	logLevelDefault := env(lookupEnv, "ENDLESSNET_STUN_LOG_LEVEL", DefaultLogLevel)

	fs := flag.NewFlagSet("endlessnet-stun", flag.ContinueOnError)
	fs.SetOutput(stderr)
	addrs := fs.String("addr", addrsDefault, "comma-separated UDP listen addresses")
	metricsAddr := fs.String("metrics-addr", metricsDefault, "HTTP health and metrics listen address")
	rate := fs.String("rate-limit-per-second", rateDefault, "requests per second allowed for one source IP")
	burst := fs.String("rate-limit-burst", burstDefault, "burst allowed for one source IP")
	logLevel := fs.String("log-level", logLevelDefault, "structured log level: debug, info, warn, or error")
	checkConfig := fs.Bool("check-config", false, "validate configuration and exit")
	showVersion := fs.Bool("version", false, "print version and exit")
	if err := fs.Parse(args); err != nil {
		return Config{}, err
	}
	if fs.NArg() != 0 {
		return Config{}, fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}

	parsedRate, err := positiveInt("rate-limit-per-second", *rate)
	if err != nil {
		return Config{}, err
	}
	parsedBurst, err := positiveInt("rate-limit-burst", *burst)
	if err != nil {
		return Config{}, err
	}
	parsedLevel, err := parseLogLevel(*logLevel)
	if err != nil {
		return Config{}, err
	}
	cfg := Config{
		ListenAddrs:        splitCSV(*addrs),
		MetricsAddr:        strings.TrimSpace(*metricsAddr),
		RateLimitPerSecond: parsedRate,
		RateLimitBurst:     parsedBurst,
		LogLevel:           parsedLevel,
		CheckConfig:        *checkConfig,
		ShowVersion:        *showVersion,
	}
	if cfg.ShowVersion {
		return cfg, nil
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c Config) Validate() error {
	if len(c.ListenAddrs) == 0 {
		return errors.New("at least one STUN listen address is required")
	}
	seen := make(map[string]struct{}, len(c.ListenAddrs))
	for _, addr := range c.ListenAddrs {
		resolved, err := net.ResolveUDPAddr("udp", addr)
		if err != nil {
			return fmt.Errorf("invalid STUN listen address %q: %w", addr, err)
		}
		if resolved.Port <= 0 {
			return fmt.Errorf("invalid STUN listen address %q: port must be between 1 and 65535", addr)
		}
		if _, exists := seen[addr]; exists {
			return fmt.Errorf("duplicate STUN listen address %q", addr)
		}
		seen[addr] = struct{}{}
	}
	if c.MetricsAddr == "" {
		return errors.New("metrics address is required so health, readiness, and metrics remain available")
	}
	metricsAddr, err := net.ResolveTCPAddr("tcp", c.MetricsAddr)
	if err != nil {
		return fmt.Errorf("invalid metrics address %q: %w", c.MetricsAddr, err)
	}
	if metricsAddr.Port <= 0 {
		return fmt.Errorf("invalid metrics address %q: port must be between 1 and 65535", c.MetricsAddr)
	}
	if metricsAddr.IP == nil || !metricsAddr.IP.IsLoopback() {
		return fmt.Errorf("invalid metrics address %q: health and metrics must bind to a loopback address", c.MetricsAddr)
	}
	if c.RateLimitPerSecond <= 0 {
		return errors.New("rate-limit-per-second must be a positive integer")
	}
	if c.RateLimitBurst <= 0 {
		return errors.New("rate-limit-burst must be a positive integer")
	}
	return nil
}

func env(lookupEnv LookupEnv, name, fallback string) string {
	if value, ok := lookupEnv(name); ok && strings.TrimSpace(value) != "" {
		return strings.TrimSpace(value)
	}
	return fallback
}

func positiveInt(name, value string) (int, error) {
	n, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("%s must be a positive integer", name)
	}
	return n, nil
}

func parseLogLevel(value string) (slog.Level, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "debug":
		return slog.LevelDebug, nil
	case "info":
		return slog.LevelInfo, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return 0, fmt.Errorf("log-level %q is invalid; want debug, info, warn, or error", value)
	}
}

func splitCSV(value string) []string {
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}
