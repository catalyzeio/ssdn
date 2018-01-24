package agent

import (
	"bufio"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"path"
	"path/filepath"
	"sync"
	"time"

	"github.com/catalyzeio/go-core/comm"
)

type Mode int

const (
	Normal Mode = iota
	Maintenance
	Locked
	Unknown

	JobChanged = "jobChanged"
	JobRemoved = "jobRemoved"
)

var modeValues = []string{
	Normal:      "normal",
	Maintenance: "maintenance",
	Locked:      "locked",
	Unknown:     "unknown",
}

func (m Mode) String() string {
	if m < 0 || int(m) >= len(modeValues) {
		return "unknown"
	}
	return modeValues[m]
}

const (
	DefaultPort = 7433

	Tenant  = "orchestration"
	Service = "agent"

	StateFileName       = "agent.json"
	MaintenanceFileName = ".maintenance"

	delimiter   = '\n'
	idleTimeout = 2 * time.Minute

	cullInterval     = 1 * time.Minute
	terminatedJobTTL = 5 * time.Minute
	saveInterval     = 15 * time.Second

	requestQueueSize = 64
	commQueueSize    = 16
)

type Update struct {
	JobID string
	State Status

	NewContext *JobContext
}

type Listener struct {
	Changes chan<- Update

	address *comm.Address
	config  *tls.Config

	policy   Policy
	handlers map[string]*Handler

	stateFile string
	jobs      JobMap
	unsaved   bool

	maintenanceFile string
	dsDir           string
	dsPath          string

	requests chan request
	updates  chan Update

	mutex      sync.Mutex
	watchers   map[*Watcher]struct{}
	lastStatus map[string]Status

	mode Mode

	ac *AgentConstraints
}

type request struct {
	msg *Message
	out chan<- interface{}
}

func NewListener(stateDir string, address *comm.Address, config *tls.Config, policy Policy, handlers map[string]*Handler, ac *AgentConstraints) (*Listener, error) {
	absPath, err := filepath.Abs(path.Join(stateDir, "agent.sock"))
	if err != nil {
		return nil, err
	}
	updates := make(chan Update, requestQueueSize)
	l := &Listener{
		Changes: updates,

		address: address,
		config:  config,

		policy:   policy,
		handlers: handlers,

		stateFile: path.Join(stateDir, StateFileName),
		jobs:      make(JobMap),

		maintenanceFile: path.Join(stateDir, MaintenanceFileName),
		dsDir:           stateDir,
		dsPath:          absPath,

		requests: make(chan request, requestQueueSize),
		updates:  updates,

		watchers:   make(map[*Watcher]struct{}),
		lastStatus: make(map[string]Status),

		ac: ac,
	}
	for _, v := range handlers {
		v.SetParent(l)
	}
	return l, nil
}

func (l *Listener) Listen() error {
	if err := l.restoreState(); err != nil {
		return fmt.Errorf("failed to restore agent state: %s", err)
	}

	go l.process()

	listener, err := l.address.Listen(l.config)
	if err != nil {
		return err
	}
	go l.accept(listener)

	// setup the domain socket
	if err = os.MkdirAll(l.dsDir, 0700); err != nil {
		return err
	}
	dsListener, err := comm.DomainSocketListener(l.dsPath)
	if err != nil {
		return err
	}
	go l.accept(dsListener)

	return nil
}

func (l *Listener) accept(listener net.Listener) {
	defer listener.Close()

	log.Info("Listening on %s", listener.Addr())
	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Warn("Failed to accept incoming connection: %s", err)
			return
		}
		go l.service(conn)
	}
}

func (l *Listener) service(conn net.Conn) {
	remoteAddr := conn.RemoteAddr()
	defer func() {
		conn.Close()
		log.Info("Client disconnected: %s", remoteAddr)
	}()

	log.Info("Inbound connection: %s", remoteAddr)

	if err := l.handle(conn, remoteAddr); err != nil {
		if err != io.EOF {
			log.Warn("Failed to handle inbound connection %s: %s", remoteAddr, err)
		}
	}
}

