package scheduler

import (
	"time"

	"github.com/catalyzeio/go-core/comm"
	"github.com/catalyzeio/paas-orchestration/agent"
)

type JobDetails struct {
	ID   string `json:"id,omitempty"`
	Name string `json:"name"`
	Type string `json:"type"`

	Location string `json:"location,omitempty"`

	State agent.Status `json:"state"`

	Created int64 `json:"created"`
	Updated int64 `json:"updated"`

	LaunchInfo []agent.ServiceLocation `json:"launchInfo"`
}

func NewJobDetails(info *agent.JobInfo, jobID string, location string) *JobDetails {
	return &JobDetails{
		ID:   jobID,
		Name: info.Name,
		Type: info.Type,

		Location: location,

		Created: info.Created.Unix(),
		Updated: info.Updated.Unix(),

		State: info.State,

		LaunchInfo: info.LaunchInfo,
	}
}

type QueueItem struct {
	JobRequest *agent.JobRequest
	Deadline   time.Time
	Backoff    *comm.Backoff
}

func NewQueueItem(jobRequest *agent.JobRequest) *QueueItem {
	return &QueueItem{
		JobRequest: jobRequest,
		Deadline:   time.Now().UTC(),
		Backoff:    comm.NewBackoff(0, queueRetryJitter, queueRetryMax),
	}
}

type JobPriorityQueue []*QueueItem

func (pq JobPriorityQueue) Len() int {
	return len(pq)
}

func (pq JobPriorityQueue) Less(i, j int) bool {
	if pq[i].JobRequest.Priority < pq[j].JobRequest.Priority {
		return false
	} else if pq[i].JobRequest.Priority > pq[j].JobRequest.Priority {
		return true
	}
	return pq[i].Deadline.Before(pq[j].Deadline)
}

func (pq JobPriorityQueue) Swap(i, j int) {
	pq[i], pq[j] = pq[j], pq[i]
}
