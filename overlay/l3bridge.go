package overlay

import (
	"fmt"
	"net"

	"github.com/catalyzeio/shadowfax/cli"
)

type L3Bridge struct {
	*L2Bridge

	pool *IPPool
	tap  *L3Tap
}

const (
	localL3IfaceTemplate = "sf3.%s.%d"
)

func NewL3Bridge(name string, mtu uint16, actionsDir string, pool *IPPool) *L3Bridge {
	return &L3Bridge{
		L2Bridge: NewL2Bridge(name, mtu, actionsDir),

		pool: pool,
	}
}

func (b *L3Bridge) Start(cli *cli.Listener, tap *L3Tap) error {
	err := b.L2Bridge.Start(cli)
	if err != nil {
		return err
	}

	b.tap = tap

	// override attach action (action script uses different arguments)
	cli.Register("attach", "[container]", "Attaches the given container to this overlay network", 1, 1, b.cliAttach)

	return nil
}

func (b *L3Bridge) cliAttach(args ...string) (string, error) {
	container := args[0]

	// grab the next local interface
	localIface, err := b.associate(localL3IfaceTemplate, container)
	if err != nil {
		return "", err
	}

	// grab the next IP
	pool := b.pool
	nextIP, err := pool.Next()
	if err != nil {
		return "", err
	}

	// attach the local interface to the bridge
	_, err = b.invoker.Execute("attach", b.name, b.mtu, container, localIface, containerIface,
		pool.FormatIP(nextIP), pool.FormatNetwork(), pool.FormatGatewayIP())
	if err != nil {
		return "", err
	}

	// seed the gateway's ARP cache
	containerIP := net.IP(IntToIPv4(nextIP))
	_, err = b.tap.Resolve(containerIP)
	if err != nil {
		log.Warn("Failed to resolve MAC address for IP %s in container %s", containerIP, container)
	}

	return fmt.Sprintf("Attached to %s", container), nil
}
