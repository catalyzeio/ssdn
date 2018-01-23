package scheduler

import (
	"crypto/tls"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/catalyzeio/paas-orchestration/agent"
	"github.com/catalyzeio/paas-orchestration/registry"
)

const (
	bidTimeout = 5 * time.Second
	numBidders = 4

	queueEmptyDelay  = 5 * time.Second
	queueRetryJitter = 2 * time.Second
	queueRetryMax    = 30 * time.Second

	maxOfferAttempts   = 10
	offerRetryInterval = 500 * time.Millisecond

	requestTimeout = 2 * time.Minute

	registryRetryInterval = 5 * time.Second
)

type Scheduler struct {
	config *tls.Config

	regClient *registry.Client

	agentsMutex sync.RWMutex
	agents      map[string]*Agent

	bids       chan bidRequest
	queueMutex sync.RWMutex
	queue      *JobPriorityQueue

	jobs *JobIndex

	alerts *Alerts
}

type bidResult struct {
	details *JobDetails
	err     error
}

type bidRequest struct {
	job    *agent.JobRequest
	result chan bidResult
}

func NewScheduler(config *tls.Config, regClient *registry.Client) *Scheduler {
	return &Scheduler{
		config: config,

		regClient: regClient,

		agents: make(map[string]*Agent),

		bids:  make(chan bidRequest, numBidders),
		queue: &JobPriorityQueue{},

		jobs: NewJobIndex(),

		alerts: NewAlerts(),
	}
}

func (s *Scheduler) Start() {
	s.jobs.Start()
	go s.watchAgents()
	for i := 0; i < numBidders; i++ {
		go s.bidder()
		go s.queueProcessor()
	}
}

func (s *Scheduler) Alerts() []Alert {
	return s.alerts.Get()
}

func (s *Scheduler) LaunchJob(job *agent.JobRequest) (*JobDetails, error) {
	if err := s.validate(job); err != nil {
		return nil, err
	}
	return s.bidLaunch(job)
}

func (s *Scheduler) ReplaceJob(jobID string, job *agent.JobRequest) (*JobDetails, error) {
	if err := s.validate(job); err != nil {
		return nil, err
	}
	client, err := s.clientForJob(jobID)
	if err != nil {
		return nil, err
	}
	job.Description.ReplaceJobID = &jobID
	jobInfo, err := client.Offer(job)
	if err != nil {
		return nil, err
	}
	if jobInfo == nil {
		return nil, fmt.Errorf("Agent %s rejected replacing job %s with a new job", client.URL, jobID)
	}
	return NewJobDetails(jobInfo, job.ID, client.URL), nil
}

func (s *Scheduler) bidLaunch(job *agent.JobRequest) (*JobDetails, error) {
	result := make(chan bidResult, 1)
	s.bids <- bidRequest{job, result}
	resp := <-result
	if resp.err != nil {
		s.alerts.Add("capacity", resp.err) // XXX may not be a capacity error, but probably is
	}
	return resp.details, resp.err
}

func (s *Scheduler) LaunchCompanionJob(jobID string, job *agent.JobRequest) (*JobDetails, error) {
	if err := s.validate(job); err != nil {
		return nil, err
	}

	client, err := s.clientForJob(jobID)
	if err != nil {
		return nil, err
	}
	info, err := client.Offer(job)
	if err != nil {
		return nil, err
	}
	location := client.URL
	if info == nil {
		err := fmt.Errorf("agent %s rejected offer for job %s (%s/%s)", location, job.ID, job.Type, job.Name)
		s.alerts.Add("capacity", err)
		return nil, err
	}
	return NewJobDetails(info, job.ID, location), nil
}

func (s *Scheduler) EnqueueJob(job *agent.JobRequest) (*JobDetails, error) {
	if err := s.validate(job); err != nil {
		return nil, err
	}

	// TODO stash queue jobs in a durable location, such as an AMQP server
	s.pushQueue(NewQueueItem(job))

	// return placeholder data for launch as job has been queued
	now := time.Now().Unix()
	placeholder := &JobDetails{
		ID:   job.ID,
		Name: job.Name,
		Type: job.Type,

		State: agent.Queued,

		Created: now,
		Updated: now,
	}
	return placeholder, nil
}

// Returns the number of unprocessed jobs.
// Excludes any jobs are currently being placed by queue processors.
func (s *Scheduler) JobQueueLength() int {
	return len(*s.queue)
}

