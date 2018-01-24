package scheduler

import (
	"sync"
	"time"

	"github.com/catalyzeio/paas-orchestration/agent"
)

const (
	jobCleanInterval = 1 * time.Minute
	jobDataTTL       = 1 * time.Hour

	jobQueryInterval = 1 * time.Hour

	jobUpdateQueueSize = 16
)

type AgentJobs struct {
	Jobs agent.JobMap

	Disconnected *time.Time
}

func (a *AgentJobs) Copy() agent.JobMap {
	m := make(agent.JobMap, len(a.Jobs))
	for id, info := range a.Jobs {
		copy := *info
		m[id] = &copy
	}
	return m
}

type JobIndex struct {
	mutex sync.RWMutex
	jobs  map[string]*AgentJobs
}

func NewJobIndex() *JobIndex {
	return &JobIndex{
		jobs: make(map[string]*AgentJobs),
	}
}

func (j *JobIndex) Start() {
	go j.maintenance()
}

func (j *JobIndex) Replace(loc string, jobs agent.JobMap) {
	j.mutex.Lock()
	defer j.mutex.Unlock()

	if jobs == nil {
		// ensure this map exists for subsequent updates
		jobs = make(agent.JobMap)
	}
	j.jobs[loc] = &AgentJobs{jobs, nil}
}

func (j *JobIndex) Update(loc string, msg *agent.Message) {
	j.mutex.Lock()
	defer j.mutex.Unlock()

	jobID := msg.JobID
	if len(jobID) == 0 {
		log.Warn("Job change notification missing job ID")
		return
	}

	jobs := j.jobs[loc]
	if jobs == nil {
		log.Warn("Job change notification referenced unknown location %s", loc)
		return
	}

	if msg.Type == agent.JobRemoved {
		delete(jobs.Jobs, jobID)
		log.Info("Job %s on agent %s was removed", jobID, loc)
		return
	}

	jobInfo := msg.JobInfo
	if jobInfo == nil {
		log.Warn("Job change notification missing job data")
		return
	}

	jobs.Jobs[jobID] = jobInfo
	log.Info(`Job %s on agent %s entered state "%s" at %s`, jobID, loc, jobInfo.State, jobInfo.Updated)
}

func (j *JobIndex) Disconnected(loc string) {
	j.mutex.Lock()
	defer j.mutex.Unlock()

	agentJobs := j.jobs[loc]
	if agentJobs == nil {
		log.Warn("Disconnection notification referenced unknown location %s", loc)
		return
	}

	now := time.Now()
	agentJobs.Disconnected = &now
}

func (j *JobIndex) Snapshot() map[string]agent.JobMap {
	j.mutex.RLock()
	defer j.mutex.RUnlock()

	m := make(map[string]agent.JobMap, len(j.jobs))
	for loc, data := range j.jobs {
		copy := data.Copy()
		if len(copy) > 0 {
			m[loc] = data.Copy()
		}
	}
	return m
}

func (j *JobIndex) FindJob(jobID string) (string, *agent.JobInfo) {
	j.mutex.RLock()
	defer j.mutex.RUnlock()

	for loc, data := range j.jobs {
		if jobInfo := data.Jobs[jobID]; jobInfo != nil {
			copy := *jobInfo
			return loc, &copy
		}
	}

	return "", nil
}

func (j *JobIndex) maintenance() {
	clean := time.NewTicker(jobCleanInterval)
	defer clean.Stop()
	for {
		j.clean()
		<-clean.C
	}
}

func (j *JobIndex) clean() {
	j.mutex.Lock()
	defer j.mutex.Unlock()

	allowance := time.Now().Add(-jobDataTTL)
	for loc, agentJobs := range j.jobs {
		t := agentJobs.Disconnected
		if t != nil && t.Before(allowance) {
			delete(j.jobs, loc)
			log.Info("Cleared job data from disconnected agent %s", loc)
		}
	}
}

type Agent struct {
	Client *agent.Client

	loc string

	updates chan *agent.Message
	cancel  chan struct{}
}

func NewAgent(client *agent.Client) *Agent {
	return &Agent{
		Client: client,

		loc: client.URL,

		updates: make(chan *agent.Message, jobUpdateQueueSize),
		cancel:  make(chan struct{}, 1),
	}
}

func (a *Agent) Start(jobs *JobIndex) {
	c := a.Client
	c.JobUpdateHandler = a.jobUpdated
	c.Start()
	go a.watchJobs(jobs)
}

func (a *Agent) Connected() bool {
	return a.Client.Connected()
}

func (a *Agent) Stop() {
	select {
	case a.cancel <- struct{}{}:
	default:
		// drop cancel request if queue is full
	}
	a.Client.StopClient()
}
func (a *Agent) jobUpdated(msg *agent.Message) {
	a.updates <- msg
}

func (a *Agent) watchJobs(jobs *JobIndex) {
	defer jobs.Disconnected(a.loc)

	a.queryJobs(jobs)
	query := time.NewTicker(jobQueryInterval)
	defer query.Stop()
	for {
		select {
		case <-a.cancel:
			return
		case msg := <-a.updates:
			jobs.Update(a.loc, msg)
		case <-query.C:
			a.queryJobs(jobs)
		}
	}
}

func (a *Agent) queryJobs(jobs *JobIndex) {
	c := a.Client
	if log.IsDebugEnabled() {
		log.Debug("Refreshing jobs at %s", a.loc)
	}
	jobList, err := c.ListJobs()
	if err != nil {
		log.Errorf("Failed to list jobs for agent at %s: %s", a.loc, err)
		return
	}
	jobs.Replace(a.loc, jobList)
}
