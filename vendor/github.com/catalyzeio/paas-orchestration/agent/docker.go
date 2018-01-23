package agent

import (
	"archive/tar"
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"path"
	"strconv"
	"strings"
	"time"

	coreapi "github.com/catalyzeio/core-api/client"
	"github.com/catalyzeio/go-core/comm"
	"github.com/catalyzeio/go-core/udocker"
	"github.com/hoisie/mustache"
	digest "github.com/opencontainers/go-digest"

	"github.com/docker/cli/cli/trust"
	"github.com/docker/distribution/reference"
	"github.com/docker/distribution/registry/client/auth"
	"github.com/docker/distribution/registry/client/auth/challenge"
	"github.com/docker/distribution/registry/client/transport"
	"github.com/docker/docker/registry"
	notary "github.com/theupdateframework/notary/client"
	"github.com/theupdateframework/notary/trustpinning"
	"github.com/theupdateframework/notary/tuf/data"
)

const (
	JobLabel    = "io.catalyze.paas.job"
	TenantLabel = "io.catalyze.paas.tenant"

	// how often to poll container statuses from the docker server
	containerPollFrequency = 15 * time.Second

	// how often to poll the container for a healthcheck when it is
	// transitioning between Running and Unhealthy
	healthCheckPollFrequency = 2 * time.Second

	// one-shot job time constraints
	oneShotMaxLifetime = 12 * time.Hour

	// proxied ports that are advertised in the service registry (externally visible)
	proxyPortStart = 18000
	// ports that are exposed directly to the outside world (externally visible)
	publishPortStart = 22000
	// internal ports that correspond with the proxied ports (externally visible via NAT)
	internalPortStart = 26000

	// DNS suffix used for loproxy localhost aliases
	dnsSuffix = "internal"

	// SSDN stable watch interval for checking container states
	stableWatchInterval = time.Second * 7

	// maximum number of times to retry a docker pull
	maxPullRetry = 3

	// how often to retry failed docker pulls
	pullRetryFrequency = time.Second * 1
)

// move to types.go
type target struct {
	name   string
	digest digest.Digest
	size   int64
}

var errJobKilled = fmt.Errorf("jobKilled")

type DockerSpawner struct {
	client       *udocker.Client
	mountKeysDir string
	templatesDir string

	registryURL    string
	serviceAddress string

	overlays    *OverlayManager
	proxyPool   *PortPool
	publishPool *PortPool

	cleanFailedJobs bool

	notaryConfig *NotaryConfig
}

type NotaryConfig struct {
	NotaryServer    string
	TrustDir        string
	AppAuthKeyPath  string
	AppAuthCertPath string
	AppAuthName     string
}

func NewDockerSpawner(dockerHost, stateDir, templatesDir, registryURL, serviceAddress string, overlays *OverlayManager, cleanFailedJobs bool, notaryConfig *NotaryConfig) (*DockerSpawner, error) {
	client, err := udocker.ClientFromURL(dockerHost)
	if err != nil {
		return nil, err
	}
	templatesDir = path.Join(templatesDir, "docker")
	return &DockerSpawner{
		client:       client,
		mountKeysDir: path.Join(stateDir, "mount-keys"),
		templatesDir: templatesDir,

		registryURL:    registryURL,
		serviceAddress: serviceAddress,

		overlays: overlays,

		proxyPool:   NewPortPool(proxyPortStart, publishPortStart-1),
		publishPool: NewPortPool(publishPortStart, internalPortStart-1),

		cleanFailedJobs: cleanFailedJobs,

		notaryConfig: notaryConfig,
	}, nil
}

func (s *DockerSpawner) Spawn(job *JobRequest, info, replacedJob *JobInfo) (Runner, error) {
	description := info.Description
	if description == nil {
		return nil, fmt.Errorf("docker job missing description")
	}
	payload := job.Payload
	if payload == nil {
		return nil, fmt.Errorf("docker job missing payload")
	}
	if len(payload.DockerImage) == 0 {
		return nil, fmt.Errorf("docker job missing image")
	}
	if payload.VerifyImage && s.notaryConfig == nil {
		return nil, fmt.Errorf("docker cannot verify image without knowing the address of a valid notary server")
	}

	// overlay network
	overlayContext, err := s.overlays.Reserve(job.ID, description.Tenant, payload.TenantToken, payload.Overlay)
	if err != nil {
		return nil, err
	}

	// allocate ports for published and proxied services
	spawned := false

	var proxied []uint16
	var published []uint16
	var launchInfo []ServiceLocation
	newContext := true
	var mounts []HostMount
	if replacedJob != nil {
		newContext = false
		proxied = replacedJob.Context.ProxyPorts
		published = replacedJob.Context.PublishPorts
		launchInfo = replacedJob.LaunchInfo
		for _, m := range replacedJob.Context.Mounts {
			mounts = append(mounts, HostMount{
				HostPath:      m.HostPath,
				ContainerPath: m.ContainerPath,
				CryptRemove:   m.CryptRemove,
				KeyFile:       m.KeyFile,
				FstabLine:     m.FstabLine,
				CrypttabLine:  m.CrypttabLine,
			})
		}
	}

	defer func() {
		if !spawned {
			s.proxyPool.ReleaseAll(proxied)
			s.publishPool.ReleaseAll(published)
		}
	}()

	if newContext {
		// proxied services
		proxy := payload.Proxy
		if proxy != nil {
			provides := proxy.Provides
			if provides != nil {
				n := len(provides)
				proxied = make([]uint16, 0, n)
				for i := 0; i < n; i++ {
					port, err := s.proxyPool.Next()
					if err != nil {
						return nil, err
					}
					proxied = append(proxied, port)
				}
			}
		}
	}
	publishes := payload.Publishes
	anyPublishesHostPort := false
	for _, s := range publishes {
		if s.HostPort > 0 {
			anyPublishesHostPort = true
			break
		}
	}
	if newContext || anyPublishesHostPort {
		// published services with launch info
		if publishes != nil {
			n := len(publishes)
			published = make([]uint16, 0, n)
			launchInfo = make([]ServiceLocation, 0, n)
			for _, service := range publishes {
				port := service.HostPort
				if port == 0 {
					var err error
					port, err = s.publishPool.Next()
					if err != nil {
						return nil, err
					}
				}
				published = append(published, port)
				launchInfo = append(launchInfo, ServiceLocation{
					Name:    service.Name,
					Address: s.serviceAddress,
					Port:    port,
				})
			}
		}
	}

	// update job info directly
	info.LaunchInfo = launchInfo
	context := &JobContext{
		Payload: payload,

		OneShot: payload.OneShot,

		Overlay:      overlayContext,
		ProxyPorts:   proxied,
		PublishPorts: published,
		Mounts:       mounts,
		HostName:     description.HostName,
	}
	info.Context = context

	// return job runner
	r := newDockerRunner(s, info, *context, proxied, published)

	// strip payload from created context if it is not needed,
	// otherwise it will get serialized in the state file
	if !payload.Incomplete {
		context.Payload = nil
	}

	spawned = true
	return r, nil
}

