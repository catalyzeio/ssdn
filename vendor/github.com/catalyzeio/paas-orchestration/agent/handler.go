package agent

import (
	"fmt"
	"time"
)

type Runner interface {
	Run(state Status, s *Signals, updates chan<- Update) (Status, error)
	Patch(payload *JobPayload) (Status, *JobContext, error)
	CleanUp(state Status, keepResources bool) error
	Cull(state Status) error
}

type Spawner interface {
	Spawn(job *JobRequest, info, replacedJob *JobInfo) (Runner, error)
	Restore(info *JobInfo) (Runner, error)
	CleanUpPublishPorts([]uint16)
}

type JobAffinity struct {
	PrefersCount  int
	DespisesCount int
}

const (
	replaceStopTimeout = 1 * time.Minute
	replaceRunTimeout  = 3 * time.Minute
)

type Handler struct {
	ac      *AgentConstraints
	spawner Spawner

	listener *Listener

	tenants map[string]*TenantServices
	jobs    map[string]*Signals

	handlerType string
}

func NewHandler(ac *AgentConstraints, spawner Spawner, handlerType string) *Handler {
	return &Handler{
		ac:      ac,
		spawner: spawner,

		tenants: make(map[string]*TenantServices),
		jobs:    make(map[string]*Signals),

		handlerType: handlerType,
	}
}

func (h *Handler) SetParent(listener *Listener) {
	h.listener = listener
}

func (h *Handler) Affinity(job *JobRequest, removedConstraints *JobDescription) *JobAffinity {
	prefersCount := 0
	despisesCount := 0

	description := job.Description
	requires := NewStringBag(description.Requires)
	provides := NewStringBag(description.Provides)
	conflicts := NewStringBag(description.Conflicts)

	// check agent-level hard placement constraints
	if !h.ac.Permitted(requires, provides, conflicts) {
		return nil
	}
	// update agent-level counts for matching soft constraints
	prefersCount += h.ac.Matches(description.Prefers)
	despisesCount += h.ac.Matches(description.Despises)

	// check tenant-level constraints
	services := h.tenants[description.Tenant]
	if services != nil {
		// check hard placement constraints
		if !services.Permitted(provides, conflicts, removedConstraints) {
			return nil
		}
		// update counts for matching soft constraints
		prefersCount += services.Matches(description.Prefers)
		despisesCount += services.Matches(description.Despises)
	}

	return &JobAffinity{prefersCount, despisesCount}
}

func (h *Handler) Active(jobInfo *JobInfo) {
	description := jobInfo.Description
	tenant := description.Tenant
	services := h.tenants[tenant]

	if services == nil {
		services = NewTenantServices()
		h.tenants[tenant] = services
	}

	services.AddProvides(description.Provides)
	services.AddConflicts(description.Conflicts)

	if log.IsDebugEnabled() {
		log.Debug("Services for tenant %s: %+v", tenant, services)
	}
}

func (h *Handler) Inactive(jobInfo *JobInfo) {
	description := jobInfo.Description
	tenant := description.Tenant
	services := h.tenants[tenant]

	if services != nil {
		services.RemoveProvides(description.Provides)
		services.RemoveConflicts(description.Conflicts)
		if services.Empty() {
			delete(h.tenants, tenant)
		}
	}

	if log.IsDebugEnabled() {
		log.Debug("Services for tenant %s: %+v", tenant, services)
	}
}

func (h *Handler) Start(job *JobRequest, info, replacedJob *JobInfo) error {
	jobID := info.ID
	_, present := h.jobs[jobID]
	if present {
		return fmt.Errorf("job %s is already registered with this handler", jobID)
	}

	r, err := h.spawner.Spawn(job, info, replacedJob)
	if err != nil {
		return err
	}

	s := NewSignals()
	h.jobs[jobID] = s
	state := Started
	updates := h.listener.Changes

	if replacedJob != nil {
		oldSignals, err := h.getJobSignals(replacedJob)
		if err != nil {
			return err
		}

		s.KeepResources()

		state = Waiting
		updates <- Update{JobID: jobID, State: state}

		go h.superviseJobReplacement(
			replacedJob.ID, jobID,
			oldSignals, s,
			removedJobPorts(replacedJob.LaunchInfo, info.LaunchInfo))
	}

	go supervise(state, jobID, r, s, updates)
	return nil
}

func (h *Handler) Restore(info *JobInfo) error {
	jobID := info.ID
	_, present := h.jobs[jobID]
	if present {
		return fmt.Errorf("job %s is already registered with this handler", jobID)
	}

	r, err := h.spawner.Restore(info)
	if err != nil {
		return err
	}

	s := NewSignals()
	h.jobs[jobID] = s
	go supervise(info.State, jobID, r, s, h.listener.Changes)
	return nil
}

func (h *Handler) Patch(info *JobInfo, resp *Message) error {
	s, err := h.getJobSignals(info)
	if err != nil {
		return err
	}

	return s.Patch(resp)
}

func (h *Handler) Kill(info *JobInfo) error {
	s, err := h.getJobSignals(info)
	if err != nil {
		return err
	}

	s.Kill()
	return nil
}

func (h *Handler) Stop(info *JobInfo) error {
	s, err := h.getJobSignals(info)
	if err != nil {
		return err
	}

	s.Stop()
	return nil
}

