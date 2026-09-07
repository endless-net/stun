package stun

import (
	"encoding/binary"
	"errors"
	"fmt"
)

// parseMessage checks framing before exposing any attribute bytes to callers.
func parseMessage(packet []byte, messageType uint16) (TransactionID, []byte, error) {
	var tx TransactionID
	if len(packet) < HeaderLength || len(packet) > MaxDatagramSize {
		return tx, nil, errors.New("STUN datagram size is outside the supported bounds")
	}
	if got := binary.BigEndian.Uint16(packet[:2]); got != messageType {
		return tx, nil, fmt.Errorf("unsupported STUN message type 0x%04x", got)
	}
	length := int(binary.BigEndian.Uint16(packet[2:4]))
	if length%4 != 0 || HeaderLength+length != len(packet) {
		return tx, nil, errors.New("STUN message length does not match the datagram")
	}
	if binary.BigEndian.Uint32(packet[4:8]) != magicCookie {
		return tx, nil, errors.New("STUN magic cookie mismatch")
	}
	copy(tx[:], packet[8:HeaderLength])
	return tx, packet[HeaderLength:], nil
}

func buildMessage(messageType uint16, tx TransactionID, attributes []byte) []byte {
	packet := make([]byte, HeaderLength+len(attributes))
	binary.BigEndian.PutUint16(packet[:2], messageType)
	binary.BigEndian.PutUint16(packet[2:4], uint16(len(attributes)))
	binary.BigEndian.PutUint32(packet[4:8], magicCookie)
	copy(packet[8:HeaderLength], tx[:])
	copy(packet[HeaderLength:], attributes)
	return packet
}

// walkAttributes validates every attribute, including padding and trailing data.
// Request and response policies remain with their respective callers.
func walkAttributes(attributes []byte, visit func(uint16, []byte) error) error {
	for len(attributes) > 0 {
		if len(attributes) < 4 {
			return errors.New("STUN attribute header is truncated")
		}
		kind := binary.BigEndian.Uint16(attributes[:2])
		length := int(binary.BigEndian.Uint16(attributes[2:4]))
		next := 4 + ((length + 3) &^ 3)
		if next > len(attributes) {
			return errors.New("STUN attribute length exceeds the datagram")
		}
		if err := visit(kind, attributes[4:4+length]); err != nil {
			return err
		}
		attributes = attributes[next:]
	}
	return nil
}
