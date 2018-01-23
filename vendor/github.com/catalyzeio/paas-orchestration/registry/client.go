package registry

import (
	"crypto/tls"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/catalyzeio/go-core/comm"
)

const (
	TenantTokenEnvVar = "REGISTRY_TENANT_TOKEN"

	normalPollInterval = 15 * time.Second
	safetyPollInterval = 5 * time.Minute

	pingInterval = normalPollInterval + time.Second
)

type Client struct {
	Tenant  string
	Changes <-chan *Message

	client *comm.ReconnectClient
	ads    []Advertisement

	calls    chan call
	listener chan *Message
	notify   bool
}

type result struct {
	msg *Message
	err error
}

func (r *result) finished(req *Message) {
	if r.msg.Type == "error" {
		r.err = fmt.Errorf("operation %s failed: %s", req.Type, r.msg.Message)
	}
}

type call struct {
	msg    *Message
	result chan<- result
}

func (c *call) aborted() {
	c.finish(result{err: fmt.Errorf("registry operation was aborted")})
}

func (c *call) disconnected() {
	c.finish(result{err: fmt.Errorf("disconnected from registry while processing request")})
}

func (c *call) failed(err error) {
	c.finish(result{err: err})
}

func (c *call) finish(r result) {
	if c.result == nil {
		return
	}
	c.result <- r
}

func NewClient(tenant string, host string, port int, config *tls.Config) *Client {
	listener := make(chan *Message, 1)
	c := Client{
		Tenant:   tenant,
		Changes:  listener,
		calls:    make(chan call, 1),
		listener: listener,
	}
	c.client = comm.NewClient(c.handler, host, port, config)
	return &c
}

func (c *Client) TargetURL() string {
	return c.client.TargetURL()
}

func (c *Client) Start(ads []Advertisement, notify bool) {
	c.ads = ads
	c.notify = notify
	c.client.Start()
}

func (c *Client) Stop() {
	c.client.Stop()
}

func (c *Client) Advertise(provides []Advertisement) error {
	reqMsg := Message{Type: "advertise", Provides: provides}
	_, err := c.call(&reqMsg)
	return err
}

func (c *Client) Query(requires string) (string, error) {
	reqMsg := Message{Type: "query", Requires: requires}
	respMsg, err := c.call(&reqMsg)
	if err != nil {
		return "", err
	}
	return respMsg.Location, nil
}

func (c *Client) QueryAll(requires string) ([]WeightedLocation, error) {
	reqMsg := Message{Type: "queryAll", Requires: requires}
	respMsg, err := c.call(&reqMsg)
	if err != nil {
		return nil, err
	}
	return respMsg.Locations, nil
}

func (c *Client) Enumerate() (*Enumeration, error) {
	reqMsg := Message{Type: "enumerate"}
	respMsg, err := c.call(&reqMsg)
	if err != nil {
		return nil, err
	}
	return respMsg.Registry, nil
}

func (c *Client) call(msg *Message) (*Message, error) {
	resultChan := make(chan result, 1)
	req := call{msg, resultChan}
	c.calls <- req
	result := <-resultChan
	return result.msg, result.err
}

func (c *Client) handler(conn net.Conn, abort <-chan struct{}) error {
	notifier := NewNotifier(c.listener)
	defer notifier.Stop()

	io := comm.WrapI(conn, newClientReader(conn, notifier), 1, idleTimeout)
	defer io.Stop()

	prio := make(chan call, 3)
	// authenticate first
	{
		token := os.Getenv(TenantTokenEnvVar)
		msg := Message{Type: "authenticate", Tenant: c.Tenant, Token: &token}
		prio <- call{&msg, nil}
	}
	// then send advertisements, if any
	ads := c.ads
	if ads != nil {
		msg := Message{Type: "advertise", Provides: ads}
		prio <- call{&msg, nil}
	}

	in, done := io.In, io.Done

	for {
		var req call
		// process high-priority requests first
		select {
		case req = <-prio:
		default:
			// then normal requests
			select {
			case <-abort:
				return nil
			case <-done:
				return nil
			case <-time.After(pingInterval):
				// send ping if interval is reached while waiting for a request
				req = call{&Message{Type: "ping"}, nil}
			case req = <-c.calls:
			}
		}

		// update latest ads if this is an advertise request
		reqMsg := req.msg
		reqType := reqMsg.Type
		if reqType == "advertise" {
			c.ads = reqMsg.Provides
		}

		// from this point on, a response must always be sent to the request's channel

		if err := messageWriter(conn, reqMsg); err != nil {
			req.failed(err)
			return err
		}

		var respVal interface{}
		select {
		case <-abort:
			req.aborted()
			return nil
		case <-done:
			req.disconnected()
			return nil
		case respVal = <-in:
		}
		resp := result{respVal.(*Message), nil}

		resp.finished(reqMsg)
		if req.result == nil {
			// high-priority request
			if resp.err != nil {
				return resp.err
			}
			if reqType == "authenticate" {
				log.Info("Authenticated as %s", c.Tenant)
				if c.notify {
					// configure notifications/polling
					interval := normalPollInterval
					if serverNotificationsSupported(resp.msg) {
						// request notifications from server
						msg := Message{Type: "register"}
						prio <- call{&msg, nil}
						// use lengthy poll as a guard against server issues
						interval = safetyPollInterval
					}
					notifier.Start(interval)
				}
			} else if reqType == "advertise" {
				if log.IsDebugEnabled() {
					log.Debug("Advertised %v", c.ads)
				}
			} else if reqType == "register" {
				if log.IsDebugEnabled() {
					log.Debug("Registered for changes to %s", c.Tenant)
				}
			}
		} else {
			// normal request
			if reqType == "advertise" {
				if log.IsDebugEnabled() {
					log.Debug("Advertised %v", c.ads)
				}
			}
			req.finish(resp)
		}
	}
}

func newClientReader(conn net.Conn, notifier *Notifier) comm.MessageReader {
	reader := newMessageReader(conn)
	return func(conn net.Conn) (interface{}, error) {
		msg, err := reader(conn)
		if err != nil {
			return nil, err
		}
		if msg.Type == "modified" {
			if log.IsDebugEnabled() {
				log.Debug("Sever sent change notification")
			}
			notifier.Notify(msg)
			return nil, nil
		}
		return msg, nil
	}
}

func serverNotificationsSupported(msg *Message) bool {
	if msg == nil {
		return false
	}
	major, minor, err := parseVersion(msg.Version)
	if err != nil {
		log.Warn("Server sent back invalid version: %s", err)
		return false
	}
	if log.IsDebugEnabled() {
		log.Debug("Server version: %d.%d", major, minor)
	}
	return major >= 1 && minor >= 1
}

func parseVersion(version string) (int, int, error) {
	if len(version) == 0 {
		return 0, 0, nil
	}
	components := strings.Split(version, ".")
	if len(components) < 2 {
		return 0, 0, nil
	}
	major, err := strconv.Atoi(components[0])
	if err != nil {
		return 0, 0, err
	}
	minor, err := strconv.Atoi(components[1])
	if err != nil {
		return 0, 0, err
	}
	return major, minor, nil
}