func (s *DockerSpawner) Restore(info *JobInfo) (Runner, error) {
	// validate context
	description := info.Description
	if description == nil {
		return nil, fmt.Errorf("docker job missing description")
	}
	context := info.Context
	if context == nil {
		return nil, fmt.Errorf("docker job missing context")
	}

	var proxied []uint16
	var published []uint16
	restored := false

	// don't restore overlay network or port allocations for terminated jobs
	if !info.State.Terminal() {
		// restore overlay settings
		if err := s.overlays.Restore(context.Overlay, info.ID, description.Tenant); err != nil {
			return nil, fmt.Errorf("could not restory overlay network: %s", err)
		}

		// restore used ports
		defer func() {
			if !restored {
				s.proxyPool.ReleaseAll(proxied)
				s.publishPool.ReleaseAll(published)
			}
		}()

		if err := s.proxyPool.AcquireAll(context.ProxyPorts); err != nil {
			return nil, fmt.Errorf("could not restore proxied ports: %s", err)
		}
		proxied = context.ProxyPorts

		if err := s.publishPool.AcquireAll(context.PublishPorts); err != nil {
			return nil, fmt.Errorf("could not restore proxied ports: %s", err)
		}
		published = context.PublishPorts
	}

	// return job runner
	r := newDockerRunner(s, info, *context, proxied, published)
	restored = true
	return r, nil
}

func (s *DockerSpawner) CleanUpPublishPorts(ports []uint16) {
	s.publishPool.ReleaseAll(ports)
}

type dockerRunner struct {
	parent *DockerSpawner

	jobID   string
	tenant  string
	context JobContext

	proxied   []uint16
	published []uint16

	healthCheck *HealthCheck
}

func newDockerRunner(parent *DockerSpawner, info *JobInfo, context JobContext, proxied, published []uint16) *dockerRunner {
	return &dockerRunner{
		parent: parent,

		jobID:   info.ID,
		tenant:  info.Description.Tenant,
		context: context,

		proxied:   proxied,
		published: published,

		healthCheck: info.Description.HealthCheck,
	}
}

