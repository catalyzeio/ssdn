package agent

import (
	"crypto/tls"
	"fmt"
	"time"

	"github.com/catalyzeio/go-core/comm"
	"github.com/catalyzeio/paas-orchestration/registry"
)

// The current state of a job.
type Status int

const (
	Scheduled Status = 0
	Queued    Status = 1

	Waiting   Status = 10
	Started   Status = 11
	Unhealthy Status = 12
	Running   Status = 13
	Stopped   Status = 14

	Finished    Status = 20
	Killed      Status = 21
	Failed      Status = 22
	Disappeared Status = 23
)

func (s Status) Active() bool {
	return s >= Waiting && s < Finished
}

func (s Status) Terminal() bool {
	return s >= Finished
}

func (s Status) String() string {
	switch s {
	case Scheduled:
		return "scheduled"
	case Queued:
		return "queued"
	case Waiting:
		return "waiting"
	case Started:
		return "started"
	case Unhealthy:
		return "unhealthy"
	case Running:
		return "running"
	case Stopped:
		return "stopped"
	case Finished:
		return "finished"
	case Killed:
		return "killed"
	case Failed:
		return "failed"
	case Disappeared:
		return "disappeared"
	default:
		return "unknown"
	}
}

type ServerConfig struct {
	CAFile          string
	DockerHost      string
	Services        string
	OverlayNetwork  string
	OverlaySubnet   string
	TemplatesDir    string
	StateDir        string
	TrustDir        string
	ServiceDir      string
	Requires        string
	Provides        string
	Conflicts       string
	Prefers         string
	Despises        string
	Handlers        string
	AMQPURL         string
	Policy          string
	Capacity        int
	Memory          int64
	MinJobMemory    int64
	MaxJobMemory    int64
	MemoryLimit     float64
	Pack            bool
	CleanFailedJobs bool
	ListenAddress   *comm.Address
	TLSConfig       *tls.Config
	RegistryClient  *registry.Client
	NotaryServer    string
	AppAuthKeyPath  string
	AppAuthCertPath string
	AppAuthName     string
	UID             string
	GID             string
}

func NewServerConfig() *ServerConfig {
	return &ServerConfig{
		DockerHost:      "unix:///var/run/docker.sock",
		TemplatesDir:    "/opt/orch/templates",
		StateDir:        "./state",
		ServiceDir:      "/etc/service",
		Handlers:        "build,docker,dockerBatch,dummy",
		Capacity:        4,
		MaxJobMemory:    -1,
		MemoryLimit:     0.9,
		CleanFailedJobs: true,
	}
}

// Message format used between agent and clients (scheduler).
type Message struct {
	ID   uint   `json:"id"`
	Type string `json:"type"`

	// hello response
	Handlers []string `json:"handlers,omitempty"`

	// kill request, patch request, listJob request
	// jobChanged notification, jobRemoved notification
	JobID string `json:"jobID,omitempty"`

	// bid request (job payload omitted), offer request
	Job *JobRequest `json:"job,omitempty"`

	// patch request
	Patch *JobPayload `json:"patch,omitempty"`

	// bid response, offer response
	Bid *float64 `json:"bid,omitempty"`

	// offer response
	Info *JobInfo `json:"info,omitempty"`

	// listJobs response
	Jobs JobMap `json:"jobs,omitempty"`

	// getMode/getAgentState response, setMode request
	Mode string `json:"mode,omitempty"`

	// getUsage/getAgentState response
	Usage *PolicyUsage `json:"usage,omitempty"`

	// agent limits on job memory
	JobMemoryLimits *JobMemoryLimits `json:"jobMemoryLimits,omitempty"`

	// getConstraints/getAgentState response
	Constraints *AgentConstraints `json:"constraints,omitempty"`

	// error responses
	Message string `json:"message,omitempty"`

	// jobChanged notification
	JobInfo *JobInfo `json:"jobInfo,omitempty"`

	respChan chan<- interface{}
}

func (m *Message) SetError(errorMessage string) {
	m.Type = "error"
	m.Message = errorMessage
}

// Be careful of field collisions with agent.PolicyUsage and
// agent.AgentConstraints and agent.JobMemoryLimits
type State struct {
	*PolicyUsage
	*AgentConstraints
	*JobMemoryLimits
	Mode string `json:"mode,omitempty"`
}

// Report of an agent's current utilization.
type PolicyUsage struct {
	Used      float64 `json:"used"`
	Available float64 `json:"available"`
	// agent type (i.e. "docker", "batch", "build")
	Handlers []string `json:"handlers"`
}

// Report of the maximum/minimum memory a job can be on the host
type JobMemoryLimits struct {
	MinJobLimit float64 `json:"minJobLimit"`
	MaxJobLimit float64 `json:"maxJobLimit"`
}

// Request for running a new job.
type JobRequest struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Name     string `json:"name"`
	Priority int    `json:"priority"`

	Description *JobDescription `json:"description"`
	Payload     *JobPayload     `json:"payload,omitempty"`
}

