package overlay

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"strings"

	"github.com/catalyzeio/go-core/comm"
)

type Client struct {
	base   string
	client *http.Client
}

func NewClient(tenant, runDir string) (*Client, error) {
	urlString := fmt.Sprintf("unix://%s/%s/ssdn.sock", runDir, tenant)
	client, base, err := comm.HTTPClientFromURL(urlString)
	if err != nil {
		return nil, err
	}
	return &Client{base, client}, nil
}

func (c *Client) Status() (map[string]string, error) {
	target := fmt.Sprintf("%s/status", c.base)
	r, err := c.client.Get(target)
	if err != nil {
		return nil, err
	}
	defer r.Body.Close()

	if err := verifyResponse(r); err != nil {
		return nil, err
	}

	var status map[string]string
	err = json.NewDecoder(r.Body).Decode(&status)
	return status, err
}

func (c *Client) Attach(req *AttachRequest) error {
	data, err := json.Marshal(req)
	if err != nil {
		return err
	}

	target := fmt.Sprintf("%s/connections", c.base)
	r, err := c.client.Post(target, "application/json", bytes.NewReader(data))
	if err != nil {
		return err
	}
	defer r.Body.Close()

	return verifyResponse(r)
}

func (c *Client) Detach(container string) error {
	target := fmt.Sprintf("%s/connections/%s", c.base, url.QueryEscape(container))
	req, err := http.NewRequest("DELETE", target, nil)
	if err != nil {
		return err
	}

	r, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer r.Body.Close()

	return verifyResponse(r)
}

func verifyResponse(response *http.Response) error {
	if response.StatusCode < 200 || response.StatusCode >= 400 {
		messageBytes, err := ioutil.ReadAll(response.Body)
		if err != nil {
			return err
		}
		details := strings.TrimSpace(string(messageBytes))
		return fmt.Errorf("request %s failed: %s (%d)", response.Request.URL, details, response.StatusCode)
	}
	return nil
}