func (r *dockerRunner) Run(state Status, s *Signals, updates chan<- Update) (Status, error) {
	context := &r.context
	isUnhealthy := r.healthCheck != nil

	payload := context.Payload
	if payload != nil {
		if payload.Incomplete {
			return Waiting, nil
		}
		if err := r.create(s.KillRequests, updates, payload); err != nil {
			dropState := Failed
			if err == errJobKilled {
				dropState = Killed
				err = nil
			}
			// ensure that payload is stripped from recorded context
			r.dropPayload(dropState, updates)
			return dropState, err
		}
		state = Running
		if isUnhealthy {
			state = Unhealthy
		}
		updates <- Update{JobID: r.jobID, State: state}
	}
	client := r.parent.client
	id := context.ContainerID

	if len(id) == 0 {
		return Failed, fmt.Errorf("job missing container ID")
	}
	freq := containerPollFrequency
	pollTimer := time.NewTimer(freq)
	defer pollTimer.Stop()
	for {
		details, err := client.InspectContainer(id)
		if udocker.IsNotFound(err) {
			log.Errorf("Container %s disappeared: %s", id, err)
			return Disappeared, nil
		}
		if err != nil {
			log.Warn("Failed to check container %s status: %s", id, err)
		} else {
			nextState := r.transition(details, isUnhealthy)
			if nextState.Terminal() {
				state = nextState
				return nextState, nil
			}
			if nextState != state {
				state = nextState
				updates <- Update{JobID: r.jobID, State: state}
			}
		}

		if r.runawayContainer(details) {
			if err := client.StopContainer(id, 0); err != nil {
				log.Warn("Job %s: failed to kill runaway container %s: %s", r.jobID, id, err)
			} else {
				log.Info("Job %s: killed runaway container %s", r.jobID, id)
			}
			return Killed, nil
		}
		select {
		case <-s.KillRequests:
			// check if the container has already been stopped
			if state == Stopped {
				log.Info("Job %s: was already killed/stopped %s", r.jobID, id)
				return Killed, nil
			}
			state = Unhealthy
			updates <- Update{JobID: r.jobID, State: state}
			time.Sleep(stableWatchInterval * 2)
			if err := client.StopContainer(id, 6); err != nil {
				log.Warn("Job %s: failed to gracefully kill container %s: %s", r.jobID, id, err)
			} else {
				log.Info("Job %s: gracefully killed container %s", r.jobID, id)
			}
			return Killed, nil
		case <-s.StopRequests:
			if state < Stopped {
				state = Unhealthy
				updates <- Update{JobID: r.jobID, State: state}
				time.Sleep(stableWatchInterval * 2)
				if err := client.StopContainer(id, 15); err != nil {
					log.Warn("Job %s: failed to stop container %s: %s", r.jobID, id, err)
				} else {
					log.Info("Job %s: stopped container %s", r.jobID, id)
				}
			} else {
				log.Warn("Job %s: container %s received redundant stop request", r.jobID, id)
			}

		case <-s.StartRequests:
			if err := r.startContainer(); err != nil {
				log.Warn("Job %s: failed to restart container %s: %s", r.jobID, id, err)
			} else {
				log.Info("Job %s: restarted container %s", r.jobID, id)
			}
		case <-pollTimer.C:
			isUnhealthy, freq = r.processHealthCheck(id, isUnhealthy, state)
		case msg := <-s.PatchRequests:
			if msg.respChan == nil {
				log.Warn("Job %s: received a live patch request without a response channel", r.jobID)
			} else {
				resp := &Message{ID: msg.ID, Type: "response"}
				r.livePatch(msg, resp)
				msg.respChan <- resp
			}

		}
		pollTimer.Reset(freq)
	}
}

func (r *dockerRunner) processHealthCheck(id string, isUnhealthy bool, state Status) (bool, time.Duration) {
	freq := containerPollFrequency
	if r.healthCheck != nil && (state == Running || state == Unhealthy) {
		stdout, _, statusCode, err := r.parent.client.ExecContainer(r.healthCheck.Command, id)
		if err != nil {
			log.Warn("Job %s: failed to execute successful healthcheck (proceeding as though health check does not exist): %s", id, err)
			isUnhealthy = false
		} else {
			change := false
			if (r.healthCheck.ExpectedStatusCode != -1 && r.healthCheck.ExpectedStatusCode != statusCode) ||
				(len(r.healthCheck.ExpectedOutput) != 0 && r.healthCheck.ExpectedOutput != stdout) {
				change = !isUnhealthy
				isUnhealthy = true
			} else {
				change = isUnhealthy
				isUnhealthy = false
			}
			if change {
				freq = healthCheckPollFrequency
			} else {
				freq = containerPollFrequency
			}
		}
	}
	return isUnhealthy, freq
}

func (r *dockerRunner) transition(details *udocker.ContainerDetails, isUnhealthy bool) Status {
	s := details.State
	if s.Running {
		if isUnhealthy {
			return Unhealthy
		}
		// container is still running
		return Running
	}

	// normal jobs never reach a terminal state unless killed
	if !r.context.OneShot {
		return Stopped
	}

	// use exit code to determine success/failure of one-shot job
	if s.ExitCode == 0 {
		return Finished
	}
	log.Warn("Job %s failed with exit code %d", r.jobID, s.ExitCode)
	return Failed
}

func (r *dockerRunner) runawayContainer(details *udocker.ContainerDetails) bool {
	// normal jobs have no lifetime limits
	if !r.context.OneShot {
		return false
	}

	s := details.State
	now := time.Now()
	// check one-shot job lifetime limit against the container's start time
	if validTime(now, s.StartedAt) && now.After(s.StartedAt.Add(oneShotMaxLifetime)) {
		log.Warn("Job %s: one-shot job exceeded time limit of %s", r.jobID, oneShotMaxLifetime)
		return true
	}
	return false
}

func validTime(now, t time.Time) bool {
	var delta time.Duration
	if now.After(t) {
		delta = now.Sub(t)
	} else {
		delta = t.Sub(now)
	}
	// if the given time is more than 10 years away, it's probably not trustworthy
	return delta < time.Hour*24*365*10
}

func (r *dockerRunner) livePatch(msg, resp *Message) {
	payload := msg.Patch
	if payload == nil {
		resp.SetError(fmt.Sprintf("job %s received a live patch request with an empty payload", r.jobID))
		return
	}
	var err error
	switch payload.Action {
	case "resizefs":
		err = r.resizeFS(payload)
	default:
		resp.SetError(fmt.Sprintf("job %s received a live patch request with unknown action %s", r.jobID, payload.Action))
	}
	if err != nil {
		resp.SetError(fmt.Sprintf("live patch for job %s failed: %s", r.jobID, err.Error()))
	}
}

