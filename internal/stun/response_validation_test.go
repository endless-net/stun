package stun

import (
	"encoding/binary"
	"net"
	"testing"
)

func TestResponseValidatesAllAttributes(t *testing.T) {
	req, tx, err := BuildBindingRequest()
	if err != nil {
		t.Fatal(err)
	}
	response, err := BuildBindingResponse(req, &net.UDPAddr{IP: net.ParseIP("192.0.2.1"), Port: 1234})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		attr []byte
		good bool
	}{
		{"optional", []byte{0x80, 0x22, 0, 0}, true},
		{"required", []byte{0, 0x0f, 0, 0}, false},
		{"truncated", []byte{0x80, 0x22, 0, 8}, false},
		{"duplicate", response[20:], true},
		{"invalid_duplicate", []byte{0, 0x20, 0, 0}, false},
	} {
		for _, before := range []bool{true, false} {
			t.Run(tc.name+map[bool]string{true: "_before", false: "_after"}[before], func(t *testing.T) {
				p := append([]byte(nil), response[:20]...)
				if before {
					p = append(p, tc.attr...)
					p = append(p, response[20:]...)
				} else {
					p = append(p, response[20:]...)
					p = append(p, tc.attr...)
				}
				binary.BigEndian.PutUint16(p[2:4], uint16(len(p)-20))
				mapped, err := ParseBindingResponse(p, tx)
				if (err == nil) != tc.good {
					t.Fatalf("accepted=%v want=%v err=%v", err == nil, tc.good, err)
				}
				if tc.good && mapped.Port != 1234 {
					t.Fatal("wrong mapping")
				}
			})
		}
	}
	second, _ := BuildBindingResponse(req, &net.UDPAddr{IP: net.ParseIP("192.0.2.2"), Port: 4321})
	p := append(append([]byte(nil), response...), second[20:]...)
	binary.BigEndian.PutUint16(p[2:4], uint16(len(p)-20))
	mapped, err := ParseBindingResponse(p, tx)
	if err != nil || mapped.Port != 1234 {
		t.Fatalf("first mapping not preserved: %v %v", mapped, err)
	}
}
