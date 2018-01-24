package agent

import (
	"flag"
	"os"
	"testing"
	"time"

	"github.com/catalyzeio/go-core/comm"
	"github.com/catalyzeio/go-core/simplelog"
)

const (
	OrchDir = "/tmp/orch/"
)

func init() {
	simplelog.AddFlags()
	comm.AddTLSFlags()
	comm.AddListenFlags(true, 8080, false)
	flag.Parse()
	os.Mkdir(OrchDir, 0700)
}

func setUpListener(handlerType string, ac *AgentConstraints, memoryGiB int) (*Listener, *DummySpawner, error) {
	handlers := make(map[string]*Handler)
	dummySpawner := NewDummySpawner()
	handlers[handlerType] = NewHandler(ac, dummySpawner, handlerType)
	policy := NewMemoryPolicy(int64(memoryGiB)<<10, 1, false, 0, 1024<<10)
	listenAddress, err := comm.GetListenAddress()
	if err != nil {
		return nil, nil, err
	}
	l, err := NewListener(OrchDir, listenAddress, nil, policy, handlers, ac)
	if err != nil {
		return nil, nil, err
	}
	go l.process()
	return l, dummySpawner, nil
}

func defaultMessage(sizeMiB int64) *Message {
	return &Message{
		Type: "offer",
		Job: &JobRequest{
			ID:   "test123",
			Type: "docker",
			Name: "fakepod1-01/sometype/deploy",
			Description: &JobDescription{
				Tenant:    "fakepod1-01",
				Requires:  []string{"agent.us-east-1"},
				Provides:  []string{"sometype"},
				Conflicts: []string{"sometype"},
				Resources: &JobLimits{
					Memory: sizeMiB << 20, // in bytes
				},
			},
			Payload: &JobPayload{
				TenantToken: "testtoken",
				DockerImage: "registry.local:5000/catalyzeio/sometype:lastest",
				Limits: &JobLimits{
					Memory: sizeMiB << 20, // in bytes
				},
				Publishes: []*PublishService{
					&PublishService{&JobService{"http", 80}, 8001},
					&PublishService{&JobService{"http", 443}, 8002},
				},
			},
		},
	}
}

func genericOffer(t *testing.T) (*Message, *Listener, *DummySpawner) {
	ac := AgentConstraints{
		Requires:  NewStringBag([]string{}),
		Provides:  NewStringBag([]string{"agent.us-east-1"}),
		Conflicts: NewStringBag([]string{}),
	}
	l, dummySpawner, err := setUpListener("docker", &ac, 4)
	if err != nil {
		t.Errorf("Failed to set up listener: %s", err)
	}

	changes := make(chan interface{}, 1)
	watcher := NewWatcher(changes)
	l.addWatcher(watcher)
	defer l.removeWatcher(watcher)

	msg := defaultMessage(2048)
	msg.Job.Payload.DummyDelay = -1

	respChan := make(chan interface{}, 1)
	req := request{
		msg: msg,
		out: respChan,
	}

	l.requests <- req
	var resp *Message
	select {
	case i := <-respChan:
		resp = i.(*Message)
	case <-time.After(20 * time.Second):
		t.Errorf("Request timed out")
	}

	if resp.Type == "error" {
		t.Error(resp.Message)
	}
	if resp.Bid == nil {
		t.Error("Offer rejected")
	} else {
		log.Info("Job bid was %f", *resp.Bid)
	}

	for {
		i := <-changes
		m := i.(*Message)
		if m.JobInfo.ID != msg.Job.ID {
			continue
		}
		if m.JobInfo.State == Running {
			break
		}
		if m.JobInfo.State.Terminal() {
			t.Error("Offered job failed to run")
		}
	}
	return msg, l, dummySpawner
}

func TestOffer(t *testing.T) {
	genericOffer(t)
}

func TestReplaceOffer(t *testing.T) {
	oldMsg, l, _ := genericOffer(t)

	msg := defaultMessage(2048)
	msg.Job.ID = "jobtest1234"
	msg.Job.Payload.DummyDelay = -1
	msg.Job.Description.ReplaceJobID = &oldMsg.Job.ID

	changes := make(chan interface{}, 1)
	watcher := NewWatcher(changes)
	l.addWatcher(watcher)
	defer l.removeWatcher(watcher)

	respChan := make(chan interface{}, 1)
	req := request{
		msg: msg,
		out: respChan,
	}

	l.requests <- req
	var resp *Message
	select {
	case i := <-respChan:
		resp = i.(*Message)
	case <-time.After(20 * time.Second):
		t.Errorf("Request timed out")
	}

	if resp.Type == "error" {
		t.Error(resp.Message)
	}
	if resp.Bid == nil {
		t.Error("Offer rejected")
	} else {
		log.Info("Job bid was %f", *resp.Bid)
	}

	for {
		i := <-changes
		m := i.(*Message)
		if m.JobInfo.ID == msg.Job.ID {
			if m.JobInfo.State == Running {
				break
			}
			if m.JobInfo.State.Terminal() {
				t.Error("Offered job failed to run")
			}
		}
	}
}

func TestReplaceOfferFail(t *testing.T) {
	oldMsg, l, _ := genericOffer(t)

	msg := defaultMessage(2048)
	msg.Job.ID = "jobtest1234"
	msg.Job.Payload.DummyDelay = 0
	msg.Job.Description.ReplaceJobID = &oldMsg.Job.ID
	msg.Job.Payload.DummyFailNow = true

	changes := make(chan interface{}, 1)
	watcher := NewWatcher(changes)
	l.addWatcher(watcher)
	defer l.removeWatcher(watcher)

	respChan := make(chan interface{}, 1)
	req := request{
		msg: msg,
		out: respChan,
	}

	l.requests <- req
	var resp *Message
	select {
	case i := <-respChan:
		resp = i.(*Message)
	case <-time.After(20 * time.Second):
		t.Errorf("Request timed out")
	}

	if resp.Type == "error" {
		t.Error(resp.Message)
	}
	if resp.Bid == nil {
		t.Error("Offer rejected")
	} else {
		log.Info("Job bid was %f", *resp.Bid)
	}

	for {
		i := <-changes
		m := i.(*Message)
		if m.JobInfo.ID == oldMsg.Job.ID {
			if m.JobInfo.State.Terminal() {
				t.Error("old job should have started to replace new job")
			}
			if m.JobInfo.State == Running {
				break
			}
		}
	}
}
