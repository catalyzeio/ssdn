package overlay

import (
	"github.com/catalyzeio/shadowfax/cli"
)

type L3Bridge struct {
	*L2Bridge
}

func NewL3Bridge(name string, mtu uint16, actionsDir string) *L3Bridge {
	return &L3Bridge{NewL2Bridge(name, mtu, actionsDir)}
}

func (b *L3Bridge) Start(cli *cli.Listener) error {
	err := b.L2Bridge.Start(cli)
	if err != nil {
		return err
	}

	// TODO override cli actions and method for attach (need to inject route)

	return nil
}
