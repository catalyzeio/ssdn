package scheduler

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"time"

	"github.com/catalyzeio/go-core/comm"
	"github.com/catalyzeio/paas-orchestration/agent"
)

type Client struct {
	*http.Client
	address *comm.Address
}

func NewClient(address *comm.Address, config *tls.Config) (*Client, error) {
	c := http.Client{
		Timeout: time.Second * 10,
	}
	if address.TLS() {
		if config != nil {
			c.Transport = &http.Transport{TLSClientConfig: config}
		} else {
			return nil, fmt.Errorf("address provided (%s) uses tls, but no tls config was provided", address.Host())
		}
	}
	return &Client{
		&c,
		address,
	}, nil
}

func (c *Client) base() string {
	proto := "http"
	if c.address.TLS() {
		proto = "https"
	}
	return fmt.Sprintf("%s://%s:%d", proto, c.address.Host(), c.address.Port())
}

func (c *Client) request(method, route string, body, v interface{}, noBody, noResponse bool) error {
	buf := bytes.NewBuffer([]byte{})
	if !noBody {
		if err := json.NewEncoder(buf).Encode(body); err != nil {
			return err
		}
	}
	req, err := http.NewRequest(method, fmt.Sprintf("%s%s", c.base(), route), buf)
	if err != nil {
		return err
	}
	req.Header.Add("Content-Type", "application/json")
	resp, err := c.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		st := ""
		bs, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			st = string(bs)
		}
		return fmt.Errorf("status code error %d: %s", resp.StatusCode, st)
	}
	if !noResponse {
		if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
			return err
		}
	}
	return nil
}

func (c *Client) Status() (*Status, error) {
	v := &Status{}
	err := c.request("GET", "/status", nil, v, true, false)
	if err != nil {
		return nil, err
	}
	return v, nil
}

func (c *Client) GetJobs() (map[string]*JobDetails, error) {
	v := make(map[string]*JobDetails)
	err := c.request("GET", "/jobs", nil, &v, true, false)
	return v, err
}

func (c *Client) AddJob(jr *agent.JobRequest) (*JobDetails, error) {
	v := &JobDetails{}
	err := c.request("POST", "/jobs", jr, v, false, false)
	if err != nil {
		return nil, err
	}
	return v, nil
}

func (c *Client) AddQueueJob(jr *agent.JobRequest) (*JobDetails, error) {
	v := &JobDetails{}
	err := c.request("POST", "/queue", jr, v, false, false)
	if err != nil {
		return nil, err
	}
	return v, nil
}

func (c *Client) AddCompanionJob(jobID string, jr *agent.JobRequest) (*JobDetails, error) {
	v := &JobDetails{}
	err := c.request("POST", fmt.Sprintf("/jobs/%s/companion", jobID), jr, v, false, false)
	if err != nil {
		return nil, err
	}
	return v, nil
}

func (c *Client) GetJob(jobID string) (*JobDetails, error) {
	v := &JobDetails{}
	err := c.request("GET", fmt.Sprintf("/jobs/%s", jobID), nil, v, true, false)
	if err != nil {
		return nil, err
	}
	return v, nil
}

func (c *Client) DeleteJob(jobID string) error {
	return c.request("DELETE", fmt.Sprintf("/jobs/%s", jobID), nil, nil, true, true)
}

func (c *Client) PatchJob(jobID string, jp *agent.JobPayload) error {
	return c.request("PATCH", fmt.Sprintf("/jobs/%s", jobID), jp, nil, false, true)
}

func (c *Client) ReplaceJob(jobID string, jr *agent.JobRequest) (*JobDetails, error) {
	v := &JobDetails{}
	err := c.request("PATCH", fmt.Sprintf("/jobs/%s", jobID), jr, v, false, false)
	if err != nil {
		return nil, err
	}
	return v, nil
}

func (c *Client) StopJob(jobID string) error {
	return c.request("POST", fmt.Sprintf("/jobs/%s/stop", jobID), nil, nil, true, true)
}

func (c *Client) StartJob(jobID string) error {
	return c.request("POST", fmt.Sprintf("/jobs/%s/start", jobID), nil, nil, true, true)
}

func (c *Client) GetAgents() (map[string][]string, error) {
	v := make(map[string][]string)
	err := c.request("GET", "/agents", nil, &v, true, false)
	return v, err
}

func (c *Client) GetUsage() (map[string]*agent.PolicyUsage, error) {
	v := make(map[string]*agent.PolicyUsage)
	err := c.request("GET", "/usage", nil, &v, true, false)
	return v, err
}

func (c *Client) GetMode() (map[string]string, error) {
	v := make(map[string]string)
	err := c.request("GET", "/mode", nil, &v, true, false)
	return v, err
}

func (c *Client) SetMode(mode string) (map[string]string, error) {
	v := make(map[string]string)
	err := c.request("POST", "/mode", &mode, &v, false, false)
	return v, err
}

func (c *Client) GetAgentsState() (map[string]*agent.State, error) {
	v := make(map[string]*agent.State)
	err := c.request("GET", "/agents-state", nil, &v, true, false)
	return v, err
}
