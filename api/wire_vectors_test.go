package api_test

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"net/netip"
	"os"
	"testing"
)

func TestWireVectors(t *testing.T) {
	var profile struct {
		MaxDatagramBytes int `json:"max_datagram_bytes"`
		Vectors          []struct {
			ID       string
			Request  string     `json:"request_hex"`
			Padding  int        `json:"append_zero_bytes"`
			Address  netip.Addr `json:"source_address"`
			Port     uint16     `json:"source_port"`
			Outcome  string
			Response *string `json:"response_hex"`
		}
	}
	data, err := os.ReadFile("vectors.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &profile); err != nil {
		t.Fatal(err)
	}
	if profile.MaxDatagramBytes != 1500 || len(profile.Vectors) == 0 {
		t.Fatal("missing vectors or unexpected datagram limit")
	}
	for _, vector := range profile.Vectors {
		t.Run(vector.ID, func(t *testing.T) {
			request, err := hex.DecodeString(vector.Request)
			if err != nil {
				t.Fatal(err)
			}
			if vector.Padding < 0 || vector.Padding > profile.MaxDatagramBytes {
				t.Fatal("invalid vector padding")
			}
			request = append(request, make([]byte, vector.Padding)...)
			accepted := validBindingRequest(request, profile.MaxDatagramBytes)
			switch vector.Outcome {
			case "drop":
				if accepted || vector.Response != nil {
					t.Fatal("drop vector has an accepted request or a response")
				}
			case "success":
				if !accepted || vector.Response == nil {
					t.Fatal("success vector needs a valid request and a response")
				}
				response, err := hex.DecodeString(*vector.Response)
				if err != nil {
					t.Fatal(err)
				}
				checkBindingResponse(t, request, response, vector.Address, vector.Port)
			default:
				t.Fatalf("unknown outcome %q", vector.Outcome)
			}
		})
	}
}

func validBindingRequest(request []byte, limit int) bool {
	if len(request) < 20 || len(request) > limit ||
		binary.BigEndian.Uint16(request) != 1 || binary.BigEndian.Uint32(request[4:]) != 0x2112a442 {
		return false
	}
	body := request[20:]
	if length := int(binary.BigEndian.Uint16(request[2:])); length != len(body) || length%4 != 0 {
		return false
	}
	for len(body) != 0 {
		kind := binary.BigEndian.Uint16(body)
		size := int(binary.BigEndian.Uint16(body[2:]))
		next := 4 + (size+3)/4*4
		if kind < 0x8000 || next > len(body) {
			return false
		}
		body = body[next:]
	}
	return true
}

func checkBindingResponse(t *testing.T, request, response []byte, address netip.Addr, port uint16) {
	t.Helper()
	ip := address.AsSlice()
	family := byte(1)
	if address.Is6() {
		family = 2
	}
	if !address.IsValid() || len(response) != 28+len(ip) ||
		binary.BigEndian.Uint16(response) != 0x0101 ||
		int(binary.BigEndian.Uint16(response[2:])) != len(response)-20 ||
		binary.BigEndian.Uint32(response[4:]) != 0x2112a442 ||
		!bytes.Equal(response[8:20], request[8:20]) {
		t.Fatalf("invalid response header: %x", response)
	}
	if binary.BigEndian.Uint16(response[20:]) != 0x0020 ||
		int(binary.BigEndian.Uint16(response[22:])) != len(response)-24 ||
		response[24] != 0 || response[25] != family ||
		binary.BigEndian.Uint16(response[26:])^0x2112 != port {
		t.Fatalf("invalid XOR-MAPPED-ADDRESS attribute: %x", response[20:])
	}
	for i, octet := range ip {
		if response[28+i]^request[4+i] != octet {
			t.Fatalf("decoded address differs from %s", address)
		}
	}
}
