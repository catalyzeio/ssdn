package cmd

import (
	"flag"
	"path"
	"runtime"

	"github.com/catalyzeio/go-core/comm"
	"github.com/catalyzeio/go-core/simplelog"

	"github.com/catalyzeio/ssdn/overlay"
)

func StartL2Link() {
	log := simplelog.NewLogger("l2link")

	simplelog.AddFlags()
	overlay.AddTenantFlags()
	overlay.AddMTUFlag()
	overlay.AddDirFlags()
	comm.AddListenFlags(false, 0, true)
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

	runDir, confDir, err := overlay.GetDirFlags()
	if err != nil {
		fail("Invalid directory config: %s\n", err)
	}
	// TODO
	_ = runDir

	listenAddress, err := comm.GetListenAddress()
	if err != nil {
		fail("Invalid listener config: %s\n", err)
	}

	config, err := comm.GenerateTLSConfig(true)
	if err != nil {
		fail("Invalid TLS config: %s\n", err)
	}

	bridge := overlay.NewL2Bridge(tenantID, mtu, path.Join(confDir, "l2link.d"))
	if err := bridge.Start(); err != nil {
		fail("Failed to start bridge: %s\n", err)
	}

	uplinks := overlay.NewL2Uplinks(config, bridge)
	// TODO
	_ = uplinks

	if listenAddress != nil {
		listener := overlay.NewL2Listener(listenAddress, config, bridge)
		if err := listener.Start(); err != nil {
			fail("Failed to start listener: %s\n", err)
		}
	}

	// wait for all other goroutines to finish
	runtime.Goexit()
}
