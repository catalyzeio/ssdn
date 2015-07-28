package overlay

import (
	"crypto/rand"
	"net"

	"github.com/catalyzeio/go-core/comm"
)

const (
	maxVethNameLength = 14
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

func RandomVethName(prefix string) (string, error) {
	chars, err := comm.GenerateChars(maxVethNameLength - len(prefix))
	if err != nil {
		return "", err
	}
	return prefix + chars, nil
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
