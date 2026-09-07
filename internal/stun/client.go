package stun

import (
	"context"
	"fmt"
	"net"
	"time"
)

func Query(ctx context.Context, serverAddr string) (*net.UDPAddr, error) {
	dialer := net.Dialer{}
	conn, err := dialer.DialContext(ctx, "udp", serverAddr)
	if err != nil {
		return nil, fmt.Errorf("dial STUN endpoint: %w", err)
	}
	defer conn.Close()
	// DialContext only covers dialing. Close the socket to interrupt an
	// in-flight read or write when the caller cancels the query.
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	return query(ctx, conn)
}

func query(ctx context.Context, conn net.Conn) (*net.UDPAddr, error) {
	deadline := time.Now().Add(2 * time.Second)
	if contextDeadline, ok := ctx.Deadline(); ok {
		deadline = contextDeadline
	}
	if err := conn.SetDeadline(deadline); err != nil {
		return nil, queryError(ctx, "set STUN deadline", err)
	}
	request, txID, err := BuildBindingRequest()
	if err != nil {
		return nil, err
	}
	if _, err := conn.Write(request); err != nil {
		return nil, queryError(ctx, "write STUN Binding request", err)
	}
	response := make([]byte, MaxDatagramSize+1)
	n, err := conn.Read(response)
	if err != nil {
		return nil, queryError(ctx, "read STUN Binding response", err)
	}
	if n > MaxDatagramSize {
		return nil, fmt.Errorf("STUN response exceeds %d bytes", MaxDatagramSize)
	}
	return ParseBindingResponse(response[:n], txID)
}

func queryError(ctx context.Context, operation string, err error) error {
	if ctx.Err() != nil {
		err = ctx.Err()
	}
	return fmt.Errorf("%s: %w", operation, err)
}
