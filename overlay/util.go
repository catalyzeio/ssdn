package overlay

import (
	"crypto/rand"
	"net"
)

// This method requires a 4-byte IP address to function properly.
// Use ip.To4() if the IPv4 address may have been encoded with 16 bytes.
func IPv4ToInt(ip []byte) uint32 {
	return uint32(ip[0])<<24 | uint32(ip[1])<<16 | uint32(ip[2])<<8 | uint32(ip[3])
}

// Reverse operation of IPv4ToInt.
func IntToIPv4(ip uint32) []byte {
	return []byte{byte(ip >> 24), byte(ip >> 16), byte(ip >> 8), byte(ip)}
}

func FormatIPWithMask(ip uint32, mask net.IPMask) string {
	net := net.IPNet{
		IP:   net.IP(IntToIPv4(ip)),
		Mask: mask,
	}
	return net.String()
}

func RandomMAC() (net.HardwareAddr, error) {
	address := make([]byte, 6)
	if _, err := rand.Read(address); err != nil {
		return nil, err
	}

	// clear multicast and set local assignment bits
	address[0] &= 0xFE
	address[0] |= 0x02
	return net.HardwareAddr(address), nil
}

func GetInterfaces() (map[string]struct{}, error) {
	interfaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}

	result := make(map[string]struct{})
	for _, v := range interfaces {
		result[v.Name] = struct{}{}
	}
	return result, nil
}
