package overlay

import (
	"crypto/tls"
	"fmt"
	"sync"

	"github.com/catalyzeio/go-core/comm"
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

func (u *L2Uplinks) UpdatePeers(peerURLs map[string]struct{}) {
	removed := make(map[string]*L2Uplink)
	added := make(map[string]struct{})
	u.processUpdate(peerURLs, removed, added)

	for url, uplink := range removed {
		uplink.Stop()
		log.Info("Removed obsolete uplink %s", url)
	}

	for url := range added {
		log.Info("Discovered uplink %s", url)
		if err := u.AddUplink(url); err != nil {
			log.Warn("Failed to add client for uplink at %s: %s", url, err)
		}
	}
}

func (u *L2Uplinks) processUpdate(current map[string]struct{}, removed map[string]*L2Uplink, added map[string]struct{}) {
	u.uplinksMutex.Lock()
	defer u.uplinksMutex.Unlock()

	// record which uplinks were removed
	for url, uplink := range u.uplinks {
		_, present := current[url]
		if !present && !uplink.Connected() { // always keep live uplinks
			removed[url] = uplink
			delete(u.uplinks, url)
		}
	}

	// record which uplinks were added
	for url := range current {
		if _, present := u.uplinks[url]; !present {
			added[url] = struct{}{}
		}
	}
}

func (u *L2Uplinks) AddUplink(url string) error {
	addr, err := comm.ParseAddress(url)
	if err != nil {
		return err
	}

	// verify no existing uplink before creating client
	if err := u.addUplink(url, nil); err != nil {
		return err
	}

	uplink, err := NewL2Uplink(u.bridge, addr, u.config)
	if err != nil {
		return err
	}

	if err := u.addUplink(url, uplink); err != nil {
		return err
	}

	uplink.Start()
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

func (u *L2Uplinks) ListUplinks() map[string]*PeerDetails {
	u.uplinksMutex.Lock()
	defer u.uplinksMutex.Unlock()

	result := make(map[string]*PeerDetails, len(u.uplinks))
	for k, v := range u.uplinks {
		state := Connecting
		if v.Connected() {
			state = Connected
		}
		result[k] = &PeerDetails{
			Type:      "uplink",
			State:     state,
			Interface: v.Name(),
		}
	}
	return result
}
