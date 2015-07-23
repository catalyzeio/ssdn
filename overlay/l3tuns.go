package overlay

import (
	"fmt"
	"net"
	"strconv"
	"sync"

	"github.com/catalyzeio/go-core/actions"
	"github.com/catalyzeio/go-core/comm"
)

type L3Tuns struct {
	subnet *IPv4Route
	routes *RouteTracker
	mtu    uint16

	state *State

	invoker *actions.Invoker

	network *net.IPNet
	pool    *comm.IPPool

	connMutex   sync.Mutex
	connections map[string]*L3Tun
}

func NewL3Tuns(subnet *IPv4Route, routes *RouteTracker, mtu uint16, state *State, actionsDir string, network *net.IPNet, pool *comm.IPPool) *L3Tuns {
	return &L3Tuns{
		subnet: subnet,
		routes: routes,
		mtu:    mtu,

		state: state,

		invoker: actions.NewInvoker(actionsDir),

		network: network,
		pool:    pool,

		connections: make(map[string]*L3Tun),
	}
}

func (t *L3Tuns) Start() {
	t.invoker.Start()
}

func (t *L3Tuns) Restore() error {
	snapshot, err := t.state.Load()
	if err != nil {
		return err
	}

	if snapshot != nil {
		for k, v := range snapshot.Connections {
			if v == nil {
				continue
			}

			if err := t.Attach(k, v.IP); err != nil {
				log.Warn("Failed to reattach to %s: %s", k, err)
				continue
			}
			log.Info("Reattached to %s", k)
		}
	}
	return nil
}

func (t *L3Tuns) InboundHandler(packet *PacketBuffer) error {
	t.routes.RoutePacket(packet)
	return nil
}

func (t *L3Tuns) Attach(container, ip string) error {
	// verify no existing attachment before creating tun
	if err := t.associate(container, nil); err != nil {
		return err
	}

	// grab the requested IP, or the next available IP
	var nextIP uint32
	var err error
	pool := t.pool
	if len(ip) > 0 {
		nextIP, err = pool.AcquireFromString(ip)
	} else {
		nextIP, err = pool.Next()
	}
	if err != nil {
		return err
	}

	// create and start the tun device
	tun := NewL3Tun(container, nextIP, t)
	if err := tun.Start(); err != nil {
		pool.Release(nextIP)
		return err
	}

	// record the successful association
	if err := t.associate(container, tun); err != nil {
		return err
	}

	return nil
}

func (t *L3Tuns) associate(container string, tun *L3Tun) error {
	t.connMutex.Lock()
	defer t.connMutex.Unlock()

	_, present := t.connections[container]
	if present {
		return fmt.Errorf("already attached to container %s", container)
	}
	if tun != nil {
		t.connections[container] = tun
		t.state.Update(t.snapshot())
	}
	return nil
}

func (t *L3Tuns) Detach(container string) error {
	// remove container association
	tun, err := t.unassociate(container)
	if err != nil {
		return err
	}

	// clean up resources that were allocated to the interface
	tun.Stop()
	t.pool.Release(tun.ip)

	return nil
}

func (t *L3Tuns) unassociate(container string) (*L3Tun, error) {
	t.connMutex.Lock()
	defer t.connMutex.Unlock()

	tun, present := t.connections[container]
	if !present {
		return nil, fmt.Errorf("not attached to container %s", container)
	}
	delete(t.connections, container)
	t.state.Update(t.snapshot())
	return tun, nil
}

func (t *L3Tuns) UpdateConnections(connections map[string]string) {
	removed := make(map[string]struct{})
	added := make(map[string]string)
	t.processUpdate(connections, removed, added)

	for container := range removed {
		log.Info("Removing obsolete container %s", container)
		if err := t.Detach(container); err != nil {
			log.Warn("Failed to detach from container %s: %s", container, err)
		}
	}

	for container, ip := range added {
		log.Info("Discovered container %s", container)
		if err := t.Attach(container, ip); err != nil {
			log.Warn("Failed to attach to container %s: %s", container, err)
		}
	}
}

func (t *L3Tuns) processUpdate(connections map[string]string, removed map[string]struct{}, added map[string]string) {
	t.connMutex.Lock()
	defer t.connMutex.Unlock()

	// record which containers were removed
	for container := range t.connections {
		if _, present := connections[container]; !present {
			removed[container] = struct{}{}
		}
	}

	// record which containers were added
	for container, ip := range connections {
		if _, present := t.connections[container]; !present {
			added[container] = ip
		}
	}
}

func (t *L3Tuns) ListConnections() map[string]*ConnectionDetails {
	t.connMutex.Lock()
	defer t.connMutex.Unlock()

	return t.snapshot().Connections
}

func (t *L3Tuns) snapshot() *Snapshot {
	result := make(map[string]*ConnectionDetails, len(t.connections))
	for k, v := range t.connections {
		ip := comm.IntToIP(v.ip)
		result[k] = &ConnectionDetails{
			IP: ip.String(),
		}
	}
	return &Snapshot{Connections: result}
}

func (t *L3Tuns) inject(container string, iface string, ip uint32) error {
	mtu := strconv.Itoa(int(t.mtu))
	_, err := t.invoker.Execute("inject", mtu, container, iface, containerIface,
		comm.FormatIPWithMask(ip, t.network.Mask))
	return err
}
