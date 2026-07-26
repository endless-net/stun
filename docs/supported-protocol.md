# Supported STUN protocol

## Standards and transport

The service implements the unauthenticated Binding method from RFC 8489 over UDP and remains interoperable with the RFC 5389 Binding wire format. It uses the RFC magic cookie and 96-bit transaction ID and returns `XOR-MAPPED-ADDRESS`.

## Messages and attributes

Supported inbound message:

- Binding Request (`0x0001`)

Supported outbound message:

- Binding Success Response (`0x0101`)

Generated response attribute:

- `XOR-MAPPED-ADDRESS` (`0x0020`)

Syntactically valid comprehension-optional request attributes are ignored. Unknown comprehension-required attributes are rejected without a response because this minimal service does not implement the RFC 8489 `420 Unknown Attribute` error flow. Malformed headers, inconsistent lengths, unsupported message types, oversized datagrams, and invalid cookies are dropped without a response.

## Address-family status

- IPv4 listening and mapped-address encoding are supported and tested.
- IPv6 listening and XOR mapped-address encoding are implemented and unit tested. Host and network IPv6 availability remains an environment prerequisite.

## Limits

- Maximum accepted UDP datagram size: 1500 bytes.
- Only Binding discovery is supported.
- Long-term and short-term credentials, `MESSAGE-INTEGRITY`, alternate-server discovery, RFC 5780 NAT behavior discovery, TCP/TLS STUN transports, and stateful transactions are not supported.
- TURN allocation, permissions, channels, and relayed user traffic are explicitly not supported.
