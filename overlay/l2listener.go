package overlay

import (
	"crypto/tls"
	"net"
	"sync"

	"github.com/catalyzeio/go-core/comm"
)

type L2Listener struct {
	address *comm.Address
	config  *tls.Config
	bridge  *L2Bridge

	downlinksMutex sync.Mutex
	downlinks      map[string]string
}

func NewL2Listener(address *comm.Address, config *tls.Config, bridge *L2Bridge) *L2Listener {
	return &L2Listener{
		address: address,
		config:  config,
		bridge:  bridge,

		downlinks: make(map[string]string),
	}
}

func (l *L2Listener) Start() error {
	listener, err := l.address.Listen(l.config)
	if err != nil {
		return err
	}
	go l.accept(listener)

	return nil
}

func (l *L2Listener) ListDownlinks() map[string]*PeerDetails {
	return l.snapshot()
}

func (l *L2Listener) accept(listener net.Listener) {
	defer listener.Close()

	log.Info("Listening on %s", listener.Addr())
	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Warn("Failed to accept incoming connection: %s", err)
			return
		}
		go l.service(conn)
	}
}

func (l *L2Listener) service(conn net.Conn) {
	remoteAddr := conn.RemoteAddr()
	defer func() {
		conn.Close()
		log.Info("Downlink disconnected: %s", remoteAddr)
	}()

	log.Info("Inbound connection: %s", remoteAddr)

	if err := l.handle(conn, remoteAddr); err != nil {
		log.Warn("Failed to handle inbound connection %s: %s", remoteAddr, err)
	}
}

func (l *L2Listener) handle(conn net.Conn, remoteAddr net.Addr) error {
	r, w, err := L2Handshake(conn)
	if err != nil {
		return err
	}

	tap, err := NewL2Tap()
	if err != nil {
		return err
	}
	defer func() {
		if err := tap.Close(); err != nil {
			log.Warn("Failed to close downlink tap: %s", err)
		} else {
			log.Info("Closed downlink tap %s", tap.Name())
		}
	}()

	l.downlinkConnected(remoteAddr, tap.Name())
	defer l.downlinkDisconnected(remoteAddr)

	return tap.Forward(l.bridge, r, w, nil)
}

func (l *L2Listener) downlinkConnected(addr net.Addr, tapName string) {
	l.downlinksMutex.Lock()
	defer l.downlinksMutex.Unlock()

	l.downlinks[addr.String()] = tapName
}

func (l *L2Listener) downlinkDisconnected(addr net.Addr) {
	l.downlinksMutex.Lock()
	defer l.downlinksMutex.Unlock()

	delete(l.downlinks, addr.String())
}

func (l *L2Listener) snapshot() map[string]*PeerDetails {
	l.downlinksMutex.Lock()
	defer l.downlinksMutex.Unlock()

	result := make(map[string]*PeerDetails, len(l.downlinks))
	for k, v := range l.downlinks {
		result[k] = &PeerDetails{
			Type:      "downlink",
			Interface: v,
		}
	}
	return result
}
