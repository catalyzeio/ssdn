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

func (u *L2Peers) Start(cli *cli.Listener) {
	cli.Register("addpeer", "[proto://host:port]", "Adds a peer at the specified address", 1, 1, u.cliAddPeer)
	cli.Register("delpeer", "[proto://host:port]", "Deletes the peer at the specified address", 1, 1, u.cliDelPeer)
	cli.Register("peers", "", "List all active peers", 0, 0, u.cliPeers)
}

func (u *L2Peers) cliAddPeer(args ...string) (string, error) {
	peerURL := args[0]

	err := u.AddPeer(peerURL)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("Added peer %s", peerURL), nil
}

func (u *L2Peers) AddPeer(url string) error {
	addr, err := proto.ParseAddress(url)
	if err != nil {
		return err
	}

	// verify new peer before creating client/tap
	err = u.addPeer(url, nil)
	if err != nil {
		return err
	}

	peer, err := NewL2Peer(addr, u.config)
	if err != nil {
		return err
	}

	tap, err := NewL2Tap()
	if err != nil {
		return err
	}

	err = u.bridge.link(tap.Name())
	if err != nil {
		return err
	}

	peer.Start(tap)

	err = u.addPeer(url, peer)
	if err != nil {
		peer.Stop()
		return err
	}
	return nil
}

func (u *L2Peers) addPeer(url string, peer *L2Peer) error {
	u.peersMutex.Lock()
	defer u.peersMutex.Unlock()

	_, present := u.peers[url]
	if present {
		return fmt.Errorf("already connected to peer %s", url)
	}
	if peer != nil {
		u.peers[url] = peer
	}
	return nil
}

func (u *L2Peers) cliDelPeer(args ...string) (string, error) {
	peerURL := args[0]

	err := u.DeletePeer(peerURL)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("Deleted peer %s", peerURL), nil
}

func (u *L2Peers) DeletePeer(url string) error {
	peer, err := u.removePeer(url)
	if err != nil {
		return err
	}
	peer.Stop()
	return nil
}

func (u *L2Peers) removePeer(url string) (*L2Peer, error) {
	u.peersMutex.Lock()
	defer u.peersMutex.Unlock()

	peer, present := u.peers[url]
	if !present {
		return nil, fmt.Errorf("no such peer %s", url)
	}
	delete(u.peers, url)
	return peer, nil
}

func (u *L2Peers) cliPeers(args ...string) (string, error) {
	return fmt.Sprintf("Peers: %s", mapValues(u.ListPeers())), nil
}

func (u *L2Peers) ListPeers() map[string]string {
	u.peersMutex.Lock()
	defer u.peersMutex.Unlock()

	m := make(map[string]string)
	for k, v := range u.peers {
		m[k] = v.Name()
	}
	return m
}
