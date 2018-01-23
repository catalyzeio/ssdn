package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/catalyzeio/go-core/comm"
	"github.com/catalyzeio/go-core/udocker"
	"github.com/catalyzeio/paas-orchestration/agent"
	"github.com/catalyzeio/paas-orchestration/cmd"
	"github.com/catalyzeio/paas-orchestration/registry"
	"github.com/catalyzeio/paas-orchestration/scheduler"
)

const tenant = "test-env1"

var (
	schedulerClient *scheduler.Client
	dockerClient    *udocker.Client
	registryClient  *registry.Client

	dockerRegistryID = "dockerio_registry"

	unhealthy = int(agent.Unhealthy)
	running   = int(agent.Running)
)

func TestMain(m *testing.M) {
	err := os.Remove("./state/agent.json")
	if err != nil && !os.IsNotExist(err) {
		printErrAndExit(err)
	}

	dockerURL := os.Getenv("DOCKER_HOST")
	if len(dockerURL) == 0 {
		dockerURL = "unix:///var/run/docker.sock"
	}

	// spin up the registry
	registryListenAddress, err := comm.ParseAddress("tcp://127.0.0.1:7411")
	if err != nil {
		printErrAndExit(err)
	}

	go cmd.RegServerArgs(registryListenAddress, nil, registry.NewMemoryBackend(nil))

	// spin up the scheduler
	schedulerListenAddress, err := comm.ParseAddress("tcp://127.0.0.1:7400")
	if err != nil {
		printErrAndExit(err)
	}
	schedulerRegistryClient, err := registry.ClientFromURL(agent.Tenant, nil, "tcp://127.0.0.1:7411")
	if err != nil {
		printErrAndExit(err)
	}
	go cmd.SchedServerArgs(schedulerListenAddress, nil, nil, schedulerRegistryClient)

	// spin up the agent
	agentListenAddress, err := comm.ParseAddress("tcp://127.0.0.1:7433")
	if err != nil {
		printErrAndExit(err)
	}
	agentRegistryClient, err := registry.ClientFromURL(agent.Tenant, nil, "tcp://127.0.0.1:7411")
	if err != nil {
		printErrAndExit(err)
	}
	agentConfig := agent.NewServerConfig()
	agentConfig.DockerHost = dockerURL
	agentConfig.OverlayNetwork = "192.168.0.0/16"
	agentConfig.OverlaySubnet = "192.168.13.0/24"
	agentConfig.TemplatesDir = "./templates"
	agentConfig.Provides = "agent.no-zone"
	agentConfig.Handlers = "docker,build"
	agentConfig.MemoryLimit = 1.2
	agentConfig.ListenAddress = agentListenAddress
	agentConfig.RegistryClient = agentRegistryClient
	go cmd.AgentServerArgs(agentConfig)

	time.Sleep(time.Second)

	schedulerClient, err = scheduler.NewClient(schedulerListenAddress, nil)
	if err != nil {
		printErrAndExit(err)
	}
	dockerClient, err = udocker.ClientFromURL(dockerURL)
	if err != nil {
		printErrAndExit(err)
	}
	registryClient = registry.NewClient(tenant, "127.0.0.1", 7411, nil)
	if err != nil {
		printErrAndExit(err)
	}
	registryClient.Start(nil, true)
	if !flag.Parsed() {
		flag.Parse()
	}
	if !testing.Short() {
		cleanUp(dockerRegistryID)
		err = setUpDockerRegsitry(dockerRegistryID)
		if err != nil {
			printErrAndExit(err)
		}
		defer cleanUp(dockerRegistryID)
	}
	state := m.Run()
	os.Exit(state)
}

func setUpDockerRegsitry(id string) error {
	err := dockerClient.PullImageTag("docker.io/registry", "latest")
	if err != nil {
		return err
	}
	portKey := fmt.Sprintf("%d/tcp", 5000)
	_, err = dockerClient.CreateContainer(id, &udocker.ContainerDefinition{
		ExposedPorts: map[string]struct{}{
			portKey: struct{}{},
		},
		Image: "docker.io/registry:latest",
		HostConfig: udocker.HostConfig{
			Memory:    1073741824,
			CpuShares: 1024,
			PortBindings: map[string][]udocker.PortBinding{
				portKey: []udocker.PortBinding{udocker.PortBinding{HostIp: "0.0.0.0", HostPort: "5001"}},
			},
		},
	})
	if err != nil {
		return err
	}
	return dockerClient.StartContainer(id, nil)
}

