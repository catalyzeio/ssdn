package main

import (
	"flag"
	"fmt"
	"os"
	"path"

	"github.com/catalyzeio/shadowfax/cli"
	"github.com/catalyzeio/shadowfax/dumblog"
	"github.com/catalyzeio/shadowfax/overlay"
	"github.com/catalyzeio/shadowfax/proto"
	"github.com/catalyzeio/shadowfax/registry"
)

var log = dumblog.NewLogger("l3direct")

func fail(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, format, args...)
	os.Exit(1)
}

func main() {
	dumblog.AddFlags()
	overlay.AddTenantFlags()
	overlay.AddMTUFlag()
	overlay.AddNetworkFlag()
	overlay.AddSubnetFlags(false)
	overlay.AddDirFlags()
	proto.AddListenFlags(true)
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

	network, err := overlay.GetNetworkFlag()
	if err != nil {
		fail("Invalid network config: %s\n", err)
	}
	log.Info("Overlay network: %s", network)

	subnet, _, err := overlay.GetSubnetFlags()
	if err != nil {
		fail("Invalid subnet config: %s\n", err)
	}
	if err := overlay.CheckSubnetInNetwork(subnet, network); err != nil {
		fail("Invalid subnet config: %s\n", err)
	}
	log.Info("Local subnet: %s", subnet)

	runDir, confDir, err := overlay.GetDirFlags()
	if err != nil {
		fail("Invalid directory config: %s\n", err)
	}

	listenAddress, err := proto.GetListenAddress()
	if err == nil && listenAddress == nil {
		err = fmt.Errorf("-listen is required")
	}
	if err != nil {
		fail("Invalid listener config: %s\n", err)
	}

	config, err := proto.GenerateTLSConfig()
	if err != nil {
		fail("Invalid TLS config: %s\n", err)
	}

	cli := cli.NewServer(runDir, tenant)

	routes := overlay.NewRouteTracker()
	routes.Start(cli)

	pool := overlay.NewIPPool(subnet)

	tuns := overlay.NewL3Tuns(subnet, routes, mtu, path.Join(confDir, "l3direct.d"), network, pool)
	tuns.Start(cli)

	peers := overlay.NewL3Peers(listenAddress.PublicString(), subnet, routes, config, mtu, tuns.InboundHandler)
	peers.Start(cli)

	listener := overlay.NewL3Listener(peers, listenAddress, config)
	if err := listener.Start(cli); err != nil {
		fail("Failed to start listener: %s\n", err)
	}

	if err = cli.Start(); err != nil {
		fail("Failed to start CLI: %s\n", err)
	}

	registryClient, err := registry.GenerateClient(tenant, config)
	if err != nil {
		fail("Failed to start registry client: %s\n", err)
	}
	if registryClient != nil {
		advertiseAddress := listenAddress.PublicString()
		overlay.WatchRegistry(registryClient, "sfl3", advertiseAddress, peers)
	} else {
		stall := make(chan interface{})
		<-stall
	}
}
