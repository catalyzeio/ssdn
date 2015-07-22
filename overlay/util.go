package overlay

import (
	"crypto/rand"
	"net"
)

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
