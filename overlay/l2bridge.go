package overlay

import (
	"fmt"
	"strconv"
	"sync"

	"github.com/catalyzeio/shadowfax/actions"
	"github.com/catalyzeio/shadowfax/cli"
)

type L2Bridge struct {
	name string
	mtu  string

	invoker *actions.Invoker

	connMutex   sync.Mutex
	connections map[string]string
	ifIndex     int
}

const (
	localL2IfaceTemplate = "sf2.%s.%d"
	containerIface       = "eth1"
)

func NewL2Bridge(name string, mtu uint16, actionsDir string) *L2Bridge {
	return &L2Bridge{
		name: name,
		mtu:  strconv.Itoa(int(mtu)),

		invoker: actions.NewInvoker(actionsDir),

		connections: make(map[string]string),
	}
}

func (b *L2Bridge) Start(cli *cli.Listener) error {
	b.invoker.Start()

	if _, err := b.invoker.Execute("create", b.name); err != nil {
		return err
	}
	log.Info("Created bridge %s", b.name)

	// TODO restore existing state (bridge, veth pairs kept)
	// TODO recover on reboots (bridge, veth pairs killed)

	cli.Register("attach", "[container]", "Attaches the given container to this overlay network", 1, 1, b.cliAttach)
	cli.Register("detach", "[container]", "Detaches the given container from this overlay network", 1, 1, b.cliDetach)
	cli.Register("connections", "", "Lists all containers attached to this overlay network", 0, 0, b.cliConnections)

	return nil
}

func (b *L2Bridge) UpdatePeers(peerURLs map[string]struct{}) {
	// TODO
}

func (b *L2Bridge) cliAttach(args ...string) (string, error) {
	container := args[0]

	localIface, err := b.associate(container)
	if err != nil {
		return "", err
	}
	_, err = b.invoker.Execute("attach", b.name, b.mtu, container,
		localIface, containerIface)
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("Attached to %s", container), nil
}

func (b *L2Bridge) associate(container string) (string, error) {
	b.connMutex.Lock()
	defer b.connMutex.Unlock()

	_, present := b.connections[container]
	if present {
		return "", fmt.Errorf("already attached to container %s", container)
	}
	i := b.ifIndex
	b.ifIndex++
	localIface := fmt.Sprintf(localL2IfaceTemplate, b.name, i)
	b.connections[container] = localIface
	return localIface, nil
}

func (b *L2Bridge) cliDetach(args ...string) (string, error) {
	container := args[0]

	localIface, err := b.unassociate(container)
	if err != nil {
		return "", err
	}
	if _, err := b.invoker.Execute("detach", b.name, localIface); err != nil {
		return "", err
	}
	return fmt.Sprintf("Detached from %s", container), nil
}

func (b *L2Bridge) unassociate(container string) (string, error) {
	b.connMutex.Lock()
	defer b.connMutex.Unlock()

	localIface, present := b.connections[container]
	if !present {
		return "", fmt.Errorf("not attached to container %s", container)
	}
	delete(b.connections, container)
	return localIface, nil
}

func (b *L2Bridge) cliConnections(args ...string) (string, error) {
	return b.listConnections(), nil
}

func (b *L2Bridge) listConnections() string {
	b.connMutex.Lock()
	defer b.connMutex.Unlock()

	return fmt.Sprintf("Connections: %s", mapValues(b.connections))
}

func (b *L2Bridge) link(tapName string) error {
	_, err := b.invoker.Execute("link", b.name, b.mtu, tapName)
	return err
}