func (s *Scheduler) KillJob(jobID string) error {
	client, err := s.clientForJob(jobID)
	if err != nil {
		return err
	}
	return client.Kill(jobID)
}

func (s *Scheduler) StopJob(jobID string) error {
	client, err := s.clientForJob(jobID)
	if err != nil {
		return err
	}
	return client.Stop(jobID)
}

func (s *Scheduler) StartJob(jobID string) error {
	client, err := s.clientForJob(jobID)
	if err != nil {
		return err
	}
	return client.StartJob(jobID)
}

func (s *Scheduler) PatchJob(jobID string, patch *agent.JobPayload) error {
	client, err := s.clientForJob(jobID)
	if err != nil {
		return err
	}
	return client.Patch(jobID, patch)
}

func (s *Scheduler) ListJob(jobID string) (*JobDetails, error) {
	client, err := s.clientForJob(jobID)
	if err != nil {
		return nil, err
	}
	res, err := client.ListJob(jobID)
	if err != nil {
		return nil, err
	}
	for _, jobInfo := range res {
		return NewJobDetails(jobInfo, jobID, client.URL), nil
	}
	return nil, fmt.Errorf("no job found that matches id %s", jobID)
}

func (s *Scheduler) ListJobs() map[string]*JobDetails {
	result := make(map[string]*JobDetails)
	for location, jobList := range s.jobs.Snapshot() {
		for jobID, jobInfo := range jobList {
			entryJobID := "" // redundant; already present as the map key
			details := NewJobDetails(jobInfo, entryJobID, location)
			result[jobID] = details
		}
	}
	return result

}

func (s *Scheduler) ListAgents() map[string][]string {
	return s.getAllHandlers()
}

func (s *Scheduler) GetUsage() (map[string]*agent.PolicyUsage, error) {
	clients := s.getAllClients()
	n := len(clients)
	pending := make(chan agent.Result, n)
	for _, client := range clients {
		client.GetUsageAsync(pending)
	}

	replies, err := s.collectReplies("getUsage", n, pending, requestTimeout)
	if err != nil {
		return nil, err
	}

	result := make(map[string]*agent.PolicyUsage)
	for k, v := range replies {
		result[k.URL] = v.Usage
	}
	return result, nil
}

func (s *Scheduler) GetMode() (map[string]string, error) {
	clients := s.getAllClients()
	n := len(clients)
	pending := make(chan agent.Result, n)
	for _, client := range clients {
		client.GetModeAsync(pending)
	}

	replies, err := s.collectReplies("getMode", n, pending, requestTimeout)
	if err != nil {
		return nil, err
	}

	result := make(map[string]string)
	for k, v := range replies {
		result[k.URL] = v.Mode
	}
	return result, nil
}

func (s *Scheduler) GetAgentsState() (map[string]*agent.State, error) {
	clients := s.getAllClients()
	n := len(clients)
	pending := make(chan agent.Result, n)
	for _, client := range clients {
		client.GetAgentStateAsync(pending)
	}

	replies, err := s.collectReplies("getAgentState", n, pending, requestTimeout)
	if err != nil {
		return nil, err
	}

	result := make(map[string]*agent.State)
	for k, v := range replies {
		result[k.URL] = &agent.State{PolicyUsage: v.Usage, AgentConstraints: v.Constraints, JobMemoryLimits: v.JobMemoryLimits, Mode: v.Mode}
	}
	return result, nil
}

func (s *Scheduler) SetMode(mode string) error {
	clients := s.getAllClients()
	n := len(clients)
	pending := make(chan agent.Result, n)
	for _, client := range clients {
		client.SetModeAsync(mode, pending)
	}

	return s.checkReplies("setMode", n, pending, requestTimeout)
}

func (s *Scheduler) validate(job *agent.JobRequest) error {
	if err := job.Validate(true); err != nil {
		return err
	}
	// XXX this check isn't perfect, but should be enough to prevent human driver error
	if loc, info := s.jobs.FindJob(job.ID); info != nil {
		return fmt.Errorf("job %s already exists at %s", job.ID, loc)
	}
	return nil
}

func (s *Scheduler) bidder() {
	for {
		req := <-s.bids
		details, err := s.bid(req.job)
		req.result <- bidResult{details, err}
	}
}