func cleanUp(id string) {
	err := dockerClient.StopContainer(id, 0)
	if err != nil {
		fmt.Printf("Failed to stop container %s: %s\n", id, err)
	}
	err = dockerClient.DeleteContainer(id)
	if err != nil {
		fmt.Printf("Failed to delete container %s: %s\n", id, err)
	}
}

func printErrAndExit(err error) {
	fmt.Println(err)
	os.Exit(2)
}

func waitForStatus(jobID string, status, starting agent.Status, timeout time.Duration) error {
	after := time.After(timeout)
	for {
		time.Sleep(time.Second)
		select {
		case <-after:
			return fmt.Errorf("job %s: timedout waiting for status %s", jobID, status)
		default:
		}
		job, err := schedulerClient.GetJob(jobID)
		if err != nil {
			return err
		}
		if job.State == status {
			return nil
		}
		if job.State > status && job.State != starting {
			return fmt.Errorf("job %s: passed status %s to %s", jobID, status, job.State)
		}
	}
}

func TestJobRunning(t *testing.T) {
	id := "jobtest1"
	cleanUp(id)
	hostName := "code-hostname"
	jr := &agent.JobRequest{
		ID:   id,
		Type: "docker",
		Name: "test-env1/test-pod/code",
		Description: &agent.JobDescription{
			Conflicts: []string{"code"},
			Requires:  []string{"agent.no-zone"},
			Tenant:    tenant,
			Provides:  []string{"code"},
			Resources: &agent.JobLimits{
				Memory: 536870912,
			},
			HostName: hostName,
		},
		Payload: &agent.JobPayload{
			DockerImage: "docker.io/datica/node-js-example:latest",
			Limits: &agent.JobLimits{
				Memory:    536870912,
				CPUShares: 1024,
			},
			Environment: map[string]string{
				"CATALYZE_JOB_ID": id,
				"PORT":            "8080",
			},
			TenantToken: "default",
			Incomplete:  false,
		},
	}
	job, err := schedulerClient.AddJob(jr)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanUp(id)
	if job.State < agent.Running {
		err = waitForStatus(job.ID, agent.Running, agent.Scheduled, time.Second*120)
		if err != nil {
			t.Fatal(err)
		}
	}
	stdout, stderr, exitCode, err := dockerClient.ExecContainer([]string{"/bin/hostname"}, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if exitCode > 0 {
		t.Fatalf("checking hostname resulted in exit code %d, and output %s", exitCode, stderr)
	}
	hostNameOut := strings.TrimSpace(stdout)
	if hostNameOut != hostName {
		t.Fatalf("result from hostname command, \"%s\", does not match input, \"%s\"", hostNameOut, hostName)
	}
}

func TestJobHealthCheck(t *testing.T) {
	id := "jobtest2"
	cleanUp(id)
	jr := &agent.JobRequest{
		ID:   id,
		Type: "docker",
		Name: "test-env2/test-pod/code",
		Description: &agent.JobDescription{
			Conflicts: []string{"code"},
			Requires:  []string{"agent.no-zone"},
			Tenant:    "test-env2",
			Provides:  []string{"code"},
			Resources: &agent.JobLimits{
				Memory: 536870912,
			},
			HealthCheck: &agent.HealthCheck{
				Command:            []string{"/usr/bin/curl", "-s", "-o", "/dev/null", "-I", "-L", "-w", "%{http_code}", "http://127.0.0.1:8080/ping"},
				ExpectedStatusCode: 0,
				ExpectedOutput:     "200",
			},
		},
		Payload: &agent.JobPayload{
			DockerImage: "docker.io/datica/node-js-example:latest",
			Limits: &agent.JobLimits{
				Memory:    536870912,
				CPUShares: 1024,
			},
			Environment: map[string]string{
				"CATALYZE_JOB_ID": id,
				"PORT":            "8080",
			},
			TenantToken: "default",
			Incomplete:  false,
		},
	}
	job, err := schedulerClient.AddJob(jr)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanUp(id)
	if job.State < agent.Running {
		err = waitForStatus(job.ID, agent.Running, agent.Scheduled, time.Second*120)
		if err != nil {
			t.Fatal(err)
		}
	}
	stdout, stderr, exitCode, err := dockerClient.ExecContainer([]string{"/usr/bin/curl", "http://127.0.0.1:8080/off"}, job.ID)
	if err != nil {
		t.Fatalf("job exec %s: stdout: %s stderr: %s exitcode: %d\n%s\n", job.ID, stdout, stderr, exitCode, err)
	}
	err = waitForStatus(job.ID, agent.Unhealthy, agent.Running, time.Second*20)
	if err != nil {
		t.Fatal(err)
	}
	stdout, stderr, exitCode, err = dockerClient.ExecContainer([]string{"/usr/bin/curl", "http://127.0.0.1:8080/on"}, job.ID)
	if err != nil {
		t.Fatalf("job exec %s: stdout: %s stderr: %s exitcode: %d\n%s\n", job.ID, stdout, stderr, exitCode, err)
	}
	err = waitForStatus(job.ID, agent.Running, agent.Unhealthy, time.Second*20)
	if err != nil {
		t.Fatal(err)
	}
}

func TestJobStopKillTransition(t *testing.T) {
	id := "jobtest4"
	cleanUp(id)
	jr := &agent.JobRequest{
		ID:   id,
		Type: "docker",
		Name: "test-env1/test-pod/code",
		Description: &agent.JobDescription{
			Conflicts: []string{"code"},
			Requires:  []string{"agent.no-zone"},
			Tenant:    tenant,
			Provides:  []string{"code"},
			Resources: &agent.JobLimits{
				Memory: 536870912,
			},
		},
		Payload: &agent.JobPayload{
			DockerImage: "docker.io/datica/node-js-example:latest",
			Limits: &agent.JobLimits{
				Memory:    536870912,
				CPUShares: 1024,
			},
			Environment: map[string]string{
				"CATALYZE_JOB_ID": id,
				"PORT":            "8080",
			},
			TenantToken: "default",
			Incomplete:  false,
		},
	}
	job, err := schedulerClient.AddJob(jr)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanUp(id)
	if job.State < agent.Running {
		err = waitForStatus(job.ID, agent.Running, agent.Scheduled, time.Second*120)
		if err != nil {
			t.Fatal(err)
		}
	}
	err = schedulerClient.StopJob(job.ID)
	if err != nil {
		t.Fatal(err)
	}
	err = waitForStatus(job.ID, agent.Unhealthy, agent.Running, time.Second*30)
	if err != nil {
		t.Fatal(err)
	}
	err = waitForStatus(job.ID, agent.Stopped, agent.Unhealthy, time.Second*30)
	if err != nil {
		t.Fatal(err)
	}
	err = schedulerClient.StartJob(job.ID)
	if err != nil {
		t.Fatal(err)
	}
	err = waitForStatus(job.ID, agent.Running, agent.Stopped, time.Second*120)
	if err != nil {
		t.Fatal(err)
	}
	err = schedulerClient.DeleteJob(job.ID)
	if err != nil {
		t.Fatal(err)
	}
	err = waitForStatus(job.ID, agent.Unhealthy, agent.Running, time.Second*30)
	if err != nil {
		t.Fatal(err)
	}
	err = waitForStatus(job.ID, agent.Killed, agent.Unhealthy, time.Second*30)
	if err != nil {
		t.Fatal(err)
	}
}

func TestAgentSocketExists(t *testing.T) {
	t.Parallel()
	_, err := os.Stat("./state/agent.sock")
	if err != nil {
		t.Fatalf("error checking agent domain socket exists: %s", err)
	}
}

func TestAgentSocketQuery(t *testing.T) {
	// startup a job - should be running
	id := "testagentsocketquery"
	cleanUp(id)
	jr := &agent.JobRequest{
		ID:   id,
		Type: "docker",
		Name: tenant + "/test-pod/" + id,
		Description: &agent.JobDescription{
			Conflicts: []string{"code"},
			Requires:  []string{"agent.no-zone"},
			Tenant:    tenant,
			Provides:  []string{"code"},
			Resources: &agent.JobLimits{
				Memory: 536870912,
			},
			HealthCheck: &agent.HealthCheck{
				Command:            []string{"/usr/bin/curl", "-s", "-o", "/dev/null", "-I", "-L", "-w", "%{http_code}", "http://127.0.0.1:8080/ping"},
				ExpectedStatusCode: 0,
				ExpectedOutput:     "200",
			},
		},
		Payload: &agent.JobPayload{
			DockerImage: "docker.io/datica/node-js-example:latest",
			Limits: &agent.JobLimits{
				Memory:    536870912,
				CPUShares: 1024,
			},
			Environment: map[string]string{
				"CATALYZE_JOB_ID": id,
				"PORT":            "8080",
			},
			TenantToken: "default",
			Incomplete:  false,
		},
	}
	job, err := schedulerClient.AddJob(jr)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanUp(id)
	if job.State < agent.Running {
		err = waitForStatus(job.ID, agent.Running, agent.Scheduled, time.Second*120)
		if err != nil {
			t.Fatal(err)
		}
	}

	// ask agent over domain socket for state - should be running
	agentClient := agent.NewSocketClientFromPath("./state/agent.sock")
	agentClient.Start()
	jobMap, err := agentClient.ListJob(id)
	if err != nil {
		t.Fatalf("failed to query the local agent for job ID %s: %s", id, err)
	}
	if jobMap[id].State != agent.Running {
		t.Fatalf("unexpected job state reported by the agent: expected %d but got %d", agent.Running, jobMap[id].State)
	}

	// make job go unhealthy
	stdout, stderr, exitCode, err := dockerClient.ExecContainer([]string{"/usr/bin/curl", "http://127.0.0.1:8080/off"}, job.ID)
	if err != nil {
		t.Fatalf("job exec %s: stdout: %s stderr: %s exitcode: %d\n%s\n", job.ID, stdout, stderr, exitCode, err)
	}
	err = waitForStatus(job.ID, agent.Unhealthy, agent.Running, time.Second*20)
	if err != nil {
		t.Fatal(err)
	}

	// ask agent over domain socket for state - should be unhealthy
	jobMap, err = agentClient.ListJob(id)
	if err != nil {
		t.Fatalf("failed to query the local agent for job ID %s: %s", id, err)
	}
	if jobMap[id].State != agent.Unhealthy {
		t.Fatalf("unexpected job state reported by the agent: expected %d but got %d", agent.Unhealthy, jobMap[id].State)
	}
}

// TestBuildPackJob will only work if the catalyze docker registry is running locally
// at registry.local:5000, otherwise it will fail to download the buildstep image.
func TestBuildPackJob(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping buildpack test on short run...")
	}
	t.Parallel()
	buildID := "jobtest3"
	runID := "jobtest3run"
	cleanUp(buildID)
	cleanUp(runID)
	jr := &agent.JobRequest{
		ID:   buildID,
		Type: "build",
		Name: "test-env3/test-pod/code",
		Description: &agent.JobDescription{
			Tenant: "test-env3",
		},
		Payload: &agent.JobPayload{
			Template: "buildpackBuild",
			Registry: "registry.local:5001",
			Tags:     []string{"latest"},
			TemplateValues: map[string]string{
				"baseImage": "registry.local:5000/catalyzeio/buildstep:master.2.0.201702031608-dadd81",
				"gitHost":   "github.com",
				"gitPort":   "22",
				"gitKey":    "",
				"gitRepo":   "https://github.com/catalyzeio/nodejs-worker-example.git",
			},
		},
	}
	job, err := schedulerClient.AddQueueJob(jr)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanUp(buildID)
	// wait for job to exist
	now := time.Now()
	for {
		job, err = schedulerClient.GetJob(jr.ID)
		if err == nil {
			break
		}
		if time.Now().Sub(now) > (time.Second * 120) {
			t.Fatal("timed out waiting for build job to run")
		}
	}
	if job.State < agent.Running {
		err = waitForStatus(job.ID, agent.Running, agent.Scheduled, time.Second*120)
		if err != nil {
			t.Fatal(err)
		}
	}
	err = waitForStatus(job.ID, agent.Finished, agent.Running, time.Minute*30)
	if err != nil {
		t.Fatal(err)
	}
	jr = &agent.JobRequest{
		ID:   runID,
		Type: "docker",
		Name: "test-env3/test-pod/code",
		Description: &agent.JobDescription{
			Conflicts: []string{"code"},
			Requires:  []string{"agent.no-zone"},
			Tenant:    "test-env3",
			Provides:  []string{"code"},
			Resources: &agent.JobLimits{
				Memory: 1073741824,
			},
		},
		Payload: &agent.JobPayload{
			DockerImage: "registry.local:5001/test-env3/test-pod/code",
			Limits: &agent.JobLimits{
				Memory:    1073741824,
				CPUShares: 1024,
			},
			Environment: map[string]string{
				"CATALYZE_JOB_ID": runID,
				"PORT":            "8080",
			},
			TenantToken: "default",
			Incomplete:  false,
		},
	}
	job, err = schedulerClient.AddJob(jr)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanUp(runID)
	if job.State < agent.Running {
		err = waitForStatus(job.ID, agent.Running, agent.Scheduled, time.Second*120)
		if err != nil {
			t.Fatal(err)
		}
	}
	stdout, stderr, exitCode, err := dockerClient.ExecContainer([]string{"/usr/bin/clog", "-v"}, job.ID)
	if err != nil {
		t.Fatalf("job exec %s: stdout: %s stderr: %s exitcode: %d\n%s\n", job.ID, stdout, stderr, exitCode, err)
	}
	stdout, stderr, exitCode, err = dockerClient.ExecContainer([]string{"/bin/cat", "/app/.datica/pre-build-status"}, job.ID)
	if err != nil {
		t.Fatalf("job exec %s: stdout: %s stderr: %s exitcode: %d\n%s\n", job.ID, stdout, stderr, exitCode, err)
	}
	if strings.TrimSpace(stdout) != "pre build hook" {
		t.Fatalf("job %s: pre build hook did not execute correctly, got, \"%s\", as file status value", job.ID, stdout)
	}
	stdout, stderr, exitCode, err = dockerClient.ExecContainer([]string{"/bin/cat", "/app/.datica/post-build-status"}, job.ID)
	if err != nil {
		t.Fatalf("job exec %s: stdout: %s stderr: %s exitcode: %d\n%s\n", job.ID, stdout, stderr, exitCode, err)
	}
	if strings.TrimSpace(stdout) != "post build hook" {
		t.Fatalf("job %s: post build hook did not execute correctly, got, \"%s\", as file status value", job.ID, stdout)
	}
}

