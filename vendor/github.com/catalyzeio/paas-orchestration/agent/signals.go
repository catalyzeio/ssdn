package agent

import (
	"fmt"
	"sync/atomic"
)

const (
	patchChanSize = 8
)

type Signals struct {
	KillRequests  <-chan struct{}
	StopRequests  <-chan struct{}
	PatchRequests <-chan *Message
	CullRequests  <-chan struct{}
	StartRequests <-chan struct{}

	kill          chan<- struct{}
	stop          chan<- struct{}
	patch         chan<- *Message
	cull          chan<- struct{}
	start         chan<- struct{}
	keepResources uint32
}

func NewSignals() *Signals {
	kill := make(chan struct{}, 1)
	stop := make(chan struct{}, 1)
	patch := make(chan *Message, patchChanSize)
	cull := make(chan struct{}, 1)
	start := make(chan struct{}, 1)
	return &Signals{
		KillRequests:  kill,
		StopRequests:  stop,
		PatchRequests: patch,
		CullRequests:  cull,
		StartRequests: start,

		kill:          kill,
		stop:          stop,
		patch:         patch,
		cull:          cull,
		start:         start,
		keepResources: 0,
	}
}

func (s *Signals) Kill() {
	select {
	case s.kill <- struct{}{}:
	default:
		// kill request already sent
	}
}

func (s *Signals) Stop() {
	select {
	case s.stop <- struct{}{}:
	default:
		// stop request already sent
	}
}

func (s *Signals) Patch(resp *Message) error {
	select {
	case s.patch <- resp:
		return nil
	default:
		return fmt.Errorf("job patch queue is full")
	}
}

func (s *Signals) Cull() {
	select {
	case s.cull <- struct{}{}:
	default:
		// cull request already sent
	}
}

func (s *Signals) Start() {
	select {
	case s.start <- struct{}{}:
	default:
		// start request already sent
	}
}

func (s *Signals) KeepResources() {
	atomic.StoreUint32(&s.keepResources, 1)
}

func (s *Signals) ShouldKeepResources() bool {
	return atomic.LoadUint32(&s.keepResources) > 0
}

func (s *Signals) CleanUpResources() {
	atomic.StoreUint32(&s.keepResources, 0)
}