func (s *Scheduler) bid(job *agent.JobRequest) (*JobDetails, error) {
	for i := 0; i < maxOfferAttempts; i++ {
		client, err := s.bestBid(job, true)
		if err != nil {
			return nil, err
		}

		if client != nil {
			info, err := client.Offer(job)
			if err != nil {
				return nil, err
			}
			if info != nil {
				location := client.URL
				log.Info("Job %s successfully scheduled at %s", job.ID, location)
				return NewJobDetails(info, job.ID, location), nil
			}
			log.Warn("Agent %s rejected offer for job %s", client.URL, job.ID)
		}

		log.Warn("Failed to schedule job %s at %s; retrying in %s", job.ID, client.URL, offerRetryInterval)
		time.Sleep(offerRetryInterval)
	}
	return nil, fmt.Errorf("failed to schedule job %s at any agent", job.ID)
}

func (s *Scheduler) popQueue() *QueueItem {
	s.queueMutex.Lock()
	defer s.queueMutex.Unlock()
	old := *s.queue
	now := time.Now().UTC()
	for i := range old {
		item := old[i]
		if item.Deadline.Before(now) {
			*s.queue = append(old[0:i], old[i+1:]...)
			return item
		}
	}
	return nil
}

func (s *Scheduler) pushQueue(item *QueueItem) {
	if item == nil {
		return
	}
	s.queueMutex.Lock()
	defer s.queueMutex.Unlock()
	*s.queue = append(*s.queue, item)
	sort.Sort(*s.queue)
}

func (s *Scheduler) queueProcessor() {
	for {
		item := s.popQueue()
		if item == nil {
			time.Sleep(queueEmptyDelay)
			continue
		}
		log.Info("Processing queue job %s", item.JobRequest.ID)
		retry := s.processQueueJob(item.JobRequest)
		if retry {
			item.Backoff.Fail()
			item.Deadline = time.Now().UTC().Add(item.Backoff.Delay)
			log.Warn("No capacity available to start job %s; retrying in %s", item.JobRequest.ID, item.Backoff.Delay)
			s.pushQueue(item)
			time.Sleep(offerRetryInterval)
		}
	}
}

// processQueueJob attempts to offer a job from the queue to a client. This
// returns `true` if the job should be thrown at the back of the queue and tried
// again later or `false` if the job was scheduled or had an unrecoverable error
// (job offering failed).
func (s *Scheduler) processQueueJob(job *agent.JobRequest) bool {
	client, err := s.bestBid(job, false)
	if err != nil {
		log.Errorf("Failed to schedule queue job %s: %s", job.ID, err)
		return false
	}

	if client != nil {
		info, err := client.Offer(job)
		if err != nil {
			log.Errorf("Job offer for job %s to agent %s failed: %s", job.ID, client.URL, err)
			return false
		}
		if info != nil {
			log.Info("Queue job %s successfully scheduled at %s", job.ID, client.URL)
			return false
		}
		log.Warn("Agent %s rejected offer for job %s", client.URL, job.ID)
	}
	return true
}

func (s *Scheduler) bestBid(job *agent.JobRequest, required bool) (*agent.Client, error) {
	clients := s.getClients(job.Type)
	n := len(clients)
	pending := make(chan agent.Result, n)
	for _, client := range clients {
		client.BidAsync(job, pending)
	}

	res, err := s.collectReplies("bid", n, pending, bidTimeout)
	if err != nil {
		return nil, err
	}

	var bestBid float64
	var bestClient *agent.Client
	for client, resp := range res {
		bid := resp.Bid
		if bid != nil {
			bidValue := *bid
			if bestClient == nil || bidValue > bestBid {
				bestBid = bidValue
				bestClient = client
			}
		}
	}

	if bestClient == nil {
		if required {
			return nil, fmt.Errorf("job %s (%s/%s) received no bids", job.ID, job.Type, job.Name)
		}
		return nil, nil
	}
	if log.IsDebugEnabled() {
		log.Debug("Best bid for job %s was %f: %s", job.ID, bestBid, bestClient.URL)
	}
	return bestClient, nil
}

func (s *Scheduler) collectReplies(requestType string, n int, pending <-chan agent.Result, timeout time.Duration) (map[*agent.Client]*agent.Message, error) {
	if n == 0 {
		return nil, nil
	}

	replies := make(map[*agent.Client]*agent.Message)
	successfulRequest := false
	var err error

	t := time.After(timeout)
collectLoop:
	for {
		select {
		case answer := <-pending:
			c := answer.Client
			if answer.Error != nil {
				err = answer.Error
				log.Warn("Request %s to agent %s failed: %s", requestType, c.URL, err)
			} else {
				replies[c] = answer.Message
				successfulRequest = true
			}
			n--
			if n <= 0 {
				break collectLoop
			}
		case <-t:
			log.Warn(`Timeout reached (%s) while waiting for replies to request "%s"`, timeout, requestType)
			break collectLoop
		}
	}

	if successfulRequest {
		// log any errors (above) but don't propagate if there was at least one successful request
		return replies, nil
	}
	return replies, err
}

