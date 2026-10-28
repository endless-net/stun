// Package server implements the bounded standalone UDP Binding service.
package server

import (
	"encoding/binary"
	"net/netip"
)

func binding(b []byte, source netip.AddrPort) ([]byte, string) {
	if len(b) < 20 || len(b) > 1500 {
		return nil, "invalid"
	}
	if binary.BigEndian.Uint16(b) != 1 || binary.BigEndian.Uint32(b[4:]) != 0x2112a442 || int(binary.BigEndian.Uint16(b[2:])) != len(b)-20 || len(b)%4 != 0 {
		return nil, "invalid"
	}
	for off := 20; off < len(b); {
		if off+4 > len(b) {
			return nil, "invalid"
		}
		kind := binary.BigEndian.Uint16(b[off:])
		n := int(binary.BigEndian.Uint16(b[off+2:]))
		off += 4 + (n+3)/4*4
		if kind < 0x8000 || off > len(b) {
			return nil, "invalid"
		}
	}
	ip := source.Addr().Unmap()
	size, family := 32, byte(1)
	if ip.Is6() {
		size, family = 44, 2
	}
	out := make([]byte, size)
	binary.BigEndian.PutUint16(out, 0x0101)
	binary.BigEndian.PutUint16(out[2:], uint16(size-20))
	copy(out[4:20], b[4:20])
	binary.BigEndian.PutUint16(out[20:], 0x0020)
	binary.BigEndian.PutUint16(out[22:], uint16(size-24))
	out[25] = family
	binary.BigEndian.PutUint16(out[26:], source.Port()^0x2112)
	mask := b[4:20]
	for i, v := range ip.AsSlice() {
		out[28+i] = v ^ mask[i]
	}
	return out, "success"
}
