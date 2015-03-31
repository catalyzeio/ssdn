package overlay

import (
	"bufio"
	"fmt"
	"net"
	"time"

	"github.com/catalyzeio/shadowfax/proto"
)

type L3Peer struct {
	peers *L3Peers

	remoteURL string
	client    *proto.ReconnectClient

	free PacketQueue
	out  PacketQueue
}

func NewL3Peer(peers *L3Peers, remoteURL string, addr *proto.Address) (*L3Peer, error) {
	config := peers.config
	if !addr.TLS() {
		config = nil
	} else if config == nil {
		return nil, fmt.Errorf("peer %s requires TLS configuration", addr)
	}

	const peerQueueSize = 1024
	free := AllocatePacketQueue(peerQueueSize, int(peers.mtu))
	out := make(PacketQueue, peerQueueSize)

	p := L3Peer{
		peers: peers,

		remoteURL: remoteURL,

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
	// basic handshake
	r, w, err := L3Handshake(conn)
	if err != nil {
		return err
	}

	// send local URL and subnet
	peers := p.peers

	localURL := peers.localURL
	const urlDelim = '\n'
	_, err = w.WriteString(localURL + string(urlDelim))
	if err != nil {
		return err
	}
	subnet := peers.subnet
	err = subnet.Write(w)
	if err != nil {
		return err
	}
	err = w.Flush()
	if err != nil {
		return err
	}

	// read peer URL and subnet
	remoteURL, err := r.ReadString(urlDelim)
	if err != nil {
		return err
	}
	remoteURL = remoteURL[:len(remoteURL)-1] // chop off delim
	remoteSubnet, err := ReadIPv4Route(r)
	if err != nil {
		return err
	}
	log.Info("Connected to peer %s, subnet %s", remoteURL, remoteSubnet)

	// ignore connections to self
	if remoteURL == localURL {
		log.Warn("Dropping redundant connection to self")
		err := peers.DeletePeer(p.remoteURL)
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
	}

	// TODO
	for {
		_, _ = remoteURL, remoteSubnet
		time.Sleep(time.Hour)
	}
}

func L3Handshake(peer net.Conn) (*bufio.Reader, *bufio.Writer, error) {
	return Handshake(peer, "SFL3 1.0")
}
