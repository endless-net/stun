package server

import (
	"testing"
)

func TestConfig(t *testing.T) {
	t.Setenv("ENDLESSNET_STUN_LISTEN", "127.0.0.1:3478")
	t.Setenv("ENDLESSNET_STUN_HTTP_LISTEN", "127.0.0.1:8080")
	t.Setenv("ENDLESSNET_STUN_OTEL_ENDPOINT", "")
	c, err := Parse([]string{"--listen", "[::1]:3478", "--check-config"})
	if err != nil || c.Listen != "[::1]:3478" || !c.Check {
		t.Fatal(c, err)
	}
	if _, err := Parse([]string{"--listen", "127.0.0.1:1,[::1]:1,127.0.0.2:1,[::2]:1"}); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"--listen", ""}, {"--listen", "localhost:3478"}, {"--listen", "127.0.0.1:0"},
		{"--listen", "127.0.0.1:1,127.0.0.1:1"}, {"--listen", "127.0.0.1:1,[::1]:1,127.0.0.2:1,[::2]:1,[::3]:1"},
		{"--http-listen", "0.0.0.0:8080"}, {"--http-listen", "localhost:8080"}, {"--listen", "[::ffff:127.0.0.1]:3478"},
		{"--otel-endpoint", "http://user:secret@example.org"}, {"--otel-endpoint", "ftp://example.org"}, {"--otel-endpoint", "http://example.org?secret=1"},
		{"--unknown"}, {"trailing"},
	} {
		if _, err := Parse(args); err == nil {
			t.Fatal("accepted", args)
		}
	}
	t.Setenv("ENDLESSNET_STUN_LISTEN", "")
	if _, err := Parse([]string{"--version"}); err != nil {
		t.Fatal(err)
	}
}
