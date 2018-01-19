package comm

import (
	"math/rand"
	"time"
)

const (
	defaultInit   = 0
	defaultJitter = 500 * time.Millisecond
	defaultMax    = 5 * time.Second
)

type Backoff struct {
	Delay time.Duration

	init   time.Duration
	jitter time.Duration
	max    time.Duration
}

func NewDefaultBackoff() *Backoff {
	return NewBackoff(defaultInit, defaultJitter, defaultMax)
}

func NewBackoff(init time.Duration, jitter time.Duration, max time.Duration) *Backoff {
	return &Backoff{0, init, jitter, max}
}

func (b *Backoff) Init() {
	b.Delay = b.init
	b.addJitter()
}

func (b *Backoff) After() <-chan time.Time {
	return time.After(b.Delay)
}

func (b *Backoff) Fail() {
	b.addJitter()
}

func (b *Backoff) addJitter() {
	jitterVal := int64(b.jitter)
	jitterVal += rand.Int63n(jitterVal)
	delta := time.Duration(jitterVal)
	b.Delay += delta
	if b.Delay > b.max {
		b.Delay = b.max + delta/2
	}
}
