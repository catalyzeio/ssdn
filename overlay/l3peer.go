package overlay

import (
	"bufio"
	"crypto/tls"
	"fmt"
	"net"

	"github.com/catalyzeio/shadowfax/proto"
)

type L3Peer struct {
	Out PacketQueue

	free   PacketQueue
	client *proto.ReconnectClient
}

func NewL3Peer(addr *proto.Address, config *tls.Config, mtu uint16) (*L3Peer, error) {
	if !addr.TLS() {
		config = nil
	} else if config == nil {
		return nil, fmt.Errorf("peer %s requires TLS configuration", addr)
	}

	const peerQueueSize = 1024
	free := AllocatePacketQueue(peerQueueSize, int(mtu))
	out := make(PacketQueue, peerQueueSize)

	p := L3Peer{
		Out: out,

		free: free,
	}
	p.client = proto.NewClient(p.connHandler, addr.Host(), addr.Port(), config)
	return &p, nil
}

func (p *L3Peer) Start() {
	p.client.Start()
}

func (p *L3Peer) Stop() {
	p.client.Stop()
}

func (p *L3Peer) connHandler(conn net.Conn, abort <-chan bool) error {
	r, w, err := L3Handshake(conn)
	if err != nil {
		return err
	}
	// TODO
	_ = r
	_ = w
	return nil
}

func L3Handshake(peer net.Conn) (*bufio.Reader, *bufio.Writer, error) {
	r, w, err := Handshake(peer, "SFL3 1.0")
	// TODO exchange subnets
	return r, w, err
}
