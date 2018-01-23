package agent

import (
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path"
	"time"

	"github.com/catalyzeio/go-core/udocker"
	"github.com/hoisie/mustache"
	"github.com/streadway/amqp"
)

const (
	// used by the post-receive hook to transition to a state where log output is not hidden
	magicNoDotsString = "/build/builder"

	// max time builds can run
	buildTimeout = 30 * time.Minute
)

type BuildSpawner struct {
	dockerHost   string
	mountKeysDir string
	templatesDir string
	amqpURL      string

	overlays *OverlayManager

	client *udocker.Client
}

func NewBuildSpawner(dockerHost, stateDir, templatesDir, amqpURL string, overlays *OverlayManager) (Spawner, error) {
	client, err := udocker.ClientFromURL(dockerHost)
	if err != nil {
		return nil, err
	}
	mountKeysDir := path.Join(stateDir, "build-mount-keys")
	templatesDir = path.Join(templatesDir, "build")
	return &BuildSpawner{dockerHost, mountKeysDir, templatesDir, amqpURL, overlays, client}, nil
}

func (s *BuildSpawner) Spawn(job *JobRequest, info, replacedJob *JobInfo) (Runner, error) {
	description := info.Description
	if description == nil {
		return nil, fmt.Errorf("build job missing description")
	}
	payload := job.Payload
	template := payload.Template
	if len(template) == 0 {
		return nil, fmt.Errorf("build job missing template")
	}
	registry := payload.Registry

	context := make(map[string]interface{})
	for k, v := range payload.TemplateValues {
		context[k] = v
	}
	if payload.Overlay != nil {
		context["overlay"] = true
	}
	tags := payload.Tags
	if tags == nil || len(tags) == 0 {
		tags = []string{""}
	}

	return &buildRunner{
		parent: s,

		jobID:       job.ID,
		tenant:      description.Tenant,
		tenantToken: payload.TenantToken,

		overlay: payload.Overlay,

		template: template,
		name:     job.Name,
		tags:     tags,
		registry: registry,
		context:  context,

		env:     payload.Environment,
		volumes: payload.Volumes,
		limits:  payload.Limits,
	}, nil
}

func (s *BuildSpawner) Restore(info *JobInfo) (Runner, error) {
	return nil, fmt.Errorf("build jobs cannot be restored")
}

func (s *BuildSpawner) CleanUpPublishPorts(ports []uint16) {}

type buildRunner struct {
	parent *BuildSpawner

	jobID       string
	tenant      string
	tenantToken string

	overlay *JobOverlay

	template string
	name     string
	tags     []string
	registry string
	context  map[string]interface{}

	env     map[string]string
	volumes []JobVolume
	limits  *JobLimits
}

func (r *buildRunner) Run(state Status, s *Signals, updates chan<- Update) (Status, error) {
	log.Info("Job %s: building container %s with tags '%v'", r.jobID, r.name, r.tags)

	// set up build logging output
	var bl *buildLogger
	amqpURL := r.parent.amqpURL
	if len(amqpURL) > 0 {
		var err error
		bl, err = newBuildLogger(amqpURL, r.jobID)
		if err != nil {
			return Failed, err
		}
		defer bl.Close()

		if err := bl.Init(); err != nil {
			return Failed, err
		}
	}

	// kick off job
	status, buildErr := r.startOverlay(s, updates, bl)
	reportedErr := buildErr

	if bl != nil {
		if buildErr != nil {
			// send build failure log message
			if err := bl.SendError(buildErr); err != nil {
				reportedErr = err
			}
		}
		// send final build log message
		const buildFinishedMarker = "You shall not PaaS!"
		if err := bl.SendText(buildFinishedMarker); err != nil {
			reportedErr = err
		}
	}

	// always log original build error
	if buildErr != nil && reportedErr != buildErr {
		log.Warn("Build job failed: %s", buildErr)
	}

	return status, reportedErr
}

