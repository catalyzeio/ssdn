package agent

import (
	"crypto/tls"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/catalyzeio/go-core/comm"
)

const (
	pingInterval = 15 * time.Second
)

type Result struct {
	Client  *Client
	Message *Message
	Error   error
}

type Client struct {
	URL string

	JobUpdateHandler func(*Message)

	client *comm.ReconnectClient

	calls chan call

	handlersMutex sync.RWMutex
	handlers      map[string]struct{}

	pendingMutex sync.Mutex
	pending      map[uint]*call
}

type call struct {
	msg    *Message
	result chan<- Result
}

func (c *call) finish(r Result) {
	if c.result == nil {
		// always log errors, even if they are not associated with a caller
		if r.Error != nil {
			log.Warn("Agent request failed: %s", r.Error)
		}
		return
	}
	c.result <- r
}

func NewClient(url string, host string, port int, config *tls.Config) *Client {
	c := Client{
		URL: url,

		calls: make(chan call, commQueueSize),

		pending: make(map[uint]*call),
	}
	c.client = comm.NewClient(c.handler, host, port, config)
	return &c
}

func NewSocketClient() *Client {
	return NewSocketClientFromPath("/var/run/agent.sock")
}

func NewSocketClientFromPath(path string) *Client {
	c := Client{
		URL: "unix://" + path,

		calls: make(chan call, commQueueSize),

		pending: make(map[uint]*call),
	}
	c.client = comm.NewSocketClient(c.handler, path)
	return &c
}

func (c *Client) Start() {
	c.client.Start()
}

func (c *Client) Connected() bool {
	return c.client.Connected()
}

func (c *Client) GetHandlers() map[string]struct{} {
	c.handlersMutex.RLock()
	defer c.handlersMutex.RUnlock()

	return c.handlers
}

func (c *Client) setHandlers(handlers []string) {
	if log.IsDebugEnabled() {
		log.Debug("Handlers at %s: %s", c.URL, handlers)
	}

	c.handlersMutex.Lock()
	defer c.handlersMutex.Unlock()

	m := make(map[string]struct{})
	for _, v := range handlers {
		m[v] = struct{}{}
	}
	c.handlers = m
}

func (c *Client) StopClient() {
	c.client.Stop()
}

func (c *Client) Bid(jobRequest *JobRequest) (*float64, error) {
	respMsg, err := c.call(bidRequest(jobRequest))
	if err != nil {
		return nil, err
	}
	return respMsg.Bid, nil
}

func (c *Client) BidAsync(jobRequest *JobRequest, result chan<- Result) {
	c.calls <- call{bidRequest(jobRequest), result}
}

func bidRequest(jobRequest *JobRequest) *Message {
	// always exclude payload when bidding a job
	jobCopy := *jobRequest
	jobCopy.Payload = nil
	return &Message{Type: "bid", Job: &jobCopy}
}

func (c *Client) Offer(jobRequest *JobRequest) (*JobInfo, error) {
	respMsg, err := c.call(offerRequest(jobRequest))
	if err != nil {
		return nil, err
	}
	return respMsg.Info, nil
}

func (c *Client) OfferAsync(jobRequest *JobRequest, result chan<- Result) {
	c.calls <- call{offerRequest(jobRequest), result}
}

func offerRequest(jobRequest *JobRequest) *Message {
	return &Message{Type: "offer", Job: jobRequest}
}

func (c *Client) Kill(jobID string) error {
	_, err := c.call(killRequest(jobID))
	return err
}

func (c *Client) KillAsync(jobID string, result chan<- Result) {
	c.calls <- call{killRequest(jobID), result}
}

func killRequest(jobID string) *Message {
	return &Message{Type: "kill", JobID: jobID}
}

func (c *Client) Stop(jobID string) error {
	_, err := c.call(stopRequest(jobID))
	return err
}

func (c *Client) StartJob(jobID string) error {
	_, err := c.call(startRequest(jobID))
	return err
}

func (c *Client) StopAsync(jobID string, result chan<- Result) {
	c.calls <- call{stopRequest(jobID), result}
}

func stopRequest(jobID string) *Message {
	return &Message{Type: "stop", JobID: jobID}
}

func startRequest(jobID string) *Message {
	return &Message{Type: "start", JobID: jobID}
}

func (c *Client) Patch(jobID string, payload *JobPayload) error {
	_, err := c.call(patchRequest(jobID, payload))
	return err
}

func (c *Client) PatchAsync(jobID string, payload *JobPayload, result chan<- Result) {
	c.calls <- call{patchRequest(jobID, payload), result}
}

func patchRequest(jobID string, payload *JobPayload) *Message {
	return &Message{Type: "patch", JobID: jobID, Patch: payload}
}

func (c *Client) ListJob(jobID string) (JobMap, error) {
	respMsg, err := c.call(listJobRequest(jobID))
	if err != nil {
		return nil, err
	}
	return respMsg.Jobs, nil
}

