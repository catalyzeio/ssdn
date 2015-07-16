package watch

import (
	"reflect"
	"time"

	"github.com/catalyzeio/go-core/udocker"

	"github.com/catalyzeio/ssdn/overlay"
)

type ContainerConnector struct {
	dc *udocker.Client

	tenant string
}

func NewContainerConnector(dc *udocker.Client, tenant string) *ContainerConnector {
	return &ContainerConnector{
		dc: dc,

		tenant: tenant,
	}
}

func (c *ContainerConnector) Watch(connector overlay.Connector) {
	go c.connect(connector)
}

func (c *ContainerConnector) connect(connector overlay.Connector) {
	var connections map[string]string

	dc := c.dc

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

		// update connections if the current set has changed
		newConnections := c.extractConnections(containers)
		if !reflect.DeepEqual(connections, newConnections) {
			log.Info("Updating container connections: %s", newConnections)
			connector.UpdateConnections(newConnections)
			connections = newConnections
		}

		// wait for container state changes
		<-changes
	}
}

func (c *ContainerConnector) extractConnections(containers []udocker.ContainerSummary) map[string]string {
	connections := make(map[string]string)
	for _, container := range containers {
		tenant, present := container.Labels[TenantLabel]
		// only examine containers belonging to this tenant
		if !present || tenant != c.tenant {
			continue
		}
		if log.IsTraceEnabled() {
			log.Trace("Container %s belongs to tenant %s", container.Id, c.tenant)
		}
		// grab any IP data for the container
		ip := container.Labels[IPLabel]
		if len(ip) > 0 {
			if log.IsTraceEnabled() {
				log.Trace("Container %s IP address: %s", container.Id, ip)
			}
		}
		// add data to results
		connections[container.Id] = ip
	}
	return connections
}
