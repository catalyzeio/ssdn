package overlay

import (
	"fmt"
	"net"
	"strconv"
	"sync"

	"github.com/catalyzeio/go-core/actions"
)

type L3Tuns struct {
	subnet *IPv4Route
	routes *RouteTracker
	mtu    uint16

	invoker *actions.Invoker

	network *net.IPNet
	pool    *IPPool

	connMutex   sync.Mutex
	connections map[string]*L3Tun
}

func NewL3Tuns(subnet *IPv4Route, routes *RouteTracker, mtu uint16, actionsDir string, network *net.IPNet, pool *IPPool) *L3Tuns {
	return &L3Tuns{
		subnet: subnet,
		routes: routes,
		mtu:    mtu,

		invoker: actions.NewInvoker(actionsDir),

		network: network,
		pool:    pool,

		connections: make(map[string]*L3Tun),
	}
}

func (t *L3Tuns) Start() {
	t.invoker.Start()

	// TODO reattach to containers on restarts
}

func (t *L3Tuns) InboundHandler(packet *PacketBuffer) error {
	t.routes.RoutePacket(packet)
	return nil
}

func (t *L3Tuns) Attach(container string) error {
	// verify no existing attachment before creating tun
	if err := t.associate(container, nil); err != nil {
		return err
	}

	// grab the next IP
	pool := t.pool
	nextIP, err := pool.Next()
	if err != nil {
		return err
	}

	// create and start the tun device
	tun := NewL3Tun(container, nextIP, t)
	if err := tun.Start(); err != nil {
		pool.Free(nextIP)
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
	t.pool.Free(tun.ip)

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
	return tun, nil
}

func (t *L3Tuns) Connections() map[string]string {
	t.connMutex.Lock()
	defer t.connMutex.Unlock()

	result := make(map[string]string, len(t.connections))
	for k, v := range t.connections {
		ip := net.IP(IntToIPv4(v.ip))
		result[k] = ip.String()
	}
	return result
}

func (t *L3Tuns) inject(container string, iface string, ip uint32) error {
	mtu := strconv.Itoa(int(t.mtu))
	_, err := t.invoker.Execute("inject", mtu, container, iface, containerIface,
		FormatIPWithMask(ip, t.network.Mask))
	return err
}
