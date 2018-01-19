package udocker

import (
	"archive/tar"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

type Client struct {
	base   string
	client *http.Client
}

func NewClient(base string, client *http.Client) *Client {
	return &Client{base, client}
}

func (c *Client) Ping() error {
	target := fmt.Sprintf("%s/_ping", c.base)
	r, err := c.client.Get(target)
	if err != nil {
		return err
	}
	defer r.Body.Close()

	return verifyResponse(r)
}

func (c *Client) ListContainers(all bool) ([]ContainerSummary, error) {
	target := fmt.Sprintf("%s/containers/json", c.base)
	if all {
		target += "?all=1"
	}
	r, err := c.client.Get(target)
	if err != nil {
		return nil, err
	}
	defer r.Body.Close()

	if err := verifyResponse(r); err != nil {
		return nil, err
	}

	var containers []ContainerSummary
	err = json.NewDecoder(r.Body).Decode(&containers)
	return containers, err
}

func (c *Client) InspectContainer(id string) (*ContainerDetails, error) {
	target := fmt.Sprintf("%s/containers/%s/json", c.base, url.QueryEscape(id))
	r, err := c.client.Get(target)
	if err != nil {
		return nil, err
	}
	defer r.Body.Close()

	if err := verifyResponse(r); err != nil {
		return nil, err
	}

	details := ContainerDetails{}
	err = json.NewDecoder(r.Body).Decode(&details)
	return &details, err
}

func (c *Client) CreateContainer(name string, def *ContainerDefinition) (*NewContainerResponse, error) {
	log.Info("Creating container %s", name)

	if def != nil {
		def.migrateLegacyFields()
	}

	data, err := json.Marshal(def)
	if err != nil {
		return nil, err
	}

	target := fmt.Sprintf("%s/containers/create", c.base)
	if len(name) > 0 {
		target += fmt.Sprintf("?name=%s", url.QueryEscape(name))
	}
	r, err := c.client.Post(target, "application/json", bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer r.Body.Close()

	if err := verifyResponse(r); err != nil {
		return nil, err
	}

	info := NewContainerResponse{}
	err = json.NewDecoder(r.Body).Decode(&info)
	return &info, err
}

func (c *Client) StartContainer(id string, config *HostConfig) error {
	log.Info("Starting container %s", id)

	var body io.Reader
	if config != nil {
		data, err := json.Marshal(config)
		if err != nil {
			return err
		}
		body = bytes.NewReader(data)
	}
	return c.postContainer(id, "start", body)
}

func (c *Client) StopContainer(id string, timeout int) error {
	log.Info("Stopping container %s with a %ds timeout", id, timeout)

	verb := "stop"
	if timeout > 0 {
		verb = fmt.Sprintf("stop?t=%d", timeout)
	}
	return c.postContainer(id, verb, nil)
}

func (c *Client) RestartContainer(id string) error {
	log.Info("Restarting container %s", id)

	return c.postContainer(id, "restart", nil)
}

func (c *Client) PauseContainer(id string) error {
	log.Info("Pausing container %s", id)

	return c.postContainer(id, "pause", nil)
}

func (c *Client) UnpauseContainer(id string) error {
	log.Info("Unpausing container %s", id)

	return c.postContainer(id, "unpause", nil)
}

func (c *Client) KillContainer(id string) error {
	log.Info("Killing container %s", id)

	return c.postContainer(id, "kill", nil)
}

func (c *Client) postContainer(id string, verb string, body io.Reader) error {
	target := fmt.Sprintf("%s/containers/%s/%s", c.base, url.QueryEscape(id), verb)
	r, err := c.client.Post(target, "application/json", body)
	if err != nil {
		return err
	}
	defer r.Body.Close()

	return verifyResponse(r)
}

func (c *Client) AttachContainer(id string) error {
	res, err := c.InspectContainer(id)
	if err != nil {
		return err
	}

	r, err := c.AttachContainerReader(id, res.Config.Tty)
	if err != nil {
		return err
	}
	defer r.Close()

	for {
		o, err := r.Read()
		if o == nil {
			return nil
		}
		if err != nil {
			return err
		}
		if o.Stderr {
			log.Errorf("Container %s: %s", id, o.Line)
		} else {
			log.Info("Container %s: %s", id, o.Line)
		}
	}
}

func (c *Client) AttachContainerReader(id string, tty bool) (OutputReader, error) {
	r, err := c.AttachContainerRaw(id)
	if err != nil {
		return nil, err
	}

	if tty {
		return newRawReader(r), nil
	}
	return newTaggedReader(r), nil
}

func (c *Client) AttachContainerRaw(id string) (io.ReadCloser, error) {
	log.Info("Attaching to container %s", id)

	target := fmt.Sprintf("%s/containers/%s/attach?logs=1&stream=1&stdin=0&stdout=1&stderr=1",
		c.base, url.QueryEscape(id))
	req, err := http.NewRequest("POST", target, nil)
	if err != nil {
		return nil, err
	}
	req.Close = true
	req.Header.Set("Content-Type", "application/json")
	r, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}

	if err := verifyResponse(r); err != nil {
		defer r.Body.Close()
		return nil, err
	}

	return r.Body, nil
}

func (c *Client) WaitContainer(id string) (*WaitResult, error) {
	log.Info("Waiting for container %s", id)

	target := fmt.Sprintf("%s/containers/%s/wait", c.base, url.QueryEscape(id))
	r, err := c.client.Post(target, "application/json", nil)
	if err != nil {
		return nil, err
	}
	defer r.Body.Close()

	if err := verifyResponse(r); err != nil {
		return nil, err
	}

	result := WaitResult{}
	err = json.NewDecoder(r.Body).Decode(&result)
	return &result, err
}

func (c *Client) CommitContainer(id, tag, repo string, def *ContainerDefinition) (*CommitResult, error) {
	log.Info("Committing container %s as tag=%s repo=%s", id, tag, repo)

	data, err := json.Marshal(def)
	if err != nil {
		return nil, err
	}

	target := fmt.Sprintf("%s/commit?container=%s&tag=%s&repo=%s",
		c.base, url.QueryEscape(id), url.QueryEscape(tag), url.QueryEscape(repo))
	r, err := c.client.Post(target, "application/json", bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer r.Body.Close()

	if err := verifyResponse(r); err != nil {
		return nil, err
	}

	result := CommitResult{}
	err = json.NewDecoder(r.Body).Decode(&result)
	return &result, err
}

func (c *Client) DeleteContainer(id string) error {
	log.Info("Deleting container %s", id)

	target := fmt.Sprintf("%s/containers/%s", c.base, url.QueryEscape(id))
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

func (c *Client) InspectImage(id string) (*ImageDetails, error) {
	target := fmt.Sprintf("%s/images/%s/json", c.base, url.QueryEscape(id))
	r, err := c.client.Get(target)
	if err != nil {
		return nil, err
	}
	defer r.Body.Close()

	if err := verifyResponse(r); err != nil {
		return nil, err
	}

	details := ImageDetails{}
	err = json.NewDecoder(r.Body).Decode(&details)
	return &details, err
}

func (c *Client) BuildImage(tarReader io.Reader, repoWithTag string, noCache bool) error {
	s, err := c.StreamBuildImage(tarReader, repoWithTag, noCache)
	if err != nil {
		return err
	}
	defer s.Close()

	for {
		msg, err := s.NextStreamMessage("build")
		if err != nil {
			return err
		}
		if msg == nil {
			return nil
		}
		log.Info("Build %s: %s", repoWithTag, msg)
	}
}

func (c *Client) StreamBuildImage(tarReader io.Reader, repoWithTag string, noCache bool) (*Stream, error) {
	log.Info("Building image %s", repoWithTag)

	target := fmt.Sprintf("%s/build?t=%s", c.base, url.QueryEscape(repoWithTag))
	if noCache {
		target += "&nocache=1"
	}
	req, err := http.NewRequest("POST", target, tarReader)
	if err != nil {
		return nil, err
	}
	req.Close = true
	req.Header.Set("Content-Type", "application/tar")
	r, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}

	if err := verifyResponse(r); err != nil {
		r.Body.Close()
		return nil, err
	}

	return newStream(r.Body), nil
}

func (c *Client) PullImage(image string) error {
	image, tag := c.ParseImage(image)
	return c.PullImageTag(image, tag)
}

func (c *Client) ParseImage(image string) (string, string) {
	index := strings.LastIndex(image, ":")
	if index >= 0 {
		tag := image[index+1:]
		if !strings.Contains(tag, "/") {
			return image[:index], tag
		}
	}
	return image, ""
}

func (c *Client) PullImageTag(name, tag string) error {
	s, err := c.StreamPullImageTag(name, tag)
	if err != nil {
		return err
	}
	defer s.Close()

	for {
		msg, err := s.NextStreamMessage("pull")
		if err != nil {
			return err
		}
		if msg == nil {
			return nil
		}
		if !msg.InterimProgress() {
			log.Info("Pull %s:%s: %+v", name, tag, msg)
		} else if log.IsTraceEnabled() {
			log.Trace("Pull %s:%s: %+v", name, tag, msg)
		}
	}
}

func (c *Client) StreamPullImageTag(name, tag string) (*Stream, error) {
	log.Info("Pulling image %s:%s", name, tag)

	target := fmt.Sprintf("%s/images/create?fromImage=%s&tag=%s", c.base, url.QueryEscape(name), url.QueryEscape(tag))
	req, err := http.NewRequest("POST", target, nil)
	if err != nil {
		return nil, err
	}
	req.Close = true
	req.Header.Set("Content-Type", "application/json")
	r, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}

	if err := verifyResponse(r); err != nil {
		r.Body.Close()
		return nil, err
	}

	return newStream(r.Body), nil
}

func (c *Client) TagImage(image, tag, repo, repoTag string, force bool) error {
	log.Info("Tagging image '%s:%s' in repo '%s:%s', force=%t", image, tag, repo, repoTag, force)

	localImage := url.QueryEscape(image)
	if len(tag) > 0 {
		localImage = fmt.Sprintf("%s:%s", localImage, url.QueryEscape(tag))
	}
	vals := url.Values{}
	if len(repo) > 0 {
		vals.Add("repo", repo)
	}
	if len(repoTag) > 0 {
		vals.Add("tag", repoTag)
	}
	if force {
		vals.Add("force", "1")
	}
	target := fmt.Sprintf("%s/images/%s/tag?%s",
		c.base, localImage, vals.Encode())

	r, err := c.client.Post(target, "application/json", nil)
	if err != nil {
		return err
	}
	defer r.Body.Close()

	return verifyResponse(r)
}

func (c *Client) PushImage(id, tag string) error {
	s, err := c.StreamPushImage(id, tag)
	if err != nil {
		return err
	}
	defer s.Close()

	for {
		msg, err := s.NextStreamMessage("push")
		if err != nil {
			return err
		}
		if msg == nil {
			return nil
		}
		if !msg.InterimProgress() {
			log.Info("Push %s:%s: %s", id, tag, msg)
		} else if log.IsTraceEnabled() {
			log.Trace("Push %s:%s: %s", id, tag, msg)
		}
	}
}

func (c *Client) StreamPushImage(id, tag string) (*Stream, error) {
	log.Info("Pushing image %s:%s", id, tag)

	target := fmt.Sprintf("%s/images/%s/push?tag=%s", c.base, url.QueryEscape(id), url.QueryEscape(tag))
	req, err := http.NewRequest("POST", target, nil)
	if err != nil {
		return nil, err
	}
	req.Close = true
	req.Header.Set("Content-Type", "application/json")
	// use the fixed value {"auth":"","email":""} for now
	req.Header.Set("X-Registry-Auth", "eyJhdXRoIjoiIiwiZW1haWwiOiIifQ==")

	r, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}

	if err := verifyResponse(r); err != nil {
		r.Body.Close()
		return nil, err
	}

	return newStream(r.Body), nil
}

