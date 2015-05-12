package registry

import (
	"crypto/tls"
	"flag"
	"fmt"

	"github.com/catalyzeio/shadowfax/proto"
)

type Client interface {
	Start(ads []Advertisement)
	Stop()
	Query(requires string) (*string, error)
	QueryAll(requires string) ([]string, error)
}

var registryURLFlag *string

func AddRegistryFlags() {
	registryURLFlag = flag.String("registry", "", "URL of the registry server")
}

func GenerateClient(tenant string, config *tls.Config) (Client, error) {
	return NewClient(tenant, config, *registryURLFlag)
}

func NewClient(tenant string, config *tls.Config, registryURL string) (Client, error) {
	if len(registryURL) < 1 {
		return nil, nil
	}

	// TODO support DNS names in host portion of URL
	addr, err := proto.ParseAddress(registryURL)
	if err != nil {
		return nil, err
	}
	if !addr.TLS() {
		config = nil
	} else if config == nil {
		return nil, fmt.Errorf("registry server %s requires TLS configuration", addr)
	}

	return NewSauronClient(tenant, addr.Host(), addr.Port(), config), nil
}
