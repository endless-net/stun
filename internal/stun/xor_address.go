package stun

import (
	"encoding/binary"
	"errors"
	"fmt"
	"net"
)

func buildXORMappedAddress(addr *net.UDPAddr, txID TransactionID) ([]byte, error) {
	if ip := addr.IP.To4(); ip != nil {
		attribute := make([]byte, 12)
		binary.BigEndian.PutUint16(attribute[0:2], xorMappedAddress)
		binary.BigEndian.PutUint16(attribute[2:4], 8)
		attribute[5] = 0x01
		binary.BigEndian.PutUint16(attribute[6:8], uint16(addr.Port)^uint16(magicCookie>>16))
		cookie := make([]byte, 4)
		binary.BigEndian.PutUint32(cookie, magicCookie)
		for i := range ip {
			attribute[8+i] = ip[i] ^ cookie[i]
		}
		return attribute, nil
	}
	ip := addr.IP.To16()
	if ip == nil {
		return nil, errors.New("remote IP address is invalid")
	}
	attribute := make([]byte, 24)
	binary.BigEndian.PutUint16(attribute[0:2], xorMappedAddress)
	binary.BigEndian.PutUint16(attribute[2:4], 20)
	attribute[5] = 0x02
	binary.BigEndian.PutUint16(attribute[6:8], uint16(addr.Port)^uint16(magicCookie>>16))
	mask := make([]byte, 16)
	binary.BigEndian.PutUint32(mask[0:4], magicCookie)
	copy(mask[4:], txID[:])
	for i := range ip {
		attribute[8+i] = ip[i] ^ mask[i]
	}
	return attribute, nil
}

func parseXORMappedAddress(value []byte, txID TransactionID) (*net.UDPAddr, error) {
	if len(value) < 4 {
		return nil, errors.New("XOR-MAPPED-ADDRESS is too short")
	}
	port := int(binary.BigEndian.Uint16(value[2:4]) ^ uint16(magicCookie>>16))
	switch value[1] {
	case 0x01:
		if len(value) != 8 {
			return nil, errors.New("IPv4 XOR-MAPPED-ADDRESS has invalid length")
		}
		cookie := make([]byte, 4)
		binary.BigEndian.PutUint32(cookie, magicCookie)
		ip := make(net.IP, net.IPv4len)
		for i := range ip {
			ip[i] = value[4+i] ^ cookie[i]
		}
		return &net.UDPAddr{IP: ip, Port: port}, nil
	case 0x02:
		if len(value) != 20 {
			return nil, errors.New("IPv6 XOR-MAPPED-ADDRESS has invalid length")
		}
		mask := make([]byte, net.IPv6len)
		binary.BigEndian.PutUint32(mask[0:4], magicCookie)
		copy(mask[4:], txID[:])
		ip := make(net.IP, net.IPv6len)
		for i := range ip {
			ip[i] = value[4+i] ^ mask[i]
		}
		return &net.UDPAddr{IP: ip, Port: port}, nil
	default:
		return nil, fmt.Errorf("XOR-MAPPED-ADDRESS family 0x%02x is unsupported", value[1])
	}
}