func (c *Client) DeleteImage(id string) ([]map[string]string, error) {
	log.Info("Deleting image %s", id)

	target := fmt.Sprintf("%s/images/%s", c.base, url.QueryEscape(id))
	req, err := http.NewRequest("DELETE", target, nil)
	if err != nil {
		return nil, err
	}

	r, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer r.Body.Close()

	if err := verifyResponse(r); err != nil {
		return nil, err
	}

	var details []map[string]string
	err = json.NewDecoder(r.Body).Decode(&details)
	return details, err
}

func (c *Client) Events(handler func(*EventMessage)) error {
	s, err := c.StreamEvents()
	if err != nil {
		return err
	}
	defer s.Close()

	for {
		msg, err := s.NextEventMessage()
		if err != nil {
			return err
		}
		if msg == nil {
			return nil
		}
		if handler != nil {
			handler(msg)
		}
	}
}

func (c *Client) StreamEvents() (*Stream, error) {
	log.Info("Subscribing to events at %s", c.base)

	target := fmt.Sprintf("%s/events", c.base)
	req, err := http.NewRequest("GET", target, nil)
	if err != nil {
		return nil, err
	}
	req.Close = true
	r, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}

	if err := verifyResponse(r); err != nil {
		return nil, err
	}

	return newStream(r.Body), nil
}

