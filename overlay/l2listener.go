package overlay

import (
	"crypto/tls"
	"fmt"
	"net"
	"strings"
	"sync"

	"github.com/catalyzeio/shadowfax/cli"
	"github.com/catalyzeio/shadowfax/proto"
)

type L2Listener struct {
	address *proto.Address
	config  *tls.Config
	bridge  *L2Bridge

	downlinksMutex sync.Mutex
	downlinks      map[string]string
}

func NewL2Listener(address *proto.Address, config *tls.Config, bridge *L2Bridge) *L2Listener {
	return &L2Listener{
		address: address,
		config:  config,
		bridge:  bridge,

		downlinks: make(map[string]string),
	}
}

func (l *L2Listener) Start(cli *cli.Listener) error {
	listener, err := l.address.Listen(l.config)
	if err != nil {
		return err
	}
	go l.accept(listener)

	cli.Register("downlinks", "", "List all active downlinks", 0, 0, l.cliDownlinks)

	return nil
}

func (l *L2Listener) cliDownlinks(args ...string) (string, error) {
	return l.listDownlinks(), nil
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

func (l *L2Listener) listDownlinks() string {
	l.downlinksMutex.Lock()
	defer l.downlinksMutex.Unlock()

	return fmt.Sprintf("Downlinks: %s", mapValues(l.downlinks))
}

func mapValues(m map[string]string) string {
	var entries []string
	for k, v := range m {
		entries = append(entries, fmt.Sprintf("%s via %s", k, v))
	}
	return strings.Join(entries, ", ")
}
