package overlay

import (
	"crypto/tls"
	"net"

	"github.com/catalyzeio/shadowfax/cli"
	"github.com/catalyzeio/shadowfax/proto"
)

type L3Listener struct {
	peers *L3Peers

	address *proto.Address
	config  *tls.Config
}

func NewL3Listener(peers *L3Peers, address *proto.Address, config *tls.Config) *L3Listener {
	return &L3Listener{
		peers: peers,

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
		go l.initialize(conn)
	}
}

func (l *L3Listener) initialize(conn net.Conn) {
	remoteAddr := conn.RemoteAddr()
	defer func() {
		conn.Close()
		log.Info("Peer disconnected: %s", remoteAddr)
	}()

	peers := l.peers
	localURL := peers.localURL
	subnet := peers.subnet

	// basic handshake
	r, w, err := L3Handshake(conn)
	if err != nil {
		log.Warn("Failed to initialize connection to %s: %s", remoteAddr, err)
		return
	}

	// send local URL and subnet
	err = WriteL3PeerInfo(localURL, subnet, r, w)
	if err != nil {
		log.Warn("Failed to send peer information to %s: %s", remoteAddr, err)
		return
	}

	// read peer URL and subnet
	remoteURL, remoteSubnet, err := ReadL3PeerInfo(r, w)
	if err != nil {
		log.Warn("Failed to read peer information from %s: %s", remoteAddr, err)
		return
	}
	log.Info("Inbound connection: peer %s, subnet %s", remoteURL, remoteSubnet)

	// register connection for this peer
	peerConn := NewL3Conn(peers, nil)

	peers.AddInboundPeer(remoteURL, peerConn)
	defer peers.DeletePeer(remoteURL, peerConn)

	// kick off packet forwarding
	peerConn.Forward(remoteSubnet, r, w)
}