func (h *Handler) Cull(info *JobInfo) error {
	s, err := h.getJobSignals(info)
	if err != nil {
		return err
	}

	s.Cull()
	delete(h.jobs, info.ID)
	return nil
}

func (h *Handler) Restart(info *JobInfo) error {
	s, err := h.getJobSignals(info)
	if err != nil {
		return err
	}

	s.Start()
	return nil
}

func (h *Handler) SupportsReplacement(jobID string) bool {
	if h.handlerType != "docker" {
		return false
	}
	_, present := h.jobs[jobID]
	return present
}

func (h *Handler) getJobSignals(info *JobInfo) (*Signals, error) {
	jobID := info.ID
	s, present := h.jobs[jobID]
	if !present {
		return nil, fmt.Errorf("job %s is not registered with this handler", jobID)
	}
	if s == nil {
		return nil, fmt.Errorf("job %s is not active", jobID)
	}
	return s, nil
}

func (h *Handler) superviseJobReplacement(oldID, newID string, oldSignals, newSignals *Signals, removedPorts []uint16) {
	if err := h.replaceJob(oldID, newID, oldSignals, newSignals, removedPorts); err != nil {
		log.Warn("Failed to replace job %s with %s: %s", oldID, newID, err)

		log.Info("Killing failed replacement job %s", newID)
		newSignals.Kill()
	}
}

func (h *Handler) replaceJob(oldID, newID string, oldSignals, newSignals *Signals, removedPorts []uint16) error {
	// stop the old job and wait for it to enter the stopped state
	if err := h.signalJob(oldID, oldSignals.Stop, Stopped, replaceStopTimeout); err != nil {
		return err
	}

	// wait for the new job to start and run
	if err := h.signalJob(newID, newSignals.Start, Running, replaceRunTimeout); err != nil {
		// new job failed to run; give up and restart the old job
		oldSignals.Start()
		return err
	}

	// new job is running successfully; update cleanup logic and kill the old job
	oldSignals.KeepResources()
	newSignals.CleanUpResources()
	h.spawner.CleanUpPublishPorts(removedPorts)
	oldSignals.Kill()

	log.Info("Replaced job %s with %s", oldID, newID)
	return nil
}

func (h *Handler) signalJob(jobID string, signal func(), state Status, timeout time.Duration) error {
	listener := h.listener

	changes := make(chan interface{}, commQueueSize)
	watcher := NewWatcher(changes)
	listener.addWatcher(watcher)
	defer listener.removeWatcher(watcher)

	current := listener.lastNotifiedStatus(jobID)
	if current.Terminal() {
		return fmt.Errorf("job %s entered state %s instead of %s", jobID, current, state)
	}
	if current == state {
		return nil
	}

	signal()
	return waitForJobState(changes, jobID, state, timeout)
}

func waitForJobState(changes chan interface{}, jobID string, state Status, timeout time.Duration) error {
	deadline := time.After(timeout)
	for {
		select {
		case <-deadline:
			return fmt.Errorf("timed out while waiting for job %s to enter state %s", jobID, state)
		case v := <-changes:
			msg := v.(*Message)
			if msg.Type == JobChanged && msg.JobID == jobID {
				current := msg.JobInfo.State
				if current.Terminal() {
					return fmt.Errorf("job %s entered state %s instead of %s", jobID, current, state)
				}
				if current == state {
					return nil
				}
			}
		}
	}
}

// Returns a list of all old ports that are not present in the new ports set.
func removedJobPorts(oldLocations, newLocations []ServiceLocation) []uint16 {
	var ports []uint16
	newPorts := make(map[uint16]struct{})
	for _, newLoc := range newLocations {
		newPorts[newLoc.Port] = struct{}{}
	}
	for _, oldLoc := range oldLocations {
		if _, present := newPorts[oldLoc.Port]; !present {
			ports = append(ports, oldLoc.Port)
		}
	}
	return ports
}

func supervise(state Status, jobID string, r Runner, s *Signals, updates chan<- Update) {
	if !state.Terminal() {
		// start/resume job
		for !state.Terminal() {
			var err error
			var ctx *JobContext
			if state == Waiting {
				// wait for patch, kill, or start signal
				select {
				case <-s.KillRequests:
					state = Killed
				case msg := <-s.PatchRequests:
					state, ctx, err = r.Patch(msg.Patch)
					if msg.respChan == nil {
						log.Warn("Job %s: received a patch request with an empty response channel", jobID)
					} else {
						msg.respChan <- msg
					}
				case <-s.StartRequests:
					state = Started
				}
			} else {
				// tell runner to start job
				state, err = r.Run(state, s, updates)
			}
			if err != nil {
				state = Failed
				log.Warn("Job %s failed: %s", jobID, err)
			}
			updates <- Update{JobID: jobID, State: state, NewContext: ctx}
		}
		// clean up job
		log.Info("Cleaning up job %s", jobID)
		if err := r.CleanUp(state, s.ShouldKeepResources()); err != nil {
			log.Warn("Failed to clean up job %s: %s", jobID, err)
		}
	}

	// cull job once the cull signal is sent
	<-s.CullRequests
	if err := r.Cull(state); err != nil {
		log.Warn("Failed to cull job %s: %s", jobID, err)
	}
}
