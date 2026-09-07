package stun

import "net"

// ListenPacket binds an explicit IP address only in its own address family.
// This allows IPv4 and IPv6 wildcard listeners to share a port. An empty
// host leaves the address family selection to the operating system and Go.
func ListenPacket(addr string) (*net.UDPConn, error) {
	resolved, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		return nil, err
	}
	network := "udp"
	if resolved.IP.To4() != nil {
		network = "udp4"
	} else if resolved.IP != nil {
		network = "udp6"
	}
	return net.ListenUDP(network, resolved)
}
