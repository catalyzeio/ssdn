package cmd

import (
	"flag"
	"fmt"
	"path"

	"github.com/catalyzeio/go-core/comm"
	"github.com/catalyzeio/go-core/simplelog"
	"github.com/catalyzeio/paas-orchestration/registry"

	"github.com/catalyzeio/ssdn/overlay"
)

func StartL3Direct() {
	log := simplelog.NewLogger("l3direct")

	simplelog.AddFlags()
	comm.AddListenFlags(true, 0, true)
	comm.AddTLSFlags()
	registry.AddFlags(false)
	overlay.AddTenantFlags()
	overlay.AddMTUFlag()
	overlay.AddNetworkFlag()
	overlay.AddSubnetFlags(false)
	overlay.AddDirFlags()
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

	tuns := overlay.NewL3Tuns(subnet, routes, mtu, path.Join(confDir, "l3direct.d"), network, pool)
	tuns.Start()

	peers := overlay.NewL3Peers(subnet, routes, config, mtu, tuns.InboundHandler)

	listener := overlay.NewL3Listener(peers, listenAddress, config)
	if err := listener.Start(); err != nil {
		fail("Failed to start listener: %s\n", err)
	}

	peers.Start(listenAddress.PublicString())

	rc, err := registry.GenerateClient(tenant, config)
	if err != nil {
		fail("Failed to start registry client: %s\n", err)
	}
	if rc != nil {
		advertiseAddress := listenAddress.PublicString()
		go overlay.WatchRegistry(rc, "sfl3", advertiseAddress, peers)
	}

	dl := overlay.NewListener(tenant, runDir)
	if err := dl.Listen(tuns, peers, routes, nil); err != nil {
		fail("Failed to start domain socket listener: %s\n", err)
	}
}