func (c *Client) ExecContainer(cmd []string, id string) (string, string, int, error) {
	b, err := json.Marshal(&ExecOptions{
		AttachStdout: true,
		AttachStderr: true,
		AttachStdin:  false,
		Cmd:          cmd,
		Privileged:   false,
		Tty:          false,
	})
	if err != nil {
		return "", "", -1, err
	}

	target := fmt.Sprintf("%s/containers/%s/exec", c.base, url.QueryEscape(id))
	r, err := c.client.Post(target, "application/json", bytes.NewBuffer(b))
	if err != nil {
		return "", "", -1, err
	}
	defer r.Body.Close()

	if err := verifyResponse(r); err != nil {
		return "", "", -1, err
	}

	execResp := ExecCreateResponse{}
	err = json.NewDecoder(r.Body).Decode(&execResp)
	if err != nil {
		return "", "", -1, err
	}
	if len(execResp.Warnings) > 0 {
		log.Info("Exec creation warnings for %s: %s", id, strings.Join(execResp.Warnings, "\n"))
	}

	reader, err := c.ExecContainerReader(execResp.Id, false)
	if err != nil {
		return "", "", -1, err
	}
	defer reader.Close()

	var stdout, stderr []string
	for {
		o, err := reader.Read()
		if o == nil {
			break
		}
		if err != nil {
			return strings.Join(stdout, "\n"), strings.Join(stderr, "\n"), -1, err
		}
		if len(o.Line) > 0 {
			if o.Stderr {
				stderr = append(stderr, o.Line)
			} else {
				stdout = append(stdout, o.Line)
			}
		}
	}

	ei, err := c.ExecInspect(execResp.Id)
	if err != nil {
		return strings.Join(stdout, "\n"), strings.Join(stderr, "\n"), -1, err
	}
	return strings.Join(stdout, "\n"), strings.Join(stderr, "\n"), ei.ExitCode, nil

}

