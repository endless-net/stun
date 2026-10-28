package server

import (
	"encoding/binary"
	"net/netip"
	"testing"
	"time"
)

func request() []byte {
	b := make([]byte, 20)
	binary.BigEndian.PutUint16(b, 1)
	binary.BigEndian.PutUint32(b[4:], 0x2112a442)
	copy(b[8:], "abcdefghijkl")
	return b
}

func TestWire(t *testing.T) {
	for _, source := range []string{"192.0.2.1:40000", "[2001:db8::1]:54321"} {
		addr := netip.MustParseAddrPort(source)
		b, code := binding(request(), addr)
		want := 32
		if addr.Addr().Is6() {
			want = 44
		}
		if code != "success" || len(b) != want {
			t.Fatalf("%s: %s %x", source, code, b)
		}
		if string(b[8:20]) != "abcdefghijkl" || binary.BigEndian.Uint16(b[26:28])^0x2112 != addr.Port() {
			t.Fatal("transaction/port")
		}
		mask := append([]byte{0x21, 0x12, 0xa4, 0x42}, request()[8:]...)
		ip := addr.Addr().AsSlice()
		for i, v := range ip {
			if b[28+i]^mask[i] != v {
				t.Fatal("address")
			}
		}
	}
	good := request()
	optional := append(request(), 0x80, 1, 0, 1, 42, 9, 8, 7)
	binary.BigEndian.PutUint16(optional[2:], 8)
	if _, code := binding(optional, netip.MustParseAddrPort("127.0.0.1:1")); code != "success" {
		t.Fatal(code)
	}
	large := append(request(), make([]byte, 1480)...)
	large[20] = 0x80
	binary.BigEndian.PutUint16(large[22:], 1476)
	binary.BigEndian.PutUint16(large[2:], 1480)
	cases := [][]byte{good[:19], append(request(), 0), append(request(), make([]byte, 1481)...)}
	for _, pos := range []int{0, 2, 4} {
		b := request()
		b[pos] ^= 1
		cases = append(cases, b)
	}
	required := append(request(), 0, 1, 0, 0)
	binary.BigEndian.PutUint16(required[2:], 4)
	cases = append(cases, required)
	broken := append(request(), 0x80, 1, 0, 8)
	binary.BigEndian.PutUint16(broken[2:], 4)
	cases = append(cases, broken)
	for _, b := range cases {
		if out, code := binding(b, netip.MustParseAddrPort("127.0.0.1:1")); out != nil || code == "success" {
			t.Fatalf("accepted %x", b)
		}
	}
	if _, code := binding(large, netip.MustParseAddrPort("127.0.0.1:1")); code != "success" {
		t.Fatal("1500 bound", code)
	}
}

func TestLimiter(t *testing.T) {
	now := time.Unix(1, 0)
	l := newLimiter()
	a := netip.MustParseAddr("192.0.2.1")
	for range 40 {
		if !l.allow(a, now) {
			t.Fatal("burst")
		}
	}
	if l.allow(a, now) || !l.allow(a, now.Add(50*time.Millisecond)) {
		t.Fatal("refill")
	}
	other := newLimiter()
	if !other.allow(a, now) {
		t.Fatal("listener independence")
	}
	for i := 1; i < 65536; i++ {
		ip := netip.AddrFrom4([4]byte{10, byte(i >> 16), byte(i >> 8), byte(i)})
		if !l.allow(ip, now) {
			t.Fatal(i)
		}
	}
	extra := netip.MustParseAddr("203.0.113.2")
	if l.allow(extra, now) || len(l.sources) != 65536 {
		t.Fatal("cap")
	}
	if !l.allow(a, now.Add(time.Second)) {
		t.Fatal("existing source")
	}
	if !l.allow(extra, now.Add(601*time.Second)) || len(l.sources) != 1 {
		t.Fatal("sweep", len(l.sources))
	}
}
