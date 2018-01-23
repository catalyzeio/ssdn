package udocker

import (
	"time"
)

const (
	retryInterval = 15 * time.Second
	minInterval   = 1 * time.Second
	maxInterval   = 5 * time.Minute
)

func (c *Client) Watch() <-chan struct{} {
	changes := make(chan struct{}, 1)
	limiter := newRateLimiter(changes, minInterval)
	go c.doWatch(limiter)
	return changes
}

func (c *Client) doWatch(limiter chan<- struct{}) {
	transitions := map[string]struct{}{
		"start":   struct{}{},
		"pause":   struct{}{},
		"unpause": struct{}{},
		"die":     struct{}{},
	}
	for {
		err := c.Events(func(msg *EventMessage) {
			// filter transitions that don't involve a change in run state
			if _, present := transitions[msg.Status]; present {
				tryNotify(limiter)
			}
		})
		if err != nil {
			log.Warn("Error watching Docker event stream: %s", err)
		}
		time.Sleep(retryInterval)
	}
}

func newRateLimiter(limited chan<- struct{}, limit time.Duration) chan<- struct{} {
	limiter := make(chan struct{}, 1)
	go func() {
		for {
			select {
			case <-limiter:
			case <-time.After(maxInterval):
			}
			tryNotify(limited)
			time.Sleep(limit)
		}
	}()
	return limiter
}

func tryNotify(c chan<- struct{}) {
	select {
	case c <- struct{}{}:
	default:
		// drop notification if queue is full
	}
}