func (l *Listener) handle(conn net.Conn, remoteAddr net.Addr) error {
	io := comm.WrapO(conn, messageWriter, commQueueSize, idleTimeout)
	defer io.Stop()

	out, done := io.Out, io.Done

	handshake := false

	reader := newMessageReader(conn)
	for {
		msg, err := reader(conn)
		if err != nil {
			return err
		}

		if !handshake {
			if msg.Type == "hello" {
				resp := &Message{ID: msg.ID, Type: "response", Handlers: l.listHandlers()}
				select {
				case <-done:
					return nil
				case out <- resp:
					handshake = true
					watcher := NewWatcher(out)
					l.addWatcher(watcher)
					defer l.removeWatcher(watcher)
					continue
				}
			}
			return fmt.Errorf("unexpected request type %s", msg.Type)
		}
		if msg.Type == "ping" {
			resp := Message{ID: msg.ID, Type: "response"}
			select {
			case <-done:
				return nil
			case out <- resp:
				handshake = true
				continue
			}
		}

		select {
		case <-done:
			return nil
		case l.requests <- request{msg, out}:
		}
	}
}

func (l *Listener) listHandlers() []string {
	var list []string
	for k := range l.handlers {
		list = append(list, k)
	}
	return list
}

func (l *Listener) process() {
	l.cull()
	cull := time.NewTicker(cullInterval)
	defer cull.Stop()
	save := time.NewTicker(saveInterval)
	defer save.Stop()
	for {
		select {
		case <-cull.C:
			l.cull()
		case <-save.C:
			if l.unsaved {
				l.writeState()
			}
		case update := <-l.updates:
			l.processUpdate(update)
		case req := <-l.requests:
			l.processRequest(req)
		}
	}
}

func (l *Listener) cull() {
	dirty := false

	allowance := time.Now().Add(-terminatedJobTTL)
	for jobID, job := range l.jobs {
		if job.State.Terminal() && job.Updated.Before(allowance) {
			delete(l.jobs, jobID)
			log.Info("Culling job %s", jobID)
			if err := job.handler.Cull(job); err != nil {
				log.Warn("Failed to cull job %s: %s", jobID, err)
			}
			l.notifyWatchers(JobRemoved, jobID, nil)
			dirty = true
		}
	}

	if dirty {
		l.writeState()
	}
}

func (l *Listener) processUpdate(update Update) {
	jobID, state := update.JobID, update.State

	job := l.jobs[jobID]
	if job == nil {
		log.Errorf("Internal error: missing information for job %s", jobID)
		return
	}

	changed := false
	notify := false

	if update.NewContext != nil {
		log.Info("Job %s updated context", jobID)
		// update job's recorded context
		job.Context = update.NewContext
		// schedule updates
		changed = true
	}

	old := job.State
	if state != old {
		log.Info("Job %s entered state %s", jobID, state)
		// update job's recorded state
		job.State = state
		// handle transitions between active and inactive states
		if !old.Active() && state.Active() {
			l.active(job)
		} else if old.Active() && !state.Active() {
			l.inactive(job)
		}
		// schedule updates
		changed = true
		notify = true
	}

	if changed {
		// update job modification time
		job.Updated = time.Now()
		// update saved data
		l.writeState()
	}

	if notify {
		// fire updates
		l.notifyWatchers(JobChanged, jobID, job)
	}
}

func (l *Listener) processRequest(req request) {
	msg, out := req.msg, req.out

	resp := &Message{ID: msg.ID, Type: "response"}
	msg.respChan = out
	switch msg.Type {
	case "bid":
		l.bid(msg, resp)
	case "offer":
		l.offer(msg, resp)
	case "kill":
		l.kill(msg, resp)
	case "stop":
		l.stop(msg, resp)
	case "start":
		l.start(msg, resp)
	case "patch":
		err := l.patch(msg)
		if err == nil {
			return
		}
		resp.SetError(err.Error())
	case "listJob":
		l.listJob(msg, resp)
	case "listJobs":
		l.listJobs(msg, resp)
	case "getUsage":
		l.getUsage(msg, resp)
	case "getMode":
		l.getMode(msg, resp)
	case "setMode":
		l.setMode(msg, resp)
	case "getAgentConstraints":
		l.getConstraints(msg, resp)
	case "getAgentState":
		l.getUsage(msg, resp)
		l.getMode(msg, resp)
		l.getConstraints(msg, resp)
		l.getMemoryLimits(msg, resp)
	default:
		resp.SetError(fmt.Sprintf("unexpected request type %s", msg.Type))
	}

	out <- resp
}