func (r *dockerRunner) resizeFS(payload *JobPayload) error {
	devPaths := make(map[string]struct{})
	for _, m := range r.context.Mounts {
		devPaths[m.HostPath] = struct{}{}
	}
	for _, v := range payload.Volumes {
		if _, ok := devPaths[v.HostPath]; !ok {
			return fmt.Errorf("resizefs failed: volume %s does not belong to this job", v.HostPath)
		}
		exists, err := pathExists(v.BlockDevice)
		if err != nil {
			return fmt.Errorf("resizefs failed: %s", err)
		}
		if !exists {
			return fmt.Errorf("resizefs failed: device %s does not exist", v.BlockDevice)
		}
	}
	var errs []string
	for _, v := range payload.Volumes {
		err := resize(v.BlockDevice)
		if err != nil {
			errs = append(errs, err.Error())
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("resizefs failed while resizing volumes: %s", strings.Join(errs, "\n"))
	}
	return nil
}

func (r *dockerRunner) Patch(payload *JobPayload) (Status, *JobContext, error) {
	current := r.context.Payload
	if current == nil {
		// container was already built
		return Failed, nil, fmt.Errorf("job can no longer be patched")
	}
	// patch current payload
	current.Incomplete = payload.Incomplete
	if payload.Volumes != nil {
		current.Volumes = payload.Volumes
	}
	// resume job if payload is now complete
	snapshot := r.context
	if current.Incomplete {
		return Waiting, &snapshot, nil
	}
	return Started, &snapshot, nil
}

func (r *dockerRunner) CleanUp(state Status, keepResources bool) error {
	parent := r.parent
	context := r.context

	// clean up overlay network resources
	if err := parent.overlays.Stopped(context.Overlay, r.jobID, r.tenant); err != nil {
		log.Warn("Job %s: failed to clean up overlay network: %s", r.jobID, err)
	}

	if !keepResources {
		// return any ports used by this job
		parent.proxyPool.ReleaseAll(r.proxied)
		parent.publishPool.ReleaseAll(r.published)

		// umount volumes and clean up fstab
		CleanUpHostMounts(r.jobID, r.context.Mounts)
	}
	return nil
}

func (r *dockerRunner) Cull(state Status) error {
	parent := r.parent

	cleanContainer := true
	if state == Failed && !parent.cleanFailedJobs {
		log.Warn("Job %s: job failed, not cleaning up job container or image", r.jobID)
		cleanContainer = false
	}

	if cleanContainer {
		client := parent.client
		context := r.context

		// delete container
		id := context.ContainerID
		if len(id) > 0 {
			if err := client.DeleteContainer(id); err != nil {
				log.Warn("Job %s: failed to delete container %s: %s", r.jobID, id, err)
			} else {
				log.Info("Job %s: deleted container %s", r.jobID, id)
			}
		}

		// delete job image
		_, err := client.DeleteImage(r.jobID)
		if err != nil {
			log.Warn("Job %s: failed to delete job image: %s", r.jobID, err)
		} else {
			log.Info("Job %s: deleted job image", r.jobID)
		}
	}

	return nil
}

type containerConfig struct {
	hostName string
	files    []*udocker.TarEntry
	scripts  []string
	fixups   Fixups

	labels map[string]string

	overlayDNS []string

	ports []portMapping
	binds []string
	hosts []string
}

func newContainerConfig(jobID, tenant string) *containerConfig {
	defaultLabels := map[string]string{
		JobLabel:    jobID,
		TenantLabel: tenant,
	}
	return &containerConfig{
		fixups: make(Fixups),
		labels: defaultLabels,
	}
}

func (c *containerConfig) addFile(file *udocker.TarEntry) {
	c.files = append(c.files, file)
}

func (c *containerConfig) addScript(script string) {
	c.scripts = append(c.scripts, script)
}

func (c *containerConfig) addPortMapping(internal, external uint16) {
	mapping := portMapping{
		internal: internal,
		external: external,
	}
	c.ports = append(c.ports, mapping)
}

func (c *containerConfig) generate(jobID string, image string, env map[string]string, dns []string, limits *JobLimits) *udocker.ContainerDefinition {
	config := &udocker.ContainerDefinition{
		Hostname: c.hostName,
		Image:    image,
		Labels:   c.labels,
	}

	var envVars []string
	for k, v := range env {
		envVars = append(envVars, fmt.Sprintf("%s=%s", k, v))
	}
	config.Env = envVars

	hc := &config.HostConfig

	hc.Binds = c.binds
	log.Info("Job %s: volume binds=%s", jobID, c.binds)
	overlayDNS := c.overlayDNS
	if overlayDNS != nil {
		if dns != nil {
			log.Warn("Overlay network DNS servers (%s) overriding configured DNS servers (%s)", overlayDNS, dns)
		}
		hc.Dns = overlayDNS
	} else {
		hc.Dns = dns
	}
	log.Info("Job %s: DNS servers=%s", jobID, hc.Dns)

	hc.ExtraHosts = c.hosts
	log.Info("Job %s: extra hosts=%s", jobID, hc.ExtraHosts)

	if limits != nil {
		hc.Memory = limits.Memory
		hc.MemorySwap = limits.Swap
		hc.CpuShares = limits.CPUShares
	}

	return config
}

type Fixups map[string][]Fixup
type Fixup map[string]string

func (f Fixups) AddUser(filename, user string) {
	f["users"] = append(f["users"], Fixup{
		"filename": filename,
		"user":     user,
	})
}

func (f Fixups) AddGroup(filename, group string) {
	f["groups"] = append(f["groups"], Fixup{
		"filename": filename,
		"group":    group,
	})
}

type portMapping struct {
	internal uint16
	external uint16
}

// ImageRefAndAuth contains all reference information and the auth config for an image request
type ImageRef struct {
	Original  string
	Reference reference.Named
	RepoInfo  *registry.RepositoryInfo
	Tag       string
	Digest    digest.Digest
}

func getTag(ref reference.Named) string {
	switch x := ref.(type) {
	case reference.Canonical, reference.Digested:
		return ""
	case reference.NamedTagged:
		return x.Tag()
	default:
		return ""
	}
}

func getDigest(ref reference.Named) digest.Digest {
	switch x := ref.(type) {
	case reference.Canonical:
		return x.Digest()
	case reference.Digested:
		return x.Digest()
	default:
		return digest.Digest("")
	}
}

func getImageReference(image string) (*ImageRef, error) {
	distributionRef, err := reference.ParseNormalizedNamed(image)
	if err != nil {
		return nil, err
	}
	if reference.IsNameOnly(distributionRef) {
		return nil, fmt.Errorf("tag not specified for %s", image)
	}
	repoInfo, err := registry.ParseRepositoryInfo(distributionRef)
	if err != nil {
		return nil, err
	}
	return &ImageRef{
		Original:  image,
		Reference: distributionRef,
		RepoInfo:  repoInfo,
		Tag:       getTag(distributionRef),
		Digest:    getDigest(distributionRef),
	}, nil
}

func getTrustedPullTarget(imgRef *ImageRef, notaryConfig *NotaryConfig) (*target, error) {
	notaryServer := fmt.Sprintf("https://%s", notaryConfig.NotaryServer)
	gun := imgRef.RepoInfo.Name.Name()
	hubTrans, err := makeHubTransport(notaryServer, notaryConfig, fmt.Sprintf("repository:%s:pull", gun))
	if err != nil {
		return nil, err
	}
	repo, err := notary.NewFileCachedRepository(
		notaryConfig.TrustDir,
		data.GUN(gun),
		notaryServer,
		hubTrans,
		nil,
		trustpinning.TrustPinConfig{},
	)
	ref := imgRef.Reference
	tagged, isTagged := ref.(reference.NamedTagged)
	if !isTagged {
		return nil, fmt.Errorf("a tag must be provided in order to verify an image")
	}
	t, err := repo.GetTargetByName(tagged.Tag(), trust.ReleasesRole, data.CanonicalTargetsRole)
	if err != nil {
		return nil, err
	}
	if t.Role != trust.ReleasesRole && t.Role != data.CanonicalTargetsRole {
		return nil, fmt.Errorf("No trust data for %s", tagged.Tag())
	}
	return convertTarget(t.Target)
}

func convertTarget(t notary.Target) (*target, error) {
	h, ok := t.Hashes["sha256"]
	if !ok {
		return nil, errors.New("no valid hash, expecting sha256")
	}
	return &target{
		name:   t.Name,
		digest: digest.NewDigestFromHex("sha256", hex.EncodeToString(h)),
		size:   t.Length,
	}, nil
}

func makeHubTransport(server string, notaryConfig *NotaryConfig, scope string) (http.RoundTripper, error) {
	name, err := os.Hostname()
	if err != nil {
		return nil, err
	}
	base := http.DefaultTransport
	modifiers := []transport.RequestModifier{
		transport.NewHeaderRequestModifier(http.Header{
			"User-Agent": []string{name},
		}),
	}
	authTransport := transport.NewTransport(base, modifiers...)
	pingClient := &http.Client{
		Transport: authTransport,
		Timeout:   5 * time.Second,
	}
	req, err := http.NewRequest("GET", server+"/v2/", nil)
	if err != nil {
		return nil, err
	}
	challengeManager := challenge.NewSimpleManager()
	resp, err := pingClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if err := challengeManager.AddResponse(resp); err != nil {
		return nil, err
	}
	authHandler := newCoreAPIHandler(notaryConfig, scope)
	modifiers = append(modifiers, auth.NewAuthorizer(challengeManager, authHandler))
	return transport.NewTransport(base, modifiers...), nil

}

type coreAPIHandler struct {
	*NotaryConfig
	tokenCache string
	expiration *time.Time
	scope      string
}

func (ca *coreAPIHandler) Scheme() string {
	return "bearer"
}

func (ca *coreAPIHandler) AuthorizeRequest(req *http.Request, params map[string]string) error {
	realm, ok := params["realm"]
	if !ok {
		return fmt.Errorf("realm required to authorize")
	}
	service, ok := params["service"]
	if !ok {
		return fmt.Errorf("service required to authorize")
	}
	scope := ca.scope
	var token string
	if len(ca.tokenCache) > 0 && ca.expiration != nil && ca.expiration.After(time.Now()) {
		token = ca.tokenCache
	} else {
		cert, err := fileToString(ca.AppAuthCertPath)
		if err != nil {
			return err
		}
		key, err := fileToString(ca.AppAuthKeyPath)
		if err != nil {
			return err
		}
		u, err := url.Parse(realm)
		if err != nil {
			return err
		}
		appAuth := fmt.Sprintf("%s://%s", u.Scheme, u.Host)
		coreapiAuth := coreapi.NewAppAuth(appAuth, key, cert)
		tk, err := coreapiAuth.GetDockerToken(service, scope, ca.AppAuthName)
		if err != nil {
			return err
		}
		token = string(tk.Token)
		ca.tokenCache = token
		expires := time.Unix(tk.Expires, 0)
		ca.expiration = &expires
	}
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token))
	return nil
}

