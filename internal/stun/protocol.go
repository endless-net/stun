package stun

import (
	"crypto/rand"
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
	return buildMessage(bindingRequestType, txID, nil), txID, nil
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
	return buildMessage(bindingSuccessType, txID, attribute), nil
}

func ParseBindingResponse(response []byte, expected TransactionID) (*net.UDPAddr, error) {
	tx, attributes, err := parseMessage(response, bindingSuccessType)
	if err != nil {
		return nil, err
	}
	if tx != expected {
		return nil, errors.New("STUN response transaction ID mismatch")
	}
	var mapped *net.UDPAddr
	err = walkAttributes(attributes, func(kind uint16, value []byte) error {
		if kind == xorMappedAddress {
			addr, err := parseXORMappedAddress(value, expected)
			if err != nil {
				return err
			}
			if mapped == nil {
				mapped = addr
			}
		} else if kind < 0x8000 {
			return fmt.Errorf("unsupported required STUN response attribute 0x%04x", kind)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if mapped == nil {
		return nil, errors.New("STUN response is missing XOR-MAPPED-ADDRESS")
	}
	return mapped, nil
}

func parseBindingRequest(request []byte) (TransactionID, error) {
	tx, attributes, err := parseMessage(request, bindingRequestType)
	if err != nil {
		return tx, err
	}
	err = walkAttributes(attributes, func(kind uint16, _ []byte) error {
		// This minimal unauthenticated service ignores optional attributes and
		// rejects required attributes instead of implementing a 420 response.
		if kind < 0x8000 {
			return fmt.Errorf("STUN request attribute 0x%04x is unsupported", kind)
		}
		return nil
	})
	return tx, err
}
