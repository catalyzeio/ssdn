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
	peers      map[string]*l3PeerEntry
}

type l3PeerEntry struct {
	peer  L3Peer
	state PeerState
}

func NewL3Peers(subnet *IPv4Route, routes *RouteTracker, config *tls.Config, mtu uint16, handler InboundHandler) *L3Peers {
	return &L3Peers{
		subnet: subnet,
		routes: routes,

		config: config,
		mtu:    mtu,

		handler: handler,

		peers: make(map[string]*l3PeerEntry),
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
	for url, entry := range p.peers {
		_, present := current[url]
		if !present && entry.state == Connecting { // always keep live peers
			removed[url] = entry.peer
			delete(p.peers, url)
		}
	}

	// record which peers were added
	for url := range current {
		entry := p.peers[url]
		if entry == nil {
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

	entry := p.peers[url]
	if entry != nil {
		return fmt.Errorf("already connected to peer %s", url)
	}
	if peer != nil {
		p.peers[url] = &l3PeerEntry{peer, Connecting}
	}
	return nil
}

func (p *L3Peers) DeletePeer(url string) error {
	peer, err := p.removePeer(url)
	if err != nil {
		return err
	}
	peer.Stop()
	return nil
}

func (p *L3Peers) removePeer(url string) (L3Peer, error) {
	p.peersMutex.Lock()
	defer p.peersMutex.Unlock()

	entry := p.peers[url]
	if entry == nil {
		return nil, fmt.Errorf("no such peer %s", url)
	}
	delete(p.peers, url)
	return entry.peer, nil
}

func (p *L3Peers) ListPeers() map[string]*PeerDetails {
	p.peersMutex.Lock()
	defer p.peersMutex.Unlock()

	result := make(map[string]*PeerDetails, len(p.peers))
	for k, v := range p.peers {
		result[k] = &PeerDetails{
			Type:  "peer",
			State: v.state,
		}
	}
	return result
}

func (p *L3Peers) Drop(url string, expected L3Peer) {
	if err := p.remove(url, expected); err != nil {
		log.Warn("Failed to drop peer %s: %s", url, err)
	}
	expected.Stop()
}

func (p *L3Peers) remove(url string, expected L3Peer) error {
	p.peersMutex.Lock()
	defer p.peersMutex.Unlock()

	entry := p.peers[url]
	if entry == nil {
		return fmt.Errorf("no such peer %s", url)
	}
	if entry.peer != expected {
		return fmt.Errorf("peer at %s has been replaced", url)
	}

	delete(p.peers, url)
	return nil
}

func (p *L3Peers) UpdateLocation(oldURL string, newURL string, expected L3Peer) bool {
	if err := p.move(oldURL, newURL, expected); err != nil {
		log.Warn("Failed to update peer location from %s to %s: %s", oldURL, newURL, err)
		expected.Stop()
		return false
	}
	return true
}

func (p *L3Peers) move(oldURL string, newURL string, expected L3Peer) error {
	p.peersMutex.Lock()
	defer p.peersMutex.Unlock()

	entry := p.peers[oldURL]
	if entry == nil {
		return fmt.Errorf("no such peer %s", oldURL)
	}
	if entry.peer != expected {
		return fmt.Errorf("peer at %s has been replaced", oldURL)
	}
	delete(p.peers, oldURL)

	_, present := p.peers[newURL]
	if present {
		return fmt.Errorf("already connected to peer %s", newURL)
	}
	p.peers[newURL] = entry
	return nil
}

func (p *L3Peers) UpdateState(url string, expected L3Peer, state PeerState) bool {
	replaced, err := p.change(url, expected, state)
	if err != nil {
		log.Warn("Failed to update state for peer %s: %s", url, err)
		expected.Stop()
		return false
	}
	if replaced != nil {
		log.Warn("Inbound connection supplanted existing client to %s", url)
		replaced.Stop()
	}
	return true
}

func (p *L3Peers) change(url string, expected L3Peer, state PeerState) (L3Peer, error) {
	p.peersMutex.Lock()
	defer p.peersMutex.Unlock()

	entry := p.peers[url]

	// handle inbound connections
	if state == Inbound {
		// verify we're not connecting to ourselves; needed for tie-break logic below
		if url == p.localURL {
			return nil, fmt.Errorf("remote peer address %s matches local address", url)
		}

		// no existing peer -> record new peer
		if entry == nil {
			p.peers[url] = &l3PeerEntry{expected, state}
			return nil, nil
		}

		// existing peer -> duplicate connection
		// break ties using string representation of address (URL)
		if url < p.localURL {
			return nil, fmt.Errorf("duplicate connection to %s; deferring to existing client", url)
		}

		// supplant the existing peer
		p.peers[url] = &l3PeerEntry{expected, state}
		return entry.peer, nil
	}

	// handle client updates
	if entry == nil {
		return nil, fmt.Errorf("no such peer %s", url)
	}
	if entry.peer != expected {
		return nil, fmt.Errorf("peer at %s has been replaced", url)
	}

	entry.state = state
	return nil, nil
}
