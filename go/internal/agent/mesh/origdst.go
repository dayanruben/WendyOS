package mesh

import (
	"encoding/binary"
	"net/netip"
)

// addrPortFromSockaddrIn decodes a raw struct sockaddr_in as returned by
// getsockopt(SO_ORIGINAL_DST): bytes [2:4] are the port in network order,
// [4:8] the IPv4 address.
func addrPortFromSockaddrIn(b []byte) netip.AddrPort {
	port := binary.BigEndian.Uint16(b[2:4])
	var ip [4]byte
	copy(ip[:], b[4:8])
	return netip.AddrPortFrom(netip.AddrFrom4(ip), port)
}
