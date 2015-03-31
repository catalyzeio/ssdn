package overlay

import (
	"crypto/tls"
	"fmt"
	"sync"

	"github.com/catalyzeio/shadowfax/cli"
	"github.com/catalyzeio/shadowfax/proto"
)

type L2Uplinks struct {
	config *tls.Config
	bridge *L2Bridge

	uplinksMutex sync.Mutex
	uplinks      map[string]*L2Uplink
}

func NewL2Uplinks(config *tls.Config, bridge *L2Bridge) *L2Uplinks {
	return &L2Uplinks{
		config: config,
		bridge: bridge,

		uplinks: make(map[string]*L2Uplink),
	}
}

func (lp *L2Uplinks) Start(cli *cli.Listener) {
	cli.Register("adduplink", "[proto://host:port]", "Adds an uplink at the specified address", 1, 1, lp.cliAddUplink)
	cli.Register("deluplink", "[proto://host:port]", "Deletes the uplink at the specified address", 1, 1, lp.cliDelUplink)
	cli.Register("uplinks", "", "List all active uplinks", 0, 0, lp.cliUplinks)
}

func (lp *L2Uplinks) cliAddUplink(args ...string) (string, error) {
	uplinkURL := args[0]

	err := lp.AddUplink(uplinkURL)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("Added uplink %s", uplinkURL), nil
}

func (lp *L2Uplinks) AddUplink(url string) error {
	addr, err := proto.ParseAddress(url)
	if err != nil {
		return err
	}

	// verify new uplink before creating client/tap
	err = lp.addUplink(url, nil)
	if err != nil {
		return err
	}

	uplink, err := NewL2Uplink(addr, lp.config)
	if err != nil {
		return err
	}

	tap, err := NewL2Tap()
	if err != nil {
		return err
	}

	err = lp.bridge.link(tap.Name())
	if err != nil {
		return err
	}

	err = lp.addUplink(url, uplink)
	if err != nil {
		return err
	}

	uplink.Start(tap)
	return nil
}

func (lp *L2Uplinks) addUplink(url string, uplink *L2Uplink) error {
	lp.uplinksMutex.Lock()
	defer lp.uplinksMutex.Unlock()

	_, present := lp.uplinks[url]
	if present {
		return fmt.Errorf("already connected to uplink %s", url)
	}
	if uplink != nil {
		lp.uplinks[url] = uplink
	}
	return nil
}

func (lp *L2Uplinks) cliDelUplink(args ...string) (string, error) {
	uplinkURL := args[0]

	err := lp.DeleteUplink(uplinkURL)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("Deleted uplink %s", uplinkURL), nil
}

func (lp *L2Uplinks) DeleteUplink(url string) error {
	uplink, err := lp.removeUplink(url)
	if err != nil {
		return err
	}
	uplink.Stop()
	return nil
}

func (lp *L2Uplinks) removeUplink(url string) (*L2Uplink, error) {
	lp.uplinksMutex.Lock()
	defer lp.uplinksMutex.Unlock()

	uplink, present := lp.uplinks[url]
	if !present {
		return nil, fmt.Errorf("no such uplink %s", url)
	}
	delete(lp.uplinks, url)
	return uplink, nil
}

func (lp *L2Uplinks) cliUplinks(args ...string) (string, error) {
	return fmt.Sprintf("Uplinks: %s", mapValues(lp.ListUplinks())), nil
}

func (lp *L2Uplinks) ListUplinks() map[string]string {
	lp.uplinksMutex.Lock()
	defer lp.uplinksMutex.Unlock()

	m := make(map[string]string)
	for k, v := range lp.uplinks {
		m[k] = v.Name()
	}
	return m
}
