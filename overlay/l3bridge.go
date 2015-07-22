package overlay

import (
	"fmt"
	"net"
	"strconv"
	"sync"

	"github.com/catalyzeio/go-core/actions"
	"github.com/catalyzeio/go-core/comm"
)

type L3Bridge struct {
	name string
	mtu  string

	state *State

	invoker *actions.Invoker

	network *net.IPNet
	pool    *comm.IPPool
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

func NewL3Bridge(name string, mtu uint16, state *State, actionsDir string, network *net.IPNet, pool *comm.IPPool, gwIP net.IP) *L3Bridge {
	return &L3Bridge{
		name: name,
		mtu:  strconv.Itoa(int(mtu)),

		state: state,

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

	b.tap = tap
	return nil
}

func (b *L3Bridge) Restore() error {
	snapshot, err := b.state.Load()
	if err != nil {
		return err
	}

	ifNames, err := GetInterfaces()
	if err != nil {
		return err
	}

	if snapshot != nil {
		for k, v := range snapshot.Connections {
			if v == nil {
				continue
			}

			ifName := v.Interface
			_, present := ifNames[ifName]
			if present {
				if err := b.restoreData(k, v); err != nil {
					log.Warn("Failed to restore state for %s: %s", k, err)
				}
				continue
			}

			log.Warn("Interface %s for connection %s not present; reattaching", ifName, k)
			if err := b.Attach(k, v.IP); err != nil {
				log.Warn("Failed to reattach to %s: %s", k, err)
				continue
			}
			log.Info("Reattached to %s", k)
		}
	}

	return nil
}

func (b *L3Bridge) restoreData(container string, d *ConnectionDetails) error {
	i, err := toL3Interface(d)
	if err != nil {
		return err
	}
	b.connections[container] = i
	log.Info("Restored state for %s", container)
	return nil
}

func toL3Interface(c *ConnectionDetails) (*l3Interface, error) {
	parsed := net.ParseIP(c.IP)
	if parsed == nil {
		return nil, fmt.Errorf("invalid IP address: %s", c.IP)
	}
	convertedIP, err := comm.IPToInt(parsed)
	if err != nil {
		return nil, err
	}

	mac, err := net.ParseMAC(c.MAC)
	if err != nil {
		return nil, err
	}

	return &l3Interface{c.Interface, convertedIP, mac}, nil
}

func (b *L3Bridge) Attach(container, ip string) error {
	// generate a MAC address
	mac, err := RandomMAC()
	if err != nil {
		return err
	}

	// grab the requested IP, or the next available IP
	var nextIP uint32
	pool := b.pool
	if len(ip) > 0 {
		nextIP, err = pool.AcquireFromString(ip)
	} else {
		nextIP, err = pool.Next()
	}
	if err != nil {
		return err
	}

	// grab the next local interface
	iface, err := b.associate(container, nextIP, mac)
	if err != nil {
		pool.Release(nextIP)
		return err
	}

	// attach the local interface to the bridge
	_, err = b.invoker.Execute("attach", b.name, b.mtu, container,
		iface.localIface, containerIface,
		pool.FormatIP(nextIP), mac.String(),
		b.network.String(), b.gwIP.String())
	if err != nil {
		pool.Release(nextIP)
		return err
	}

	// seed the gateway's ARP cache
	b.tap.SeedMAC(iface.containerIP, iface.containerMAC)

	return nil
}

func (b *L3Bridge) associate(container string, ip uint32, mac net.HardwareAddr) (*l3Interface, error) {
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

		containerIP:  ip,
		containerMAC: mac,
	}
	b.connections[container] = iface
	b.state.Update(b.snapshot())
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
	b.pool.Release(iface.containerIP)

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
	b.state.Update(b.snapshot())
	return iface, nil
}

func (b *L3Bridge) UpdateConnections(connections map[string]string) {
	removed := make(map[string]struct{})
	added := make(map[string]string)
	b.processUpdate(connections, removed, added)

	for container := range removed {
		log.Info("Removing obsolete container %s", container)
		if err := b.Detach(container); err != nil {
			log.Warn("Failed to detach from container %s: %s", container, err)
		}
	}

	for container, ip := range added {
		log.Info("Discovered container %s", container)
		if err := b.Attach(container, ip); err != nil {
			log.Warn("Failed to attach to container %s: %s", container, err)
		}
	}
}

func (b *L3Bridge) processUpdate(connections map[string]string, removed map[string]struct{}, added map[string]string) {
	b.connMutex.Lock()
	defer b.connMutex.Unlock()

	// record which containers were removed
	for container := range b.connections {
		if _, present := connections[container]; !present {
			removed[container] = struct{}{}
		}
	}

	// record which containers were added
	for container, ip := range connections {
		if _, present := b.connections[container]; !present {
			added[container] = ip
		}
	}
}

func (b *L3Bridge) ListConnections() map[string]*ConnectionDetails {
	b.connMutex.Lock()
	defer b.connMutex.Unlock()

	return b.snapshot().Connections
}

func (b *L3Bridge) snapshot() *Snapshot {
	result := make(map[string]*ConnectionDetails, len(b.connections))
	for k, v := range b.connections {
		ip := net.IP(comm.IntToIPv4(v.containerIP))
		result[k] = &ConnectionDetails{
			Interface: v.localIface,
			IP:        ip.String(),
			MAC:       v.containerMAC.String(),
		}
	}
	return &Snapshot{Connections: result}
}

func (b *L3Bridge) link(tapName string) error {
	_, err := b.invoker.Execute("link", b.name, b.mtu, tapName)
	return err
}
