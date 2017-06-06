package watch

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net"
	"net/url"
	"os"
	"path"
	"reflect"
	"time"

	"github.com/catalyzeio/go-core/comm"
	"github.com/catalyzeio/go-core/udocker"
	"github.com/catalyzeio/paas-orchestration/agent"
	"github.com/catalyzeio/paas-orchestration/registry"
	"github.com/catalyzeio/ssdn/watch/types"
	"github.com/hoisie/mustache"
)

const (
	startDelimiter = "## START WATCH UPDATE ##"
	endDelimiter   = "## END WATCH UPDATE ##"
)

type ContainerDNS struct {
	dc *udocker.Client
	rc *registry.Client
	ac *agent.Client

	tenant       string
	outputDir    string
	templatePath string

	advertiseJobState bool
}

func NewContainerDNS(dc *udocker.Client, rc *registry.Client, ac *agent.Client, tenant, outputDir, confDir string, advertiseJobState bool) *ContainerDNS {
	return &ContainerDNS{
		dc: dc,
		rc: rc,
		ac: ac,

		tenant:       tenant,
		outputDir:    outputDir,
		templatePath: path.Join(confDir, "cdns.d", "data.mustache"),

		advertiseJobState: advertiseJobState,
	}
}

func (c *ContainerDNS) Watch() {
	go c.advertise()
	go c.query()
}

type serviceSet map[string]locationSet
type locationSet map[string]bool

func (s serviceSet) add(name, location string, running bool) {
	locs, present := s[name]
	if !present {
		locs = make(locationSet)
		s[name] = locs
	}
	locs[location] = running
}

func (s serviceSet) toAds() []registry.Advertisement {
	var ads []registry.Advertisement
	for name, locs := range s {
		for loc, running := range locs {
			ads = append(ads, registry.Advertisement{
				Name:     name,
				Location: loc,
				Running:  running,
			})
		}
	}
	return ads
}

func (c *ContainerDNS) advertise() {
	var set serviceSet
	var containers []udocker.ContainerSummary
	var err error
	dc := c.dc
	rc := c.rc

	// we can't do anything useful without an initial list of containers
	for {
		containers, err = dc.ListContainers(false)
		if err != nil {
			log.Warn("Error querying initial list of Docker containers: %s", err)
			time.Sleep(types.DockerRetryInterval)
		} else {
			break
		}
	}

	changes := dc.Watch()
	freq := types.UnstableWatchInterval
	timer := time.NewTimer(freq)
	for ; ; timer.Reset(freq) {
		// wait for container state changes or a timer
		select {
		case <-changes:
			// grab list of containers
			if log.IsDebugEnabled() {
				log.Debug("Updating list of containers")
			}
			containers, err = dc.ListContainers(false)
			if err != nil {
				log.Warn("Error querying list of Docker containers: %s", err)
				time.Sleep(types.DockerRetryInterval)
				continue
			}
		case <-timer.C:
			// refresh advertisements if the current set has changed
			newSet := c.extractSet(containers)
			if !reflect.DeepEqual(set, newSet) {
				freq = types.UnstableWatchInterval
				ads := newSet.toAds()
				log.Info("Updating registry advertisements: %s", ads)
				if err := rc.Advertise(ads); err != nil {
					log.Warn("Error updating registry: %s", err)
					time.Sleep(types.RegistryRetryInterval)
					continue
				}
				set = newSet
			} else {
				freq = types.StableWatchInterval
			}
		}
	}
}

func (c *ContainerDNS) extractSet(containers []udocker.ContainerSummary) serviceSet {
	ac := c.ac
	set := make(serviceSet)
	for _, container := range containers {
		tenant, present := container.Labels[types.TenantLabel]
		// only examine containers belonging to this tenant
		if !present || tenant != c.tenant {
			continue
		}
		if log.IsTraceEnabled() {
			log.Trace("Container %s belongs to tenant %s", container.Id, c.tenant)
		}
		// check if the container has any service data
		data, present := container.Labels[types.ServicesLabel]
		if !present {
			continue
		}
		// extract service data
		var services []types.Service
		if err := json.Unmarshal([]byte(data), &services); err != nil {
			log.Warn("Container %s has invalid service definition: %s", container.Id, err)
			continue
		}
		if log.IsTraceEnabled() {
			log.Trace("Container %s services: %v", container.Id, services)
		}
		running := true
		if c.advertiseJobState {
			// check if the container has a job label
			jobID, present := container.Labels[agent.JobLabel]
			if !present {
				continue
			}
			jobDetails, err := ac.ListJob(jobID)
			if err != nil || jobDetails == nil || jobDetails[jobID] == nil {
				log.Warn("Error retrieving job %s from the agent: %s", jobID, err)
				running = false
			} else {
				running = jobDetails[jobID].State == agent.Running
			}
		}

		// add service data to results
		for _, v := range services {
			set.add(v.Name, v.Location, running)
		}
	}
	return set
}

func (c *ContainerDNS) query() {
	var oldCtx map[string]interface{}
	var dataFile *os.File
	var err error

	dataFilePath := path.Join(c.outputDir, "data")
	for {
		dataFile, err = os.OpenFile(dataFilePath, os.O_WRONLY, 0644)
		if err != nil {
			log.Errorf("Failed to open %s for writing: %s - retrying", dataFilePath, err)
			time.Sleep(1 * time.Second)
			continue
		}
		break
	}

	rc := c.rc
	for {
		// wait for more changes
		<-rc.Changes

		// get dump of all services
		enum, err := rc.Enumerate()
		if err != nil {
			log.Warn("Error querying registry: %s", err)
			time.Sleep(types.RegistryRetryInterval)
			continue
		}

		// ignore spurious updates
		ctx := c.templateContext(enum)
		if reflect.DeepEqual(ctx, oldCtx) {
			continue
		}

		// update data file
		if err := c.render(ctx, dataFile); err != nil {
			log.Warn("Failed to update DNS configuration: %s", err)
			continue
		}

		oldCtx = ctx
	}
}

func (c *ContainerDNS) templateContext(enum *registry.Enumeration) map[string]interface{} {
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
					"name":   comm.SanitizeService(service),
					"scheme": u.Scheme,
					"host":   host,
					"port":   port,
				})
			}
		}
	}
	return map[string]interface{}{
		"provides": provides,
	}
}

func (c *ContainerDNS) render(ctx map[string]interface{}, dataFile *os.File) error {
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
	if _, err := dataFile.WriteString(fmt.Sprintf("%s\n%s\n%s\n", startDelimiter, renderedTemplate, endDelimiter)); err != nil {
		return err
	}
	log.Info("Updated %s", dataFile.Name())
	return nil
}