func (l *Listener) bid(msg *Message, resp *Message) {
	_, bid, err := l.generateBid(msg, false)
	if err != nil {
		resp.SetError(fmt.Sprintf("invalid bid request: %s", err))
		return
	}
	resp.Bid = bid
}

func (l *Listener) offer(msg *Message, resp *Message) {
	var prevJob *JobInfo
	job := msg.Job
	if job.Description.ReplaceJobID != nil {
		var ok bool
		if prevJob, ok = l.jobs[*job.Description.ReplaceJobID]; !ok {
			resp.SetError(fmt.Sprintf("invalid offer: replace job %s does not reside on this agent", *job.Description.ReplaceJobID))
			return
		}
	}
	// re-bid to verify job can still run here
	handler, bid, err := l.generateBid(msg, true)
	if err != nil {
		resp.SetError(fmt.Sprintf("invalid offer: %s", err))
		return
	}
	resp.Bid = bid
	if bid == nil {
		// job cannot run here right now, regardless of any previous bids
		return
	}

	jobInfo := NewJobInfo(job, Scheduled, handler)
	if err := handler.Start(job, jobInfo, prevJob); err != nil {
		resp.SetError(fmt.Sprintf("could not start job: %s", err))
		return
	}
	l.jobs[job.ID] = jobInfo
	resp.Info = sanitizeJobInfo(jobInfo)
	l.processUpdate(Update{JobID: job.ID, State: Started})
}

func (l *Listener) generateBid(msg *Message, checkPayload bool) (*Handler, *float64, error) {
	// validate submitted job
	job := msg.Job
	if err := job.Validate(checkPayload); err != nil {
		return nil, nil, err
	}
	// make sure job isn't already here
	id := job.ID
	if _, present := l.jobs[id]; present {
		return nil, nil, fmt.Errorf("job %s already present on this agent", id)
	}
	// make sure we can bid for this job
	if l.currentMode() != Normal {
		if log.IsDebugEnabled() {
			log.Debug("Bidding disabled; will not bid for job %s", id)
		}
		return nil, nil, nil
	}
	jobType := job.Type
	handler := l.handlers[jobType]
	if handler == nil {
		if log.IsDebugEnabled() {
			log.Debug("No handler for jobs of type %s on this node; cannot bid", jobType)
		}
		return nil, nil, nil
	}
	var replacedJob *JobInfo
	var replacedJobDesc *JobDescription
	if job.Description.ReplaceJobID != nil {
		replaceJobID := *job.Description.ReplaceJobID
		if handler.SupportsReplacement(replaceJobID) {
			if rJob, ok := l.jobs[replaceJobID]; ok {
				replacedJob = rJob
				replacedJobDesc = rJob.Description
			}
		} else {
			if log.IsDebugEnabled() {
				log.Debug("Job %s cannot replace job %s; %s handler does not support replacements or is not the handler for job %s", id, replaceJobID, jobType, replaceJobID)
			}
			return nil, nil, nil
		}
	}
	// run bid request by handler first
	affinity := handler.Affinity(job, replacedJobDesc)
	if affinity == nil {
		if log.IsDebugEnabled() {
			log.Debug("Job %s cannot run here due to conflict", id)
		}
		return nil, nil, nil
	}
	// finally use the resource policy to generate a numeric bid value
	bidValue := l.policy.Bid(id, job.Description, affinity, replacedJob)
	if log.IsDebugEnabled() {
		if bidValue != nil {
			log.Debug("Bid for job %s: %f", id, *bidValue)
		} else {
			log.Debug("Bid for job %s: <none>", id)
		}
	}
	return handler, bidValue, nil
}

