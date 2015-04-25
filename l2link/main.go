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

var log = dumblog.NewLogger("l2link")

func fail(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, format, args...)
	os.Exit(1)
}

func main() {
	dumblog.AddFlags()
	overlay.AddTenantFlags()
	overlay.AddMTUFlag()
	overlay.AddDirFlags()
	proto.AddListenFlags(false)
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

	// TODO registry integration
	for {
		time.Sleep(time.Hour)
	}
}
