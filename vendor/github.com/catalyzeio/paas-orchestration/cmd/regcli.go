package cmd

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/catalyzeio/go-core/comm"
	"github.com/catalyzeio/go-core/simplelog"
	"github.com/pborman/uuid"

	"github.com/catalyzeio/paas-orchestration/registry"
)

type Environment struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func RegCLI() {
	simplelog.AddFlags()
	registry.AddFlags(true)
	comm.AddTLSFlags()
	tenantFlag := flag.String("tenant", "", "tenant")
	rawFlag := flag.Bool("raw", false, "raw json output")
	tenantFileFlag := flag.String("tenants-file", "", "tenants file")
	destinationFlag := flag.String("destination", "", "destination file")
	flag.Parse()

	log = simplelog.NewLogger("regcli")

	var destination io.Writer
	destinationFile := *destinationFlag
	if len(destinationFile) == 0 {
		destination = os.Stdout
	} else {
		f, err := os.OpenFile(destinationFile, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
		if err != nil {
			fail("Failed to open destination file: %s\n", err)
		}
		defer f.Close()
		destination = f
	}
	tenant := *tenantFlag
	tenantsFile := *tenantFileFlag
	switch {
	case len(tenant) > 0:
		raw := *rawFlag
		EnumerateTenant(destination, tenant, raw)
	case len(tenantsFile) > 0:
		EnumerateTenants(destination, tenantsFile)
	default:
		fail("Either a tenant or tenants-file must be specified\n")
	}
}

func getTenantClient(tenant string) *registry.Client {
	config, err := comm.GenerateTLSConfig(false)
	if err != nil {
		fail("Invalid TLS config: %s\n", err)
	}

	client, err := registry.GenerateClient(tenant, config)
	if err != nil {
		fail("Failed to start registry client: %s\n", err)
	}
	if client == nil {
		fail("Invalid registry config: -registry is required\n")
	}

	host, err := os.Hostname()
	if err != nil {
		host = "unknown"
	}
	ad := registry.Advertisement{Name: "watcher", Location: fmt.Sprintf("%s.%s", host, uuid.New())}
	ads := []registry.Advertisement{ad}
	client.Start(ads, true)
	return client
}

func EnumerateTenant(destination io.Writer, tenant string, raw bool) {
	client := getTenantClient(tenant)
	jsonEnc := json.NewEncoder(destination)
	for {
		<-client.Changes
		enum, err := client.Enumerate()
		if err != nil {
			log.Warn("Error querying registry: %s", err)
			time.Sleep(1 * time.Second)
		} else {
			if raw {
				if err := jsonEnc.Encode(enum); err != nil {
					log.Warn("Error encoding registry: %s", err)
				}
			} else {
				log.Info("Registry contents: %+v", enum)
			}
		}
	}
}

func EnumerateTenants(destination io.Writer, tenantsFile string) {
	tF, err := os.Open(tenantsFile)
	if err != nil {
		fail("Error opening tenants file %s: %s\n", tenantsFile, err)
	}
	var tenants []Environment
	if err := json.NewDecoder(tF).Decode(&tenants); err != nil {
		fail("Error decoding tenants file %s: %s\n", tenantsFile, err)
	}
	tenantMap := make(map[string]*registry.Enumeration)
	for _, tenant := range tenants {
		client := getTenantClient(tenant.ID)
		<-client.Changes
		enum, err := client.Enumerate()
		if err != nil {
			fail("Error enumerating tenant %s: %s\n", tenant.ID, err)
		}
		tenantMap[tenant.ID] = enum
	}
	if err := json.NewEncoder(destination).Encode(&tenantMap); err != nil {
		fail("Error encoding tenants file to enumeration destination: %s\n", err)
	}
}
