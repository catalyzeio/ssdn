package registry

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"log"

	"github.com/catalyzeio/shadowfax/proto"
)

type Advertisement struct {
	Name     string
	Location string
}

type SauronRegistry struct {
	Tenant string

	client *proto.ReconnectClient
	ads    []Advertisement

	txChan chan *transaction
	done   chan bool
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

type transaction struct {
	request  *message
	respChan chan *message
}

func NewRegistry(tenant string, host string, port int, config *tls.Config) *SauronRegistry {
	return &SauronRegistry{
		Tenant: tenant,
		client: proto.NewTLSClient(host, port, config),
		txChan: make(chan *transaction, 16),
	}
}

func (reg *SauronRegistry) Start(ads []Advertisement) {
	reg.ads = ads
	reg.client.Start()
	go reg.run()
}

func (reg *SauronRegistry) Query(requires string) (*string, error) {
	request := message{
		Type:     "query",
		Requires: requires,
	}
	resp, err := reg.transact(&request)
	if err != nil {
		return nil, err
	}
	return &resp.Location, nil
}

func (reg *SauronRegistry) QueryAll(requires string) ([]string, error) {
	request := message{
		Type:     "queryAll",
		Requires: requires,
	}
	resp, err := reg.transact(&request)
	if err != nil {
		return nil, err
	}

	var locations []string
	for _, wloc := range resp.Locations {
		locations = append(locations, wloc.Location)
	}
	return locations, nil
}

func (reg *SauronRegistry) Stop() {
	reg.done <- true
	reg.client.Stop()
}

func (reg *SauronRegistry) transact(request *message) (*message, error) {
	respChan := make(chan *message)
	tx := transaction{
		request:  request,
		respChan: respChan,
	}
	reg.txChan <- &tx
	resp := <-respChan
	if resp.Type == "error" {
		return nil, fmt.Errorf("registry operation %s failed: %s", request.Type, resp.Message)
	}
	return resp, nil
}

func (reg *SauronRegistry) run() {
	for {
		// wait for connection event, or bail if stopped
		if reg.waitForConnection() {
			return
		}
		// do message loop until next disconnection, or bail if stopped
		if reg.msgLoop() {
			return
		}
	}
}

func (reg *SauronRegistry) waitForConnection() bool {
	e := reg.client.Events
	for {
		select {
		case <-reg.done:
			return true
		case event := <-e:
			switch event {
			case proto.Connected:
				return false
			}
		}
	}
}

func (reg *SauronRegistry) msgLoop() bool {
	// send auth message
	authMsg := message{
		Type:   "authenticate",
		Tenant: reg.Tenant,
		Token:  "foo",
	}
	respMsg, err := reg.sendRecv(&authMsg)
	if err != nil {
		log.Printf("Failed to authenticate with registry: %s", err)
		return false
	}
	if respMsg.Type != "authenticated" {
		log.Printf("Failed to authenticate with registry: %s -> %s", respMsg.Type, respMsg.Message)
		return false
	}

	// TODO ping/pong every 15s of inactivity
	// send other messages
	for {
		select {
		case <-reg.done:
			return true
		case tx := <-reg.txChan:
			respMsg, err := reg.sendRecv(tx.request)
			if err != nil {
				respMsg = &message{
					Type:    "error",
					Message: err.Error(),
				}
			}
			tx.respChan <- respMsg
		}
	}
}

func (reg *SauronRegistry) sendRecv(request *message) (*message, error) {
	client := reg.client
	// send request
	reqBytes, err := json.Marshal(request)
	if err != nil {
		// hopefully the json encoding error is transient; disconnect should eventually trigger retry
		client.Disconnect()
		return nil, err
	}
	client.Out <- append(reqBytes, '\n')

	// wait for response to come back or client to disconnect
	in := client.In
	e := client.Events
	for {
		select {
		case event := <-e:
			switch event {
			case proto.Disconnected:
				return nil, fmt.Errorf("disconnected from registry")
			}
		case respBytes := <-in:
			response := message{}
			err = json.Unmarshal(respBytes, &response)
			if err != nil {
				// server sent bogus JSON; disconnect
				client.Disconnect()
				return nil, err
			}
			return &response, nil
		}
	}
}
