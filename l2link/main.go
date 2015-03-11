package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path"
	"regexp"
	"time"

	"github.com/catalyzeio/shadowfax/actions"
	"github.com/catalyzeio/shadowfax/cli"
)

var TenantPattern = regexp.MustCompile("^[-0-9A-Za-z_]+$")

const (
	TenantIdLength = 6
)

func fail(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, format, args...)
	os.Exit(1)
}

func main() {
	tenantFlag := flag.String("tenant", "", "tenant identifier (required)")
	runDirFlag := flag.String("rundir", "/var/run/shadowfax", "server socket directory")
	confDirFlag := flag.String("confdir", "/etc/shadowfax", "configuration directory")
	flag.Parse()

	// validate tenant
	tenant := *tenantFlag
	tlen := len(tenant)
	if tlen == 0 {
		fail("Missing -tenant argument\n")
	}
	if !TenantPattern.MatchString(tenant) {
		fail("Invalid -tenant argument '%s'\n", tenant)
	}

	tenID := tenant
	if len(tenID) > TenantIdLength {
		tenID = tenID[:TenantIdLength]
	}
	log.Printf("Tenant: %s, tenant ID: %s", tenant, tenID)

	// init bridge
	invoker := actions.NewInvoker(path.Join(*confDirFlag, "l2link.d"))
	err := invoker.Execute("create", tenID)
	if err != nil {
		fail("Could not initialize bridge: %s\n", err)
	}

	// start CLI server
	s := cli.NewServer(*runDirFlag, tenant)

	s.Register("addpeer", "[proto://host:port]", "Adds a peer at the specified address", 1, 1, todo)
	s.Register("delpeer", "[proto://host:port]", "Deletes the peer at the specified address", 1, 1, todo)
	s.Register("peers", "", "List all active peers", 0, 0, todo)

	s.Register("attach", "[container]", "Attaches the given container to this overlay network", 1, 1, todo)
	s.Register("detach", "[container]", "Detaches the given container from this overlay network", 1, 1, todo)
	s.Register("connections", "", "Lists all containers attached to this overlay network", 0, 0, todo)

	err = s.Start()
	if err != nil {
		fail("Could not start CLI listener: %s\n", err)
	}

	// TODO start packet listener (if configured)

	// TODO registry integration

	for {
		time.Sleep(time.Hour)
	}
}

func todo(arg ...string) (string, error) {
	return "TODO", nil
}
