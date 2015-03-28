package overlay

import (
	"crypto/tls"
	"fmt"
	"net"

	"github.com/catalyzeio/shadowfax/proto"
)

type L2Peer struct {
	client *proto.ReconnectClient
	tap    *L2Tap
}

func NewL2Peer(addr *proto.Address, config *tls.Config) (*L2Peer, error) {
	if !addr.TLS() {
		config = nil
	} else if config == nil {
		return nil, fmt.Errorf("peer %s requires TLS configuration", addr)
	}

	p := L2Peer{}
	p.client = proto.NewClient(p.connHandler, addr.Host(), addr.Port(), config)
	return &p, nil
}

func (p *L2Peer) Start(tap *L2Tap) {
	p.tap = tap
	p.client.Start()
}

func (p *L2Peer) Stop() {
	p.client.Stop()
	p.tap.Close()
}

func (p *L2Peer) Name() string {
	return p.tap.Name()
}

func (p *L2Peer) connHandler(conn net.Conn, abort <-chan bool) error {
	r, w, err := L2Handshake(conn)
	if err != nil {
		return err
	}
	p.tap.Forward(r, w)
	return nil
}
