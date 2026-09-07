//go:build e2e

package e2e_test

import (
	"net"
	"testing"
)

func TestProductDualStackWildcardBind(t *testing.T) {
	// Reserve a common port using separate address families, so an occupied
	// IPv6 port cannot be mistaken for the product's dual-stack bind conflict.
	v4, err := net.ListenPacket("udp4", "0.0.0.0:0")
	if err != nil {
		t.Fatal(err)
	}
	defer v4.Close()
	_, port, err := net.SplitHostPort(v4.LocalAddr().String())
	if err != nil {
		t.Fatal(err)
	}
	addrs := []string{net.JoinHostPort("0.0.0.0", port), net.JoinHostPort("::", port)}
	v6, err := net.ListenPacket("udp6", addrs[1])
	if err != nil {
		t.Fatalf("required IPv6 socket on shared port unavailable: %v", err)
	}
	if err := v6.Close(); err != nil {
		t.Fatal(err)
	}
	if err := v4.Close(); err != nil {
		t.Fatal(err)
	}

	// Match configs/stun.example.env, using an ephemeral port instead of 3478.
	start(t, addrs, 10000, 10000)
	for _, host := range []string{"127.0.0.1", "::1"} {
		t.Run(host, func(t *testing.T) {
			c := socket(t, net.JoinHostPort(host, port))
			exchange(t, c, request(), true)
		})
	}
}
