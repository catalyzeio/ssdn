package overlay

import (
	"time"

	"github.com/catalyzeio/paas-orchestration/registry"
)

const (
	registryRetryInterval = 5 * time.Second
)

func WatchRegistry(client *registry.Client, key string, advertiseURL string, consumer RegistryConsumer) {
	var ads []registry.Advertisement
	if len(advertiseURL) > 0 {
		ads = append(ads, registry.Advertisement{
			Name:     key,
			Location: advertiseURL,
		})
	}
	client.Start(ads, true)

	for {
		// pull in latest set of peers
		peers, err := client.QueryAll(key)
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
		if len(advertiseURL) > 0 {
			delete(peerURLs, advertiseURL)
		}
		log.Debug("Peers: %s", peerURLs)
		consumer.UpdatePeers(peerURLs)

		// wait for more changes
		<-client.Changes
	}
}
