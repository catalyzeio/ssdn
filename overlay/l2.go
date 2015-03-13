package overlay

import (
	"crypto/tls"
	"fmt"
	"log"
	"net"
	"strconv"
	"strings"
	"sync"

	"github.com/catalyzeio/shadowfax/actions"
	"github.com/catalyzeio/shadowfax/cli"
	"github.com/catalyzeio/shadowfax/proto"
)

type L2Overlay struct {
	bridge string
	mtu    uint16

	listenAddress *proto.Address
	config        *tls.Config

	invoker *actions.Invoker
	cli     *cli.Listener

	peerMutex sync.Mutex
	peers     map[string]*L2Peer

	clientMutex sync.Mutex
	clients     map[string]string

	connMutex   sync.Mutex
	connections map[string]string
	ifIndex     int
}

const (
	localIfaceTemplate = "sfl2.%s.%d"
	containerIface     = "eth1"
)

func NewL2Overlay(bridge string, mtu uint16, listenAddress *proto.Address, config *tls.Config, invoker *actions.Invoker, cli *cli.Listener) *L2Overlay {
	l := L2Overlay{
		bridge:        bridge,
		mtu:           mtu,
		listenAddress: listenAddress,
		config:        config,
		invoker:       invoker,
		cli:           cli,
		peers:         make(map[string]*L2Peer),
		connections:   make(map[string]string),
		clients:       make(map[string]string),
	}

	cli.Register("addpeer", "[proto://host:port]", "Adds a peer at the specified address", 1, 1, l.cliAddPeer)
	cli.Register("delpeer", "[proto://host:port]", "Deletes the peer at the specified address", 1, 1, l.cliDelPeer)
	cli.Register("peers", "", "List all active peers", 0, 0, l.cliPeers)
	cli.Register("clients", "", "List all active clients", 0, 0, l.cliClients)

	cli.Register("attach", "[container]", "Attaches the given container to this overlay network", 1, 1, l.cliAttach)
	cli.Register("detach", "[container]", "Detaches the given container from this overlay network", 1, 1, l.cliDetach)
	cli.Register("connections", "", "Lists all containers attached to this overlay network", 0, 0, l.cliConnections)

	return &l
}

func (o *L2Overlay) Start() error {
	// TODO restore existing state (bridge, veth pairs kept)
	// TODO recover on reboots (bridge, veth pairs killed)

	var err error
	initCLI := false
	defer func() {
		if err != nil {
			o.invoker.Stop()
			if initCLI {
				o.cli.Stop()
			}
		}
	}()

	// start action invoker
	o.invoker.Start()

	// initialize bridge
	_, err = o.invoker.Execute("create", o.bridge)
	if err != nil {
		return err
	}
	log.Printf("Created bridge %s", o.bridge)

	// initialize CLI
	err = o.cli.Start()
	if err != nil {
		return err
	}
	initCLI = true

	// initialize listener
	if o.listenAddress != nil {
		listener, err := o.listenAddress.Listen(o.config)
		if err != nil {
			return err
		}
		go o.accept(listener)
	}

	return nil
}

func (o *L2Overlay) AddPeer(url string) error {
	addr, err := proto.ParseAddress(url)
	if err != nil {
		return err
	}

	// verify new peer before creating client/tap
	err = o.addPeer(url, nil)
	if err != nil {
		return err
	}

	peer, err := NewL2Peer(addr, o.config)
	if err != nil {
		return err
	}

	tap, err := NewL2Tap()
	if err != nil {
		return err
	}

	err = o.link(tap.Name())
	if err != nil {
		return err
	}

	peer.Start(tap)

	err = o.addPeer(url, peer)
	if err != nil {
		peer.Stop()
		return err
	}
	return nil
}

func (o *L2Overlay) addPeer(url string, peer *L2Peer) error {
	o.peerMutex.Lock()
	defer o.peerMutex.Unlock()

	_, present := o.peers[url]
	if present {
		return fmt.Errorf("already connected to peer %s", url)
	}
	if peer != nil {
		o.peers[url] = peer
	}
	return nil
}

func (o *L2Overlay) DeletePeer(url string) error {
	peer, err := o.removePeer(url)
	if err != nil {
		return err
	}
	peer.Stop()
	return nil
}

func (o *L2Overlay) removePeer(url string) (*L2Peer, error) {
	o.peerMutex.Lock()
	defer o.peerMutex.Unlock()

	peer, present := o.peers[url]
	if !present {
		return nil, fmt.Errorf("no such peer %s", url)
	}
	delete(o.peers, url)
	return peer, nil
}