func newCoreAPIHandler(notaryConfig *NotaryConfig, scope string) *coreAPIHandler {
	return &coreAPIHandler{
		notaryConfig,
		"",
		nil,
		scope,
	}
}

func (r *dockerRunner) create(kill <-chan struct{}, updates chan<- Update, payload *JobPayload) error {
	var image, tag string
	image = payload.DockerImage
	tag = ""
	if payload.VerifyImage {
		notaryConfig := r.parent.notaryConfig
		if notaryConfig == nil {
			return fmt.Errorf("create needs a notary config to verify an image")
		}
		imgRef, err := getImageReference(image)
		if err != nil {
			return err
		}
		t, err := getTrustedPullTarget(imgRef, notaryConfig)
		if err != nil {
			return err
		}
		ref := imgRef.Reference
		tag = string(t.digest)
		trustedRef, err := reference.WithDigest(reference.TrimNamed(ref), t.digest)
		if err != nil {
			// couldn't create canonical reference to image
			return err
		}
		updatedImgRef, err := getImageReference(trustedRef.String())
		if err != nil {
			// parsing trusted reference failed
			return err
		}
		image = reference.FamiliarName(updatedImgRef.Reference)
	} else {
		image, tag = r.parent.client.ParseImage(image)
	}

	// pull latest image if image name references a remote registry
	index := strings.Index(image, "/")
	if index >= 0 {
		maybeHost := image[:index]
		if strings.ContainsAny(maybeHost, ":.") {
			var err error
			for i := 0; i < maxPullRetry; i++ {
				select {
				case <-kill:
					log.Warn("Job %s was killed in between attempts to pull down its image", r.jobID)
					return errJobKilled
				default:
				}

				err = r.parent.client.PullImageTag(image, tag)
				if err == nil {
					break
				}
				log.Info("Failed to pull image %s: %s - retrying", image, err)
				time.Sleep(pullRetryFrequency)
			}
			if err != nil {
				return err
			}
		}
	}

	c := newContainerConfig(r.jobID, r.tenant)

	// add inline files from payload
	if err := r.addFiles(c, payload.Files); err != nil {
		return err
	}
	// add host name to containerConfig
	c.hostName = r.context.HostName
	// Docker's tar support only supports uid/gid in tar files, not symbolic names.
	// If symbolic names are specified we have to set them in the container with a script.
	if len(c.fixups) > 0 {
		setowner, err := r.render("setowner", c.fixups)
		if err != nil {
			return err
		}
		const fixupScriptPath = "/tmp/setowner.sh"
		c.addFile(udocker.NewTarEntry(fixupScriptPath, []byte(setowner), 0700))
		c.addScript(fixupScriptPath)
	}

	// extract and render template files from the container
	if err := r.addTemplates(c, payload.Templates, payload.TemplateValues, payload.Environment); err != nil {
		return err
	}

	// generate overlay network configuration
	if err := r.addOverlay(c, payload.TenantToken, payload.Overlay); err != nil {
		return err
	}

	// generate loproxy configuration
	if err := r.addProxy(c, payload.Proxy, payload.TenantToken); err != nil {
		return err
	}

	// record port mappings for published services
	for i, service := range payload.Publishes {
		c.addPortMapping(service.Port, r.published[i])
	}

	// build finalized image
	buff, err := r.createDockerTarball(c, payload.DockerImage, payload.DockerCommand)
	if err != nil {
		return err
	}
	if err := r.build(kill, buff); err != nil {
		return err
	}

	log.Info("Job %s: built docker image", r.jobID)

	// If Mounts have already been added to the context by the Spawner (this
	// can happen when a job is replacing another job), we do not need to
	// convert the volumes to mounts, which is what processVolumes does.
	if r.context.Mounts == nil {
		if err := r.processVolumes(kill, updates, payload.Volumes); err != nil {
			return err
		}
	}

	// If mounts have been added either by the Spawner (replacement jobs),
	// or the processVolumes function (volume conversion) they still
	// both need to be bound to the container.
	if r.context.Mounts != nil {
		c.binds = generateBinds(r.context.Mounts)
	}
	// create container
	containerID, err := r.createContainer(c, payload)
	if err != nil {
		return err
	}
	log.Info("Job %s: created container %s", r.jobID, containerID)

	// update context with container ID
	r.context.ContainerID = containerID
	// drop payload so next invocation does not try to rebuild the container
	r.dropPayload(Started, updates)

	// start container
	if err := r.startContainer(); err != nil {
		return err
	}
	log.Info("Job %s: started container %s", r.jobID, r.context.ContainerID)

	// done!
	return nil
}

