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
	// TODO restore existing state

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

// TODO Stop function

func (o *L2Link) cliAddPeer(args ...string) (string, error) {
	peerURL := args[0]
	_ = peerURL

	return "TODO", nil
}

func (o *L2Link) cliDelPeer(args ...string) (string, error) {
	peerURL := args[0]
	_ = peerURL

	return "TODO", nil
}

func (o *L2Link) cliPeers(args ...string) (string, error) {
	return "TODO", nil
}

func (o *L2Link) cliClients(args ...string) (string, error) {
	return "TODO", nil
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

	var links []string
	for k, v := range o.connections {
		links = append(links, fmt.Sprintf("%s via %s", k, v))
	}
	return fmt.Sprintf("Connections: %s", strings.Join(links, ", "))
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
		o.service(conn)
	}
}

func (o *L2Link) service(conn net.Conn) {
	defer conn.Close()

	log.Printf("Inbound connection: %s", conn.RemoteAddr())
	// TODO
}