func (r *buildRunner) startOverlay(s *Signals, updates chan<- Update, bl *buildLogger) (Status, error) {
	overlays := r.parent.overlays

	// reserve overlay network address for the build
	overlayContext, err := overlays.Reserve(r.jobID, r.tenant, r.tenantToken, r.overlay)
	if err != nil {
		return Failed, err
	}

	// always clean up the overlay network if it was successfully reserved
	defer func() {
		if err := overlays.Stopped(overlayContext, r.jobID, r.tenant); err != nil {
			log.Warn("Job %s: failed to clean up overlay network: %s", r.jobID, err)
		}
	}()

	// start the overlay network
	c := newContainerConfig(r.jobID, r.tenant)
	if err := overlays.Initialize(overlayContext, r.jobID, r.tenant, r.tenantToken, r.overlay, c); err != nil {
		return Failed, err
	}

	// continue the rest of the build
	return r.mountVolumes(s, updates, bl, c)
}

func (r *buildRunner) mountVolumes(s *Signals, updates chan<- Update, bl *buildLogger, c *containerConfig) (Status, error) {
	mountKeysDir := r.parent.mountKeysDir

	// extract mounts from volumes data
	mounts, err := ConvertVolumes(s.KillRequests, r.jobID, r.volumes, mountKeysDir)
	if err != nil {
		return Failed, err
	}

	if mounts != nil {
		c.binds = generateBinds(mounts)
		// configure mounts to persist while job is running
		if err := ConfigureHostMounts(s.KillRequests, r.jobID, mounts); err != nil {
			return Failed, err
		}
		defer CleanUpHostMounts(r.jobID, mounts)
	}

	// continue the rest of the build
	return r.renderDockerfile(s, updates, bl, c)
}

func (r *buildRunner) renderDockerfile(s *Signals, updates chan<- Update, bl *buildLogger, c *containerConfig) (Status, error) {
	client := r.parent.client
	templatesDir := r.parent.templatesDir

	// load template
	templateFile := path.Join(templatesDir, fmt.Sprintf("%s.mustache", r.template))
	template, err := ioutil.ReadFile(templateFile)
	if err != nil {
		return Failed, err
	}
	renderedTemplate := mustache.Render(string(template), r.context)

	// load the build script, if any
	scriptFile := path.Join(templatesDir, fmt.Sprintf("%sScript.mustache", r.template))
	script, err := ioutil.ReadFile(scriptFile)
	if err != nil {
		if !os.IsNotExist(err) {
			return Failed, err
		}
		// ignore if the file does not exist
	}
	var renderedScript string
	if script != nil {
		renderedScript = mustache.Render(string(script), r.context)
	}

	// generate the Docker tar bundle
	files := make(map[string][]byte)
	files["Dockerfile"] = []byte(renderedTemplate)
	if len(renderedScript) > 0 {
		files["script.sh"] = []byte(renderedScript)
	}

	if log.IsTraceEnabled() {
		log.Trace("Generated files: %s", files)
	}

	buf, err := udocker.TarBufferFromMap(files)
	if err != nil {
		return Failed, err
	}

	// kick off the build
	target := r.name
	if r.tags[0] != "" {
		target = fmt.Sprintf("%s:%s", r.name, r.tags[0])
	}

	updates <- Update{JobID: r.jobID, State: Running}
	if err := r.buildImage(s.KillRequests, buf, bl, target); err != nil {
		return Failed, err
	}

	// Run the script (if present) via a run and commit sequence.
	// This is done separately from the build process to take full
	// advantage of features not available during builds, such as volumes
	// and container annotations used by the overlay network.
	if len(renderedScript) > 0 {
		if err := r.runScript(s.KillRequests, bl, target, c); err != nil {
			return Failed, err
		}
	}

	// tag and push if the job included a registry setting
	if len(r.registry) > 0 {
		// tag the image with the destination registry
		dest := fmt.Sprintf("%s/%s", r.registry, r.name)
		for _, tag := range r.tags {
			if err := client.TagImage(r.name, r.tags[0], dest, tag, true); err != nil {
				return Failed, err
			}

			// Push the built image. Pushes cannot be cancelled, so we don't
			// bother to check for job kills from here on out.
			if err := client.PushImage(dest, tag); err != nil {
				return Failed, err
			}
		}
	}

	// done!
	return Finished, nil
}

