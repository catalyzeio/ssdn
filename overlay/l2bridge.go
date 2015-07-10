package overlay

import (
	"fmt"
	"strconv"
	"sync"

	"github.com/catalyzeio/go-core/actions"
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

func (b *L2Bridge) Start() error {
	b.invoker.Start()

	if _, err := b.invoker.Execute("create", b.name); err != nil {
		return err
	}
	log.Info("Created bridge %s", b.name)

	// TODO restore existing state (bridge, veth pairs kept)
	// TODO recover on reboots (bridge, veth pairs killed)

	return nil
}

func (b *L2Bridge) Attach(container string) error {
	localIface, err := b.associate(container)
	if err != nil {
		return err
	}
	_, err = b.invoker.Execute("attach", b.name, b.mtu, container,
		localIface, containerIface)
	if err != nil {
		return err
	}
	return nil
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

func (b *L2Bridge) Detach(container string) error {
	localIface, err := b.unassociate(container)
	if err != nil {
		return err
	}
	if _, err := b.invoker.Execute("detach", b.name, localIface); err != nil {
		return err
	}
	return nil
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

func (b *L2Bridge) ListConnections() map[string]*ConnectionDetails {
	b.connMutex.Lock()
	defer b.connMutex.Unlock()

	result := make(map[string]*ConnectionDetails, len(b.connections))
	for k, v := range b.connections {
		result[k] = &ConnectionDetails{
			Interface: v,
		}
	}
	return result
}

func (b *L2Bridge) link(tapName string) error {
	_, err := b.invoker.Execute("link", b.name, b.mtu, tapName)
	return err
}