func (r *dockerRunner) startContainer() error {
	return r.parent.client.StartContainer(r.context.ContainerID, nil)
}

func (r *dockerRunner) dropPayload(state Status, updates chan<- Update) {
	if r.context.Payload == nil {
		// already dropped
		return
	}
	r.context.Payload = nil
	snapshot := r.context
	updates <- Update{JobID: r.jobID, State: state, NewContext: &snapshot}
}

func (r *dockerRunner) render(templateName string, context interface{}) (string, error) {
	templateFile := path.Join(r.parent.templatesDir, fmt.Sprintf("%s.mustache", templateName))
	template, err := ioutil.ReadFile(templateFile)
	if err != nil {
		return "", err
	}
	rendered := mustache.Render(string(template), context)
	if log.IsTraceEnabled() {
		log.Trace("Rendered template %s: %s", templateName, rendered)
	}
	return rendered, nil
}

func (r *dockerRunner) addFiles(c *containerConfig, files map[string]*JobFile) error {
	if files == nil {
		return nil
	}

	fixups := c.fixups
	for name, data := range files {
		if data == nil {
			continue
		}
		// validate file data
		strMode := data.Mode
		if len(strMode) == 0 {
			strMode = "0600"
		}
		mode, err := strconv.ParseInt(strMode, 0, 16)
		if err != nil {
			return fmt.Errorf("invalid mode for file %s: %s", name, err)
		}
		// create basic entry
		file := udocker.NewTarEntry(name, []byte(data.Contents), mode)
		header := file.Header
		target := data.Target
		if len(target) > 0 {
			// handle symlinks
			header.Typeflag = tar.TypeSymlink
			header.Linkname = target
		} else {
			// modify file metadata to match submitted data
			if len(data.User) > 0 {
				fixups.AddUser(name, data.User)
			}
			if len(data.Group) > 0 {
				fixups.AddGroup(name, data.Group)
			}
			if data.UserID != nil {
				header.Uid = *data.UserID
			}
			if data.GroupID != nil {
				header.Gid = *data.GroupID
			}
		}
		// add completed file
		c.addFile(file)
	}
	return nil
}

