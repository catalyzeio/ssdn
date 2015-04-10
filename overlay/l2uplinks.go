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

func (u *L2Uplinks) Start(cli *cli.Listener) {
	cli.Register("adduplink", "[proto://host:port]", "Adds an uplink at the specified address", 1, 1, u.cliAddUplink)
	cli.Register("deluplink", "[proto://host:port]", "Deletes the uplink at the specified address", 1, 1, u.cliDelUplink)
	cli.Register("uplinks", "", "List all active uplinks", 0, 0, u.cliUplinks)
}

func (u *L2Uplinks) cliAddUplink(args ...string) (string, error) {
	uplinkURL := args[0]

	err := u.AddUplink(uplinkURL)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("Added uplink %s", uplinkURL), nil
}

func (u *L2Uplinks) AddUplink(url string) error {
	addr, err := proto.ParseAddress(url)
	if err != nil {
		return err
	}

	// verify no existing uplink before creating client/tap
	err = u.addUplink(url, nil)
	if err != nil {
		return err
	}

	uplink, err := NewL2Uplink(addr, u.config)
	if err != nil {
		return err
	}

	tap, err := NewL2Tap()
	if err != nil {
		return err
	}

	err = u.bridge.link(tap.Name())
	if err != nil {
		return err
	}

	err = u.addUplink(url, uplink)
	if err != nil {
		return err
	}

	uplink.Start(tap)
	return nil
}

func (u *L2Uplinks) addUplink(url string, uplink *L2Uplink) error {
	u.uplinksMutex.Lock()
	defer u.uplinksMutex.Unlock()

	_, present := u.uplinks[url]
	if present {
		return fmt.Errorf("already connected to uplink %s", url)
	}
	if uplink != nil {
		u.uplinks[url] = uplink
	}
	return nil
}

func (u *L2Uplinks) cliDelUplink(args ...string) (string, error) {
	uplinkURL := args[0]

	err := u.DeleteUplink(uplinkURL)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("Deleted uplink %s", uplinkURL), nil
}

func (u *L2Uplinks) DeleteUplink(url string) error {
	uplink, err := u.removeUplink(url)
	if err != nil {
		return err
	}
	uplink.Stop()
	return nil
}

func (u *L2Uplinks) removeUplink(url string) (*L2Uplink, error) {
	u.uplinksMutex.Lock()
	defer u.uplinksMutex.Unlock()

	uplink, present := u.uplinks[url]
	if !present {
		return nil, fmt.Errorf("no such uplink %s", url)
	}
	delete(u.uplinks, url)
	return uplink, nil
}

func (u *L2Uplinks) cliUplinks(args ...string) (string, error) {
	return fmt.Sprintf("Uplinks: %s", mapValues(u.ListUplinks())), nil
}

func (u *L2Uplinks) ListUplinks() map[string]string {
	u.uplinksMutex.Lock()
	defer u.uplinksMutex.Unlock()

	m := make(map[string]string)
	for k, v := range u.uplinks {
		m[k] = v.Name()
	}
	return m
}
