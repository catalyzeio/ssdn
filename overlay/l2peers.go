package overlay

import (
	"crypto/tls"
	"fmt"
	"net"
	"sync"

	"github.com/catalyzeio/shadowfax/proto"
)

type L2Peers struct {
	config *tls.Config

	mutex sync.Mutex
	peers map[string]*l2Peer
}

func NewL2Peers(config *tls.Config) *L2Peers {
	return &L2Peers{
		config: config,
		peers:  make(map[string]*l2Peer),
	}
}

func (lp *L2Peers) Start() {
	// no-op
}

func (lp *L2Peers) Stop() {
	lp.mutex.Lock()
	defer lp.mutex.Unlock()

	for _, v := range lp.peers {
		v.stop()
	}
}

func (lp *L2Peers) AddPeer(url string) error {
	addr, err := proto.ParseAddress(url)
	if err != nil {
		return err
	}

	peer, err := newL2Peer(addr, lp.config)
	if err != nil {
		return err
	}

	tap, err := NewL2Tap()
	if err != nil {
		return err
	}
	peer.start(tap)

	lp.mutex.Lock()
	defer lp.mutex.Unlock()
	lp.peers[url] = peer
	return nil
}

func (lp *L2Peers) DeletePeer(url string) error {
	peer, err := lp.removePeer(url)
	if err != nil {
		return err
	}
	peer.stop()
	return nil
}

func (lp *L2Peers) removePeer(url string) (*l2Peer, error) {
	lp.mutex.Lock()
	defer lp.mutex.Unlock()

	peer, present := lp.peers[url]
	if !present {
		return nil, fmt.Errorf("no such peer %s", url)
	}
	delete(lp.peers, url)
	return peer, nil
}

func (lp *L2Peers) ListPeers() map[string]string {
	lp.mutex.Lock()
	defer lp.mutex.Unlock()

	res := make(map[string]string)
	for k, v := range lp.peers {
		res[k] = v.tap.Name()
	}
	return res
}

type l2Peer struct {
	client *proto.ReconnectClient
	tap    *L2Tap
}

func newL2Peer(addr *proto.Address, config *tls.Config) (*l2Peer, error) {
	if !addr.TLS() {
		config = nil
	} else if config == nil {
		return nil, fmt.Errorf("peer %s requires TLS configuration", addr)
	}

	p := l2Peer{}
	p.client = proto.NewClient(p.connHandler, addr.Host(), addr.Port(), config)
	return &p, nil
}

func (p *l2Peer) start(tap *L2Tap) {
	p.tap = tap
	p.client.Start()
}

func (p *l2Peer) stop() {
	p.client.Stop()
	p.tap.Close()
}

func (p *l2Peer) connHandler(conn net.Conn, abort <-chan bool) error {
	p.tap.Forward(conn)
	return nil
}
