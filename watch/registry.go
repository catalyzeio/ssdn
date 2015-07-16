package watch

import (
	"time"

	"github.com/catalyzeio/paas-orchestration/registry"

	"github.com/catalyzeio/ssdn/overlay"
)

type RegistryWatcher struct {
	rc *registry.Client

	key          string
	advertiseURL string
	poll         bool
}

func NewRegistryWatcher(rc *registry.Client, key string, advertiseURL string, poll bool) *RegistryWatcher {
	return &RegistryWatcher{
		rc: rc,

		key:          key,
		advertiseURL: advertiseURL,
		poll:         poll,
	}
}

func (r *RegistryWatcher) Watch(consumer overlay.RegistryConsumer) {
	go r.query(consumer)
}

func (r *RegistryWatcher) query(consumer overlay.RegistryConsumer) {
	rc := r.rc

	var ads []registry.Advertisement
	if len(r.advertiseURL) > 0 {
		ads = append(ads, registry.Advertisement{
			Name:     r.key,
			Location: r.advertiseURL,
		})
	}
	notify := !r.poll
	rc.Start(ads, notify)

	for {
		// pull in latest set of peers
		peers, err := rc.QueryAll(r.key)
		if err != nil {
			log.Warn("Failed to query registry for peers: %s", err)
			time.Sleep(registryRetryInterval)
			continue
		}

		// assemble results, excluding the local peer
		peerURLs := make(map[string]struct{})
		for _, peer := range peers {
			peerURLs[peer.Location] = struct{}{}
		}
		if len(r.advertiseURL) > 0 {
			delete(peerURLs, r.advertiseURL)
		}
		log.Debug("Peers: %s", peerURLs)
		consumer.UpdatePeers(peerURLs)

		// wait for more changes
		if notify {
			<-rc.Changes
		} else {
			time.Sleep(registryPollInterval)
		}
	}
}
