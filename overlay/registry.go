package overlay

import (
	"time"

	"github.com/catalyzeio/ssdn/registry"
)

type RegistryConsumer interface {
	UpdatePeers(peerURLs map[string]struct{})
}

const (
	queryInterval = 15 * time.Second
)

func WatchRegistry(client registry.Client, key string, advertiseURL string, consumer RegistryConsumer) {
	var ads []registry.Advertisement
	if len(advertiseURL) > 0 {
		ads = append(ads, registry.Advertisement{key, advertiseURL})
	}
	client.Start(ads)

	for {
		peers, err := client.QueryAll(key)
		if err != nil {
			log.Warn("Failed to query registry for peers: %s", err)
		} else {
			peerURLs := make(map[string]struct{})
			for _, peer := range peers {
				peerURLs[peer] = struct{}{}
			}
			if len(advertiseURL) > 0 {
				delete(peerURLs, advertiseURL)
			}
			log.Debug("Peers: %s", peerURLs)
			consumer.UpdatePeers(peerURLs)
		}

		time.Sleep(queryInterval)
	}
}
