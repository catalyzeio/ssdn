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

type L2Link struct {
	tenantID string
	mtu      uint16

	listenAddress *proto.Address
	config        *tls.Config

	invoker *actions.Invoker
	cli     *cli.Listener

	connMutex   sync.Mutex
	connections map[string]string
	ifIndex     int

	clientMutex sync.Mutex
	clients     map[string]string
}

const (
	localIfaceTemplate = "sfl2.%s.%d"
	containerIface     = "eth1"
)

func NewL2Link(tenantID string, mtu uint16, listenAddress *proto.Address, config *tls.Config, invoker *actions.Invoker, cli *cli.Listener) *L2Link {
	l := L2Link{
		tenantID:      tenantID,
		mtu:           mtu,
		listenAddress: listenAddress,
		config:        config,
		invoker:       invoker,
		cli:           cli,
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

func (o *L2Link) Start() error {
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
	_, err = o.invoker.Execute("create", o.tenantID)
	if err != nil {
		return err
	}

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

func (o *L2Link) AddPeer(url string) error {
	// TODO
	return fmt.Errorf("Not implemented")
}

func (o *L2Link) DeletePeer(url string) error {
	// TODO
	return fmt.Errorf("Not implemented")
}

func (o *L2Link) ListPeers() map[string]string {
	// TODO
	return nil
}

func (o *L2Link) cliAddPeer(args ...string) (string, error) {
	peerURL := args[0]

	err := o.AddPeer(peerURL)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("Added peer %s", peerURL), nil
}

func (o *L2Link) cliDelPeer(args ...string) (string, error) {
	peerURL := args[0]

	err := o.AddPeer(peerURL)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("Deleted peer %s", peerURL), nil
}

func (o *L2Link) cliPeers(args ...string) (string, error) {
	return fmt.Sprintf("Peers: %s", mapValues(o.ListPeers())), nil
}

func (o *L2Link) cliClients(args ...string) (string, error) {
	return o.listClients(), nil
}

func (o *L2Link) cliAttach(args ...string) (string, error) {
	container := args[0]

	localIface, err := o.attach(container)
	if err != nil {
		return "", err
	}
	mtuStr := strconv.Itoa(int(o.mtu))
	_, err = o.invoker.Execute("attach", o.tenantID, mtuStr, container, localIface, containerIface)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("Attached to %s", container), nil
}

func (o *L2Link) cliDetach(args ...string) (string, error) {
	container := args[0]
	_ = container

	localIface, err := o.detach(container)
	if err != nil {
		return "", err
	}
	_, err = o.invoker.Execute("detach", o.tenantID, localIface)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("Detached from %s", container), nil
}

func (o *L2Link) cliConnections(args ...string) (string, error) {
	return o.containerConnections(), nil
}

func (o *L2Link) attach(container string) (string, error) {
	o.connMutex.Lock()
	defer o.connMutex.Unlock()

	_, present := o.connections[container]
	if present {
		return "", fmt.Errorf("already attached to container %s", container)
	}
	i := o.ifIndex
	o.ifIndex++
	localIface := fmt.Sprintf(localIfaceTemplate, o.tenantID, i)
	o.connections[container] = localIface
	return localIface, nil
}

func (o *L2Link) detach(container string) (string, error) {
	o.connMutex.Lock()
	defer o.connMutex.Unlock()

	localIface, present := o.connections[container]
	if !present {
		return "", fmt.Errorf("not attached to container %s", container)
	}
	delete(o.connections, container)
	return localIface, nil
}

func (o *L2Link) containerConnections() string {
	o.connMutex.Lock()
	defer o.connMutex.Unlock()

	return fmt.Sprintf("Connections: %s", mapValues(o.connections))
}

func (o *L2Link) accept(listener net.Listener) {
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

func (o *L2Link) service(conn net.Conn) {
	var client net.Addr
	defer func() {
		conn.Close()
		if client != nil {
			o.clientDisconnected(client)
		}
	}()

	log.Printf("Inbound connection: %s", conn.RemoteAddr())

	tap, err := NewL2Tap(conn)
	if err != nil {
		log.Printf("Error creating tap: %s", err)
		return
	}

	_, err = o.invoker.Execute("link", tap.Name)
	if err != nil {
		log.Printf("Error linking tap to bridge: %s", err)
		return
	}

	client = conn.RemoteAddr()
	o.clientConnected(client, tap.Name)

	tap.Forward()
}

func (o *L2Link) clientConnected(addr net.Addr, downlinkIface string) {
	o.clientMutex.Lock()
	defer o.clientMutex.Unlock()

	o.clients[addr.String()] = downlinkIface
}

func (o *L2Link) clientDisconnected(addr net.Addr) {
	o.clientMutex.Lock()
	defer o.clientMutex.Unlock()

	delete(o.clients, addr.String())
}

func (o *L2Link) listClients() string {
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
