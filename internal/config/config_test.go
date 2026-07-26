package config

import (
	"io"
	"log/slog"
	"strings"
	"testing"
)

func TestParseUsesEnvironmentAndFlags(t *testing.T) {
	env := map[string]string{
		"ENDLESSNET_STUN_ADDRS":                 "127.0.0.1:3478,[::1]:3478",
		"ENDLESSNET_STUN_METRICS_ADDR":          "127.0.0.1:9191",
		"ENDLESSNET_STUN_RATE_LIMIT_PER_SECOND": "30",
		"ENDLESSNET_STUN_RATE_LIMIT_BURST":      "60",
		"ENDLESSNET_STUN_LOG_LEVEL":             "debug",
	}
	cfg, err := Parse([]string{"--rate-limit-burst=70"}, func(name string) (string, bool) {
		value, ok := env[name]
		return value, ok
	}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.ListenAddrs) != 2 || cfg.ListenAddrs[1] != "[::1]:3478" {
		t.Fatalf("listen addresses = %#v", cfg.ListenAddrs)
	}
	if cfg.MetricsAddr != "127.0.0.1:9191" || cfg.RateLimitPerSecond != 30 || cfg.RateLimitBurst != 70 || cfg.LogLevel != slog.LevelDebug {
		t.Fatalf("config = %#v", cfg)
	}
}

func TestParseRejectsInvalidConfiguration(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "missing address", args: nil, want: "at least one"},
		{name: "bad address", args: []string{"--addr=not-an-address"}, want: "invalid STUN listen address"},
		{name: "zero port", args: []string{"--addr=127.0.0.1:0"}, want: "port must be"},
		{name: "bad metrics", args: []string{"--addr=127.0.0.1:3478", "--metrics-addr=bad"}, want: "invalid metrics address"},
		{name: "public metrics", args: []string{"--addr=127.0.0.1:3478", "--metrics-addr=0.0.0.0:9090"}, want: "loopback"},
		{name: "bad rate", args: []string{"--addr=127.0.0.1:3478", "--rate-limit-per-second=zero"}, want: "positive integer"},
		{name: "bad burst", args: []string{"--addr=127.0.0.1:3478", "--rate-limit-burst=0"}, want: "positive integer"},
		{name: "bad log level", args: []string{"--addr=127.0.0.1:3478", "--log-level=trace"}, want: "log-level"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse(tc.args, nil, io.Discard)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want substring %q", err, tc.want)
			}
		})
	}
}

func TestParseVersionDoesNotRequireRuntimeConfiguration(t *testing.T) {
	cfg, err := Parse([]string{"--version"}, nil, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.ShowVersion {
		t.Fatal("ShowVersion = false")
	}
}
