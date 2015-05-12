package registry

import (
	"crypto/tls"
	"flag"
	"fmt"
	"net"
	"net/url"
	"strconv"
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

	u, err := url.Parse(registryURL)
	if err != nil {
		return nil, err
	}

	scheme := u.Scheme
	tls := false
	if scheme == "tcps" {
		tls = true
	} else if scheme != "tcp" {
		return nil, fmt.Errorf("unsupported scheme: %s", scheme)
	}

	if !tls {
		config = nil
	} else {
		return nil, fmt.Errorf("registry server %s requires TLS configuration", registryURL)
	}

	var port int = sauronDefaultPort
	host, portStr, err := net.SplitHostPort(u.Host)
	if err == nil {
		port, err = strconv.Atoi(portStr)
		if err != nil {
			return nil, err
		}
	}

	return NewSauronClient(tenant, host, port, config), nil
}
