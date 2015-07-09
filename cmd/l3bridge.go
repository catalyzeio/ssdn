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

func StartL3Bridge() {
	log := dumblog.NewLogger("l3bridge")

	dumblog.AddFlags()
	overlay.AddTenantFlags()
	overlay.AddMTUFlag()
	overlay.AddNetworkFlag()
	overlay.AddSubnetFlags(true)
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

	subnet, gwIP, err := overlay.GetSubnetFlags()
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
	if err := pool.Acquire(gwIP); err != nil {
		fail("Failed to initialize IP pool: %s\n", err)
	}

	bridge := overlay.NewL3Bridge(tenantID, mtu, path.Join(confDir, "l3bridge.d"), network, pool, gwIP)

	tap, err := overlay.NewL3Tap(gwIP, mtu, bridge, routes)
	if err != nil {
		fail("Failed to create tap: %s\n", err)
	}

	if err := bridge.Start(cli, tap); err != nil {
		fail("Failed to start bridge: %s\n", err)
	}

	if err := tap.Start(cli); err != nil {
		fail("Failed to start tap: %s\n", err)
	}

	peers := overlay.NewL3Peers(subnet, routes, config, mtu, tap.InboundHandler)

	listener := overlay.NewL3Listener(peers, listenAddress, config)
	if err := listener.Start(cli); err != nil {
		fail("Failed to start listener: %s\n", err)
	}

	peers.Start(cli, listenAddress.PublicString())

	if err := cli.Start(); err != nil {
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
