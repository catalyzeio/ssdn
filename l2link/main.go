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
	proto.AddTLSFlags()
	overlay.AddTenantFlags()
	runDirFlag := flag.String("rundir", "/var/run/shadowfax", "server socket directory")
	confDirFlag := flag.String("confdir", "/etc/shadowfax", "configuration directory")
	flag.Parse()

	tenant, tenID, err := overlay.GetTenantFlags()
	log.Printf("Running for tenant: %s, tenant ID: %s", tenant, tenID)
	if err != nil {
		fail("Invalid tenant config: %s\n", err)
	}

	config, err := proto.GenerateTLSConfig()
	if err != nil {
		fail("Invalid TLS config: %s\n", err)
	}

	ai := actions.NewInvoker(path.Join(*confDirFlag, "l2link.d"))
	cl := cli.NewServer(*runDirFlag, tenant)

	overlay := overlay.NewL2Link(tenID, ai, cl, config)
	err = overlay.Start()
	if err != nil {
		fail("Failed to start overlay: %s\n", err)
	}

	// TODO registry integration
	for {
		time.Sleep(time.Hour)
	}
}
