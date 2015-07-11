package watch

import (
	"encoding/json"
	"io/ioutil"
	"net"
	"net/url"
	"os"
	"path"
	"reflect"
	"time"

	"github.com/hoisie/mustache"

	"github.com/catalyzeio/go-core/udocker"
	"github.com/catalyzeio/paas-orchestration/registry"
)

const (
	dockerRetryInterval   = 15 * time.Second
	registryRetryInterval = 5 * time.Second
)

type ContainerDNS struct {
	dc *udocker.Client
	rc *registry.Client

	tenant       string
	dataDir      string
	templatePath string
}

func NewContainerDNS(dc *udocker.Client, rc *registry.Client, tenant, runDir, confDir string) *ContainerDNS {
	return &ContainerDNS{
		dc: dc,
		rc: rc,

		tenant:       tenant,
		dataDir:      path.Join(runDir, tenant, "cdns"),
		templatePath: path.Join(confDir, "cdns.d", "data.mustache"),
	}
}

func (c *ContainerDNS) Watch() {
	go c.advertise()
	go c.query()
}

type serviceSet map[string]locationSet
type locationSet map[string]struct{}

func (s serviceSet) add(name, location string) {
	locs, present := s[name]
	if !present {
		locs = make(locationSet)
		s[name] = locs
	}
	locs[location] = struct{}{}
}

func (s serviceSet) toAds() []registry.Advertisement {
	var ads []registry.Advertisement
	for name, locs := range s {
		for loc, _ := range locs {
			ads = append(ads, registry.Advertisement{
				Name:     name,
				Location: loc,
			})
		}
	}
	return ads
}

func (c *ContainerDNS) advertise() {
	var set serviceSet

	dc := c.dc
	rc := c.rc

	changes := dc.Watch()
	for {
		// grab list of containers
		if log.IsDebugEnabled() {
			log.Debug("Updating list of containers")
		}
		containers, err := dc.ListContainers(false)
		if err != nil {
			log.Warn("Error querying list of Docker containers: %s", err)
			time.Sleep(dockerRetryInterval)
			continue
		}

		// refresh advertisements if the current set has changed
		newSet := c.extractSet(containers)
		if !reflect.DeepEqual(set, newSet) {
			ads := newSet.toAds()
			log.Info("Updating registry advertisements: %s", ads)
			if err := rc.Advertise(ads); err != nil {
				log.Warn("Error updating registry: %s", err)
				time.Sleep(registryRetryInterval)
				continue
			}
			set = newSet
		}

		// wait for container state changes
		<-changes
	}
}

func (c *ContainerDNS) extractSet(containers []udocker.ContainerSummary) serviceSet {
	set := make(serviceSet)
	for _, container := range containers {
		tenant, present := container.Labels[TenantLabel]
		// only examine containers belonging to this tenant
		if !present || tenant != c.tenant {
			continue
		}
		if log.IsTraceEnabled() {
			log.Trace("Container %s belongs to tenant %s", container.Id, c.tenant)
		}
		// check if the container has any service data
		data, present := container.Labels[ServicesLabel]
		if !present {
			continue
		}
		// extract service data
		var services []Service
		if err := json.Unmarshal([]byte(data), &services); err != nil {
			log.Warn("Container %s has invalid service definition: %s", container.Id, err)
			continue
		}
		if log.IsTraceEnabled() {
			log.Trace("Container %s services: %v", container.Id, services)
		}
		// add service data to results
		for _, v := range services {
			set.add(v.Name, v.Location)
		}
	}
	return set
}

func (c *ContainerDNS) query() {
	rc := c.rc
	for {
		// wait for more changes
		<-rc.Changes

		// get dump of all services
		enum, err := rc.Enumerate()
		if err != nil {
			log.Warn("Error querying registry: %s", err)
			time.Sleep(registryRetryInterval)
			continue
		}

		// update data file
		if err := c.render(enum); err != nil {
			log.Warn("Failed to update DNS configuration: %s", err)
		}
	}
}

func (c *ContainerDNS) render(enum *registry.Enumeration) error {
	// translate the returned data
	var provides []map[string]string
	if enum != nil {
		for service, locations := range enum.Provides {
			for _, location := range locations {
				u, err := url.Parse(location.Location)
				if err != nil {
					log.Warn("Invalid location for %s: %s", service, err)
					continue
				}
				host, port, err := net.SplitHostPort(u.Host)
				if err != nil {
					// no port
					host = u.Host
				}
				provides = append(provides, map[string]string{
					"name":   registry.Sanitize(service),
					"scheme": u.Scheme,
					"host":   host,
					"port":   port,
				})
			}
		}
	}
	ctx := map[string]interface{}{
		"provides": provides,
	}

	// load template
	template, err := ioutil.ReadFile(c.templatePath)
	if err != nil {
		return err
	}

	// render it
	renderedTemplate := mustache.Render(string(template), ctx)
	if log.IsTraceEnabled() {
		log.Trace("Rendered template: %s", renderedTemplate)
	}

	// dump it out
	if err := os.MkdirAll(c.dataDir, 0755); err != nil {
		return err
	}
	dataFile := path.Join(c.dataDir, "data")
	if err := ioutil.WriteFile(dataFile, []byte(renderedTemplate), 0644); err != nil {
		return err
	}
	log.Info("Updated %s", dataFile)
	return nil
}