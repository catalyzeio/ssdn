package agent

import (
	"fmt"
	"time"
)

type DummySpawner struct {
	publishPool *PortPool
}

func NewDummySpawner() *DummySpawner {
	return &DummySpawner{
		publishPool: NewPortPool(publishPortStart, internalPortStart-1),
	}
}

func (s *DummySpawner) Spawn(job *JobRequest, info, replacedJob *JobInfo) (Runner, error) {
	payload := job.Payload
	publishes := payload.Publishes
	n := len(publishes)
	published := make([]uint16, 0, n)
	launchInfo := make([]ServiceLocation, 0, n)
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
			Address: "tcp://127.0.0.1",
			Port:    port,
		})
	}
	info.LaunchInfo = launchInfo
	info.Context = &JobContext{
		Payload: payload,

		OneShot: payload.OneShot,

		PublishPorts: published,
	}
	r := &dummyRunner{
		jobID: job.ID,

		stalled: payload.Incomplete,
		delay:   time.Duration(payload.DummyDelay) * time.Millisecond,
		fail:    payload.DummyFail,
		failNow: payload.DummyFailNow,
	}
	return r, nil
}

func (d *DummySpawner) Restore(info *JobInfo) (Runner, error) {
	return nil, fmt.Errorf("dummy handler does not support restoring jobs")
}

func (s *DummySpawner) CleanUpPublishPorts(ports []uint16) {
	s.publishPool.ReleaseAll(ports)
}

type dummyRunner struct {
	jobID string

	stalled bool
	delay   time.Duration
	fail    bool
	failNow bool
}

func (r *dummyRunner) Run(state Status, s *Signals, updates chan<- Update) (Status, error) {
	if r.stalled {
		return Waiting, nil
	}

	if r.failNow {
		return Failed, nil
	}
	updates <- Update{JobID: r.jobID, State: Running}
	var done <-chan time.Time
	if r.delay >= 0 {
		done = time.After(r.delay)
	}
	for {
		pollDeadline := time.After(time.Second * 2)
		select {
		case <-s.KillRequests:
			return Killed, nil
		case <-s.StopRequests:
			if r.fail {
				return Failed, nil
			}
			updates <- Update{JobID: r.jobID, State: Stopped}
		case <-s.StartRequests:
			updates <- Update{JobID: r.jobID, State: Running}
		case <-done:
			if r.fail {
				return Failed, nil
			}
			return Finished, nil
		case <-pollDeadline:
		}
	}
}

func (r *dummyRunner) Patch(payload *JobPayload) (Status, *JobContext, error) {
	r.stalled = payload.Incomplete
	if r.stalled {
		return Waiting, nil, nil
	}
	return Started, nil, nil
}

func (r *dummyRunner) CleanUp(state Status, keepResources bool) error {
	return nil
}

func (r *dummyRunner) Cull(state Status) error {
	return nil
}