func (j *JobRequest) Validate(checkPayload bool) error {
	if j == nil {
		return fmt.Errorf("request missing job data")
	}
	if len(j.ID) == 0 {
		return fmt.Errorf("request missing job ID")
	}
	if len(j.Type) == 0 {
		return fmt.Errorf("request missing job type")
	}
	if j.Description == nil {
		return fmt.Errorf("request missing job description")
	}
	if checkPayload && j.Payload == nil {
		return fmt.Errorf("request missing job payload")
	}
	return nil
}

// High-level information used to control job placement.
// Should *not* contain any sensitive data.
type JobDescription struct {
	Tenant string `json:"tenant"`

	// Hard constraints that must be satisfied during job placement.
	Requires  []string `json:"requires,omitempty"`  // agent-provided only
	Provides  []string `json:"provides,omitempty"`  // agent- or job-provided
	Conflicts []string `json:"conflicts,omitempty"` // agent- or job-provided

	// Soft constraints that may not be satisfied during job placement.
	Prefers  []string `json:"prefers,omitempty"`  // agent- or job-provided
	Despises []string `json:"despises,omitempty"` // agent- or job-provided

	Resources *JobLimits `json:"resources,omitempty"`

	ReplaceJobID *string `json:"replaceJobID,omitempty"`

	HealthCheck *HealthCheck `json:"healthCheck,omitempty"`

	HostName string `json:"hostName,omitempty"`
}

// HealthCheck information
type HealthCheck struct {
	Command            []string `json:"command"`
	ExpectedStatusCode int      `json:"expectedStatusCode"`
	ExpectedOutput     string   `json:"expectedOutput"`
}

// Resource limits that are applied to jobs.
type JobLimits struct {
	Memory    int64 `json:"memory,omitempty"`
	Swap      int64 `json:"swap,omitempty"`
	CPUShares int64 `json:"cpuShares,omitempty"`
}

// Full details on how to run a job.
// This may contain sensitive information; it will be discarded once
// any containers for the job are created/initialized.
type JobPayload struct {
	Action string `json:"action,omitempty"`

	Incomplete bool `json:"incomplete,omitempty"`

	// dummy jobs
	DummyDelay   int32 `json:"dummyDelay,omitempty"`
	DummyFail    bool  `json:"dummyFail,omitempty"`
	DummyFailNow bool  `json:"dummyFailNow,omitempty"`

	// build, docker jobs
	// - variable name -> variable value
	Environment map[string]string `json:"environment,omitempty"`
	// - variable name -> variable value
	TemplateValues map[string]string `json:"templateValues,omitempty"`
	// - network overlay configuration
	Overlay *JobOverlay `json:"overlay,omitempty"`
	// - data volume configuration
	Volumes []JobVolume `json:"volumes,omitempty"`

	// build jobs
	Template string   `json:"template,omitempty"`
	Registry string   `json:"registry,omitempty"`
	Tags     []string `json:"tags,omitempty"`

	// docker jobs
	TenantToken string `json:"tenantToken,omitempty"`

	DockerImage   string `json:"dockerImage,omitempty"`
	DockerCommand string `json:"dockerCommand,omitempty"`
	VerifyImage   bool   `json:"verifyImage,omitempty"`
	AppArmor      string `json:"appArmor,omitempty"`

	// - destination filename -> file contents and metadata
	Files map[string]*JobFile `json:"files,omitempty"`

	// - destination filename -> source filename
	Templates map[string]string `json:"templates,omitempty"`

	Proxy *JobProxy `json:"proxy,omitempty"`

	Publishes []*PublishService `json:"publishes,omitempty"`

	OneShot bool `json:"oneShot,omitempty"`
	Console bool `json:"console,omitempty"`

	DNS []string `json:"dns,omitempty"`

	Limits *JobLimits `json:"limits,omitempty"`
}

// Data and metadata for files injected into Docker containers.
type JobFile struct {
	// normal file
	Contents string `json:"contents,omitempty"`

	// symlink; file contents will be ignored
	Target string `json:"target,omitempty"`

	Mode    string `json:"mode,omitempty"`
	User    string `json:"user,omitempty"`
	Group   string `json:"group,omitempty"`
	UserID  *int   `json:"uid,omitempty"`
	GroupID *int   `json:"gid,omitempty"`
}

