package registry

import (
	"crypto/tls"
)

type Advertisement struct {
	Name     string
	Location string
}

type SauronRegistry struct {
	Tenant string

	ads []Advertisement
}

type weightedLocation struct {
	Location string  `json:"location"`
	Weight   float32 `json:"weight"`
}

type message struct {
	Type string `json:"type"`

	// authenticate request
	Tenant string `json:"tenant,omitempty"`
	Token  string `json:"token,omitempty"`

	// advertise request
	Provides  []Advertisement `json:"provides,omitempty"`
	Publishes []Advertisement `json:"publishes,omitempty"`

	// query, queryAll request
	Requires string `json:"requires,omitempty"`

	// query response
	Location string `json:"location,omitempty"`

	// queryAll response
	Locations []weightedLocation `json:"locations,omitempty"`

	// error responses
	Message string `json:"message,omitempty"`
}

func NewRegistry(tenant string, host string, port int, config *tls.Config) *SauronRegistry {
	return &SauronRegistry{
		Tenant: tenant,
	}
}

func (reg *SauronRegistry) Start(ads []Advertisement) {
	reg.ads = ads
	// TODO
}

func (reg *SauronRegistry) Stop() {
	// TODO
}

func (reg *SauronRegistry) Query(requires string) (*string, error) {
	// TODO
	return nil, nil
}

func (reg *SauronRegistry) QueryAll(requires string) ([]string, error) {
	// TODO
	return nil, nil
}
