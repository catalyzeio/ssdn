package overlay

import (
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"

	"github.com/catalyzeio/shadowfax/actions"
	"github.com/catalyzeio/shadowfax/cli"
)

type L3Bridge struct {
	name string
	mtu  string

	invoker *actions.Invoker

	pool *IPPool
	tap  *L3Tap

	connMutex   sync.Mutex
	connections map[string]*l3Interface
	ifIndex     int
}

type l3Interface struct {
	localIface string

	containerIP  uint32
	containerMAC net.HardwareAddr
}

const (
	localL3IfaceTemplate = "sf3.%s.%d"
)

func NewL3Bridge(name string, mtu uint16, actionsDir string, pool *IPPool) *L3Bridge {
	return &L3Bridge{
		name: name,
		mtu:  strconv.Itoa(int(mtu)),

		invoker: actions.NewInvoker(actionsDir),

		connections: make(map[string]*l3Interface),

		pool: pool,
	}
}

func (b *L3Bridge) Start(cli *cli.Listener, tap *L3Tap) error {
	b.invoker.Start()

	_, err := b.invoker.Execute("create", b.name)
	if err != nil {
		return err
	}
	log.Info("Created bridge %s", b.name)

	// TODO restore existing state (bridge, veth pairs kept)
	// TODO recover on reboots (bridge, veth pairs killed)

	b.tap = tap

	cli.Register("attach", "[container]", "Attaches the given container to this overlay network", 1, 1, b.cliAttach)
	cli.Register("detach", "[container]", "Detaches the given container from this overlay network", 1, 1, b.cliDetach)
	cli.Register("connections", "", "Lists all containers attached to this overlay network", 0, 0, b.cliConnections)

	return nil
}

func (b *L3Bridge) cliAttach(args ...string) (string, error) {
	container := args[0]

	// grab the next local interface
	iface, err := b.associate(container)
	if err != nil {
		return "", err
	}

	// generate a MAC address
	mac, err := RandomMAC()
	if err != nil {
		return "", err
	}
	iface.containerMAC = mac

	// grab the next IP
	pool := b.pool
	nextIP, err := pool.Next()
	if err != nil {
		return "", err
	}
	iface.containerIP = nextIP

	// attach the local interface to the bridge
	_, err = b.invoker.Execute("attach", b.name, b.mtu, container,
		iface.localIface, containerIface,
		pool.FormatIP(nextIP), mac.String(),
		pool.FormatNetwork(), pool.FormatGatewayIP())
	if err != nil {
		return "", err
	}

	// seed the gateway's ARP cache
	b.tap.SeedMAC(iface.containerIP, iface.containerMAC)

	return fmt.Sprintf("Attached to %s", container), nil
}

func (b *L3Bridge) associate(container string) (*l3Interface, error) {
	b.connMutex.Lock()
	defer b.connMutex.Unlock()

	_, present := b.connections[container]
	if present {
		return nil, fmt.Errorf("already attached to container %s", container)
	}
	i := b.ifIndex
	b.ifIndex++
	iface := &l3Interface{
		localIface: fmt.Sprintf(localL3IfaceTemplate, b.name, i),
	}
	b.connections[container] = iface
	return iface, nil
}

func (b *L3Bridge) cliDetach(args ...string) (string, error) {
	container := args[0]

	// remove container association
	iface, err := b.unassociate(container)
	if err != nil {
		return "", err
	}

	// detach local interface from the bridge
	_, err = b.invoker.Execute("detach", b.name, iface.localIface)
	if err != nil {
		return "", err
	}

	// remove the entry from the ARP cache
	b.tap.UnseedMAC(iface.containerIP)

	return fmt.Sprintf("Detached from %s", container), nil
}

func (b *L3Bridge) unassociate(container string) (*l3Interface, error) {
	b.connMutex.Lock()
	defer b.connMutex.Unlock()

	iface, present := b.connections[container]
	if !present {
		return nil, fmt.Errorf("not attached to container %s", container)
	}
	delete(b.connections, container)
	return iface, nil
}

func (b *L3Bridge) cliConnections(args ...string) (string, error) {
	return b.listConnections(), nil
}

func (b *L3Bridge) listConnections() string {
	b.connMutex.Lock()
	defer b.connMutex.Unlock()

	return fmt.Sprintf("Connections: %s", mapInterfaces(b.connections))
}

func mapInterfaces(m map[string]*l3Interface) string {
	var entries []string
	for k, v := range m {
		ip := net.IP(IntToIPv4(v.containerIP))
		entries = append(entries, fmt.Sprintf("%s via %s (%s)", k, v.localIface, ip))
	}
	return strings.Join(entries, ", ")
}

func (b *L3Bridge) link(tapName string) error {
	_, err := b.invoker.Execute("link", b.name, b.mtu, tapName)
	return err
}
