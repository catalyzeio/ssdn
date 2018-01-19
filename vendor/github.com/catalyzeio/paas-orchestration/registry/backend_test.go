package registry

import (
	"fmt"
	"os"
	"testing"

	"github.com/catalyzeio/go-core/udocker"
)

const (
	etcdContainerTag   = "v3.1.7"
	etcdContainerImage = "quay.io/coreos/etcd"
	etcdContainerID    = "etcd-orch-tests"
)

var dockerClient *udocker.Client

func TestMain(m *testing.M) {
	dockerURL := os.Getenv("DOCKER_HOST")
	if len(dockerURL) == 0 {
		dockerURL = "unix:///var/run/docker.sock"
	}

	var err error
	dockerClient, err = udocker.ClientFromURL(dockerURL)
	if err != nil {
		fmt.Printf("Error setting up docker client: %s\n", err)
		os.Exit(2)
	}

	err = setUpEtcdContainer()
	if err != nil {
		fmt.Printf("Error starting etcd container: %s\n", err)
	}
	exitCode := m.Run()
	defer os.Exit(exitCode)
	cleanUp()
}

func setUpEtcdContainer() error {
	err := dockerClient.PullImageTag(etcdContainerImage, etcdContainerTag)
	if err != nil {
		return err
	}
	portKey := fmt.Sprintf("%d/tcp", 2379)
	_, err = dockerClient.CreateContainer(etcdContainerID, &udocker.ContainerDefinition{
		ExposedPorts: map[string]struct{}{
			portKey: struct{}{},
		},
		Image: fmt.Sprintf("%s:%s", etcdContainerImage, etcdContainerTag),
		HostConfig: udocker.HostConfig{
			Memory:    1073741824,
			CpuShares: 1024,
			PortBindings: map[string][]udocker.PortBinding{
				portKey: []udocker.PortBinding{udocker.PortBinding{HostIp: "0.0.0.0", HostPort: "2379"}},
			},
		},
		// from https://github.com/coreos/etcd/releases/
		Cmd: []string{
			"/usr/local/bin/etcd",
			"--name", "orch-etcd",
			"--data-dir", "/etcd-data",
			"--listen-client-urls", "http://0.0.0.0:2379",
			"--advertise-client-urls", "http://0.0.0.0:2379",
			"--listen-peer-urls", "http://0.0.0.0:2380",
			"--initial-advertise-peer-urls", "http://0.0.0.0:2380",
			"--initial-cluster", "orch-etcd=http://0.0.0.0:2380",
			"--initial-cluster-token", "orch-etcd-token",
			"--initial-cluster-state", "new",
			"--auto-compaction-retention", "1"},
	})
	if err != nil {
		return err
	}
	return dockerClient.StartContainer(etcdContainerID, nil)
}

func cleanUp() {
	err := dockerClient.StopContainer(etcdContainerID, 0)
	if err != nil {
		fmt.Printf("Failed to stop container %s: %s\n", etcdContainerID, err)
	}
	err = dockerClient.DeleteContainer(etcdContainerID)
	if err != nil {
		fmt.Printf("Failed to delete container %s: %s\n", etcdContainerID, err)
	}
}

func TestIsolation(t *testing.T) {
	for _, b := range backends(nil) {
		fooAd := Advertisement{Name: "service1", Location: "here"}
		fooMsg := advertise(t, b, "t1", fooAd)

		barAd := Advertisement{Name: "service1", Location: "there"}
		barMsg := advertise(t, b, "t2", barAd)

		if query(t, b, "t1", "service1") != "here" {
			t.Error("t1 should see here")
		}

		if query(t, b, "t2", "service1") != "there" {
			t.Error("t2 should see there")
		}
		unadvertise(t, b, "t1", fooMsg, false)
		unadvertise(t, b, "t2", barMsg, false)
	}
}

func TestReplacements(t *testing.T) {
	// test case for https://github.com/catalyzeio/paas-orchestration/issues/46
	for _, b := range backends(nil) {
		ad1 := Advertisement{Name: "service1", Location: "loc1"}
		msg1 := advertise(t, b, "t1", ad1)

		ad2 := Advertisement{Name: "service1", Location: "loc1"}
		msg2 := advertise(t, b, "t1", ad2)

		fmt.Println(query(t, b, "t1", "service1"))
		if query(t, b, "t1", "service1") != "loc1" {
			t.Error("invalid location")
		}

		unadvertise(t, b, "t1", msg1, true)

		if query(t, b, "t1", "service1") != "loc1" {
			t.Error("obsolete unadvertise request should be ignored")
		}

		unadvertise(t, b, "t1", msg2, false)

		if query(t, b, "t1", "service1") != "" {
			t.Error("query result should be empty")
		}
	}
}

func TestReplacementsReverse(t *testing.T) {
	// test case for https://github.com/catalyzeio/paas-orchestration/issues/46
	for _, b := range backends(nil) {
		ad1 := Advertisement{Name: "service1", Location: "loc1"}
		msg1 := advertise(t, b, "t1", ad1)

		ad2 := Advertisement{Name: "service1", Location: "loc1"}
		msg2 := advertise(t, b, "t1", ad2)

		if query(t, b, "t1", "service1") != "loc1" {
			t.Error("invalid location")
		}

		unadvertise(t, b, "t1", msg2, true)

		if query(t, b, "t1", "service1") != "" {
			t.Error("advertisement should have been replaced")
		}

		unadvertise(t, b, "t1", msg1, false)

		if query(t, b, "t1", "service1") != "" {
			t.Error("query result should be empty")
		}
	}
}

func backends(seedData *map[string]*Enumeration) []Backend {
	backends := []Backend{NewMemoryBackend(seedData)}
	if !testing.Short() {
		backends = append(backends, NewEtcdBackend([]string{"127.0.0.1:2379"}))
	}
	return backends
}

func advertise(t *testing.T, b Backend, tenant string, ad Advertisement) *Message {
	req := Message{Type: "advertise", Provides: []Advertisement{ad}}
	resp := Message{}
	if err := b.Advertise(tenant, &req, &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Type != "advertised" {
		t.Fatal("invalid advertise response")
	}
	return &req
}

func unadvertise(t *testing.T, b Backend, tenant string, ads *Message, notify bool) {
	if err := b.Unadvertise(tenant, ads, notify); err != nil {
		t.Fatal(err)
	}
}

func query(t *testing.T, b Backend, tenant, requires string) string {
	req := Message{Type: "query", Requires: requires}
	resp := Message{}
	if err := b.Query(tenant, &req, &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Type != "answer" {
		t.Fatal("invalid query response")
	}
	return resp.Location
}
