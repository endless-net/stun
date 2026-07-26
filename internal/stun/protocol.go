package stun

import (
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
)

const (
	HeaderLength       = 20
	TransactionIDSize  = 12
	MaxDatagramSize    = 1500
	bindingRequestType = 0x0001
	bindingSuccessType = 0x0101
	xorMappedAddress   = 0x0020
	magicCookie        = 0x2112A442
)

type TransactionID [TransactionIDSize]byte

func BuildBindingRequest() ([]byte, TransactionID, error) {
	var txID TransactionID
	if _, err := io.ReadFull(rand.Reader, txID[:]); err != nil {
		return nil, txID, fmt.Errorf("generate transaction ID: %w", err)
	}
	request := make([]byte, HeaderLength)
	binary.BigEndian.PutUint16(request[0:2], bindingRequestType)
	binary.BigEndian.PutUint32(request[4:8], magicCookie)
	copy(request[8:20], txID[:])
	return request, txID, nil
}

func BuildBindingResponse(request []byte, remote *net.UDPAddr) ([]byte, error) {
	txID, err := parseBindingRequest(request)
	if err != nil {
		return nil, err
	}
	if remote == nil || remote.IP == nil || remote.Port < 0 || remote.Port > 65535 {
		return nil, errors.New("remote UDP address is invalid")
	}
	attribute, err := buildXORMappedAddress(remote, txID)
	if err != nil {
		return nil, err
	}
	response := make([]byte, HeaderLength+len(attribute))
	binary.BigEndian.PutUint16(response[0:2], bindingSuccessType)
	binary.BigEndian.PutUint16(response[2:4], uint16(len(attribute)))
	binary.BigEndian.PutUint32(response[4:8], magicCookie)
	copy(response[8:20], txID[:])
	copy(response[20:], attribute)
	return response, nil
}

func ParseBindingResponse(response []byte, expected TransactionID) (*net.UDPAddr, error) {
	if len(response) < HeaderLength {
		return nil, errors.New("STUN response is shorter than the header")
	}
	if got := binary.BigEndian.Uint16(response[0:2]); got != bindingSuccessType {
		return nil, fmt.Errorf("STUN message type 0x%04x is not Binding success", got)
	}
	length := int(binary.BigEndian.Uint16(response[2:4]))
	if length%4 != 0 || HeaderLength+length != len(response) {
		return nil, errors.New("STUN response length does not match the datagram")
	}
	if binary.BigEndian.Uint32(response[4:8]) != magicCookie {
		return nil, errors.New("STUN response magic cookie mismatch")
	}
	if TransactionID(response[8:20]) != expected {
		return nil, errors.New("STUN response transaction ID mismatch")
	}
	attributes := response[HeaderLength:]
	for len(attributes) >= 4 {
		attributeType := binary.BigEndian.Uint16(attributes[0:2])
		attributeLength := int(binary.BigEndian.Uint16(attributes[2:4]))
		next := 4 + paddedLength(attributeLength)
		if next > len(attributes) || 4+attributeLength > len(attributes) {
			return nil, errors.New("STUN attribute length exceeds the datagram")
		}
		if attributeType == xorMappedAddress {
			return parseXORMappedAddress(attributes[4:4+attributeLength], expected)
		}
		attributes = attributes[next:]
	}
	return nil, errors.New("STUN response is missing XOR-MAPPED-ADDRESS")
}

func parseBindingRequest(request []byte) (TransactionID, error) {
	var txID TransactionID
	if len(request) < HeaderLength {
		return txID, errors.New("STUN request is shorter than the header")
	}
	if len(request) > MaxDatagramSize {
		return txID, errors.New("STUN request exceeds the maximum datagram size")
	}
	if request[0]&0xc0 != 0 {
		return txID, errors.New("STUN request has invalid leading message bits")
	}
	if got := binary.BigEndian.Uint16(request[0:2]); got != bindingRequestType {
		return txID, fmt.Errorf("STUN message type 0x%04x is unsupported", got)
	}
	length := int(binary.BigEndian.Uint16(request[2:4]))
	if length%4 != 0 || HeaderLength+length != len(request) {
		return txID, errors.New("STUN request length does not match the datagram")
	}
	if binary.BigEndian.Uint32(request[4:8]) != magicCookie {
		return txID, errors.New("STUN request magic cookie mismatch")
	}
	if err := validateRequestAttributes(request); err != nil {
		return txID, err
	}
	copy(txID[:], request[8:20])
	return txID, nil
}

func validateRequestAttributes(request []byte) error {
	attributes := request[HeaderLength:]
	for len(attributes) > 0 {
		if len(attributes) < 4 {
			return errors.New("STUN request has a truncated attribute header")
		}
		attributeType := binary.BigEndian.Uint16(attributes[0:2])
		attributeLength := int(binary.BigEndian.Uint16(attributes[2:4]))
		next := 4 + paddedLength(attributeLength)
		if next > len(attributes) || 4+attributeLength > len(attributes) {
			return errors.New("STUN request attribute length exceeds the datagram")
		}
		// Comprehension-optional attributes are safe to ignore for an unauthenticated
		// Binding request. Unknown comprehension-required attributes need an RFC 8489
		// 420 response, which this deliberately minimal service does not implement.
		if attributeType < 0x8000 {
			return fmt.Errorf("STUN request attribute 0x%04x is unsupported", attributeType)
		}
		attributes = attributes[next:]
	}
	return nil
}

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

func paddedLength(length int) int {
	return (length + 3) &^ 3
}
