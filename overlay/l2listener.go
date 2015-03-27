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

	clientsMutex sync.Mutex
	clients      map[string]string
}

func NewL2Listener(address *proto.Address, config *tls.Config, bridge *L2Bridge) *L2Listener {
	return &L2Listener{
		address: address,
		config:  config,
		bridge:  bridge,

		clients: make(map[string]string),
	}
}

func (l *L2Listener) Start(cli *cli.Listener) error {
	listener, err := l.address.Listen(l.config)
	if err != nil {
		return err
	}
	go l.accept(listener)

	cli.Register("clients", "", "List all active clients", 0, 0, l.cliClients)

	return nil
}

func (l *L2Listener) cliClients(args ...string) (string, error) {
	return l.listClients(), nil
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
	client := conn.RemoteAddr()
	defer func() {
		conn.Close()
		log.Info("Client disconnected: %s", conn.RemoteAddr())
	}()

	log.Info("Inbound connection: %s", client)

	tap, err := NewL2Tap()
	if err != nil {
		log.Warn("Failed to create tap: %s", err)
		return
	}
	defer tap.Close()

	tapName := tap.Name()
	err = l.bridge.link(tapName)
	if err != nil {
		log.Warn("Failed to link tap to bridge: %s", err)
		return
	}

	l.clientConnected(client, tapName)
	defer l.clientDisconnected(client)

	tap.Forward(conn)
}

func (l *L2Listener) clientConnected(addr net.Addr, downlinkIface string) {
	l.clientsMutex.Lock()
	defer l.clientsMutex.Unlock()

	l.clients[addr.String()] = downlinkIface
}

func (l *L2Listener) clientDisconnected(addr net.Addr) {
	l.clientsMutex.Lock()
	defer l.clientsMutex.Unlock()

	delete(l.clients, addr.String())
}

func (l *L2Listener) listClients() string {
	l.clientsMutex.Lock()
	defer l.clientsMutex.Unlock()

	return fmt.Sprintf("Clients: %s", mapValues(l.clients))
}

func mapValues(m map[string]string) string {
	var entries []string
	for k, v := range m {
		entries = append(entries, fmt.Sprintf("%s via %s", k, v))
	}
	return strings.Join(entries, ", ")
}
