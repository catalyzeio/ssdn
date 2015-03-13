package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path"
	"time"

	"github.com/catalyzeio/shadowfax/actions"
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
	proto.AddListenFlags(false)
	proto.AddTLSFlags()
	runDirFlag := flag.String("rundir", "/var/run/shadowfax", "server socket directory")
	confDirFlag := flag.String("confdir", "/etc/shadowfax", "configuration directory")
	mtuFlag := flag.Int("mtu", 9000, "MTU to use for virtual interfaces")
	flag.Parse()

	tenant, tenantID, err := overlay.GetTenantFlags()
	if err != nil {
		fail("Invalid tenant config: %s\n", err)
	}
	log.Printf("Servicing tenant: %s, tenant ID: %s", tenant, tenantID)

	mtuVal := *mtuFlag
	if mtuVal < 0x400 || mtuVal > overlay.MaxPacketSize {
		fail("Invalid MTU: %d\n", mtuVal)
	}
	mtu := uint16(mtuVal)

	listenAddress, err := proto.GetListenAddress()
	if err != nil {
		fail("Invalid listener config: %s\n", err)
	}

	config, err := proto.GenerateTLSConfig()
	if err != nil {
		fail("Invalid TLS config: %s\n", err)
	}

	invoker := actions.NewInvoker(path.Join(*confDirFlag, "l2link.d"))
	cli := cli.NewServer(*runDirFlag, tenant)

	overlay := overlay.NewL2Overlay(tenantID, mtu, listenAddress, config, invoker, cli)
	err = overlay.Start()
	if err != nil {
		fail("Failed to start overlay: %s\n", err)
	}

	// TODO registry integration
	for {
		time.Sleep(time.Hour)
	}
}
