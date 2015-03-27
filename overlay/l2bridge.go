package overlay

import (
	"fmt"
	"strconv"
	"sync"

	"github.com/catalyzeio/shadowfax/actions"
	"github.com/catalyzeio/shadowfax/cli"
)

type L2Bridge struct {
	name   string
	mtu    uint16
	mtuStr string

	invoker *actions.Invoker

	connMutex   sync.Mutex
	connections map[string]string
	ifIndex     int
}

const (
	localIfaceTemplate = "sf2.%s.%d"
	containerIface     = "eth1"
)

func NewL2Bridge(name string, mtu uint16, actionsDir string) *L2Bridge {
	b := L2Bridge{
		name:   name,
		mtu:    mtu,
		mtuStr: strconv.Itoa(int(mtu)),

		invoker: actions.NewInvoker(actionsDir),

		connections: make(map[string]string),
	}

	return &b
}

func (b *L2Bridge) Start(cli *cli.Listener) error {
	b.invoker.Start()

	_, err := b.invoker.Execute("create", b.name)
	if err != nil {
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

func (b *L2Bridge) cliAttach(args ...string) (string, error) {
	container := args[0]

	localIface, err := b.attach(container)
	if err != nil {
		return "", err
	}
	_, err = b.invoker.Execute("attach", b.name, b.mtuStr, container, localIface, containerIface)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("Attached to %s", container), nil
}

func (b *L2Bridge) attach(container string) (string, error) {
	b.connMutex.Lock()
	defer b.connMutex.Unlock()

	_, present := b.connections[container]
	if present {
		return "", fmt.Errorf("already attached to container %s", container)
	}
	i := b.ifIndex
	b.ifIndex++
	localIface := fmt.Sprintf(localIfaceTemplate, b.name, i)
	b.connections[container] = localIface
	return localIface, nil
}

func (b *L2Bridge) cliDetach(args ...string) (string, error) {
	container := args[0]
	_ = container

	localIface, err := b.detach(container)
	if err != nil {
		return "", err
	}
	_, err = b.invoker.Execute("detach", b.name, localIface)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("Detached from %s", container), nil
}

func (b *L2Bridge) detach(container string) (string, error) {
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
	_, err := b.invoker.Execute("link", b.name, b.mtuStr, tapName)
	return err
}
