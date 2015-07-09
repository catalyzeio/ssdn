package overlay

import (
	"fmt"
	"net"
	"strconv"
	"sync"

	"github.com/catalyzeio/go-core/actions"
)

type L3Bridge struct {
	name string
	mtu  string

	invoker *actions.Invoker

	network *net.IPNet
	pool    *IPPool
	gwIP    net.IP

	tap *L3Tap

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

func NewL3Bridge(name string, mtu uint16, actionsDir string, network *net.IPNet, pool *IPPool, gwIP net.IP) *L3Bridge {
	return &L3Bridge{
		name: name,
		mtu:  strconv.Itoa(int(mtu)),

		invoker: actions.NewInvoker(actionsDir),

		network: network,
		pool:    pool,
		gwIP:    gwIP,

		connections: make(map[string]*l3Interface),
	}
}

func (b *L3Bridge) Start(tap *L3Tap) error {
	b.invoker.Start()

	if _, err := b.invoker.Execute("create", b.name); err != nil {
		return err
	}
	log.Info("Created bridge %s", b.name)

	// TODO restore existing state (bridge, veth pairs kept)
	// TODO recover on reboots (bridge, veth pairs killed)

	b.tap = tap

	return nil
}

func (b *L3Bridge) Attach(container string) error {
	// grab the next local interface
	iface, err := b.associate(container)
	if err != nil {
		return err
	}

	// generate a MAC address
	mac, err := RandomMAC()
	if err != nil {
		return err
	}
	iface.containerMAC = mac

	// grab the next IP
	pool := b.pool
	nextIP, err := pool.Next()
	if err != nil {
		return err
	}
	iface.containerIP = nextIP

	// attach the local interface to the bridge
	_, err = b.invoker.Execute("attach", b.name, b.mtu, container,
		iface.localIface, containerIface,
		pool.FormatIP(nextIP), mac.String(),
		b.network.String(), b.gwIP.String())
	if err != nil {
		pool.Free(nextIP)
		return err
	}

	// seed the gateway's ARP cache
	b.tap.SeedMAC(iface.containerIP, iface.containerMAC)

	return nil
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

func (b *L3Bridge) Detach(container string) error {
	// remove container association
	iface, err := b.unassociate(container)
	if err != nil {
		return err
	}

	// detach local interface from the bridge
	_, err = b.invoker.Execute("detach", b.name, iface.localIface)

	// unconditionally clean up resources that were allocated to the interface
	b.tap.UnseedMAC(iface.containerIP)
	b.pool.Free(iface.containerIP)

	// return any errors that occurred when detaching the interface from the bridge
	return err
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

type L3Connection struct {
	Interface string
	IP        net.IP
}

func (b *L3Bridge) Connections() map[string]*L3Connection {
	b.connMutex.Lock()
	defer b.connMutex.Unlock()

	result := make(map[string]*L3Connection, len(b.connections))
	for k, v := range b.connections {
		ip := net.IP(IntToIPv4(v.containerIP))
		l3conn := &L3Connection{v.localIface, ip}
		result[k] = l3conn
	}
	return result
}

func (b *L3Bridge) link(tapName string) error {
	_, err := b.invoker.Execute("link", b.name, b.mtu, tapName)
	return err
}
