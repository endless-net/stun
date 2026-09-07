package stun

import (
	"encoding/binary"
	"net"
	"testing"
)

func TestBindingRoundTripIPv4AndIPv6(t *testing.T) {
	tests := []struct {
		name   string
		remote *net.UDPAddr
	}{
		{name: "IPv4", remote: &net.UDPAddr{IP: net.ParseIP("203.0.113.7"), Port: 54321}},
		{name: "IPv6", remote: &net.UDPAddr{IP: net.ParseIP("2001:db8::7"), Port: 54321}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			request, txID, err := BuildBindingRequest()
			if err != nil {
				t.Fatal(err)
			}
			response, err := BuildBindingResponse(request, tc.remote)
			if err != nil {
				t.Fatal(err)
			}
			mapped, err := ParseBindingResponse(response, txID)
			if err != nil {
				t.Fatal(err)
			}
			if !mapped.IP.Equal(tc.remote.IP) || mapped.Port != tc.remote.Port {
				t.Fatalf("mapped address = %s, want %s", mapped, tc.remote)
			}
		})
	}
}

func TestBuildBindingResponseRejectsMalformedAndUnsupportedRequests(t *testing.T) {
	valid, _, err := BuildBindingRequest()
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name    string
		request func() []byte
	}{
		{name: "short", request: func() []byte { return []byte("short") }},
		{name: "unsupported type", request: func() []byte {
			p := append([]byte(nil), valid...)
			binary.BigEndian.PutUint16(p[0:2], 0x0003)
			return p
		}},
		{name: "bad length", request: func() []byte { p := append([]byte(nil), valid...); binary.BigEndian.PutUint16(p[2:4], 4); return p }},
		{name: "bad cookie", request: func() []byte { p := append([]byte(nil), valid...); binary.BigEndian.PutUint32(p[4:8], 1); return p }},
		{name: "truncated attribute", request: func() []byte {
			p := append(append([]byte(nil), valid...), 0x80, 0x22, 0, 4)
			binary.BigEndian.PutUint16(p[2:4], 4)
			return p
		}},
		{name: "unsupported required attribute", request: func() []byte {
			p := append(append([]byte(nil), valid...), 0, 3, 0, 0)
			binary.BigEndian.PutUint16(p[2:4], 4)
			return p
		}},
		{name: "oversized", request: func() []byte { return make([]byte, MaxDatagramSize+1) }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := BuildBindingResponse(tc.request(), &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1234}); err == nil {
				t.Fatal("malformed request was accepted")
			}
		})
	}
}

func TestParseBindingResponseRejectsTransactionMismatch(t *testing.T) {
	request, txID, err := BuildBindingRequest()
	if err != nil {
		t.Fatal(err)
	}
	response, err := BuildBindingResponse(request, &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1234})
	if err != nil {
		t.Fatal(err)
	}
	txID[0] ^= 0xff
	if _, err := ParseBindingResponse(response, txID); err == nil {
		t.Fatal("transaction mismatch was accepted")
	}
}

func FuzzBuildBindingResponseDoesNotPanic(f *testing.F) {
	valid, _, err := BuildBindingRequest()
	if err != nil {
		f.Fatal(err)
	}
	f.Add(valid)
	f.Add([]byte("not-stun"))
	f.Fuzz(func(t *testing.T, packet []byte) {
		_, _ = BuildBindingResponse(packet, &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1234})
	})
}

func FuzzParseBindingResponse(f *testing.F) {
	request, tx, err := BuildBindingRequest()
	if err != nil {
		f.Fatal(err)
	}
	valid, err := BuildBindingResponse(request, &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1234})
	if err != nil {
		f.Fatal(err)
	}
	f.Add(valid)
	f.Add([]byte("not-stun"))
	f.Fuzz(func(t *testing.T, packet []byte) {
		mapped, err := ParseBindingResponse(packet, tx)
		if err == nil && (mapped == nil || mapped.IP.To16() == nil) {
			t.Fatal("accepted response without a valid address")
		}
	})
}

func TestBindingAttributesWithPadding(t *testing.T) {
	request, tx, err := BuildBindingRequest()
	if err != nil {
		t.Fatal(err)
	}
	remote := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1234}
	response, err := BuildBindingResponse(request, remote)
	if err != nil {
		t.Fatal(err)
	}
	for _, original := range [][]byte{request, response} {
		for length := 0; length < 8; length++ {
			attr := make([]byte, 4+((length+3)&^3))
			binary.BigEndian.PutUint16(attr[:2], 0x8022)
			binary.BigEndian.PutUint16(attr[2:4], uint16(length))
			packet := append(append([]byte(nil), original...), attr...)
			binary.BigEndian.PutUint16(packet[2:4], uint16(len(packet)-HeaderLength))
			if original[0] == 0 {
				_, err = BuildBindingResponse(packet, remote)
			} else {
				_, err = ParseBindingResponse(packet, tx)
			}
			if err != nil {
				t.Fatalf("valid padded attribute length %d: %v", length, err)
			}
		}
	}
}
