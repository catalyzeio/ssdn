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

	state *State

	invoker *actions.Invoker

	connMutex   sync.Mutex
	connections map[string]string
	ifIndex     int
}

const (
	localL2IfaceTemplate = "sf2.%s.%d"
	containerIface       = "eth1"
)

func NewL2Bridge(name string, mtu uint16, state *State, actionsDir string) *L2Bridge {
	return &L2Bridge{
		name: name,
		mtu:  strconv.Itoa(int(mtu)),

		state: state,

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

	return nil
}

func (b *L2Bridge) Restore() error {
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
				b.connections[k] = ifName
				log.Info("Restored state for %s", k)
				continue
			}

			log.Warn("Interface %s for connection %s not present; reattaching", ifName, k)
			if err := b.Attach(k, ""); err != nil {
				log.Warn("Failed to reattach to %s: %s", k, err)
				continue
			}
			log.Info("Reattached to %s", k)
		}
	}

	return nil
}

func (b *L2Bridge) Attach(container, ip string) error {
	if len(ip) > 0 {
		return fmt.Errorf("cannot attach with IP address (%s); unsupported operation", ip)
	}

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
	b.state.Update(b.snapshot())
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
	b.state.Update(b.snapshot())
	return localIface, nil
}

func (b *L2Bridge) ListConnections() map[string]*ConnectionDetails {
	b.connMutex.Lock()
	defer b.connMutex.Unlock()

	return b.snapshot().Connections
}

func (b *L2Bridge) snapshot() *Snapshot {
	result := make(map[string]*ConnectionDetails, len(b.connections))
	for k, v := range b.connections {
		result[k] = &ConnectionDetails{
			Interface: v,
		}
	}
	return &Snapshot{Connections: result}
}

func (b *L2Bridge) link(tapName string) error {
	_, err := b.invoker.Execute("link", b.name, b.mtu, tapName)
	return err
}
