package main

import (
	"flag"
	"fmt"
	"os"
	"path"
	"time"

	"github.com/catalyzeio/shadowfax/cli"
	"github.com/catalyzeio/shadowfax/dumblog"
	"github.com/catalyzeio/shadowfax/overlay"
	"github.com/catalyzeio/shadowfax/proto"
)

var log = dumblog.NewLogger("l3bridge")

func fail(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, format, args...)
	os.Exit(1)
}

func main() {
	dumblog.AddFlags()
	overlay.AddTenantFlags()
	overlay.AddMTUFlag()
	overlay.AddNetworkFlag()
	overlay.AddSubnetFlags(true)
	overlay.AddDirFlags()
	proto.AddListenFlags(true)
	proto.AddTLSFlags()
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
	err = overlay.CheckSubnetInNetwork(subnet, network)
	if err != nil {
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
	err = pool.Acquire(gwIP)
	if err != nil {
		fail("Failed to initialize IP pool: %s\n", err)
	}

	bridge := overlay.NewL3Bridge(tenantID, mtu, path.Join(confDir, "l3bridge.d"), network, pool, gwIP)

	tap, err := overlay.NewL3Tap(gwIP, mtu, bridge, routes)
	if err != nil {
		fail("Failed to create tap: %s\n", err)
	}

	err = bridge.Start(cli, tap)
	if err != nil {
		fail("Failed to start bridge: %s\n", err)
	}

	err = tap.Start(cli)
	if err != nil {
		fail("Failed to start tap: %s\n", err)
	}

	peers := overlay.NewL3Peers(listenAddress.PublicString(), subnet, routes, config, mtu, tap.InboundHandler)
	peers.Start(cli)

	listener := overlay.NewL3Listener(peers, listenAddress, config)
	err = listener.Start(cli)
	if err != nil {
		fail("Failed to start listener: %s\n", err)
	}

	err = cli.Start()
	if err != nil {
		fail("Failed to start CLI: %s\n", err)
	}

	// TODO registry integration
	for {
		time.Sleep(time.Hour)
	}
}
