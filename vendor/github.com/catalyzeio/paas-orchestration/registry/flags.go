package registry

import (
	"crypto/tls"
	"flag"
	"fmt"
	"net"
	"net/url"
	"strconv"
)

var registryURLFlag *string

func AddFlags(defaultURL bool) {
	url := ""
	if defaultURL {
		url = "tcp://127.0.0.1"
	}
	registryURLFlag = flag.String("registry", url, "URL of the registry server")
}

func GenerateClient(tenant string, config *tls.Config) (*Client, error) {
	return ClientFromURL(tenant, config, *registryURLFlag)
}

func ClientFromURL(tenant string, config *tls.Config, registryURL string) (*Client, error) {
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
	} else if config == nil {
		return nil, fmt.Errorf("registry server %s requires TLS configuration", registryURL)
	}

	port := DefaultPort
	host, portStr, err := net.SplitHostPort(u.Host)
	if err == nil {
		port, err = strconv.Atoi(portStr)
		if err != nil {
			return nil, err
		}
	} else {
		host = u.Host
	}

	return NewClient(tenant, host, port, config), nil
}