func (s *Scheduler) checkReplies(requestType string, n int, pending <-chan agent.Result, timeout time.Duration) error {
	if n == 0 {
		return nil
	}

	successfulRequest := false
	var err error

	t := time.After(timeout)
checkLoop:
	for {
		select {
		case answer := <-pending:
			loc := answer.Client.URL
			if answer.Error != nil {
				err = answer.Error
				log.Warn(`Request "%s" to agent %s failed: %s`, requestType, loc, err)
			} else {
				successfulRequest = true
			}
			n--
			if n <= 0 {
				break checkLoop
			}
		case <-t:
			log.Warn(`Timeout reached (%s) while waiting for replies to request "%s"`, timeout, requestType)
			break checkLoop
		}
	}

	if successfulRequest {
		// log any errors (above) but don't propagate if there was at least one successful request
		return nil
	}
	return err
}

func (s *Scheduler) clientForJob(jobID string) (*agent.Client, error) {
	loc, info := s.jobs.FindJob(jobID)
	if info == nil {
		return nil, fmt.Errorf("unknown job %s", jobID)
	}

	client := s.getClient(loc)
	if client == nil {
		return nil, fmt.Errorf("agent %s is offline", loc)
	}

	return client, nil
}

func (s *Scheduler) getClient(loc string) *agent.Client {
	s.agentsMutex.RLock()
	defer s.agentsMutex.RUnlock()

	agent := s.agents[loc]
	if agent != nil {
		return agent.Client
	}
	return nil
}

func (s *Scheduler) getClients(handler string) []*agent.Client {
	s.agentsMutex.RLock()
	defer s.agentsMutex.RUnlock()

	result := make([]*agent.Client, 0, len(s.agents))
	for _, v := range s.agents {
		client := v.Client
		if _, present := client.GetHandlers()[handler]; present {
			result = append(result, client)
		}
	}
	return result
}

func (s *Scheduler) getAllClients() []*agent.Client {
	s.agentsMutex.RLock()
	defer s.agentsMutex.RUnlock()

	result := make([]*agent.Client, 0, len(s.agents))
	for _, v := range s.agents {
		result = append(result, v.Client)
	}
	return result
}

func (s *Scheduler) getAllHandlers() map[string][]string {
	s.agentsMutex.RLock()
	defer s.agentsMutex.RUnlock()

	result := make(map[string][]string)
	for k, v := range s.agents {
		handlers := v.Client.GetHandlers()
		entry := make([]string, 0, len(handlers))
		for h := range handlers {
			entry = append(entry, h)
		}
		result[k] = entry
	}
	return result
}

func (s *Scheduler) watchAgents() {
	regClient := s.regClient
	for {
		// wait for more changes
		<-regClient.Changes

		// get current list of agents
		locs, err := regClient.QueryAll(agent.Service)
		if err != nil {
			log.Warn("Error querying registry: %s", err)
			time.Sleep(registryRetryInterval)
			continue
		}

		// compare with current set of clients
		s.coalesceAgents(locs)
	}
}

func (s *Scheduler) coalesceAgents(locs []registry.WeightedLocation) {
	ads := make(map[string]struct{})
	for _, loc := range locs {
		ads[loc.Location] = struct{}{}
	}

	s.agentsMutex.Lock()
	defer s.agentsMutex.Unlock()

	for loc := range s.agents {
		if _, present := ads[loc]; !present {
			if log.IsDebugEnabled() {
				log.Debug("Removing agent: %s", loc)
			}
			obsolete := s.agents[loc]
			if obsolete.Connected() {
				if log.IsDebugEnabled() {
					log.Debug("Kept live agent: %s", loc)
				}
			} else {
				delete(s.agents, loc)
				obsolete.Stop()
				log.Info("Removed agent: %s", loc)
			}
		}
	}

	for loc := range ads {
		if _, present := s.agents[loc]; !present {
			log.Info("Adding agent: %s", loc)
			client, err := agent.ClientFromURL(s.config, loc)
			if err != nil {
				log.Warn("Could not create client for agent at %s: %s", loc, err)
			} else {
				agent := NewAgent(client)
				agent.Start(s.jobs)
				s.agents[loc] = agent
			}
		}
	}
}
