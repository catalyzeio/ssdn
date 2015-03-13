package proto

import (
	"crypto/tls"
	"flag"
	"fmt"
	"net"
	"net/url"
	"strconv"
)

type Address struct {
	ip   net.IP
	port uint16
	tls  bool
}

var listenFlag *bool
var addressFlag *string
var portFlag *int

func AddListenFlags(defaultValue bool) {
	listenFlag = flag.Bool("listen", defaultValue, "whether to listen for incoming connections")
	addressFlag = flag.String("address", "0.0.0.0", "listen address")
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

	return &Address{
		ip:   ip,
		port: uint16(port),
		tls:  *useTLSFlag,
	}, nil
}

// TODO get public address

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

func (a *Address) String() string {
	proto := "tcp"
	if a.tls {
		proto = "tcp"
	}
	return fmt.Sprintf("%s://%s:%d", proto, a.ip, a.port)
}