func (c *Client) ListJobAsync(jobID string, result chan<- Result) {
	c.calls <- call{listJobRequest(jobID), result}
}

func listJobRequest(jobID string) *Message {
	return &Message{Type: "listJob", JobID: jobID}
}

func (c *Client) ListJobs() (JobMap, error) {
	respMsg, err := c.call(listJobsRequest())
	if err != nil {
		return nil, err
	}
	return respMsg.Jobs, nil
}

func (c *Client) ListJobsAsync(result chan<- Result) {
	c.calls <- call{listJobsRequest(), result}
}

func listJobsRequest() *Message {
	return &Message{Type: "listJobs"}
}

func (c *Client) GetUsage() (*PolicyUsage, error) {
	respMsg, err := c.call(getUsageRequest())
	if err != nil {
		return nil, err
	}
	return respMsg.Usage, nil
}

func (c *Client) GetUsageAsync(result chan<- Result) {
	c.calls <- call{getUsageRequest(), result}
}

func getUsageRequest() *Message {
	return &Message{Type: "getUsage"}
}

func (c *Client) GetMode() (string, error) {
	respMsg, err := c.call(getModeRequest())
	if err != nil {
		return "", err
	}
	return respMsg.Mode, nil
}

func (c *Client) GetModeAsync(result chan<- Result) {
	c.calls <- call{getModeRequest(), result}
}

func getModeRequest() *Message {
	return &Message{Type: "getMode"}
}

func (c *Client) GetAgentStateAsync(result chan<- Result) {
	c.calls <- call{getAgentStateRequest(), result}
}

func getAgentStateRequest() *Message {
	return &Message{Type: "getAgentState"}
}

func (c *Client) SetMode(mode string) error {
	_, err := c.call(setModeRequest(mode))
	return err
}

func (c *Client) SetModeAsync(mode string, result chan<- Result) {
	c.calls <- call{setModeRequest(mode), result}
}

func setModeRequest(mode string) *Message {
	return &Message{Type: "setMode", Mode: mode}
}

func (c *Client) call(msg *Message) (*Message, error) {
	resultChan := make(chan Result, 1)
	req := call{msg, resultChan}
	c.calls <- req
	result := <-resultChan
	return result.Message, result.Error
}

func (c *Client) handler(conn net.Conn, abort <-chan struct{}) error {
	io := comm.WrapI(conn, c.newClientReader(conn), 0, idleTimeout)
	defer func() {
		// shut down I/O goroutines
		io.Stop()
		// flush out any pending messages
		c.abortPending()
		// reset record of agent's handlers
		c.setHandlers(nil)
	}()

	id := uint(1)

	prio := make(chan call, 1)
	// send hello first
	{
		msg := Message{Type: "hello"}
		prio <- call{&msg, nil}
	}

	done := io.Done

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

		// tag outgoing message with identifier
		currentID := id
		id++
		c.recordPending(currentID, &req)

		if err := messageWriter(conn, req.msg); err != nil {
			c.finishPending(currentID, nil, err)
			return err
		}
	}
}

func (c *Client) recordPending(id uint, req *call) {
	c.pendingMutex.Lock()
	defer c.pendingMutex.Unlock()

	req.msg.ID = id
	c.pending[id] = req
}

func (c *Client) abortPending() {
	c.pendingMutex.Lock()
	defer c.pendingMutex.Unlock()

	resp := Result{c, nil, fmt.Errorf("disconnected from agent")}
	for _, req := range c.pending {
		req.finish(resp)
	}
	c.pending = make(map[uint]*call)
}

func (c *Client) finishPending(id uint, msg *Message, err error) {
	c.pendingMutex.Lock()
	defer c.pendingMutex.Unlock()

	req := c.pending[id]
	if req == nil {
		log.Warn("Ignoring unmatched response id %d", id)
		return
	}
	delete(c.pending, id)

	resp := Result{c, msg, err}
	if err == nil && msg.Type == "error" {
		resp.Error = fmt.Errorf("operation %s failed: %s", req.msg.Type, msg.Message)
	}
	req.finish(resp)
}

func (c *Client) newClientReader(conn net.Conn) comm.MessageReader {
	reader := newMessageReader(conn)
	return func(conn net.Conn) (interface{}, error) {
		msg, err := reader(conn)
		if err != nil {
			return nil, err
		}
		if msg.Type == JobChanged || msg.Type == JobRemoved {
			if log.IsDebugEnabled() {
				log.Debug("Received job change notification")
			}
			if c.JobUpdateHandler != nil {
				c.JobUpdateHandler(msg)
			}
		} else {
			// record handlers on initial response
			if msg.ID == 1 {
				c.setHandlers(msg.Handlers)
			}
			// correlate response with pending requests
			c.finishPending(msg.ID, msg, nil)
		}
		return nil, nil
	}
}
