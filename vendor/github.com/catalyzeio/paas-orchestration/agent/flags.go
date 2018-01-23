package agent

import (
	"crypto/tls"
	"flag"
	"fmt"
	"net"
	"net/url"
	"strconv"
)

var agentURLFlag *string

func AddFlags(defaultURL bool) {
	url := ""
	if defaultURL {
		url = "tcp://127.0.0.1"
	}
	agentURLFlag = flag.String("agent", url, "URL of the agent server")
}

func GenerateClient(config *tls.Config) (*Client, error) {
	return ClientFromURL(config, *agentURLFlag)
}

func ClientFromURL(config *tls.Config, agentURL string) (*Client, error) {
	if len(agentURL) < 1 {
		return nil, nil
	}

	u, err := url.Parse(agentURL)
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
		return nil, fmt.Errorf("agent server %s requires TLS configuration", agentURL)
	}

	var port int = DefaultPort
	host, portStr, err := net.SplitHostPort(u.Host)
	if err == nil {
		port, err = strconv.Atoi(portStr)
		if err != nil {
			return nil, err
		}
	} else {
		host = u.Host
	}

	return NewClient(agentURL, host, port, config), nil
}
