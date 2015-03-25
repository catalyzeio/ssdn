package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path"
	"time"

	"github.com/catalyzeio/shadowfax/cli"
	"github.com/catalyzeio/shadowfax/overlay"
	"github.com/catalyzeio/shadowfax/proto"
)

func fail(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, format, args...)
	os.Exit(1)
}

func main() {
	overlay.AddTenantFlags()
	overlay.AddMTUFlag()
	overlay.AddDirFlags()
	proto.AddListenFlags(true)
	proto.AddTLSFlags()
	flag.Parse()

	tenant, tenantID, err := overlay.GetTenantFlags()
	if err != nil {
		fail("Invalid tenant config: %s\n", err)
	}
	log.Printf("Servicing tenant: %s, tenant ID: %s", tenant, tenantID)

	mtu, err := overlay.GetMTUFlag()
	if err != nil {
		fail("Invalid MTU config: %s\n", err)
	}

	runDir, confDir, err := overlay.GetDirFlags()
	if err != nil {
		fail("Invalid directory config: %s\n", err)
	}

	listenAddress, err := proto.GetListenAddress()
	if listenAddress == nil {
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

	bridge := overlay.NewL3Bridge(tenantID, mtu, path.Join(confDir, "l3bridge.d"))
	err = bridge.Start(cli)
	if err != nil {
		fail("Failed to start bridge: %s\n", err)
	}

	tap := overlay.NewL3Tap(bridge)
	err = tap.Start(cli)
	if err != nil {
		fail("Failed to start tap: %s\n", err)
	}

	peers := overlay.NewL3Peers()
	peers.Start(cli)

	listener := overlay.NewL3Listener(listenAddress, config)
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
