package cmd

import (
	"crypto/tls"
	"flag"
	"fmt"

	"github.com/catalyzeio/go-core/comm"
	"github.com/catalyzeio/go-core/simplelog"

	"github.com/catalyzeio/paas-orchestration/agent"
	"github.com/catalyzeio/paas-orchestration/registry"
	"github.com/catalyzeio/paas-orchestration/scheduler"
)

func SchedServer() {
	simplelog.AddFlags()
	comm.AddListenFlags(true, scheduler.DefaultPort, false)
	comm.AddTLSFlags()
	clientCertFileFlag := flag.String("tls-client-cert", "", "client certificate to use in TLS mode; for agent connections")
	clientKeyFileFlag := flag.String("tls-client-key", "", "client certificate key to use in TLS mode; for agent connections")
	registry.AddFlags(true)
	flag.Parse()

	listenAddress, err := comm.GetListenAddress()
	if err == nil && listenAddress == nil {
		err = fmt.Errorf("-listen is required")
	}
	if err != nil {
		fail("Invalid listener config: %s\n", err)
	}

	config, err := comm.GenerateTLSConfig(true)
	if err != nil {
		fail("Invalid server TLS config: %s\n", err)
	}

	clientConfig, err := comm.GenerateAltTLSConfig(*clientCertFileFlag, *clientKeyFileFlag, false)
	if err != nil {
		fail("Invalid client TLS config: %s\n", err)
	}

	client, err := registry.GenerateClient(agent.Tenant, clientConfig)
	if err != nil {
		fail("Failed to start registry client: %s\n", err)
	}
	if client == nil {
		fail("Invalid registry config: -registry is required\n")
	}

	SchedServerArgs(listenAddress, config, clientConfig, client)
}

func SchedServerArgs(listenAddress *comm.Address, config *tls.Config, clientConfig *tls.Config, client *registry.Client) {
	client.Start(nil, true)

	sched := scheduler.NewScheduler(clientConfig, client)
	sched.Start()

	listener := scheduler.NewListener(listenAddress, config, sched)
	if err := listener.Listen(); err != nil {
		fail("Failed to start listener: %s\n", err)
	}
}