// Volume definitions for jobs.
// For various reasons, we can't use Docker's built-in support for
// volumes, at least directly. The entities described here are actually
// host bindings (directories shared with the host's file system).
type JobVolume struct {
	Type string `json:"type"`

	// all volume types ("simple", "block")

	// Where the volume is located on the host.
	HostPath string `json:"hostPath"`

	// Where the volume should show up in the container.
	ContainerPath string `json:"containerPath"`

	// block device volumes (type "block")

	// Block device path for this volume. If present, the agent will wait
	// until this device shows up on the host before initializing and
	// mounting the volume.
	BlockDevice string `json:"blockDevice"`

	// Raw initialization command for initializing the volume. This will
	// be run before any mount operation; executed via bash.
	Init string `json:"init"`

	// Raw line to add to /etc/fstab. If present, the agent will mount
	// the volume at the given host path before starting the job.
	Fstab string `json:"fstab"`

	// SCSI LUN device. Must be in colon-separated format (e.g., "5:0:0:0").
	// If present, any "blockDevice" value will be ignored.
	SCSILun string `json:"scsiLun"`

	// Raw line to add to /etc/crypttab.
	Crypttab string `json:"crypttab"`

	// Raw initialization command for configuring any volumes referenced in the
	// crypttab entry above; executed via bash.
	CryptInit string `json:"cryptInit"`

	// Raw command for unconfiguring any volumes referenced in the crypttab
	// entry above; executed via bash.
	CryptRemove string `json:"cryptRemove"`

	// Base64-encoded key used for encrypting any volumes referenced above.
	// This will be written underneath the agent's state directory.
	CryptKey string `json:"cryptKey"`

	// Raw command to run after volume has been mounted.
	PostMountCommand string `json:"postMountCommand"`
}

// Configuration data for setting up loproxy.
type JobProxy struct {
	// list of certificates in PEM format
	TrustAnchors []string `json:"trustAnchors"`
	// service name -> certificate data
	KeyPairs map[string]*ServiceCertificate `json:"keyPairs"`

	// "service@port" or "service@ip:port"
	Requires []string `json:"requires"`
	// "service@ip:port"
	Provides []string `json:"provides"`
}

// TLS certificate data used by loproxy.
type ServiceCertificate struct {
	// certificate chain in PEM format
	Chain string `json:"chain"`
	// certificate private key in PEM format
	Key string `json:"key"`
}

// Configuration data for setting up overlay networks.
type JobOverlay struct {
	// overlay type
	Type string `json:"type"`

	// SecureSDN overlay (type "ssdn")

	// Total network range for the overlay network, in CIDR format.
	// If present, overrides the -overlay-network flag for this agent.
	Network string `json:"network,omitempty"`
	// Local subnet for the overlay network, in CIDR format.
	// If present, overrides the -overlay-subnet flag for this agent.
	Subnet string `json:"subnet,omitempty"`

	// TLS CA certificate, in PEM format.
	CA string `json:"ca"`
	// TLS certificate, in PEM format.
	Cert string `json:"cert"`
	// TLS key, in PEM format.
	Key string `json:"key"`
	// TLS peer name; used when validating peer certificates.
	PeerName string `json:"peerName"`

	// Services present on this job.
	// Services listed here do not have to be exposed externally from the
	// container; these advertisements will only be visible to other
	// containers attached to this tenant's overlay network by way of DNS
	// records.
	Services []JobService `json:"services"`
}

// Used for to control which services are advertised to other
// containers on overlay networks.
type JobService struct {
	Name string `json:"name"`
	Port uint16 `json:"port"`
}

// Describes a service published by a job, and which host port
// to link the service to.
// Specified in job requests; turned into ServiceLocation data via the
// "launchInfo" field after a job is launched.
type PublishService struct {
	*JobService
	HostPort uint16 `json:"hostPort,omitempty"`
}

type JobMap map[string]*JobInfo

// Information about a launched job.
// Some of the fields below are stripped out before being returned to
// clients, particularly job description and job context data.
type JobInfo struct {
	ID   string `json:"-"`
	Name string `json:"name"`
	Type string `json:"type"`

	State Status `json:"state"`

	Created time.Time `json:"created"`
	Updated time.Time `json:"updated"`

	Description *JobDescription   `json:"description,omitempty"`
	Context     *JobContext       `json:"context,omitempty"`
	LaunchInfo  []ServiceLocation `json:"launchInfo"`

	handler *Handler
}

func NewJobInfo(job *JobRequest, state Status, handler *Handler) *JobInfo {
	return &JobInfo{
		ID:   job.ID,
		Name: job.Name,
		Type: job.Type,

		State: state,

		Created: time.Now(),
		Updated: time.Now(),

		Description: job.Description,

		handler: handler,
	}
}

// Host-specific information for a job.
// This data is retained after a job has been created/initialized and
// should *not* contain sensitive information unless absolutely
// necessary.
// This data is included in the serialized state file.
type JobContext struct {
	// should be retained no longer than necessary (see above)
	Payload *JobPayload `json:"payload,omitempty"`

	ContainerID string `json:"containerID"`

	OneShot bool `json:"oneShot"`

	Mounts []HostMount `json:"mounts,omitempty"`

	Overlay      *OverlayContext `json:"overlay,omitempty"`
	ProxyPorts   []uint16        `json:"proxyPorts,omitempty"`
	PublishPorts []uint16        `json:"publishPorts,omitempty"`

	HostName string `json:"hostName,omitempty"`
}

// Host-specific information for a job's overlay network.
type OverlayContext struct {
	Type string `json:"type"`

	Network string `json:"network"`
	Subnet  string `json:"subnet"`

	IP string `json:"ip"`
}

// Detailed information about services being published by a job.
type ServiceLocation struct {
	Name    string `json:"name"`
	Address string `json:"address"`
	Port    uint16 `json:"port"`
}
