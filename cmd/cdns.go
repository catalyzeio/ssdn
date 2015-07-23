package cmd

import (
	"flag"
	"runtime"

	"github.com/catalyzeio/go-core/comm"
	"github.com/catalyzeio/go-core/simplelog"
	"github.com/catalyzeio/go-core/udocker"
	"github.com/catalyzeio/paas-orchestration/registry"

	"github.com/catalyzeio/ssdn/overlay"
	"github.com/catalyzeio/ssdn/watch"
)

func StartCDNS() {
	log := simplelog.NewLogger("cdns")

	simplelog.AddFlags()
	overlay.AddTenantFlags()
	overlay.AddDirFlags(false, true)
	comm.AddTLSFlags()
	registry.AddFlags(true)
	udocker.AddFlags("")
	stateDirFlag := flag.String("state-dir", "./state", "where to store state information")
	flag.Parse()

	dc, err := udocker.GenerateClient(true)
	if err != nil {
		fail("Invalid Docker configuration: %s", err)
	}

	tenant, tenantID, err := overlay.GetTenantFlags()
	if err != nil {
		fail("Invalid tenant config: %s\n", err)
	}
	log.Info("Servicing tenant: %s, tenant ID: %s", tenant, tenantID)

	_, confDir, err := overlay.GetDirFlags()
	if err != nil {
		fail("Invalid directory config: %s\n", err)
	}

	config, err := comm.GenerateTLSConfig(true)
	if err != nil {
		fail("Invalid TLS config: %s\n", err)
	}

	rc, err := registry.GenerateClient(tenant, config)
	if err != nil {
		fail("Failed to create registry client: %s\n", err)
	}
	if rc == nil {
		fail("Invalid registry config: -registry is required\n")
	}
	rc.Start(nil, true)

	c := watch.NewContainerDNS(dc, rc, tenant, *stateDirFlag, confDir)
	c.Watch()

	// wait for all other goroutines to finish
	runtime.Goexit()
}
