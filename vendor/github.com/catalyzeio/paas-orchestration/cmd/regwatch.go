package cmd

import (
	"flag"
	"path"
	"path/filepath"
	"time"

	"github.com/catalyzeio/go-core/comm"
	"github.com/catalyzeio/go-core/simplelog"

	"github.com/catalyzeio/paas-orchestration/registry"
)

func RegWatch() {
	log := simplelog.NewLogger("regwatch")

	simplelog.AddFlags()
	registry.AddFlags(true)
	comm.AddTLSFlags()
	tenantFlag := flag.String("tenant", "", "tenant [required]")
	configDirFlag := flag.String("config-dir", "/etc/regwatch", "location of configuration files")
	flag.Parse()

	tenant := *tenantFlag
	if len(tenant) == 0 {
		fail("Invalid tenant config: -tenant is required\n")
	}

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

	glob := path.Join(*configDirFlag, "*.json")
	configFiles, err := filepath.Glob(glob)
	if err != nil {
		fail("Could not enumerate config directory: %s\n", err)
	}

	var templates []*registry.ConfigTemplate
	for _, v := range configFiles {
		log.Info("Loading configuration %s", v)
		template, err := registry.LoadConfigTemplate(v)
		if err != nil {
			fail("Failed to load configuration %s: %s\n", v, err)
		}
		templates = append(templates, template)
	}

	client.Start(nil, true)

	for {
		<-client.Changes
		enum, err := client.Enumerate()
		if err != nil {
			log.Warn("Error querying registry: %s", err)
			continue
		}
		for _, template := range templates {
			if err := template.Update(template.Translate(enum)); err != nil {
				log.Warn("Failed to update %s: %s", template.Destination, err)
			}
		}
		time.Sleep(time.Second)
	}
}
