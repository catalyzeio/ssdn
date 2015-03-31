package proto

import (
	"crypto/tls"
	"flag"
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
)

type Address struct {
	ip       net.IP
	publicIP net.IP
	port     uint16
	tls      bool
}

var listenFlag *bool
var addressFlag *string
var publicAddressFlag *string
var portFlag *int

func AddListenFlags(defaultValue bool) {
	listenFlag = flag.Bool("listen", defaultValue, "whether to listen for incoming connections")
	addressFlag = flag.String("address", "0.0.0.0", "listen address")
	publicAddressFlag = flag.String("public", "", "public address to advertise")
	portFlag = flag.Int("port", 0, "listen port")
}

func GetListenAddress() (*Address, error) {
	if !*listenFlag {
		return nil, nil
	}

	address := *addressFlag
	if len(address) == 0 {
		address = "0.0.0.0"
	}
	ip := net.ParseIP(address)
	if ip == nil {
		return nil, fmt.Errorf("invalid address: %s", address)
	}

	port := *portFlag
	if port < 0 || port > 0xFFFF {
		return nil, fmt.Errorf("invalid port value: %d", port)
	}

	publicAddress := *publicAddressFlag
	if len(publicAddress) == 0 {
		publicAddress = address
	}
	publicIP := net.ParseIP(publicAddress)
	if publicIP == nil {
		return nil, fmt.Errorf("invalid public address: %s", address)
	}
	if publicIP.IsUnspecified() {
		guessedIP, err := guessPublicIP()
		if err != nil {
			return nil, err
		}
		publicIP = guessedIP
	}

	return &Address{
		ip:       ip,
		publicIP: publicIP,
		port:     uint16(port),
		tls:      *useTLSFlag,
	}, nil
}

func guessPublicIP() (net.IP, error) {
	hostname, err := os.Hostname()
	if err != nil {
		return nil, fmt.Errorf("could not determine hostname (%s); -public is required", err)
	}
	hostAddr, err := net.ResolveIPAddr("ip4", hostname)
	if err != nil {
		return nil, fmt.Errorf("could not resolve hostname (%s); -public is required", err)
	}
	hostIP := hostAddr.IP
	if hostIP.IsLoopback() {
		return nil, fmt.Errorf("could not detect public address; -public is required")
	}
	log.Info("Derived public IP: %s", hostIP)
	return hostIP, nil
}

func ParseAddress(addressURL string) (*Address, error) {
	u, err := url.Parse(addressURL)
	if err != nil {
		return nil, err
	}

	scheme := u.Scheme
	tls := false
	if scheme == "tcps" {
		tls = true
	} else if scheme != "tcp" {
		return nil, fmt.Errorf("unsupported scheme: %s", scheme)
	}

	host, portStr, err := net.SplitHostPort(u.Host)
	if err != nil {
		return nil, err
	}

	ip := net.ParseIP(host)
	if ip == nil {
		return nil, fmt.Errorf("invalid address: %s", host)
	}

	port, err := strconv.Atoi(portStr)
	if err != nil {
		return nil, err
	}
	if port < 0 || port > 0xFFFF {
		return nil, fmt.Errorf("invalid port value: %d", port)
	}

	return &Address{
		ip:   ip,
		port: uint16(port),
		tls:  tls,
	}, nil
}

func (a *Address) Listen(config *tls.Config) (net.Listener, error) {
	loc := net.JoinHostPort(a.Host(), strconv.Itoa(a.Port()))
	if a.tls {
		return tls.Listen("tcp", loc, config)
	}
	return net.Listen("tcp", loc)
}

func (a *Address) Host() string {
	return a.ip.String()
}

func (a *Address) Port() int {
	return int(a.port)
}

func (a *Address) TLS() bool {
	return a.tls
}

func (a *Address) PublicString() string {
	return a.urlString(a.publicIP)
}

func (a *Address) String() string {
	return a.urlString(a.ip)
}

func (a *Address) urlString(ip net.IP) string {
	proto := "tcp"
	if a.tls {
		proto = "tcp"
	}
	return fmt.Sprintf("%s://%s:%d", proto, ip, a.port)
}
