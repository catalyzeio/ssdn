package cmd

import (
	"flag"
	"fmt"
	"path"

	"github.com/catalyzeio/go-core/comm"
	"github.com/catalyzeio/go-core/simplelog"

	"github.com/catalyzeio/ssdn/overlay"
)

func StartL3Bridge() {
	log := simplelog.NewLogger("l3bridge")

	simplelog.AddFlags()
	overlay.AddTenantFlags()
	overlay.AddMTUFlag()
	overlay.AddNetworkFlag()
	overlay.AddSubnetFlags(true)
	overlay.AddDirFlags()
	comm.AddListenFlags(true, 0, true)
	comm.AddTLSFlags()
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

	listenAddress, err := comm.GetListenAddress()
	if err == nil && listenAddress == nil {
		err = fmt.Errorf("-listen is required")
	}
	if err != nil {
		fail("Invalid listener config: %s\n", err)
	}

	config, err := comm.GenerateTLSConfig(true)
	if err != nil {
		fail("Invalid TLS config: %s\n", err)
	}

	routes := overlay.NewRouteTracker()

	pool := overlay.NewIPPool(subnet)
	if err := pool.Acquire(gwIP); err != nil {
		fail("Failed to initialize IP pool: %s\n", err)
	}

	bridge := overlay.NewL3Bridge(tenantID, mtu, path.Join(confDir, "l3bridge.d"), network, pool, gwIP)

	tap, err := overlay.NewL3Tap(gwIP, mtu, bridge, routes)
	if err != nil {
		fail("Failed to create tap: %s\n", err)
	}

	if err := bridge.Start(tap); err != nil {
		fail("Failed to start bridge: %s\n", err)
	}

	if err := tap.Start(); err != nil {
		fail("Failed to start tap: %s\n", err)
	}

	peers := overlay.NewL3Peers(subnet, routes, config, mtu, tap.InboundHandler)

	listener := overlay.NewL3Listener(peers, listenAddress, config)
	if err := listener.Start(); err != nil {
		fail("Failed to start listener: %s\n", err)
	}

	peers.Start(listenAddress.PublicString())

	dl := overlay.NewListener(tenant, runDir)
	if err := dl.Listen(bridge, peers, routes); err != nil {
		fail("Failed to start domain socket listener: %s\n", err)
	}
}
