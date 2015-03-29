package overlay

import (
	"crypto/tls"
	"net"

	"github.com/catalyzeio/shadowfax/cli"
	"github.com/catalyzeio/shadowfax/proto"
)

type L3Listener struct {
	subnet *IPv4Route
	routes *RouteTracker

	address *proto.Address
	config  *tls.Config
}

func NewL3Listener(subnet *IPv4Route, routes *RouteTracker, address *proto.Address, config *tls.Config) *L3Listener {
	return &L3Listener{
		subnet: subnet,
		routes: routes,

		address: address,
		config:  config,
	}
}

func (l *L3Listener) Start(cli *cli.Listener) error {
	listener, err := l.address.Listen(l.config)
	if err != nil {
		return err
	}
	go l.accept(listener)
	return nil
}

func (l *L3Listener) accept(listener net.Listener) {
	defer listener.Close()

	log.Info("Listening on %s", listener.Addr())
	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Warn("Failed to accept incoming connection: %s", err)
			return
		}
		go l.service(conn)
	}
}

func (l *L3Listener) service(conn net.Conn) {
	defer conn.Close()

	// TODO
}
