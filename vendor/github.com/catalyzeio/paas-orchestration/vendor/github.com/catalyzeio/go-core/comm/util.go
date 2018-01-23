package comm

import (
	"fmt"
	"net"
)

// This method requires a 4-byte IP address to function properly.
// Use ip.To4() if the IPv4 address may have been encoded with 16 bytes.
func IPv4ToInt(ip []byte) uint32 {
	return uint32(ip[0])<<24 | uint32(ip[1])<<16 | uint32(ip[2])<<8 | uint32(ip[3])
}

// Convenience wrapper for net.IP; see note above about address format restrictions.
func IPToInt(ip net.IP) (uint32, error) {
	ipv4 := ip.To4()
	if ipv4 == nil {
		return 0, fmt.Errorf("address must be IPv4: %s", ip)
	}
	return IPv4ToInt(ipv4), nil
}

// Reverse operation of IPv4ToInt.
func IntToIPv4(ip uint32) []byte {
	return []byte{byte(ip >> 24), byte(ip >> 16), byte(ip >> 8), byte(ip)}
}

// Convenience wrapper for net.IP.
func IntToIP(ip uint32) net.IP {
	return net.IP(IntToIPv4(ip))
}

// Translates the network and netmask using IPv4ToInt.
func NetworkToInt(network *net.IPNet) (uint32, uint32, error) {
	ip := network.IP.To4()
	if ip == nil {
		return 0, 0, fmt.Errorf("network must be IPv4")
	}

	_, bits := network.Mask.Size()
	if bits != 32 {
		return 0, 0, fmt.Errorf("netmask must be canonical IPv4")
	}

	return IPv4ToInt(ip), IPv4ToInt(network.Mask), nil
}

func FormatIPWithMask(ip uint32, mask net.IPMask) string {
	net := net.IPNet{
		IP:   net.IP(IntToIPv4(ip)),
		Mask: mask,
	}
	return net.String()
}
