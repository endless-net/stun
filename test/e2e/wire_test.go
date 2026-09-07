//go:build e2e

// Product E2E intentionally does not import the product wire codec.
package e2e_test

import (
	"bytes"
	"crypto/rand"
	"encoding/binary"
	"net"
	"testing"
	"time"
)

func request() []byte {
	p := make([]byte, 20)
	p[1] = 1
	copy(p[4:8], []byte{0x21, 0x12, 0xa4, 0x42})
	if _, err := rand.Read(p[8:]); err != nil {
		panic(err)
	}
	return p
}
func withAttrs(p, a []byte) []byte {
	p = append(append([]byte(nil), p...), a...)
	binary.BigEndian.PutUint16(p[2:4], uint16(len(p)-20))
	return p
}
func socket(t *testing.T, addr string) net.Conn {
	t.Helper()
	c, err := net.Dial("udp", addr)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}
func exchange(t *testing.T, c net.Conn, p []byte, want bool) {
	t.Helper()
	_ = c.SetDeadline(time.Now().Add(time.Second))
	if _, err := c.Write(p); err != nil {
		t.Fatal(err)
	}
	if !want {
		_ = c.SetReadDeadline(time.Now().Add(80 * time.Millisecond))
	}
	r := make([]byte, 2048)
	n, err := c.Read(r)
	if !want {
		if e, ok := err.(net.Error); !ok || !e.Timeout() {
			t.Fatalf("expected dropped packet, n=%d err=%v", n, err)
		}
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	r = r[:n]
	if n < 32 || binary.BigEndian.Uint16(r[:2]) != 0x0101 || int(binary.BigEndian.Uint16(r[2:4])) != n-20 || !bytes.Equal(r[4:20], p[4:20]) {
		t.Fatal("invalid response header")
	}
	if binary.BigEndian.Uint16(r[20:22]) != 0x20 {
		t.Fatal("missing mapped attribute")
	}
	local := c.LocalAddr().(*net.UDPAddr)
	ip := local.IP.To4()
	family := byte(1)
	if ip == nil {
		ip = local.IP.To16()
		family = 2
	}
	if r[25] != family || len(r) != 28+len(ip) || int(binary.BigEndian.Uint16(r[22:24])) != 4+len(ip) {
		t.Fatal("invalid mapped family/length")
	}
	if int(binary.BigEndian.Uint16(r[26:28])^0x2112) != local.Port {
		t.Fatal("mapped port mismatch")
	}
	for i := range ip {
		if r[28+i]^p[4+i] != ip[i] {
			t.Fatal("mapped IP mismatch")
		}
	}
}
