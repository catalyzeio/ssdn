package overlay

import (
	"fmt"
	"net"
	"strings"
	"sync"

	"github.com/catalyzeio/shadowfax/actions"
	"github.com/catalyzeio/shadowfax/cli"
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

	localRoutes *RouteTracker
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

		localRoutes: NewRouteTracker(),
	}
}

func (t *L3Tuns) Start(cli *cli.Listener) {
	// TODO reattach to containers on restarts

	// rename local routes CLI action to avoid conflict with remote routes
	t.localRoutes.StartAs(cli, "local", "List all local routes")

	cli.Register("attach", "[container]", "Attaches the given container to this overlay network", 1, 1, t.cliAttach)
	cli.Register("detach", "[container]", "Detaches the given container from this overlay network", 1, 1, t.cliDetach)
	cli.Register("connections", "", "Lists all containers attached to this overlay network", 0, 0, t.cliConnections)
}

func (t *L3Tuns) InboundHandler(packet *PacketBuffer) error {
	t.localRoutes.RoutePacket(packet)
	return nil
}

func (t *L3Tuns) cliAttach(args ...string) (string, error) {
	container := args[0]

	// verify no existing attachment before creating tun
	err := t.associate(container, nil)
	if err != nil {
		return "", err
	}

	// grab the next IP
	pool := t.pool
	nextIP, err := pool.Next()
	if err != nil {
		return "", err
	}

	// create and start the tun device
	tun := NewL3Tun(container, nextIP, t)
	err = tun.Start()
	if err != nil {
		pool.Free(nextIP)
		return "", err
	}

	// record the successful association
	err = t.associate(container, tun)
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("Attached to %s", container), nil
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

func (t *L3Tuns) cliDetach(args ...string) (string, error) {
	container := args[0]

	// remove container association
	tun, err := t.unassociate(container)
	if err != nil {
		return "", err
	}

	// clean up resources that were allocated to the interface
	tun.Stop()
	t.pool.Free(tun.ip)

	return fmt.Sprintf("Detached from %s", container), nil
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

func (t *L3Tuns) cliConnections(args ...string) (string, error) {
	return t.listConnections(), nil
}

func (t *L3Tuns) listConnections() string {
	t.connMutex.Lock()
	defer t.connMutex.Unlock()

	return fmt.Sprintf("Connections: %s", mapL3TunInterfaces(t.connections))
}

func mapL3TunInterfaces(m map[string]*L3Tun) string {
	var entries []string
	for k, v := range m {
		entries = append(entries, fmt.Sprintf("%s (%s)", k, v.ip))
	}
	return strings.Join(entries, ", ")
}

func (t *L3Tuns) inject(container string, iface string, ip uint32) error {
	_, err := t.invoker.Execute("inject", container, iface, containerIface,
		FormatIPWithMask(ip, t.network.Mask))
	return err
}
