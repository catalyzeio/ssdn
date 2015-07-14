package overlay

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"path"
	"path/filepath"
	"strings"

	"github.com/catalyzeio/go-core/comm"
)

type Client struct {
	base   string
	client *http.Client
}

func NewClient(tenant, runDir string) (*Client, error) {
	dsPath, err := filepath.Abs(path.Join(runDir, tenant, "ssdn.sock"))
	if err != nil {
		return nil, err
	}
	urlString := fmt.Sprintf("unix://%s", dsPath)
	client, base, err := comm.HTTPClientFromURL(urlString)
	if err != nil {
		return nil, err
	}
	return &Client{base, client}, nil
}

func (c *Client) Status() (*Status, error) {
	target := fmt.Sprintf("%s/status", c.base)
	r, err := c.client.Get(target)
	if err != nil {
		return nil, err
	}
	defer r.Body.Close()

	if err := verifyResponse(r); err != nil {
		return nil, err
	}

	status := &Status{}
	err = json.NewDecoder(r.Body).Decode(&status)
	return status, err
}

func (c *Client) Attach(req *AttachRequest) error {
	data, err := json.Marshal(req)
	if err != nil {
		return err
	}

	target := fmt.Sprintf("%s/connections/attach", c.base)
	r, err := c.client.Post(target, "application/json", bytes.NewReader(data))
	if err != nil {
		return err
	}
	defer r.Body.Close()

	return verifyResponse(r)
}

func (c *Client) Detach(container string) error {
	data := []byte(container)

	target := fmt.Sprintf("%s/connections/detach", c.base)
	r, err := c.client.Post(target, "application/json", bytes.NewReader(data))
	if err != nil {
		return err
	}
	defer r.Body.Close()

	return verifyResponse(r)
}

func (c *Client) ListConnections() (map[string]*ConnectionDetails, error) {
	target := fmt.Sprintf("%s/connections", c.base)
	r, err := c.client.Get(target)
	if err != nil {
		return nil, err
	}
	defer r.Body.Close()

	if err := verifyResponse(r); err != nil {
		return nil, err
	}

	var connections map[string]*ConnectionDetails
	err = json.NewDecoder(r.Body).Decode(&connections)
	return connections, err
}

func (c *Client) AddPeer(peer string) error {
	data := []byte(peer)

	target := fmt.Sprintf("%s/peers/add", c.base)
	r, err := c.client.Post(target, "application/json", bytes.NewReader(data))
	if err != nil {
		return err
	}
	defer r.Body.Close()

	return verifyResponse(r)
}

func (c *Client) DeletePeer(peer string) error {
	data := []byte(peer)

	target := fmt.Sprintf("%s/peers/delete", c.base)
	r, err := c.client.Post(target, "application/json", bytes.NewReader(data))
	if err != nil {
		return err
	}
	defer r.Body.Close()

	return verifyResponse(r)
}

func (c *Client) ListPeers() (map[string]*PeerDetails, error) {
	target := fmt.Sprintf("%s/peers", c.base)
	r, err := c.client.Get(target)
	if err != nil {
		return nil, err
	}
	defer r.Body.Close()

	if err := verifyResponse(r); err != nil {
		return nil, err
	}

	var peers map[string]*PeerDetails
	err = json.NewDecoder(r.Body).Decode(&peers)
	return peers, err
}

func (c *Client) ListRoutes() ([]string, error) {
	target := fmt.Sprintf("%s/routes", c.base)
	r, err := c.client.Get(target)
	if err != nil {
		return nil, err
	}
	defer r.Body.Close()

	if err := verifyResponse(r); err != nil {
		return nil, err
	}

	var routes []string
	err = json.NewDecoder(r.Body).Decode(&routes)
	return routes, err
}

func (c *Client) ARPTable() (map[string]string, error) {
	target := fmt.Sprintf("%s/arp", c.base)
	r, err := c.client.Get(target)
	if err != nil {
		return nil, err
	}
	defer r.Body.Close()

	if err := verifyResponse(r); err != nil {
		return nil, err
	}

	var table map[string]string
	err = json.NewDecoder(r.Body).Decode(&table)
	return table, err
}

func (c *Client) Resolve(ip string) (string, error) {
	target := fmt.Sprintf("%s/arp/%s", c.base, url.QueryEscape(ip))
	r, err := c.client.Get(target)
	if err != nil {
		return "", err
	}
	defer r.Body.Close()

	if err := verifyResponse(r); err != nil {
		return "", err
	}

	data, err := ioutil.ReadAll(r.Body)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func verifyResponse(response *http.Response) error {
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		messageBytes, err := ioutil.ReadAll(response.Body)
		if err != nil {
			return err
		}
		details := strings.TrimSpace(string(messageBytes))
		return fmt.Errorf("request %s failed: %s (%d)", response.Request.URL, details, response.StatusCode)
	}
	return nil
}
