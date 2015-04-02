package overlay

import (
	"bufio"
	"fmt"
	"net"

	"github.com/catalyzeio/shadowfax/proto"
)

type L3Client struct {
	peers *L3Peers

	remoteURL string
	client    *proto.ReconnectClient

	relay *L3Relay
}

const (
	urlDelim      = '\n'
	peerQueueSize = 1024
)

func NewL3Client(peers *L3Peers, remoteURL string, addr *proto.Address) (*L3Client, error) {
	config := peers.config
	if !addr.TLS() {
		config = nil
	} else if config == nil {
		return nil, fmt.Errorf("peer %s requires TLS configuration", addr)
	}

	free := AllocatePacketQueue(peerQueueSize, ethernetHeaderSize+int(peers.mtu))
	out := make(PacketQueue, peerQueueSize)

	p := L3Client{
		peers: peers,

		remoteURL: remoteURL,

		relay: NewL3RelayWithQueues(peers, free, out),
	}
	p.client = proto.NewClient(p.connHandler, addr.Host(), addr.Port(), config)
	return &p, nil
}

func (p *L3Client) Start() {
	p.client.Start()
}

func (p *L3Client) Stop() {
	p.client.Stop()
}

func (p *L3Client) connHandler(conn net.Conn, abort <-chan bool) error {
	peers := p.peers
	localURL := peers.localURL
	subnet := peers.subnet

	// basic handshake
	r, w, err := L3Handshake(conn)
	if err != nil {
		return err
	}

	// read peer URL and subnet
	remoteURL, remoteSubnet, err := ReadL3PeerInfo(r, w)
	if err != nil {
		return err
	}
	log.Info("Connected to peer %s, subnet %s", remoteURL, remoteSubnet)

	// ignore connections to self
	if remoteURL == localURL {
		log.Warn("Dropping redundant connection to self")
		err := peers.DeletePeer(p.remoteURL, p)
		if err != nil {
			log.Warn("Failed to prune connection to self: %s", err)
			p.Stop()
		}
		return nil
	}

	// update peer registration if remote responded with different public address
	if p.remoteURL != remoteURL {
		log.Info("Peer at %s is actually %s", p.remoteURL, remoteURL)
		err := peers.UpdatePeer(p.remoteURL, remoteURL, p)
		if err != nil {
			log.Warn("Failed to update connection URL: %s", err)
			p.Stop()
			return nil
		}
		p.remoteURL = remoteURL
	}

	// send local URL and subnet
	err = WriteL3PeerInfo(localURL, subnet, r, w)
	if err != nil {
		return err
	}

	// kick off packet forwarding
	p.relay.Forward(remoteSubnet, r, w)

	return nil
}

func L3Handshake(peer net.Conn) (*bufio.Reader, *bufio.Writer, error) {
	return Handshake(peer, "SFL3 1.0")
}

func WriteL3PeerInfo(localURL string, subnet *IPv4Route, r *bufio.Reader, w *bufio.Writer) error {
	_, err := w.WriteString(localURL + string(urlDelim))
	if err != nil {
		return err
	}

	err = subnet.Write(w)
	if err != nil {
		return err
	}

	err = w.Flush()
	if err != nil {
		return err
	}

	return nil
}

func ReadL3PeerInfo(r *bufio.Reader, w *bufio.Writer) (string, *IPv4Route, error) {
	remoteURL, err := r.ReadString(urlDelim)
	if err != nil {
		return "", nil, err
	}
	remoteURL = remoteURL[:len(remoteURL)-1] // chop off delim

	remoteSubnet, err := ReadIPv4Route(r)
	if err != nil {
		return "", nil, err
	}

	return remoteURL, remoteSubnet, nil
}
