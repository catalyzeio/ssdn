package overlay

import (
	"crypto/tls"

	"github.com/catalyzeio/shadowfax/cli"
	"github.com/catalyzeio/shadowfax/proto"
)

type L3Listener struct {
}

func NewL3Listener(address *proto.Address, config *tls.Config) *L3Listener {
	// TODO
	return &L3Listener{}
}

func (l *L3Listener) Start(cli *cli.Listener) error {
	// TODO
	return nil
}
