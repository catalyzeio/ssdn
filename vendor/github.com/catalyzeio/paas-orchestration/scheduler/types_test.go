package scheduler

import (
	"fmt"
	"math/rand"
	"testing"
	"time"

	"github.com/catalyzeio/go-core/comm"
	"github.com/catalyzeio/paas-orchestration/agent"
)

// makes sure the priority queue gives back all elements that were pushed
func TestPriorityQueuePop(t *testing.T) {
	s := NewScheduler(nil, nil)
	expected := 10
	for i := 0; i < expected; i++ {
		job := &agent.JobRequest{ID: fmt.Sprintf("job%d", i), Type: "build", Name: "bob", Priority: 0, Description: nil, Payload: nil}
		s.pushQueue(&QueueItem{
			Deadline:   time.Now().UTC(),
			Backoff:    comm.NewBackoff(0, queueRetryJitter, queueRetryMax),
			JobRequest: job,
		})
	}
	popped := 0
	for {
		item := s.popQueue()
		if item == nil {
			break
		}
		popped = popped + 1
	}
	if popped != expected {
		t.Fatalf("Incorrect number of items popped off the queue. Expected %d but got %d", expected, popped)
	}
}

// makes sure the priorty queue properly sort jobs based on priority
func TestPriorityQueueOrder(t *testing.T) {
	s := NewScheduler(nil, nil)
	for i := 0; i < 10; i++ {
		job := &agent.JobRequest{ID: fmt.Sprintf("job%d", i), Type: "build", Name: "bob", Priority: rand.Intn(100), Description: nil, Payload: nil}
		s.pushQueue(&QueueItem{
			Deadline:   time.Now().UTC().Add(-30 * time.Minute),
			Backoff:    comm.NewBackoff(0, queueRetryJitter, queueRetryMax),
			JobRequest: job,
		})
	}
	lastPriority := 101
	for {
		item := s.popQueue()
		if item == nil {
			break
		}
		if item.JobRequest.Priority > lastPriority {
			t.Fatalf("Queue priority sorting is incorrect. Last priority was %d but got priority %d", lastPriority, item.JobRequest.Priority)
		}
		lastPriority = item.JobRequest.Priority
	}
}

// makes sure jobs of the same priority are sorted by deadline
func TestDeadlineQueueOrder(t *testing.T) {
	s := NewScheduler(nil, nil)
	for i := 0; i < 10; i++ {
		job := &agent.JobRequest{ID: fmt.Sprintf("job%d", i), Type: "build", Name: "bob", Priority: 0, Description: nil, Payload: nil}
		s.pushQueue(&QueueItem{
			Deadline:   time.Now().UTC().Add(-1 * time.Duration(rand.Intn(60)) * time.Minute),
			Backoff:    comm.NewBackoff(0, queueRetryJitter, queueRetryMax),
			JobRequest: job,
		})
	}
	lastDeadline := time.Now().UTC().Add(-2 * time.Hour)
	for {
		item := s.popQueue()
		if item == nil {
			break
		}
		if item.Deadline.Before(lastDeadline) {
			t.Fatalf("Queue deadline sorting is incorrect. Last deadline was %s but got deadline %s", lastDeadline, item.Deadline)
		}
		lastDeadline = item.Deadline
	}
}

// makes sure jobs with a deadline set to a time in the future aren't popped
func TestFutureDeadlineJobs(t *testing.T) {
	s := NewScheduler(nil, nil)
	job := &agent.JobRequest{ID: fmt.Sprintf("job"), Type: "build", Name: "bob", Priority: 0, Description: nil, Payload: nil}
	s.pushQueue(&QueueItem{
		Deadline:   time.Now().UTC().Add(5 * time.Minute),
		Backoff:    comm.NewBackoff(0, queueRetryJitter, queueRetryMax),
		JobRequest: job,
	})
	item := s.popQueue()
	if item != nil {
		t.Fatalf("Queue deadline restrictions are incorrect. Job with deadline in the future %s was popped", item.Deadline)
	}
}
