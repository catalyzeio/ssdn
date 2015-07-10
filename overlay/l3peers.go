package overlay

import (
	"crypto/tls"
	"fmt"
	"sync"

	"github.com/catalyzeio/go-core/comm"
)

type L3Peer interface {
	Stop()
}

type L3Peers struct {
	localURL string

	subnet *IPv4Route
	routes *RouteTracker

	config *tls.Config
	mtu    uint16

	handler InboundHandler

	peersMutex sync.Mutex
	peers      map[string]L3Peer
}

func NewL3Peers(subnet *IPv4Route, routes *RouteTracker, config *tls.Config, mtu uint16, handler InboundHandler) *L3Peers {
	return &L3Peers{
		subnet: subnet,
		routes: routes,

		config: config,
		mtu:    mtu,

		handler: handler,

		peers: make(map[string]L3Peer),
	}
}

func (p *L3Peers) Start(localURL string) {
	p.localURL = localURL
}

func (p *L3Peers) UpdatePeers(peerURLs map[string]struct{}) {
	removed := make(map[string]L3Peer)
	added := make(map[string]struct{})
	p.processUpdate(peerURLs, removed, added)

	for url, peer := range removed {
		peer.Stop()
		log.Info("Removed obsolete peer %s", url)
	}

	for url := range added {
		log.Info("Discovered peer %s", url)
		err := p.AddPeer(url)
		if err != nil {
			log.Warn("Failed to add client for peer at %s: %s", url, err)
		}
	}
}

func (p *L3Peers) processUpdate(current map[string]struct{}, removed map[string]L3Peer, added map[string]struct{}) {
	p.peersMutex.Lock()
	defer p.peersMutex.Unlock()

	// record which peers were removed
	for url, peer := range p.peers {
		_, present := current[url]
		if !present {
			removed[url] = peer
			delete(p.peers, url)
		}
	}

	// record which peers were added
	for url := range current {
		_, present := p.peers[url]
		if !present {
			added[url] = struct{}{}
		}
	}
}

func (p *L3Peers) AddPeer(url string) error {
	addr, err := comm.ParseAddress(url)
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

func (p *L3Peers) DeletePeer(url string) error {
	return p.RemovePeer(url, nil)
}

func (p *L3Peers) RemovePeer(url string, expected L3Peer) error {
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

func (p *L3Peers) ListPeers() map[string]*PeerDetails {
	p.peersMutex.Lock()
	defer p.peersMutex.Unlock()

	result := make(map[string]*PeerDetails, len(p.peers))
	for k := range p.peers {
		result[k] = &PeerDetails{
			Type: "peer",
		}
	}
	return result
}