func (l *Listener) kill(msg *Message, resp *Message) {
	jobID := msg.JobID
	info, present := l.jobs[jobID]
	if !present {
		resp.SetError(fmt.Sprintf("job %s not present on this node", jobID))
		return
	}
	if err := info.handler.Kill(info); err != nil {
		resp.SetError(fmt.Sprintf("failed to kill job %s: %s", jobID, err))
	}
}

func (l *Listener) stop(msg *Message, resp *Message) {
	jobID := msg.JobID
	info, present := l.jobs[jobID]
	if !present {
		resp.SetError(fmt.Sprintf("job %s not present on this node", jobID))
		return
	}
	if err := info.handler.Stop(info); err != nil {
		resp.SetError(fmt.Sprintf("failed to stop job %s: %s", jobID, err))
	}
}

func (l *Listener) start(msg *Message, resp *Message) {
	jobID := msg.JobID
	info, present := l.jobs[jobID]
	if !present {
		resp.SetError(fmt.Sprintf("job %s not present on this node", jobID))
		return
	}
	if err := info.handler.Restart(info); err != nil {
		resp.SetError(fmt.Sprintf("failed to start job %s: %s", jobID, err))
	}
}

func (l *Listener) patch(msg *Message) error {
	jobID := msg.JobID
	info, present := l.jobs[jobID]
	if !present {
		return fmt.Errorf("job %s not present on this node", jobID)
	}
	if err := info.handler.Patch(info, msg); err != nil {
		return fmt.Errorf("failed to patch job %s: %s", jobID, err)
	}
	return nil
}

func (l *Listener) listJob(msg *Message, resp *Message) {
	result := make(JobMap)
	jobID := msg.JobID
	result[jobID] = sanitizeJobInfo(l.jobs[jobID])
	resp.Jobs = result
}

func (l *Listener) listJobs(msg *Message, resp *Message) {
	result := make(JobMap)
	for k, v := range l.jobs {
		result[k] = sanitizeJobInfo(v)
	}
	resp.Jobs = result
}

func (l *Listener) getUsage(msg *Message, resp *Message) {
	used, available := l.policy.Utilization()
	resp.Usage = &PolicyUsage{used, available, l.listHandlers()}
}

func (l *Listener) getMode(msg *Message, resp *Message) {
	resp.Mode = l.currentMode().String()
}

func (l *Listener) getConstraints(msg *Message, resp *Message) {
	resp.Constraints = l.ac
}

func (l *Listener) getMemoryLimits(msg *Message, resp *Message) {
	memPolicy, ok := l.policy.(*MemoryPolicy)
	if ok {
		resp.JobMemoryLimits = &JobMemoryLimits{float64(memPolicy.minSize), float64(memPolicy.maxSize)}
	}
}

func (l *Listener) setMode(msg *Message, resp *Message) {
	switch msg.Mode {
	case "maintenance":
		l.mode = Maintenance
	case "normal":
		l.mode = Normal
	default:
		// other modes cannot be set directly
		resp.SetError(fmt.Sprintf("unsupported mode: %s", msg.Mode))
	}
}

func (l *Listener) addWatcher(watcher *Watcher) {
	l.mutex.Lock()
	defer l.mutex.Unlock()

	l.watchers[watcher] = struct{}{}
}

func (l *Listener) removeWatcher(watcher *Watcher) {
	// Convenience done on behalf of the caller.
	// Must be done while the mutex is *not* locked.
	watcher.Done()

	l.mutex.Lock()
	defer l.mutex.Unlock()

	delete(l.watchers, watcher)
}

func (l *Listener) notifyWatchers(messageType, jobID string, jobInfo *JobInfo) {
	l.mutex.Lock()
	defer l.mutex.Unlock()

	switch messageType {
	case JobChanged:
		l.lastStatus[jobID] = jobInfo.State
	case JobRemoved:
		delete(l.lastStatus, jobID)
	}

	msg := &Message{
		Type:    messageType,
		JobID:   jobID,
		JobInfo: sanitizeJobInfo(jobInfo),
	}
	for watcher := range l.watchers {
		if !watcher.Notify(msg) {
			delete(l.watchers, watcher)
		}
	}
}

