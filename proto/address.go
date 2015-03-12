package proto

import (
	"crypto/tls"
	"flag"
	"fmt"
	"net"
)

type Address struct {
	host net.IP
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
	host := net.ParseIP(address)
	if host == nil {
		return nil, fmt.Errorf("invalid address: %s", address)
	}

	port := *portFlag
	if port < 0 || port > 0xFFFF {
		return nil, fmt.Errorf("invalid port value: %d", port)
	}

	return &Address{
		host: host,
		port: uint16(port),
		tls:  *useTLSFlag,
	}, nil
}

func (a *Address) String() string {
	proto := "tcp"
	if a.tls {
		proto = "tcp"
	}
	return fmt.Sprintf("%s://%s:%d", proto, a.host, a.port)
}

func (a *Address) Listen(config *tls.Config) (net.Listener, error) {
	loc := fmt.Sprintf("%s:%d", a.host, a.port)
	if a.tls {
		return tls.Listen("tcp", loc, config)
	}
	return net.Listen("tcp", loc)
}
