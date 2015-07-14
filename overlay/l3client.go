package overlay

import (
	"bufio"
	"fmt"
	"net"

	"github.com/catalyzeio/go-core/comm"
)

type L3Client struct {
	peers *L3Peers

	remoteURL string
	client    *comm.ReconnectClient

	relay *L3Relay
}

const (
	urlDelim = '\n'
)

func NewL3Client(peers *L3Peers, remoteURL string, addr *comm.Address) (*L3Client, error) {
	config := peers.config
	if !addr.TLS() {
		config = nil
	} else if config == nil {
		return nil, fmt.Errorf("peer %s requires TLS configuration", addr)
	}

	free := AllocatePacketQueue(tapQueueSize, ethernetHeaderSize+int(peers.mtu))
	out := make(PacketQueue, tapQueueSize)

	c := L3Client{
		peers: peers,

		remoteURL: remoteURL,

		relay: NewL3RelayWithQueues(peers, free, out),
	}
	c.client = comm.NewClient(c.connHandler, addr.Host(), addr.Port(), config)
	return &c, nil
}

func (c *L3Client) Start() {
	c.client.Start()
}

func (c *L3Client) Connected() bool {
	return c.client.Connected()
}

func (c *L3Client) Stop() {
	c.client.Stop()
}

func (c *L3Client) connHandler(conn net.Conn, abort <-chan struct{}) error {
	peers := c.peers
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
		if err := peers.RemovePeer(c.remoteURL, c); err != nil {
			log.Warn("Failed to prune connection to self: %s", err)
			c.Stop()
		}
		return nil
	}

	// update peer registration if remote responded with different public address
	if c.remoteURL != remoteURL {
		log.Info("Peer at %s is actually %s", c.remoteURL, remoteURL)
		if err := peers.UpdatePeer(c.remoteURL, remoteURL, c); err != nil {
			log.Warn("Failed to update connection URL: %s", err)
			c.Stop()
			return nil
		}
		c.remoteURL = remoteURL
	}

	// send local URL and subnet
	if err := WriteL3PeerInfo(localURL, subnet, r, w); err != nil {
		return err
	}

	// kick off packet forwarding
	c.relay.Forward(remoteSubnet, r, w, abort)

	return nil
}

func L3Handshake(peer net.Conn) (*bufio.Reader, *bufio.Writer, error) {
	return Handshake(peer, "SFL3 1.0")
}

func WriteL3PeerInfo(localURL string, subnet *IPv4Route, r *bufio.Reader, w *bufio.Writer) error {
	if _, err := w.WriteString(localURL + string(urlDelim)); err != nil {
		return err
	}

	if err := subnet.Write(w); err != nil {
		return err
	}

	if err := w.Flush(); err != nil {
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