func (o *L2Overlay) ListPeers() map[string]string {
	o.peerMutex.Lock()
	defer o.peerMutex.Unlock()

	m := make(map[string]string)
	for k, v := range o.peers {
		m[k] = v.Name()
	}
	return m
}

func (o *L2Overlay) cliAddPeer(args ...string) (string, error) {
	peerURL := args[0]

	err := o.AddPeer(peerURL)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("Added peer %s", peerURL), nil
}

func (o *L2Overlay) cliDelPeer(args ...string) (string, error) {
	peerURL := args[0]

	err := o.AddPeer(peerURL)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("Deleted peer %s", peerURL), nil
}

func (o *L2Overlay) cliPeers(args ...string) (string, error) {
	return fmt.Sprintf("Peers: %s", mapValues(o.ListPeers())), nil
}

func (o *L2Overlay) cliClients(args ...string) (string, error) {
	return o.listClients(), nil
}

func (o *L2Overlay) cliAttach(args ...string) (string, error) {
	container := args[0]

	localIface, err := o.attach(container)
	if err != nil {
		return "", err
	}
	mtuStr := strconv.Itoa(int(o.mtu))
	_, err = o.invoker.Execute("attach", o.bridge, mtuStr, container, localIface, containerIface)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("Attached to %s", container), nil
}

func (o *L2Overlay) cliDetach(args ...string) (string, error) {
	container := args[0]
	_ = container

	localIface, err := o.detach(container)
	if err != nil {
		return "", err
	}
	_, err = o.invoker.Execute("detach", o.bridge, localIface)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("Detached from %s", container), nil
}

func (o *L2Overlay) cliConnections(args ...string) (string, error) {
	return o.containerConnections(), nil
}

func (o *L2Overlay) attach(container string) (string, error) {
	o.connMutex.Lock()
	defer o.connMutex.Unlock()

	_, present := o.connections[container]
	if present {
		return "", fmt.Errorf("already attached to container %s", container)
	}
	i := o.ifIndex
	o.ifIndex++
	localIface := fmt.Sprintf(localIfaceTemplate, o.bridge, i)
	o.connections[container] = localIface
	return localIface, nil
}

func (o *L2Overlay) detach(container string) (string, error) {
	o.connMutex.Lock()
	defer o.connMutex.Unlock()

	localIface, present := o.connections[container]
	if !present {
		return "", fmt.Errorf("not attached to container %s", container)
	}
	delete(o.connections, container)
	return localIface, nil
}

func (o *L2Overlay) containerConnections() string {
	o.connMutex.Lock()
	defer o.connMutex.Unlock()

	return fmt.Sprintf("Connections: %s", mapValues(o.connections))
}

func (o *L2Overlay) accept(listener net.Listener) {
	defer listener.Close()

	log.Printf("Listening on %s", listener.Addr())
	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Printf("Error accepting connections: %s", err)
			return
		}
		go o.service(conn)
	}
}

func (o *L2Overlay) service(conn net.Conn) {
	client := conn.RemoteAddr()
	defer func() {
		conn.Close()
		log.Printf("Client disconnected: %s", conn.RemoteAddr())
	}()

	log.Printf("Inbound connection: %s", client)

	tap, err := NewL2Tap()
	if err != nil {
		log.Printf("Error creating tap: %s", err)
		return
	}
	defer tap.Close()

	tapName := tap.Name()
	err = o.link(tapName)
	if err != nil {
		log.Printf("Error linking tap to bridge: %s", err)
		return
	}

	o.clientConnected(client, tapName)
	defer o.clientDisconnected(client)

	tap.Forward(conn)
}

func (o *L2Overlay) link(tapName string) error {
	mtuStr := strconv.Itoa(int(o.mtu))
	_, err := o.invoker.Execute("link", o.bridge, mtuStr, tapName)
	return err
}

func (o *L2Overlay) clientConnected(addr net.Addr, downlinkIface string) {
	o.clientMutex.Lock()
	defer o.clientMutex.Unlock()

	o.clients[addr.String()] = downlinkIface
}

func (o *L2Overlay) clientDisconnected(addr net.Addr) {
	o.clientMutex.Lock()
	defer o.clientMutex.Unlock()

	delete(o.clients, addr.String())
}

func (o *L2Overlay) listClients() string {
	o.clientMutex.Lock()
	defer o.clientMutex.Unlock()

	return fmt.Sprintf("Clients: %s", mapValues(o.clients))
}

func mapValues(m map[string]string) string {
	var entries []string
	for k, v := range m {
		entries = append(entries, fmt.Sprintf("%s via %s", k, v))
	}
	return strings.Join(entries, ", ")
}
