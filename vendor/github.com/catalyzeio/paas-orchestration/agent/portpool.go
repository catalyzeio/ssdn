package agent

import (
	"fmt"
	"sync"
)

type PortPool struct {
	start uint16
	end   uint16

	mutex sync.Mutex
	next  uint16
	used  map[uint16]struct{}
}

func NewPortPool(start, end uint16) *PortPool {
	return &PortPool{
		start: start,
		end:   end,

		next: start,
		used: make(map[uint16]struct{}),
	}
}

func (p *PortPool) Next() (uint16, error) {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	return p.lockedNext()
}

func (p *PortPool) lockedNext() (uint16, error) {
	current := p.next

	// XXX this is a particularly stupid algorithm, but should be OK in practice
	for {
		next := current + 1
		if next > p.end {
			next = p.start
		}
		_, present := p.used[current]
		if !present {
			p.used[current] = struct{}{}
			p.next = next
			if log.IsTraceEnabled() {
				log.Trace("Allocated port %d from pool", current)
			}
			return current, nil
		}
		if next == p.next {
			break
		}
		current = next
	}

	return 0, fmt.Errorf("no more ports available in [%d, %d]", p.start, p.end)
}

func (p *PortPool) Acquire(port uint16) error {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	return p.lockedAcquire(port)
}

func (p *PortPool) lockedAcquire(port uint16) error {
	_, present := p.used[port]
	if present {
		return fmt.Errorf("port %d already in use", port)
	}
	p.used[port] = struct{}{}
	if log.IsTraceEnabled() {
		log.Trace("Acquired port %d from pool", port)
	}
	return nil
}

func (p *PortPool) AcquireAll(ports []uint16) error {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	return p.lockedAcquireAll(ports)
}

func (p *PortPool) lockedAcquireAll(ports []uint16) error {
	var acquired []uint16
	for _, port := range ports {
		err := p.lockedAcquire(port)
		if err != nil {
			p.lockedReleaseAll(acquired)
			return err
		}
		acquired = append(acquired, port)
	}
	return nil
}

func (p *PortPool) Release(port uint16) {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	p.lockedRelease(port)
}

func (p *PortPool) lockedRelease(port uint16) {
	delete(p.used, port)
	if log.IsTraceEnabled() {
		log.Trace("Released port %d back to pool", port)
	}
}

func (p *PortPool) ReleaseAll(ports []uint16) {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	p.lockedReleaseAll(ports)
}

func (p *PortPool) lockedReleaseAll(ports []uint16) {
	for _, port := range ports {
		p.lockedRelease(port)
	}
}
