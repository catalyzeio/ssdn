package overlay

import (
	"bufio"
	"crypto/tls"
	"fmt"
	"net"

	"github.com/catalyzeio/shadowfax/proto"
)

type L3Peer struct {
	subnet *IPv4Route
	routes *RouteTracker

	client *proto.ReconnectClient

	free PacketQueue
	out  PacketQueue
}

func NewL3Peer(subnet *IPv4Route, routes *RouteTracker, addr *proto.Address, config *tls.Config, mtu uint16) (*L3Peer, error) {
	if !addr.TLS() {
		config = nil
	} else if config == nil {
		return nil, fmt.Errorf("peer %s requires TLS configuration", addr)
	}

	const peerQueueSize = 1024
	free := AllocatePacketQueue(peerQueueSize, int(mtu))
	out := make(PacketQueue, peerQueueSize)

	p := L3Peer{
		subnet: subnet,
		routes: routes,

		free: free,
		out:  out,
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
	r, w, route, err := L3Handshake(p.subnet, conn)
	if err != nil {
		return err
	}

	// TODO
	_, _, _ = r, w, route

	return nil
}

func L3Handshake(subnet *IPv4Route, peer net.Conn) (*bufio.Reader, *bufio.Writer, *IPv4Route, error) {
	// basic handshake
	r, w, err := Handshake(peer, "SFL3 1.0")
	if err != nil {
		return nil, nil, nil, err
	}

	// exchange subnets
	err = subnet.Write(w)
	if err != nil {
		return nil, nil, nil, err
	}
	route, err := ReadIPv4Route(r)
	if err != nil {
		return nil, nil, nil, err
	}
	log.Info("Subnet %s is at %s", route, peer.RemoteAddr())

	return r, w, route, nil
}
