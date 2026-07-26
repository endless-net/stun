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
	deadline := time.Now().Add(2 * time.Second)
	if contextDeadline, ok := ctx.Deadline(); ok {
		deadline = contextDeadline
	}
	if err := conn.SetDeadline(deadline); err != nil {
		return nil, fmt.Errorf("set STUN deadline: %w", err)
	}
	request, txID, err := BuildBindingRequest()
	if err != nil {
		return nil, err
	}
	if _, err := conn.Write(request); err != nil {
		return nil, fmt.Errorf("write STUN Binding request: %w", err)
	}
	response := make([]byte, MaxDatagramSize+1)
	n, err := conn.Read(response)
	if err != nil {
		return nil, fmt.Errorf("read STUN Binding response: %w", err)
	}
	if n > MaxDatagramSize {
		return nil, fmt.Errorf("STUN response exceeds %d bytes", MaxDatagramSize)
	}
	return ParseBindingResponse(response[:n], txID)
}
