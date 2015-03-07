package registry

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/catalyzeio/shadowfax/proto"
)

const (
	TenantTokenEnvVar = "REGISTRY_TENANT_TOKEN"
)

type Advertisement struct {
	Name     string `json:"name"`
	Location string `json:"location"`
}

type SauronRegistry struct {
	Tenant string

	client *proto.SyncClient
	ads    []Advertisement
}

type weightedLocation struct {
	Location string  `json:"location"`
	Weight   float32 `json:"weight"`
}

type message struct {
	Type string `json:"type"`

	// authenticate request
	Tenant string  `json:"tenant,omitempty"`
	Token  *string `json:"token,omitempty"`

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

const (
	pingInterval = 15 * time.Second
)

func NewRegistry(tenant string, host string, port int, config *tls.Config) *SauronRegistry {
	reg := SauronRegistry{
		Tenant: tenant,

		client: proto.NewSyncClient(host, port, config, pingInterval),
	}
	reg.client.Handshaker = reg.handshake
	reg.client.IdleHandler = reg.idle
	return &reg
}

func (reg *SauronRegistry) Start(ads []Advertisement) {
	reg.ads = ads
	reg.client.Start()
}

func (reg *SauronRegistry) Stop() {
	reg.client.Stop()
}

func (reg *SauronRegistry) Query(requires string) (*string, error) {
	resp, err := call(reg.client, &message{Type: "query", Requires: requires})
	if err != nil {
		return nil, err
	}
	return &resp.Location, nil
}

func (reg *SauronRegistry) QueryAll(requires string) ([]string, error) {
	resp, err := call(reg.client, &message{Type: "queryAll", Requires: requires})
	if err != nil {
		return nil, err
	}
	var locations []string
	for _, wloc := range resp.Locations {
		locations = append(locations, wloc.Location)
	}
	return locations, nil
}

func call(caller proto.SyncCaller, req *message) (*message, error) {
	reqMsg, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	respMsg, err := caller.SyncCall(reqMsg)
	if err != nil {
		return nil, err
	}
	resp := message{}
	err = json.Unmarshal(respMsg, &resp)
	if err != nil {
		return nil, err
	}
	if resp.Type == "error" {
		return nil, fmt.Errorf("registry operation %s failed: %s", req.Type, resp.Message)
	}
	return &resp, nil
}

func (reg *SauronRegistry) handshake(caller proto.SyncCaller) error {
	token := os.Getenv(TenantTokenEnvVar)
	req := message{
		Type:   "authenticate",
		Tenant: reg.Tenant,
		Token:  &token,
	}
	_, err := call(caller, &req)
	if err != nil {
		return err
	}
	log.Printf("Authenticated as %s", reg.Tenant)

	if reg.ads != nil {
		req := message{
			Type:     "advertise",
			Provides: reg.ads,
		}
		_, err := call(caller, &req)
		if err != nil {
			return err
		}
		log.Printf("Advertised %v", reg.ads)
	}

	return nil
}

func (reg *SauronRegistry) idle(caller proto.SyncCaller) error {
	_, err := call(caller, &message{Type: "ping"})
	return err
}
