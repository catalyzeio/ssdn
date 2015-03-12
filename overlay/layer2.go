package overlay

import (
	"crypto/tls"

	"github.com/catalyzeio/shadowfax/actions"
	"github.com/catalyzeio/shadowfax/cli"
)

type L2Link struct {
	tenID  string
	ai     *actions.Invoker
	cl     *cli.Listener
	config *tls.Config
}

func NewL2Link(tenID string, ai *actions.Invoker, cl *cli.Listener, config *tls.Config) *L2Link {
	l := L2Link{
		tenID:  tenID,
		ai:     ai,
		cl:     cl,
		config: config,
	}

	cl.Register("addpeer", "[proto://host:port]", "Adds a peer at the specified address", 1, 1, l.cliAddPeer)
	cl.Register("delpeer", "[proto://host:port]", "Deletes the peer at the specified address", 1, 1, l.cliDelPeer)
	cl.Register("peers", "", "List all active peers", 0, 0, l.cliPeers)

	cl.Register("attach", "[container]", "Attaches the given container to this overlay network", 1, 1, l.cliAttach)
	cl.Register("detach", "[container]", "Detaches the given container from this overlay network", 1, 1, l.cliDetach)
	cl.Register("connections", "", "Lists all containers attached to this overlay network", 0, 0, l.cliConnections)

	return &l
}

func (o *L2Link) Start() error {
	var err error
	initCLI := false
	initListener := false
	defer func() {
		if err != nil {
			o.ai.Stop()
			if initCLI {
				o.cl.Stop()
			}
			if initListener {
				// TODO
			}
		}
	}()

	// start action invoker
	o.ai.Start()

	// initialize bridge
	_, err = o.ai.Execute("create", o.tenID)
	if err != nil {
		return err
	}

	// initialize CLI
	err = o.cl.Start()
	if err != nil {
		return err
	}

	// initialize listener
	// TODO

	return nil
}

// TODO Stop function

func (o *L2Link) cliAddPeer(args ...string) (string, error) {
	peerURL := args[0]
	_ = peerURL

	return "TODO", nil
}

func (o *L2Link) cliDelPeer(args ...string) (string, error) {
	peerURL := args[0]
	_ = peerURL

	return "TODO", nil
}

func (o *L2Link) cliPeers(args ...string) (string, error) {
	return "TODO", nil
}

func (o *L2Link) cliAttach(args ...string) (string, error) {
	container := args[0]
	_ = container

	return "TODO", nil
}

func (o *L2Link) cliDetach(args ...string) (string, error) {
	container := args[0]
	_ = container

	return "TODO", nil
}

func (o *L2Link) cliConnections(args ...string) (string, error) {
	return "TODO", nil
}
