package cmd

import (
	"flag"
	"path/filepath"
	"runtime"

	"github.com/catalyzeio/go-core/comm"
	"github.com/catalyzeio/go-core/simplelog"
	"github.com/catalyzeio/go-core/udocker"
	"github.com/catalyzeio/paas-orchestration/agent"
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
	outputDirFlag := flag.String("output-dir", "./output", "where to store generated configuration data")
	agentSocketFlag := flag.String("agent-sock", "/data/orch/state/agent.sock", "path to the agent unix socket")
	advertiseJobStateFlag := flag.Bool("job-state", false, "whether or not to advertise job state")
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

	absSockPath, err := filepath.Abs(*agentSocketFlag)
	if err != nil {
		fail("Could not determine the absolute path to the agent socket: %s\n", err)
	}

	ac := agent.NewSocketClientFromPath(absSockPath)
	ac.Start()

	c := watch.NewContainerDNS(dc, rc, ac, tenant, *outputDirFlag, confDir, *advertiseJobStateFlag)
	c.Watch()

	// wait for all other goroutines to finish
	runtime.Goexit()
}
