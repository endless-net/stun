package server

import (
	"errors"
	"flag"
	"io"
	"log/slog"
	"net/netip"
	"net/url"
	"strings"

	"github.com/caarlos0/env/v11"
)

// Config exposes addresses only; resource budgets are fixed by the small profile.
type Config struct {
	Listen    string `env:"ENDLESSNET_STUN_LISTEN"`
	HTTP      string `env:"ENDLESSNET_STUN_HTTP_LISTEN" envDefault:"127.0.0.1:8080"`
	Endpoint  string `env:"ENDLESSNET_STUN_OTEL_ENDPOINT"`
	LogLevel  string `env:"ENDLESSNET_STUN_LOG_LEVEL" envDefault:"info"`
	LogFormat string `env:"ENDLESSNET_STUN_LOG_FORMAT" envDefault:"json"`
	Check     bool
	Version   bool
}

// Parse validates all environment/flag inputs without opening sockets.
func Parse(args []string) (Config, error) {
	c := Config{}
	if err := env.Parse(&c); err != nil {
		return c, errors.New("config_environment")
	}
	f := flag.NewFlagSet("stun", flag.ContinueOnError)
	f.SetOutput(io.Discard)
	f.StringVar(&c.Listen, "listen", c.Listen, "comma-separated numeric UDP IP:port")
	f.StringVar(&c.HTTP, "http-listen", c.HTTP, "numeric loopback HTTP IP:port")
	f.StringVar(&c.Endpoint, "otel-endpoint", c.Endpoint, "explicit OTLP HTTP base URL")
	f.StringVar(&c.LogLevel, "log-level", c.LogLevel, "debug, info, warn or error")
	f.StringVar(&c.LogFormat, "log-format", c.LogFormat, "json")
	f.BoolVar(&c.Check, "check-config", false, "validate without binding")
	f.BoolVar(&c.Version, "version", false, "show version without binding")
	if err := f.Parse(args); err != nil || f.NArg() != 0 {
		return c, errors.New("config_flags")
	}
	if c.Version {
		return c, nil
	}
	_, err := c.validate()
	return c, err
}

type validatedConfig struct {
	udp      []socketAddress
	http     socketAddress
	logLevel slog.Level
}

type socketAddress struct {
	text string
	ipv6 bool
}

func (c Config) validate() (validatedConfig, error) {
	var validated validatedConfig
	switch c.LogLevel {
	case "", "info":
		validated.logLevel = slog.LevelInfo
	case "debug":
		validated.logLevel = slog.LevelDebug
	case "warn":
		validated.logLevel = slog.LevelWarn
	case "error":
		validated.logLevel = slog.LevelError
	default:
		return validatedConfig{}, errors.New("config_log_level")
	}
	if c.LogFormat != "" && c.LogFormat != "json" {
		return validatedConfig{}, errors.New("config_log_format")
	}
	addresses := strings.Split(c.Listen, ",")
	if len(addresses) > 4 {
		return validatedConfig{}, errors.New("config_listeners")
	}
	seen := map[netip.AddrPort]bool{}
	for _, s := range addresses {
		a, err := netip.ParseAddrPort(s)
		if err != nil || a.Port() == 0 || a.Addr().Zone() != "" || a.Addr().Is4In6() || seen[a] {
			return validatedConfig{}, errors.New("config_listener")
		}
		seen[a] = true
		validated.udp = append(validated.udp, socketAddress{text: s, ipv6: a.Addr().Is6()})
	}
	a, err := netip.ParseAddrPort(c.HTTP)
	if err != nil || a.Port() == 0 || !a.Addr().IsLoopback() || a.Addr().Zone() != "" || a.Addr().Is4In6() {
		return validatedConfig{}, errors.New("config_http")
	}
	validated.http = socketAddress{text: c.HTTP, ipv6: a.Addr().Is6()}
	if c.Endpoint != "" {
		u, err := url.Parse(c.Endpoint)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			return validatedConfig{}, errors.New("config_exporter")
		}
	}
	return validated, nil
}
