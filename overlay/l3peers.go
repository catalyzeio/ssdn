package overlay

import (
	"crypto/tls"
	"fmt"
	"strings"
	"sync"

	"github.com/catalyzeio/shadowfax/cli"
	"github.com/catalyzeio/shadowfax/proto"
)

type L3Peer interface {
	Stop()
}

type L3Peers struct {
	localURL string
	subnet   *IPv4Route
	routes   *RouteTracker

	config *tls.Config
	mtu    uint16

	handler InboundHandler

	peersMutex sync.Mutex
	peers      map[string]L3Peer
}

func NewL3Peers(localURL string, subnet *IPv4Route, routes *RouteTracker, config *tls.Config, mtu uint16, handler InboundHandler) *L3Peers {
	return &L3Peers{
		localURL: localURL,
		subnet:   subnet,
		routes:   routes,

		config: config,
		mtu:    mtu,

		handler: handler,

		peers: make(map[string]L3Peer),
	}
}

func (p *L3Peers) Start(cli *cli.Listener) {
	cli.Register("addpeer", "[proto://host:port]", "Adds a peer at the specified address", 1, 1, p.cliAddPeer)
	cli.Register("delpeer", "[proto://host:port]", "Deletes the peer at the specified address", 1, 1, p.cliDelPeer)
	cli.Register("peers", "", "List all active peers", 0, 0, p.cliPeers)
}

func (p *L3Peers) cliAddPeer(args ...string) (string, error) {
	peerURL := args[0]

	if err := p.AddClient(peerURL); err != nil {
		return "", err
	}
	return fmt.Sprintf("Added peer %s", peerURL), nil
}

func (p *L3Peers) AddClient(url string) error {
	addr, err := proto.ParseAddress(url)
	if err != nil {
		return err
	}

	// verify no existing peer before creating client
	if err := p.addClient(url, nil); err != nil {
		return err
	}

	client, err := NewL3Client(p, url, addr)
	if err != nil {
		return err
	}

	if err := p.addClient(url, client); err != nil {
		return err
	}

	client.Start()
	return nil
}

func (p *L3Peers) addClient(url string, peer L3Peer) error {
	p.peersMutex.Lock()
	defer p.peersMutex.Unlock()

	_, present := p.peers[url]
	if present {
		return fmt.Errorf("already connected to peer %s", url)
	}
	if peer != nil {
		p.peers[url] = peer
	}
	return nil
}

func (p *L3Peers) cliDelPeer(args ...string) (string, error) {
	peerURL := args[0]

	if err := p.DeletePeer(peerURL, nil); err != nil {
		return "", err
	}
	return fmt.Sprintf("Deleted peer %s", peerURL), nil
}

func (p *L3Peers) DeletePeer(url string, expected L3Peer) error {
	peer, err := p.removePeer(url, expected)
	if err != nil {
		return err
	}
	peer.Stop()
	return nil
}

func (p *L3Peers) removePeer(url string, expected L3Peer) (L3Peer, error) {
	p.peersMutex.Lock()
	defer p.peersMutex.Unlock()

	current, present := p.peers[url]
	if !present {
		return nil, fmt.Errorf("no such peer %s", url)
	}
	if expected != nil && current != expected {
		return nil, fmt.Errorf("peer at %s has been replaced", url)
	}
	delete(p.peers, url)
	return current, nil
}

func (p *L3Peers) UpdatePeer(oldURL string, newURL string, peer L3Peer) error {
	p.peersMutex.Lock()
	defer p.peersMutex.Unlock()

	current, present := p.peers[oldURL]
	if !present {
		return fmt.Errorf("no such peer %s", oldURL)
	}
	if current != peer {
		return fmt.Errorf("peer at %s has been replaced", oldURL)
	}
	delete(p.peers, oldURL)

	_, present = p.peers[newURL]
	if present {
		return fmt.Errorf("already connected to peer %s", newURL)
	}
	p.peers[newURL] = peer
	return nil
}

func (p *L3Peers) AddInboundPeer(url string, peer L3Peer) {
	replaced := p.replace(url, peer)
	if replaced != nil {
		log.Warn("Inbound peer replaced existing peer at %s", url)
		replaced.Stop()
	}
}

func (p *L3Peers) replace(url string, peer L3Peer) L3Peer {
	p.peersMutex.Lock()
	defer p.peersMutex.Unlock()

	var existing L3Peer
	current, present := p.peers[url]
	if present {
		existing = current
	}
	p.peers[url] = peer
	return existing
}

func (p *L3Peers) cliPeers(args ...string) (string, error) {
	return fmt.Sprintf("Peers: %s", strings.Join(p.ListPeers(), ", ")), nil
}

func (p *L3Peers) ListPeers() []string {
	p.peersMutex.Lock()
	defer p.peersMutex.Unlock()

	l := make([]string, len(p.peers))
	offset := 0
	for k := range p.peers {
		l[offset] = k
		offset++
	}
	return l
}
