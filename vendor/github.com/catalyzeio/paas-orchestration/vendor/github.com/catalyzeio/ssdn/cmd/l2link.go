package cmd

import (
	"flag"
	"path"

	"github.com/catalyzeio/go-core/comm"
	"github.com/catalyzeio/go-core/simplelog"
	"github.com/catalyzeio/go-core/udocker"
	"github.com/catalyzeio/paas-orchestration/registry"

	"github.com/catalyzeio/ssdn/overlay"
	"github.com/catalyzeio/ssdn/watch"
)

func StartL2Link() {
	log := simplelog.NewLogger("l2link")

	simplelog.AddFlags()
	comm.AddListenFlags(false, 0, true)
	comm.AddTLSFlags()
	udocker.AddFlags("")
	registry.AddFlags(false)
	overlay.AddTenantFlags()
	overlay.AddMTUFlag()
	overlay.AddDirFlags(true, true)
	overlay.AddPeerTLSFlags()
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

	listenAddress, err := comm.GetListenAddress()
	if err != nil {
		fail("Invalid listener config: %s\n", err)
	}

	config, err := comm.GenerateTLSConfig(true)
	if err != nil {
		fail("Invalid TLS config: %s\n", err)
	}

	dc, err := udocker.GenerateClient(false)
	if err != nil {
		fail("Invalid Docker configuration: %s", err)
	}

	state := overlay.NewState(tenant, runDir)
	state.Start()

	bridge := overlay.NewL2Bridge(tenantID, mtu, state, path.Join(confDir, "l2link.d"))
	if err := bridge.Start(); err != nil {
		fail("Failed to start bridge: %s\n", err)
	}
	if err := bridge.Restore(); err != nil {
		fail("Failed to restore state: %s\n", err)
	}

	peerConfig := overlay.GetPeerTLSConfig(config)
	uplinks := overlay.NewL2Uplinks(peerConfig, bridge)

	var listener *overlay.L2Listener
	if listenAddress != nil {
		listener = overlay.NewL2Listener(listenAddress, config, bridge)
		if err := listener.Start(); err != nil {
			fail("Failed to start listener: %s\n", err)
		}
	}

	rc, err := registry.GenerateClient(tenant, config)
	if err != nil {
		fail("Failed to start registry client: %s\n", err)
	}
	if rc != nil {
		advertiseAddress := ""
		if listenAddress != nil {
			advertiseAddress = listenAddress.PublicString()
		}
		rw := watch.NewRegistryWatcher(rc, "ssdn-l2", advertiseAddress, false)
		rw.Watch(uplinks)
	}

	if dc != nil {
		cc := watch.NewContainerConnector(dc, tenant)
		cc.Watch(bridge)
	}

	dl := overlay.NewListener(tenant, runDir)
	wrapper := overlay.NewL2PeersWrapper(uplinks, listener)
	if err := dl.Listen(bridge, wrapper, nil, nil); err != nil {
		fail("Failed to start domain socket listener: %s\n", err)
	}
}