type TemplateContext map[string][]TemplateData
type TemplateData map[string]string

func NewTemplateContext() TemplateContext {
	return make(TemplateContext)
}

func (t TemplateContext) AddVar(key, value string) {
	item := TemplateData{
		"key":   quoteEnvName(key),
		"value": quoteEnvValue(value),
	}
	t["vars"] = append(t["vars"], item)
}

func (t TemplateContext) AddShave(file, template string) {
	item := TemplateData{
		"file":     file,
		"template": template,
	}
	t["shaves"] = append(t["shaves"], item)
}

func (r *dockerRunner) addTemplates(c *containerConfig, templates map[string]string,
	templateValues map[string]string, environment map[string]string) error {
	// Docker doesn't have a convenient method for pulling specific files out of an image.
	// Rather than playing games with creating temporary containers, we render templates
	// inside the container using an oversimplified Perl implementation of mustache.
	if templates == nil {
		return nil
	}

	// add the fu-manchu script to the container
	fmFile := path.Join(r.parent.templatesDir, "fu-manchu")
	fm, err := ioutil.ReadFile(fmFile)
	if err != nil {
		return err
	}
	c.addFile(udocker.NewTarEntry("/usr/bin/fu-manchu", fm, 0700))

	// prepare the context for the template substitution script
	tc := NewTemplateContext()
	for k, v := range environment {
		tc.AddVar(k, v)
	}
	for k, v := range templateValues {
		tc.AddVar(k, v)
	}
	for k, v := range templates {
		tc.AddShave(k, v)
	}

	// render the substitution script
	shave, err := r.render("shave-configs", tc)
	if err != nil {
		return err
	}
	const templateScriptPath = "/tmp/shave-configs.sh"
	c.addFile(udocker.NewTarEntry(templateScriptPath, []byte(shave), 0700))
	c.addScript(templateScriptPath)

	return nil
}

func (r *dockerRunner) addOverlay(c *containerConfig, tenantToken string, overlay *JobOverlay) error {
	oc := r.context.Overlay
	return r.parent.overlays.Initialize(oc, r.jobID, r.tenant, tenantToken, overlay, c)
}

func (r *dockerRunner) addProxy(c *containerConfig, proxy *JobProxy, tenantToken string) error {
	if proxy == nil {
		return nil
	}

	context := map[string]interface{}{
		"tenant":      r.tenant,
		"tenantToken": tenantToken,

		"registry":      r.parent.registryURL,
		"publicAddress": r.parent.serviceAddress,
		"tls":           false,
	}

	const loproxyConfigDir = "/etc/loproxy/config"

	for i, anchor := range proxy.TrustAnchors {
		path := fmt.Sprintf("%s/keystore/ca/cert-%d.pem", loproxyConfigDir, i)
		c.addFile(udocker.NewTarEntry(path, []byte(anchor), 0400))
	}

	for service, cert := range proxy.KeyPairs {
		if cert == nil {
			continue
		}

		context["tls"] = true

		chainPath := fmt.Sprintf("%s/keystore/chain/%s/managed/%s.chain", loproxyConfigDir, r.tenant, service)
		c.addFile(udocker.NewTarEntry(chainPath, []byte(cert.Chain), 0400))

		keyPath := fmt.Sprintf("%s/keystore/keys/%s/managed/%s.key", loproxyConfigDir, r.tenant, service)
		c.addFile(udocker.NewTarEntry(keyPath, []byte(cert.Key), 0400))
	}

	requires := proxy.Requires
	if requires != nil {
		sanitized := make([]string, len(requires))
		var hosts []string
		for i, v := range requires {
			off := strings.Index(v, "@")
			if off < 0 {
				sanitized[i] = comm.SanitizeService(v)
			} else {
				serviceName := v[:off]
				hostPort := v[off+1:]
				sanitizedService := comm.SanitizeService(serviceName)
				sanitized[i] = fmt.Sprintf("%s@%s", sanitizedService, hostPort)
				pOff := strings.Index(hostPort, ":")
				if pOff >= 0 {
					hostName := hostPort[:pOff]
					hosts = append(hosts, fmt.Sprintf("%s.%s:%s", sanitizedService, dnsSuffix, hostName))
				}
			}
		}
		context["requires"] = fmt.Sprintf("-requires=%s", strings.Join(sanitized, ","))
		c.hosts = hosts
	}

	provides := proxy.Provides
	if provides != nil {
		args := make([]string, len(provides))
		for i, service := range provides {
			proxyPort := r.proxied[i]
			internalPort := uint16(internalPortStart + i)
			var sanitized string
			off := strings.Index(service, "@")
			if off < 0 {
				sanitized = comm.SanitizeService(service)
			} else {
				serviceName := service[:off]
				location := service[off+1:]
				sanitizedService := comm.SanitizeService(serviceName)
				sanitized = fmt.Sprintf("%s@%s", sanitizedService, location)
			}
			args[i] = fmt.Sprintf("%s;%d;%d", sanitized, internalPort, proxyPort)
			c.addPortMapping(internalPort, proxyPort)
		}
		context["provides"] = fmt.Sprintf("-provides=%s", strings.Join(args, ","))
	}

	// TODO loproxy publish mappings seem to be broken; for now they are omitted

	config, err := r.render("loproxy", context)
	if err != nil {
		return err
	}
	const supervisorConfigPath = "/etc/supervisor/conf.d/loproxy.conf"
	c.addFile(udocker.NewTarEntry(supervisorConfigPath, []byte(config), 0600))

	return nil
}