func (c *Client) ExecContainerReader(id string, tty bool) (OutputReader, error) {
	r, err := c.ExecContainerRaw(id, tty)
	if err != nil {
		return nil, err
	}
	if tty {
		return newRawReader(r), nil
	}
	return newTaggedReader(r), nil
}

func (c *Client) ExecContainerRaw(id string, tty bool) (io.ReadCloser, error) {
	b, err := json.Marshal(&ExecStartOptions{
		Detach: false,
		Tty:    tty,
	})
	if err != nil {
		return nil, err
	}
	target := fmt.Sprintf("%s/exec/%s/start",
		c.base, url.QueryEscape(id))
	req, err := http.NewRequest("POST", target, bytes.NewBuffer(b))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Close = true
	r, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}

	if err := verifyResponse(r); err != nil {
		defer r.Body.Close()
		return nil, err
	}

	return r.Body, nil
}

func (c *Client) ExecInspect(id string) (*ExecInspectResponse, error) {
	target := fmt.Sprintf("%s/exec/%s/json",
		c.base, url.QueryEscape(id))
	r, err := c.client.Get(target)
	if err != nil {
		return nil, err
	}

	if err := verifyResponse(r); err != nil {
		defer r.Body.Close()
		return nil, err
	}
	resp := &ExecInspectResponse{}
	err = json.NewDecoder(r.Body).Decode(resp)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func (c *Client) ContainerLogs(id string, since *time.Time, follow, tail bool) (OutputReader, error) {
	rd, err := c.ContainerLogsRaw(id, since, follow, tail)
	if err != nil {
		return nil, err
	}
	return newTaggedReader(rd), nil
}

func (c *Client) ContainerLogsRaw(id string, since *time.Time, follow, tail bool) (io.ReadCloser, error) {
	var ts int64
	if since != nil {
		ts = since.Unix()
	}
	f := 0
	t := 0
	if follow {
		f = 1
	}
	if tail {
		t = 1
	}
	target := fmt.Sprintf("%s/containers/%s/logs?stderr=1&stdout=1&timestamps=1&follow=%d&tail=%d&since=%d", c.base, url.QueryEscape(id), f, t, ts)
	r, err := c.client.Get(target)
	if err != nil {
		return nil, err
	}
	if err := verifyResponse(r); err != nil {
		defer r.Body.Close()
		return nil, err
	}
	return r.Body, nil
}

func (c *Client) CopyFileTo(id, dir, name string, mode, size int64, uid, gid int, contents io.Reader) error {
	tarHeader := &tar.Header{
		Name:       name,
		Mode:       mode,
		Uid:        uid,
		Gid:        gid,
		Size:       size,
		ModTime:    time.Now(),
		Typeflag:   tar.TypeReg,
		AccessTime: time.Now(),
	}
	buf := bytes.NewBuffer([]byte{})
	tarball := tar.NewWriter(buf)
	defer tarball.Close()
	if err := tarball.WriteHeader(tarHeader); err != nil {
		return err
	}
	io.Copy(tarball, contents)
	target := fmt.Sprintf("%s/containers/%s/archive?path=%s", c.base, url.QueryEscape(id), url.QueryEscape(dir))
	req, err := http.NewRequest("PUT", target, buf)
	if err != nil {
		return err
	}
	req.Close = true
	req.Header.Set("Content-Type", "application/x-tar")
	res, err := c.client.Do(req)
	if err != nil {
		return err
	}
	if err := verifyResponse(res); err != nil {
		return err
	}
	return nil
}

func (c *Client) CopyFileFrom(id, containerPath string) (io.Reader, os.FileMode, error) {
	target := fmt.Sprintf("%s/containers/%s/archive?path=%s", c.base, url.QueryEscape(id), url.QueryEscape(containerPath))
	req, err := http.NewRequest("GET", target, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Close = true
	r, err := c.client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	if err := verifyResponse(r); err != nil {
		defer r.Body.Close()
		return nil, 0, err
	}
	defer r.Body.Close()
	bs, err := base64.StdEncoding.DecodeString(r.Header.Get("X-Docker-Container-Path-Stat"))
	if err != nil {
		return nil, 0, err
	}
	var fr FileResponse
	err = json.Unmarshal(bs, &fr)
	if err != nil {
		return nil, 0, err
	}
	tarReader := tar.NewReader(r.Body)
	_, err = tarReader.Next()
	if err != nil {
		return nil, 0, err
	}
	buf := bytes.NewBuffer([]byte{})
	io.Copy(buf, tarReader)
	return buf, os.FileMode(fr.Mode), nil
}

func verifyResponse(response *http.Response) error {
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		messageBytes, err := ioutil.ReadAll(response.Body)
		if err != nil {
			return err
		}
		details := strings.TrimSpace(string(messageBytes))
		return &ClientError{response.Request.URL, response.StatusCode, details}
	}
	return nil

}