var registryJobStatesData = []struct {
	name     string
	location string
	running  bool
}{
	{"job1", "127.0.0.1:8080", false},
	{"job2", "192.168.0.1:8081", true},
}

func TestRegistryJobStates(t *testing.T) {
	t.Parallel()
	for _, data := range registryJobStatesData {
		// advertise
		ads := []registry.Advertisement{registry.Advertisement{Name: data.name, Location: data.location, Running: data.running}}
		err := registryClient.Advertise(ads)
		if err != nil {
			t.Fatalf("failed to advertise a job on the registry: %s", err)
		}

		// enumerate and test
		enum, err := registryClient.Enumerate()
		if err != nil {
			t.Fatalf("failed to enumerate entries in the registry: %s", err)
		}
		weightedLocations, ok := enum.Provides[data.name]
		if !ok {
			t.Fatalf("expected to find %s in the registry, but found %+v", data.name, enum.Provides)
		}
		if len(weightedLocations) != 1 {
			t.Fatalf("only one weighted location expected in the registry, found %+v", weightedLocations)
		}
		if weightedLocations[0].Location != data.location {
			t.Fatalf("unexpected location - expected %s but got %s", data.location, weightedLocations[0].Location)
		}
		if weightedLocations[0].Running != data.running {
			t.Fatalf("unexpected running value - expected %t but got %t", data.running, weightedLocations[0].Running)
		}
		if weightedLocations[0].Weight != 1.0 {
			t.Fatalf("unexpected weight - expected 1.0 but got %v", weightedLocations[0].Weight)
		}
	}
}