func (r *dockerRunner) createDockerTarball(c *containerConfig, from, cmd string) (*bytes.Buffer, error) {
	// render Dockerfile
	context := map[string]interface{}{
		"from":   from,
		"hasCmd": false,
	}
	if len(cmd) > 0 {
		context["hasCmd"] = true
		context["cmd"] = cmd
	}
	if c.files != nil {
		context["files"] = "ADD files.tar /"
	}
	if c.scripts != nil {
		invocations := ""
		for _, script := range c.scripts {
			invocations += fmt.Sprintf("RUN (sh -e %s && rm %s)\n", script, script)
		}
		context["scripts"] = invocations
	}
	dockerFile, err := r.render("Dockerfile", context)
	if err != nil {
		return nil, err
	}

	// create Docker tarball for building the image
	tarMap := map[string][]byte{
		"Dockerfile": []byte(dockerFile),
	}
	if c.files != nil {
		// embedding all files as a single tar cuts down on the number of image layers
		buff, err := udocker.TarBuffer(c.files)
		if err != nil {
			return nil, err
		}
		tarMap["files.tar"] = buff.Bytes()
	}
	return udocker.TarBufferFromMap(tarMap)
}

func (r *dockerRunner) build(kill <-chan struct{}, tarReader io.Reader) error {
	s, err := r.parent.client.StreamBuildImage(tarReader, r.jobID, true)
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
		if !msg.InterimProgress() {
			log.Info("Job %s: build output: %s", r.jobID, msg)
		} else if log.IsTraceEnabled() {
			log.Trace("Job %s: build output: %s", r.jobID, msg)
		}

		select {
		case <-kill:
			log.Warn("Job %s was killed during build phase", r.jobID)
			return errJobKilled
		default:
		}
	}
}

func (r *dockerRunner) processVolumes(kill <-chan struct{}, updates chan<- Update, volumes []JobVolume) error {
	// process the volume requests
	mounts, err := ConvertVolumes(kill, r.jobID, volumes, r.parent.mountKeysDir)
	if err != nil {
		return err
	}

	if mounts != nil {
		// add mounts to job context
		r.context.Mounts = mounts
		snapshot := r.context
		updates <- Update{JobID: r.jobID, State: Started, NewContext: &snapshot}

		// initialize all block mounts
		if err := ConfigureHostMounts(kill, r.jobID, mounts); err != nil {
			return err
		}
	}

	return nil
}

func (r *dockerRunner) createContainer(c *containerConfig, payload *JobPayload) (string, error) {
	config := c.generate(r.jobID, r.jobID, payload.Environment, payload.DNS, payload.Limits)

	hc := &config.HostConfig

	exposed := make(map[string]struct{})
	bindings := make(map[string][]udocker.PortBinding)
	for _, mapping := range c.ports {
		key := fmt.Sprintf("%d/tcp", mapping.internal)
		// record exposed port
		exposed[key] = struct{}{}
		// update port binding
		binding := udocker.PortBinding{
			HostIp:   r.parent.serviceAddress,
			HostPort: fmt.Sprintf("%d", mapping.external),
		}
		bindings[key] = append(bindings[key], binding)
	}
	config.ExposedPorts = exposed
	hc.PortBindings = bindings
	log.Info("Job %s: exposed ports=%s, port bindings=%s", r.jobID, config.ExposedPorts, hc.PortBindings)

	if payload.Console {
		config.Tty = true
		config.OpenStdin = true
	}

	// Ensure containers are always restarted, especially after Docker daemon
	// restarts. Using "on-failure" with a maximum failure count would be
	// preferable but unfortunately that policy doesn't handle daemon restarts.
	if !payload.OneShot {
		hc.RestartPolicy.Name = "always"
	}

	if len(payload.AppArmor) > 0 {
		hc.SecurityOpt = []string{fmt.Sprintf("apparmor=%s", payload.AppArmor)}
	}

	resp, err := r.parent.client.CreateContainer(r.jobID, config)
	if err != nil {
		return "", err
	}
	return resp.Id, nil
}
