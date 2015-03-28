package overlay

import (
	"crypto/tls"
	"fmt"
	"strings"
	"sync"

	"github.com/catalyzeio/shadowfax/cli"
	"github.com/catalyzeio/shadowfax/proto"
)

type L3Peers struct {
	routes *RouteTracker
	config *tls.Config
	mtu    uint16

	peersMutex sync.Mutex
	peers      map[string]*L3Peer
}

func NewL3Peers(routes *RouteTracker, config *tls.Config, mtu uint16) *L3Peers {
	return &L3Peers{
		routes: routes,
		config: config,
		mtu:    mtu,

		peers: make(map[string]*L3Peer),
	}
}

func (lp *L3Peers) Start(cli *cli.Listener) {
	cli.Register("addpeer", "[proto://host:port]", "Adds a peer at the specified address", 1, 1, lp.cliAddPeer)
	cli.Register("delpeer", "[proto://host:port]", "Deletes the peer at the specified address", 1, 1, lp.cliDelPeer)
	cli.Register("peers", "", "List all active peers", 0, 0, lp.cliPeers)
}

func (lp *L3Peers) cliAddPeer(args ...string) (string, error) {
	peerURL := args[0]

	err := lp.AddPeer(peerURL)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("Added peer %s", peerURL), nil
}

func (lp *L3Peers) AddPeer(url string) error {
	addr, err := proto.ParseAddress(url)
	if err != nil {
		return err
	}

	peer, err := NewL3Peer(addr, lp.config, lp.mtu)
	if err != nil {
		return err
	}

	err = lp.addPeer(url, peer)
	if err != nil {
		return err
	}

	peer.Start()
	return nil
}

func (lp *L3Peers) addPeer(url string, peer *L3Peer) error {
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

	err := lp.DeletePeer(peerURL)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("Deleted peer %s", peerURL), nil
}

func (lp *L3Peers) DeletePeer(url string) error {
	peer, err := lp.removePeer(url)
	if err != nil {
		return err
	}
	peer.Stop()
	return nil
}

func (lp *L3Peers) removePeer(url string) (*L3Peer, error) {
	lp.peersMutex.Lock()
	defer lp.peersMutex.Unlock()

	peer, present := lp.peers[url]
	if !present {
		return nil, fmt.Errorf("no such peer %s", url)
	}
	delete(lp.peers, url)
	return peer, nil
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
