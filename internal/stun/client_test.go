package stun

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"
)

func TestQueryCancellationInterruptsResponseWait(t *testing.T) {
	server, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := Query(ctx, server.LocalAddr().String()); done <- err }()
	// Cancel only after the request arrived, so this exercises the read rather
	// than DialContext's existing cancellation behavior.
	if err := server.SetReadDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, _, err := server.ReadFrom(make([]byte, MaxDatagramSize)); err != nil {
		t.Fatal(err)
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("got %v, want context cancellation", err)
		}
	case <-time.After(time.Second):
		t.Fatal("query remained blocked after cancellation")
	}
}

func TestQueryDeadline(t *testing.T) {
	server, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err = Query(ctx, server.LocalAddr().String())
	var timeout net.Error
	if !errors.Is(err, context.DeadlineExceeded) && !(errors.As(err, &timeout) && timeout.Timeout()) {
		t.Fatalf("got %v, want deadline or socket timeout", err)
	}
}
