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

func (lp *L3Peers) Start(cli *cli.Listener) {
	cli.Register("addpeer", "[proto://host:port]", "Adds a peer at the specified address", 1, 1, lp.cliAddPeer)
	cli.Register("delpeer", "[proto://host:port]", "Deletes the peer at the specified address", 1, 1, lp.cliDelPeer)
	cli.Register("peers", "", "List all active peers", 0, 0, lp.cliPeers)
}

func (lp *L3Peers) cliAddPeer(args ...string) (string, error) {
	peerURL := args[0]

	err := lp.AddClient(peerURL)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("Added peer %s", peerURL), nil
}

func (lp *L3Peers) AddClient(url string) error {
	addr, err := proto.ParseAddress(url)
	if err != nil {
		return err
	}

	// verify no existing peer before creating client
	err = lp.addClient(url, nil)
	if err != nil {
		return err
	}

	client, err := NewL3Client(lp, url, addr)
	if err != nil {
		return err
	}

	err = lp.addClient(url, client)
	if err != nil {
		return err
	}

	client.Start()
	return nil
}

func (lp *L3Peers) addClient(url string, peer L3Peer) error {
	lp.peersMutex.Lock()
	defer lp.peersMutex.Unlock()

	_, present := lp.peers[url]
	if present {
		return fmt.Errorf("already connected to peer %s", url)
	}
	if peer != nil {
		lp.peers[url] = peer
	}
	return nil
}

func (lp *L3Peers) cliDelPeer(args ...string) (string, error) {
	peerURL := args[0]

	err := lp.DeletePeer(peerURL, nil)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("Deleted peer %s", peerURL), nil
}

func (lp *L3Peers) DeletePeer(url string, expected L3Peer) error {
	peer, err := lp.removePeer(url, expected)
	if err != nil {
		return err
	}
	peer.Stop()
	return nil
}

func (lp *L3Peers) removePeer(url string, expected L3Peer) (L3Peer, error) {
	lp.peersMutex.Lock()
	defer lp.peersMutex.Unlock()

	current, present := lp.peers[url]
	if !present {
		return nil, fmt.Errorf("no such peer %s", url)
	}
	if expected != nil && current != expected {
		return nil, fmt.Errorf("peer at %s has been replaced", url)
	}
	delete(lp.peers, url)
	return current, nil
}

func (lp *L3Peers) UpdatePeer(oldURL string, newURL string, peer L3Peer) error {
	lp.peersMutex.Lock()
	defer lp.peersMutex.Unlock()

	current, present := lp.peers[oldURL]
	if !present {
		return fmt.Errorf("no such peer %s", oldURL)
	}
	if current != peer {
		return fmt.Errorf("peer at %s has been replaced", oldURL)
	}
	delete(lp.peers, oldURL)

	_, present = lp.peers[newURL]
	if present {
		return fmt.Errorf("already connected to peer %s", newURL)
	}
	lp.peers[newURL] = peer
	return nil
}

func (lp *L3Peers) AddInboundPeer(url string, peer L3Peer) {
	replaced := lp.replace(url, peer)
	if replaced != nil {
		log.Warn("Inbound peer replaced existing peer at %s", url)
		replaced.Stop()
	}
}

func (lp *L3Peers) replace(url string, peer L3Peer) L3Peer {
	lp.peersMutex.Lock()
	defer lp.peersMutex.Unlock()

	var existing L3Peer
	current, present := lp.peers[url]
	if present {
		existing = current
	}
	lp.peers[url] = peer
	return existing
}

func (lp *L3Peers) cliPeers(args ...string) (string, error) {
	return fmt.Sprintf("Peers: %s", strings.Join(lp.ListPeers(), ", ")), nil
}

func (lp *L3Peers) ListPeers() []string {
	lp.peersMutex.Lock()
	defer lp.peersMutex.Unlock()

	l := make([]string, len(lp.peers))
	offset := 0
	for k, _ := range lp.peers {
		l[offset] = k
		offset++
	}
	return l
}
