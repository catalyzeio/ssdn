package udocker

import (
	"bytes"
	"fmt"
	"net/url"
	"time"
)

type ClientError struct {
	URL        *url.URL
	StatusCode int
	Message    string
}

func (c *ClientError) Error() string {
	return fmt.Sprintf("request %s failed: %s (%d)", c.URL, c.Message, c.StatusCode)
}

func IsNotFound(err error) bool {
	if val, ok := err.(*ClientError); ok {
		return val.StatusCode == 404
	}
	return false
}

// XXX These types are intentionally bare compared with the full set of fields
// supported by the Docker API. Only the fields necessary for our (basic) uses
// have been added; the rest are omitted.

type ContainerSummary struct {
	Id      string
	Names   []string
	Image   string
	Command string
	Created int64
	Ports   []struct {
		PrivatePort int
		PublicPort  int
		Type        string
	}
	Labels map[string]string
	Status string
}

type ContainerDefinition struct {
	Hostname     string
	ExposedPorts map[string]struct{}

	Tty       bool
	OpenStdin bool

	Env []string
	Cmd []string

	Image string

	NetworkDisabled bool

	Labels map[string]string

	HostConfig HostConfig

	// These fields have been deprecated in newer versions of Docker.
	// These should be ignored by callers; they will be filled in
	// automatically as necessary.

	// moved in API v1.18
	LegacyMemory     int64 `json:"Memory"`
	LegacyMemorySwap int64 `json:"MemorySwap"`
	LegacyCpuShares  int64 `json:"CpuShares"`
}

func (c *ContainerDefinition) migrateLegacyFields() {
	hc := c.HostConfig
	c.LegacyMemory = hc.Memory
	c.LegacyMemorySwap = hc.MemorySwap
	c.LegacyCpuShares = hc.CpuShares
}

type HostConfig struct {
	Binds []string

	Memory     int64
	MemorySwap int64
	CpuShares  int64

	PortBindings map[string][]PortBinding

	Dns         []string
	DnsSearch   []string
	ExtraHosts  []string
	SecurityOpt []string

	RestartPolicy struct {
		Name              string
		MaximumRetryCount int
	}
}

type PortBinding struct {
	HostIp   string `json:"HostIp,omitempty"`
	HostPort string `json:"HostPort,omitempty"`
}

type NewContainerResponse struct {
	Id string
}

type ContainerDetails struct {
	Id string

	State struct {
		Running    bool
		Paused     bool
		Restarting bool
		Dead       bool
		ExitCode   int
		StartedAt  time.Time
		FinishedAt time.Time
	}

	Image string

	Name string

	RestartCount int

	Config struct {
		Tty bool

		Image string

		Labels map[string]string
	}
}

type CommitResult struct {
	Id string
}

type WaitResult struct {
	StatusCode int
}

type ImageDetails struct {
	Id string

	Config struct {
		Tty       bool
		OpenStdin bool

		Env []string
		Cmd []string

		Image string

		Labels map[string]string
	}
}

type StreamMessage struct {
	Stream string `json:"stream"`

	Status string `json:"status"`

	Progress       string `json:"progress"`
	ProgressDetail struct {
		Current int64 `json:"current"`
		Total   int64 `json:"total"`
		Start   int64 `json:"start"`
	} `json:"progressDetail"`

	ID string `json:"id"`

	Error       string `json:"error"`
	ErrorDetail struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"errorDetail"`
}

func (s *StreamMessage) InterimProgress() bool {
	if len(s.Error) > 0 {
		return false
	}
	if len(s.Stream) > 0 {
		return false
	}
	return s.ProgressDetail.Current != s.ProgressDetail.Total
}

func (s *StreamMessage) String() string {
	if len(s.Error) > 0 {
		return s.Error
	}
	if len(s.Stream) > 0 {
		return s.Stream
	}

	var buf bytes.Buffer
	if len(s.Status) > 0 {
		buf.WriteString(s.Status)
	}
	if len(s.Progress) > 0 {
		buf.WriteRune(' ')
		buf.WriteString(s.Progress)
	}
	if len(s.ID) > 0 {
		buf.WriteString(" (")
		buf.WriteString(s.ID)
		buf.WriteRune(')')
	}
	return buf.String()
}

type EventMessage struct {
	Status string `json:"status"`

	ID   string `json:"id"`
	From string `json:"from"`
	Time int64  `json:"time"`
}

type OutputReader interface {
	Read() (*OutputLine, error)
	Close() error
}

type OutputLine struct {
	Line   string
	Stderr bool
}

type ExecOptions struct {
	AttachStderr bool
	AttachStdin  bool
	AttachStdout bool
	Cmd          []string
	Privileged   bool
	Tty          bool
}

type ExecCreateResponse struct {
	Id       string
	Warnings []string
}

type ExecStartOptions struct {
	Detach bool
	Tty    bool
}

type ExecInspectResponse struct {
	ExitCode int
	Running  bool
}

type FileResponse struct {
	Name       string `json:"name"`
	Size       int    `json:"size"`
	Mode       int    `json:"mode"`
	MakeTime   string `json:"mktime"`
	LinkTarget string `json:"linkTarget"`
}