func (l *Listener) lastNotifiedStatus(jobID string) Status {
	l.mutex.Lock()
	defer l.mutex.Unlock()

	status, present := l.lastStatus[jobID]
	if !present {
		status = Disappeared
	}
	return status
}

func (l *Listener) currentMode() Mode {
	mode, err := l.determineMode()
	if err != nil {
		log.Warn("Maintenance mode check failed: %s", err)
	}
	return mode
}

func (l *Listener) determineMode() (Mode, error) {
	// the maintenance lock file trumps any other setting
	exists, err := pathExists(l.maintenanceFile)
	if err != nil {
		return Unknown, err
	} else if exists {
		return Locked, nil
	}
	// use current setting
	return l.mode, nil
}

func (l *Listener) restoreState() error {
	// load saved JSON
	file, err := os.Open(l.stateFile)
	if os.IsNotExist(err) {
		log.Info("State file '%s' not found; assuming fresh start", l.stateFile)
		return nil
	}
	if err != nil {
		return err
	}
	defer file.Close()
	if err := json.NewDecoder(file).Decode(&l.jobs); err != nil {
		return err
	}

	// patch up deserialized jobs with missing data
	for jobID, job := range l.jobs {
		job.ID = jobID

		// refuse to start if handler is not available for saved job
		handler := l.handlers[job.Type]
		if handler == nil {
			return fmt.Errorf("saved state references missing handler %s", job.Type)
		}
		job.handler = handler

		// restore handler/supervisor job state
		if err := handler.Restore(job); err != nil {
			log.Errorf("Failed to restore data for job %s: %s", jobID, err)
			// exclude broken jobs
			delete(l.jobs, jobID)
			continue
		}

		// resume active status for the job
		if job.State.Active() {
			l.active(job)
		}

		l.lastStatus[jobID] = job.State

		if log.IsDebugEnabled() {
			log.Debug("Restored state for job %s", jobID)
		}
	}

	log.Info("Restored state from %s", l.stateFile)
	return nil
}

func (l *Listener) active(jobInfo *JobInfo) {
	l.policy.Active(jobInfo)
	jobInfo.handler.Active(jobInfo)
}

func (l *Listener) inactive(jobInfo *JobInfo) {
	l.policy.Inactive(jobInfo)
	jobInfo.handler.Inactive(jobInfo)
}

func (l *Listener) writeState() {
	l.unsaved = true

	tempFile := l.stateFile + ".new"
	file, err := os.OpenFile(tempFile, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		log.Errorf("Failed to create new state file: %s", err)
		return
	}
	defer file.Close()

	if err := json.NewEncoder(file).Encode(l.jobs); err != nil {
		log.Errorf("Failed to serialize state file: %s", err)
		return
	}
	if log.IsDebugEnabled() {
		log.Debug("Wrote state file to %s", tempFile)
	}

	if err := os.Rename(tempFile, l.stateFile); err != nil {
		log.Errorf("Failed to rename state file: %s", err)
		return
	}

	l.unsaved = false
	log.Info("Updated state file %s", l.stateFile)
}

func sanitizeJobInfo(jobInfo *JobInfo) *JobInfo {
	if jobInfo == nil {
		return nil
	}
	// return an abbreviated snapshot of the current value
	copy := *jobInfo
	copy.Description = nil
	copy.Context = nil
	return &copy
}

func newMessageReader(conn net.Conn) func(conn net.Conn) (*Message, error) {
	r := bufio.NewReader(conn)
	return func(conn net.Conn) (*Message, error) {
		if err := conn.SetReadDeadline(time.Now().Add(idleTimeout)); err != nil {
			return nil, err
		}
		data, err := r.ReadBytes(delimiter)
		if err != nil {
			return nil, err
		}
		if log.IsTraceEnabled() {
			log.Trace("<- %s", data)
		}
		msg := &Message{}
		if err := json.Unmarshal(data, msg); err != nil {
			return nil, err
		}
		return msg, nil
	}
}

func messageWriter(conn net.Conn, msg interface{}) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	if log.IsTraceEnabled() {
		log.Trace("-> %s", data)
	}
	_, err = conn.Write(append(data, delimiter))
	return err
}