func (r *buildRunner) buildImage(kill <-chan struct{}, tarReader io.Reader, bl *buildLogger, target string) error {
	client := r.parent.client

	s, err := client.StreamBuildImage(tarReader, target, true)
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
			log.Info("Build %s: %s", r.name, msg)
		} else if log.IsTraceEnabled() {
			log.Trace("Build %s: %s", r.name, msg)
		}

		if bl != nil {
			if err := bl.SendMessage(msg.String()); err != nil {
				return err
			}
		}

		select {
		case <-kill:
			return fmt.Errorf("build job %s was killed", r.name)
		default:
		}
	}
}

func (r *buildRunner) runScript(kill <-chan struct{}, bl *buildLogger, target string, c *containerConfig) error {
	client := r.parent.client

	// record the state of the image so we can restore it later
	imageDetails, err := client.InspectImage(target)
	if err != nil {
		return err
	}

	// create a container from the image
	config := c.generate(r.jobID, target, r.env, nil, r.limits)
	// XXX assumes Dockerfile template places script in /root/script.sh
	config.Cmd = []string{"/bin/sh", "-c", "/bin/bash -e /root/script.sh && rm /root/script.sh"}

	resp, err := client.CreateContainer(r.jobID, config)
	if err != nil {
		return err
	}

	containerID := resp.Id
	log.Info("Build %s: created container %s", r.name, containerID)

	success := false
	defer func() {
		// clean up the build container if the build succeeded
		if success {
			if err := client.DeleteContainer(containerID); err != nil {
				log.Warn("Failed to delete build container %s: %s", containerID, err)
			}
		}
	}()

	// attach and capture the output of the build script
	const outputBufferSize = 16
	messages := make(chan string, outputBufferSize)
	reader, err := client.AttachContainerReader(containerID, false)
	if err != nil {
		return err
	}
	go watchBuildOutput(r.name, reader, messages)

	// start container
	if err := client.StartContainer(containerID, nil); err != nil {
		return err
	}
	stopped := false
	defer func() {
		// make sure the build container is never left in a running state
		if !stopped {
			if err := client.KillContainer(containerID); err != nil {
				log.Warn("Failed to kill build container: %s", err)
			}
		}
	}()

	// forward log output and wait until timeout
	timeout := time.After(buildTimeout)
	for {
		select {
		case <-kill:
			return fmt.Errorf("build job %s was killed", r.name)
		case <-timeout:
			return fmt.Errorf("build for %s exceeded time limit of %s", r.name, buildTimeout)
		case msg, ok := <-messages:
			if !ok {
				messages = nil
				break
			}
			log.Info("Build %s: %s", r.name, msg)
			if bl != nil {
				if err := bl.SendMessage(msg); err != nil {
					return err
				}
			}
		}
		if messages == nil {
			break
		}
	}

	// check container exit status code
	res, err := client.WaitContainer(containerID)
	if err != nil {
		return err
	}
	stopped = true
	if res.StatusCode != 0 {
		return fmt.Errorf("build script returned exit code %d", res.StatusCode)
	}

	// commit the container using the previous settings
	commitDef := &udocker.ContainerDefinition{
		Cmd: imageDetails.Config.Cmd,

		// Docker merges these values with the existing config; we must override
		// them with empty values to get rid of them.
		Labels: map[string]string{
			JobLabel:    "",
			TenantLabel: "",

			OverlayTenantLabel:   "",
			OverlayIPLabel:       "",
			OverlayServicesLabel: "",
		},
	}
	if _, err := client.CommitContainer(r.jobID, r.tags[0], r.name, commitDef); err != nil {
		return err
	}

	// success!
	success = true
	return nil
}

