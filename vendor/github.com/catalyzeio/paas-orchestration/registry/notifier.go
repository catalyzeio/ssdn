package registry

import (
	"time"
)

type Notifier struct {
	target chan *Message

	cancel chan struct{}
}

func NewNotifier(target chan *Message) *Notifier {
	return &Notifier{
		target: target,

		cancel: make(chan struct{}, 1),
	}
}

func (n *Notifier) Notify(msg *Message) {
	select {
	case n.target <- nil:
	default:
		// drop notification if queue is full
	}
}

func (n *Notifier) Start(interval time.Duration) {
	n.Notify(nil)
	go n.tick(interval)
}

func (n *Notifier) tick(interval time.Duration) {
	t := time.NewTicker(interval)
	defer t.Stop()

	for {
		select {
		case <-n.cancel:
			return
		case <-t.C:
			n.Notify(nil)
		}
	}
}

func (n *Notifier) Stop() {
	select {
	case n.cancel <- struct{}{}:
	default:
		// drop cancel request if queue is full
	}
}
