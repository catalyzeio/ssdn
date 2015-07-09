package cmd

import (
	"flag"
	"fmt"
	"os"
	"path"

	"github.com/catalyzeio/ssdn/cli"
	"github.com/catalyzeio/ssdn/dumblog"
	"github.com/catalyzeio/ssdn/overlay"
	"github.com/catalyzeio/ssdn/proto"
	"github.com/catalyzeio/ssdn/registry"
)

func StartL2Link() {
	log := dumblog.NewLogger("l2link")

	dumblog.AddFlags()
	overlay.AddTenantFlags()
	overlay.AddMTUFlag()
	overlay.AddDirFlags()
	proto.AddListenFlags(false)
	proto.AddTLSFlags()
	registry.AddRegistryFlags()
	flag.Parse()

	tenant, tenantID, err := overlay.GetTenantFlags()
	if err != nil {
		fail("Invalid tenant config: %s\n", err)
	}
	log.Info("Servicing tenant: %s, tenant ID: %s", tenant, tenantID)

	mtu, err := overlay.GetMTUFlag()
	if err != nil {
		fail("Invalid MTU config: %s\n", err)
	}

	runDir, confDir, err := overlay.GetDirFlags()
	if err != nil {
		fail("Invalid directory config: %s\n", err)
	}

	listenAddress, err := proto.GetListenAddress()
	if err != nil {
		fail("Invalid listener config: %s\n", err)
	}

	config, err := proto.GenerateTLSConfig()
	if err != nil {
		fail("Invalid TLS config: %s\n", err)
	}

	cli := cli.NewServer(runDir, tenant)

	bridge := overlay.NewL2Bridge(tenantID, mtu, path.Join(confDir, "l2link.d"))
	if err := bridge.Start(cli); err != nil {
		fail("Failed to start bridge: %s\n", err)
	}

	uplinks := overlay.NewL2Uplinks(config, bridge)
	uplinks.Start(cli)

	if listenAddress != nil {
		listener := overlay.NewL2Listener(listenAddress, config, bridge)
		if err := listener.Start(cli); err != nil {
			fail("Failed to start listener: %s\n", err)
		}
	}

	if err := cli.Start(); err != nil {
		fail("Failed to start CLI: %s\n", err)
	}

	registryClient, err := registry.GenerateClient(tenant, config)
	if err != nil {
		fail("Failed to start registry client: %s\n", err)
	}
	if registryClient != nil {
		advertiseAddress := ""
		if listenAddress != nil {
			advertiseAddress = listenAddress.PublicString()
		}
		overlay.WatchRegistry(registryClient, "sfl2", advertiseAddress, uplinks)
	} else {
		stall := make(chan interface{})
		<-stall
	}
}
