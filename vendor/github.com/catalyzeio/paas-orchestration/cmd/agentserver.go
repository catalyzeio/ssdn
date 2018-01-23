package cmd

import (
	"flag"
	"fmt"
	"io/ioutil"
	"net"
	"os"
	"runtime"
	"strings"

	"github.com/catalyzeio/go-core/comm"
	"github.com/catalyzeio/go-core/simplelog"
	"github.com/catalyzeio/go-core/udocker"

	"github.com/catalyzeio/paas-orchestration/agent"
	"github.com/catalyzeio/paas-orchestration/registry"
)

func AgentServer() {
	simplelog.AddFlags()
	comm.AddListenFlags(true, agent.DefaultPort, true)
	comm.AddTLSFlags()
	registry.AddFlags(true)
	servicesFlag := flag.String("services", "", "address to advertise for services")
	stateDirFlag := flag.String("state-dir", "./state", "where to store state information")
	trustDirFlag := flag.String("trust-dir", "./trust", "where to story trust data")
	serviceDirFlag := flag.String("service-dir", "/etc/service", `process supervisor directory; for "docker" handler`)
	requiresFlag := flag.String("requires", "", "required services for this agent; comma-separated list")
	providesFlag := flag.String("provides", "", "provided services for this agent; comma-separated list")
	conflictsFlag := flag.String("conflicts", "", "conflicting services for this agent; comma-separated list")
	prefersFlag := flag.String("prefers", "", "preferred services for this agent; comma-separated list")
	despisesFlag := flag.String("despises", "", "despised services for this agent; comma-separated list")
	handlersFlag := flag.String("handlers", "build,docker,dockerBatch,dummy", "which handlers to enable")
	policyFlag := flag.String("policy", "", "resource allocation policy")
	packFlag := flag.Bool("pack", false, "adjust bids to run as many jobs as possible on this host")
	capacityFlag := flag.Int("capacity", 4, `number of jobs that can be run; for "fixed" policy`)
	memoryFlag := flag.Int64("memory", 0, `amount of available memory, in MiB; for "memory" policy`)
	memoryLimitFlag := flag.Float64("memory-limit", 0.9, `percent of memory to provision, in decimal; for "memory" policy`)
	minJobMemoryFlag := flag.Int64("min-job-memory", 0, `minimum job size, in MiB; for "memory" policy`)
	maxJobMemoryFlag := flag.Int64("max-job-memory", -1, `maximum job size, in MiB (ignored if negative); for "memory" policy`)
	udocker.AddFlags(`docker host; for "docker" and "build" handlers`)
	cleanFailedJobsFlags := flag.Bool("clean-failed-jobs", true, `whether to clean up failed jobs; for "docker" handler`)
	templatesDirFlag := flag.String("templates-dir", "/opt/orch/templates", "location of template files")
	amqpURLFlag := flag.String("amqp-url", "", `URL of AMQP server; for "build" handler output`)
	overlayNetworkFlag := flag.String("overlay-network", "", `overlay network range; for "docker" handler`)
	overlaySubnetFlag := flag.String("overlay-subnet", "", `local subnet of overlay network; for "docker" handler`)
	notaryServerFlag := flag.String("notary-server", "", "notary server endpoint")
	appAuthKeyPathFlag := flag.String("coreapi-key", "", "coreapi app auth key path")
	appAuthCertPathFlag := flag.String("coreapi-cert", "", "coreapi app auth cert path")
	appAuthNameFlag := flag.String("coreapi-name", "", "coreapi app auth name")
	uIDFlag := flag.String("uid", "0", "default uid of app context")
	gIDFlag := flag.String("gid", "0", "default gid of app context")
	flag.Parse()

	if len(*trustDirFlag) > 0 {
		if err := os.MkdirAll(*trustDirFlag, 0700); err != nil {
			panic(err)
		}
	}

	listenAddress, err := comm.GetListenAddress()
	if err == nil && listenAddress == nil {
		err = fmt.Errorf("-listen is required")
	}
	if err != nil {
		fail("Invalid listener config: %s\n", err)
	}

	config, err := comm.GenerateTLSConfig(true)
	if err != nil {
		fail("Invalid TLS config: %s\n", err)
	}

	client, err := registry.GenerateClient(agent.Tenant, config)
	if err != nil {
		fail("Failed to create registry client: %s\n", err)
	}
	if client == nil {
		fail("Invalid registry config: -registry is required\n")
	}

	AgentServerArgs(&agent.ServerConfig{
		CAFile:          comm.TLSCAFile(),
		DockerHost:      udocker.GetFlag(),
		Services:        *servicesFlag,
		OverlayNetwork:  *overlayNetworkFlag,
		OverlaySubnet:   *overlaySubnetFlag,
		TemplatesDir:    *templatesDirFlag,
		StateDir:        *stateDirFlag,
		TrustDir:        *trustDirFlag,
		ServiceDir:      *serviceDirFlag,
		Requires:        *requiresFlag,
		Provides:        *providesFlag,
		Conflicts:       *conflictsFlag,
		Prefers:         *prefersFlag,
		Despises:        *despisesFlag,
		Handlers:        *handlersFlag,
		AMQPURL:         *amqpURLFlag,
		Policy:          *policyFlag,
		Capacity:        *capacityFlag,
		Memory:          *memoryFlag,
		MinJobMemory:    *minJobMemoryFlag,
		MaxJobMemory:    *maxJobMemoryFlag,
		MemoryLimit:     *memoryLimitFlag,
		Pack:            *packFlag,
		CleanFailedJobs: *cleanFailedJobsFlags,
		ListenAddress:   listenAddress,
		TLSConfig:       config,
		RegistryClient:  client,
		NotaryServer:    *notaryServerFlag,
		AppAuthKeyPath:  *appAuthKeyPathFlag,
		AppAuthCertPath: *appAuthCertPathFlag,
		AppAuthName:     *appAuthNameFlag,
		UID:             *uIDFlag,
		GID:             *gIDFlag,
	})
}