func watchBuildOutput(name string, reader udocker.OutputReader, messages chan<- string) {
	defer func() {
		reader.Close()
		close(messages)
		if log.IsDebugEnabled() {
			log.Debug("Build %s: finished watching build script output", name)
		}
	}()
	if log.IsDebugEnabled() {
		log.Debug("Build %s: watching build script output", name)
	}

	for {
		line, err := reader.Read()
		if err != nil {
			msg := fmt.Sprintf("Failed to retrieve build output: %s", err)
			log.Errorf(msg)
			messages <- msg
			return
		}
		if line == nil {
			return
		}
		messages <- line.Line
	}
}

func (r *buildRunner) Patch(payload *JobPayload) (Status, *JobContext, error) {
	return Failed, nil, fmt.Errorf("build jobs do not support patch requests")
}

func (r *buildRunner) CleanUp(state Status, keepResources bool) error {
	// build jobs do not require additional cleanup
	return nil
}

func (r *buildRunner) Cull(state Status) error {
	// build jobs have no deferred cleanups
	return nil
}

/*
TODO Revamp build logging. The current process is overly complicated
and in some cases may not convey build failures to the post-commit git
hook.
*/

type buildLogger struct {
	conn  *amqp.Connection
	jobID string

	ch *amqp.Channel
}

func newBuildLogger(amqpURL, jobID string) (*buildLogger, error) {
	conn, err := amqp.Dial(amqpURL)
	if err != nil {
		return nil, fmt.Errorf("could not connect to AMQP server: %s", err)
	}
	return &buildLogger{
		conn:  conn,
		jobID: jobID,
	}, nil
}

func (l *buildLogger) Init() error {
	// grab channel
	ch, err := l.conn.Channel()
	if err != nil {
		return err
	}
	l.ch = ch
	// set up exchange and queue
	if err := ch.ExchangeDeclare(l.jobID, "direct", true, true, false, false, nil); err != nil {
		return err
	}
	opts := amqp.Table{
		"x-expires": int32(24 * time.Hour.Seconds() * 1000), // in milliseconds
	}
	if _, err := ch.QueueDeclare(l.jobID, true, true, false, false, opts); err != nil {
		return err
	}
	if err := ch.QueueBind(l.jobID, "", l.jobID, false, nil); err != nil {
		return err
	}
	return nil
}

type BuildLogMessage struct {
	Stream string `json:"stream"`
}

func (l *buildLogger) SendMessage(message string) error {
	logMessage := BuildLogMessage{Stream: message}
	data, err := json.Marshal(&logMessage)
	if err != nil {
		return err
	}
	return l.publish(string(data))
}

func (l *buildLogger) SendText(msg string) error {
	return l.publish(msg)
}

func (l *buildLogger) SendError(buildErr error) error {
	// ensure hook is in a state where it is examining build output
	magicMsg := fmt.Sprintf("%s: could not build application", magicNoDotsString)
	if err := l.SendText(magicMsg); err != nil {
		return err
	}
	// send along build failure message
	msg := fmt.Sprintf("Build job failed: %s", buildErr.Error())
	return l.SendText(msg)
}

func (l *buildLogger) publish(body string) error {
	return l.ch.Publish(l.jobID, "", false, false, amqp.Publishing{
		ContentType:     "application/data",
		ContentEncoding: "binary",
		Body:            []byte(body),
	})
}

func (l *buildLogger) Close() {
	if l.ch != nil {
		if err := l.ch.Close(); err != nil {
			log.Warn("Failed to close AMQP channel: %s", err)
		}
		l.ch = nil
	}
	if err := l.conn.Close(); err != nil {
		log.Warn("Failed to close AMQP connection: %s", err)
	}
}
