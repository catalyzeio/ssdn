package cmd

import (
	"crypto/tls"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path"
	"strings"
	"time"

	"github.com/catalyzeio/go-core/comm"
	"github.com/catalyzeio/go-core/simplelog"

	"github.com/catalyzeio/paas-orchestration/registry"
)

const stateFile = "registry.json"

type stringslice []string

func (s *stringslice) String() string {
	return strings.Join(*s, ", ")
}

func (s *stringslice) Set(value string) error {
	*s = append(*s, value)
	return nil
}

var (
	log       *simplelog.SimpleLogger
	endpoints stringslice
)

func RegServer() {
	simplelog.AddFlags()
	comm.AddListenFlags(true, registry.DefaultPort, false)
	comm.AddTLSFlags()
	authFlag := flag.String("auth", "none", "which authentication mode to use")
	backendFlag := flag.String("backend", "memory", "which backend to use (memory or etcd)")
	stateDirFlag := flag.String("state-dir", "./state", "where to store state information")
	flag.Var(&endpoints, "endpoints", "list of etcd endpoints in the form 127.0.0.1:2379")
	flag.Parse()

	log = simplelog.NewLogger("registryserver")

	listenAddress, err := comm.GetListenAddress()
	if err == nil && listenAddress == nil {
		err = fmt.Errorf("-listen is required")
	}
	if err != nil {
		fail("Invalid listener config: %s\n", err)
	}

	config, err := comm.GenerateTLSConfig(false)
	if err != nil {
		fail("Invalid TLS config: %s\n", err)
	}

	authValue := *authFlag
	if authValue != "none" {
		fail("Auth mode %s not available\n", authValue)
	}

	var seedData *map[string]*registry.Enumeration

	var backend registry.Backend
	backendValue := *backendFlag
	switch backendValue {
	case "memory":
		stateFilePath := path.Join(*stateDirFlag, stateFile)
		if _, err = os.Stat(stateFilePath); os.IsNotExist(err) {
			log.Warn("Seed data file %s does not exist, skipping import", stateFilePath)
		} else {
			sD, err := GetEnumerationFromFile(stateFilePath)
			if err != nil {
				log.Warn("Error getting seed data from %s: %s", stateFile, err)
			} else {
				seedData = &sD
			}
		}

		backend = registry.NewMemoryBackend(seedData)

		go TrackRegistryState(stateFilePath, backend)
	case "etcd":
		backend = registry.NewEtcdBackend(endpoints)
		if backend == nil {
			fail("Failed to construct a(n) %s backend", backendValue)
		}
	case "multi":
		// TODO add multi-master backend
		fail("Backend multi is not available\n")
	default:
		fail("Unsupported backend: %s\n", backendValue)
	}
	if seedData != nil {
		go RemoveSeed(backend)
	}
	RegServerArgs(listenAddress, config, backend)
}

func RegServerArgs(listenAddress *comm.Address, config *tls.Config, backend registry.Backend) {
	listener := registry.NewListener(listenAddress, config, backend)
	if err := listener.Listen(); err != nil {
		fail("Failed to start listener: %s\n", err)
	}
}

func GetEnumerationFromFile(path string) (map[string]*registry.Enumeration, error) {
	v := make(map[string]*registry.Enumeration)
	f, err := os.Open(path)
	if err != nil {
		return v, err
	}
	defer f.Close()
	dec := json.NewDecoder(f)
	err = dec.Decode(&v)
	return v, err
}

func RemoveSeed(be registry.Backend) {
	time.Sleep(time.Minute)
	err := be.RemoveSeed()
	if err != nil {
		log.Warn("Error removing seed data from registry backend: %s", err)
	}
}

func TrackRegistryState(stateFile string, be registry.Backend) {
	ticker := time.NewTicker(time.Minute * 4)
	defer ticker.Stop()
	trapCh := make(chan os.Signal, 1)
	// we use interrupt, because that's what supervisord sends on "stop"
	signal.Notify(trapCh, os.Interrupt)
	noTrap := true
	exitCode := 0
	for noTrap {
		select {
		case <-ticker.C:
		case <-trapCh:
			noTrap = false
		}
		err := DumpBackendToFile(stateFile, be)
		if err != nil {
			log.Warn("Error writing registry state to registry state file %s: %s", stateFile, err)
			if !noTrap {
				exitCode = 1
			}
		}
	}
	os.Exit(exitCode)
}

func DumpBackendToFile(stateFile string, be registry.Backend) error {
	tmpFile := fmt.Sprintf("%s.%s", stateFile, ".tmp")
	enum, err := be.EnumerateAll()
	if err != nil {
		return err
	}
	f, err := os.OpenFile(tmpFile, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	err = json.NewEncoder(f).Encode(&enum)
	if err != nil {
		return err
	}
	return os.Rename(tmpFile, stateFile)
}
