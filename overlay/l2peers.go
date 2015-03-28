package overlay

import (
	"crypto/tls"
	"fmt"
	"sync"

	"github.com/catalyzeio/shadowfax/cli"
	"github.com/catalyzeio/shadowfax/proto"
)

type L2Peers struct {
	config *tls.Config
	bridge *L2Bridge

	peersMutex sync.Mutex
	peers      map[string]*L2Peer
}

func NewL2Peers(config *tls.Config, bridge *L2Bridge) *L2Peers {
	return &L2Peers{
		config: config,
		bridge: bridge,

		peers: make(map[string]*L2Peer),
	}
}

func (lp *L2Peers) Start(cli *cli.Listener) {
	cli.Register("addpeer", "[proto://host:port]", "Adds a peer at the specified address", 1, 1, lp.cliAddPeer)
	cli.Register("delpeer", "[proto://host:port]", "Deletes the peer at the specified address", 1, 1, lp.cliDelPeer)
	cli.Register("peers", "", "List all active peers", 0, 0, lp.cliPeers)
}

func (lp *L2Peers) cliAddPeer(args ...string) (string, error) {
	peerURL := args[0]

	err := lp.AddPeer(peerURL)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("Added peer %s", peerURL), nil
}

func (lp *L2Peers) AddPeer(url string) error {
	addr, err := proto.ParseAddress(url)
	if err != nil {
		return err
	}

	// verify new peer before creating client/tap
	err = lp.addPeer(url, nil)
	if err != nil {
		return err
	}

	peer, err := NewL2Peer(addr, lp.config)
	if err != nil {
		return err
	}

	tap, err := NewL2Tap()
	if err != nil {
		return err
	}

	err = lp.bridge.link(tap.Name())
	if err != nil {
		return err
	}

	err = lp.addPeer(url, peer)
	if err != nil {
		return err
	}

	peer.Start(tap)
	return nil
}

func (lp *L2Peers) addPeer(url string, peer *L2Peer) error {
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

func (lp *L2Peers) cliDelPeer(args ...string) (string, error) {
	peerURL := args[0]

	err := lp.DeletePeer(peerURL)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("Deleted peer %s", peerURL), nil
}

func (lp *L2Peers) DeletePeer(url string) error {
	peer, err := lp.removePeer(url)
	if err != nil {
		return err
	}
	peer.Stop()
	return nil
}

func (lp *L2Peers) removePeer(url string) (*L2Peer, error) {
	lp.peersMutex.Lock()
	defer lp.peersMutex.Unlock()

	peer, present := lp.peers[url]
	if !present {
		return nil, fmt.Errorf("no such peer %s", url)
	}
	delete(lp.peers, url)
	return peer, nil
}

func (lp *L2Peers) cliPeers(args ...string) (string, error) {
	return fmt.Sprintf("Peers: %s", mapValues(lp.ListPeers())), nil
}

func (lp *L2Peers) ListPeers() map[string]string {
	lp.peersMutex.Lock()
	defer lp.peersMutex.Unlock()

	m := make(map[string]string)
	for k, v := range lp.peers {
		m[k] = v.Name()
	}
	return m
}
