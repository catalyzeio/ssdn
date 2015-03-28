package main

import (
	"flag"
	"fmt"
	"net"
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
	overlay.AddDirFlags()
	proto.AddListenFlags(true)
	proto.AddTLSFlags()
	gwIPFlag := flag.String("gateway", "192.168.0.254", "virtual gateway IP address")
	flag.Parse()

	tenant, tenantID, err := overlay.GetTenantFlags()
	if err != nil {
		fail("Invalid tenant config: %s\n", err)
	}
	log.Info("Servicing tenant: %s, tenant ID: %s\n", tenant, tenantID)

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

	gwIP := net.ParseIP(*gwIPFlag)
	if gwIP == nil {
		fail("Invalid gateway IP address: %s\n", *gwIPFlag)
	}
	gwIP = gwIP.To4()
	if gwIP == nil {
		fail("Gateway IP must be an IPv4 address: %s\n", *gwIPFlag)
	}

	cli := cli.NewServer(runDir, tenant)

	routes := overlay.NewRouteTracker()

	bridge := overlay.NewL3Bridge(tenantID, mtu, path.Join(confDir, "l3bridge.d"))
	err = bridge.Start(cli)
	if err != nil {
		fail("Failed to start bridge: %s\n", err)
	}

	tap, err := overlay.NewL3Tap(gwIP, mtu, bridge)
	if err != nil {
		fail("Failed to create tap: %s\n", err)
	}
	err = tap.Start(cli)
	if err != nil {
		fail("Failed to start tap: %s\n", err)
	}

	peers := overlay.NewL3Peers(routes, config, mtu)
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
