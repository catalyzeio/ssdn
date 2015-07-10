package watch

import (
	"time"

	"github.com/catalyzeio/go-core/udocker"
)

const (
	dockerRetryInterval = 15 * time.Second
	dockerMinInterval   = 1 * time.Second
)

func RateLimitWatch(c *udocker.Client) <-chan struct{} {
	changes := make(chan struct{}, 1)
	limiter := newRateLimiter(changes, dockerMinInterval)
	go watchStream(c, limiter)
	return changes
}

func watchStream(c *udocker.Client, limiter chan<- struct{}) {
	transitions := map[string]struct{}{
		"start":   struct{}{},
		"pause":   struct{}{},
		"unpause": struct{}{},
		"die":     struct{}{},
	}
	for {
		err := c.Events(func(msg *udocker.EventMessage) {
			if _, present := transitions[msg.Status]; present {
				tryNotify(limiter)
			}
		})
		if err != nil {
			log.Warn("Error watching Docker event stream: %s", err)
			time.Sleep(dockerRetryInterval)
		}
	}
}

func newRateLimiter(limited chan<- struct{}, limit time.Duration) chan<- struct{} {
	limiter := make(chan struct{}, 1)
	go func() {
		for {
			<-limiter
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