func AgentServerArgs(config *agent.ServerConfig) {
	log := simplelog.NewLogger("agentserver")

	registryCA := ""
	if config.TLSConfig != nil {
		if len(config.CAFile) > 0 {
			bytes, err := ioutil.ReadFile(config.CAFile)
			if err != nil {
				fail("Failed to read CA certificate file: %s\n", err)
			}
			registryCA = string(bytes)
		}
	}

	registryURL := config.RegistryClient.TargetURL()

	serviceAddress := config.ListenAddress.PublicHost()
	if len(config.Services) > 0 {
		serviceAddress = config.Services
	}

	log.Info("Bind address: %s, agent address: %s, service address: %s",
		config.ListenAddress.Host(), config.ListenAddress.PublicHost(), serviceAddress)

	var err error
	var network, subnet *net.IPNet
	if len(config.OverlayNetwork) > 0 {
		_, network, err = net.ParseCIDR(config.OverlayNetwork)
		if err != nil {
			fail("Invalid overlay network config: %s\n", err)
		}
	}
	if len(config.OverlaySubnet) > 0 {
		_, subnet, err = net.ParseCIDR(config.OverlaySubnet)
		if err != nil {
			fail("Invalid overlay subnet config: %s\n", err)
		}
	}
	overlays, err := agent.NewOverlayManager(config.DockerHost, config.TemplatesDir, config.StateDir, config.ServiceDir,
		registryURL, registryCA, serviceAddress, network, subnet, config.UID, config.GID)
	if err != nil {
		fail("Invalid overlay config: %s\n", err)
	}
	if network != nil || subnet != nil {
		log.Info("Overlay network: %s, local subnet: %s", network, subnet)
	}

	ac := &agent.AgentConstraints{}
	if len(config.Requires) > 0 {
		ac.Requires = agent.SplitStringBag(config.Requires, ",")
	}
	if len(config.Provides) > 0 {
		ac.Provides = agent.SplitStringBag(config.Provides, ",")
	}
	if len(config.Conflicts) > 0 {
		ac.Conflicts = agent.SplitStringBag(config.Conflicts, ",")
	}
	if len(config.Prefers) > 0 {
		ac.Prefers = agent.SplitStringBag(config.Prefers, ",")
	}
	if len(config.Despises) > 0 {
		ac.Despises = agent.SplitStringBag(config.Despises, ",")
	}
	log.Info("Agent-level job constraints: %+v", ac)

	handlers := make(map[string]*agent.Handler)
	dockerHandler := false
	var notaryConfig *agent.NotaryConfig
	if len(config.NotaryServer) > 0 {
		notaryConfig = &agent.NotaryConfig{
			NotaryServer:    config.NotaryServer,
			TrustDir:        config.TrustDir,
			AppAuthKeyPath:  config.AppAuthKeyPath,
			AppAuthCertPath: config.AppAuthCertPath,
			AppAuthName:     config.AppAuthName,
		}
	}
	for _, v := range strings.Split(config.Handlers, ",") {
		var spawner agent.Spawner
		var err error
		switch v {
		case "build":
			spawner, err = agent.NewBuildSpawner(config.DockerHost, config.StateDir, config.TemplatesDir, config.AMQPURL, overlays)
		case "docker":
			spawner, err = agent.NewDockerSpawner(config.DockerHost, config.StateDir, config.TemplatesDir, registryURL,
				serviceAddress, overlays, config.CleanFailedJobs, notaryConfig)
			dockerHandler = true
		case "dockerBatch":
			spawner, err = agent.NewDockerSpawner(config.DockerHost, config.StateDir, config.TemplatesDir, registryURL,
				serviceAddress, overlays, config.CleanFailedJobs, notaryConfig)
			dockerHandler = true
		case "dummy":
			spawner = agent.NewDummySpawner()
		default:
			fail("Unsupported handler: %s\n", v)
		}
		if err != nil {
			fail("Could not initialize handler %s: %s\n", v, err)
		}
		handlers[v] = agent.NewHandler(ac, spawner, v)
	}

	if len(config.Policy) == 0 {
		// default to a memory-based policy if using the docker handler
		if dockerHandler {
			config.Policy = "memory"
		} else {
			config.Policy = "fixed"
		}
	}
	var policy agent.Policy
	switch config.Policy {
	case "fixed":
		policy = agent.NewFixedPolicy(config.Capacity, config.Pack)
	case "memory":
		policy = agent.NewMemoryPolicy(config.Memory, config.MemoryLimit, config.Pack, config.MinJobMemory, config.MaxJobMemory)
	default:
		fail("Unsupported resource policy: %s\n", config.Policy)
	}

	listener, err := agent.NewListener(config.StateDir, config.ListenAddress, config.TLSConfig, policy, handlers, ac)
	if err != nil {
		fail("Failed to create listener: %s\n", err)
	}
	if err := listener.Listen(); err != nil {
		fail("Failed to start listener: %s\n", err)
	}

	ad := registry.Advertisement{
		Name:     agent.Service,
		Location: config.ListenAddress.PublicString(),
	}
	config.RegistryClient.Start([]registry.Advertisement{ad}, false)

	// wait for all other goroutines to finish
	runtime.Goexit()
}
