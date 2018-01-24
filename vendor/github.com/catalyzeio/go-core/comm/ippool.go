package comm

import (
	"fmt"
	"net"
	"sync"
)

type IPPool struct {
	network uint32
	mask    uint32

	start uint32
	end   uint32

	mutex sync.Mutex
	next  uint32
	used  map[uint32]struct{}
}

func NewIPPool(network, mask uint32) *IPPool {
	start := network&mask + 1
	end := network | ^mask - 1

	return &IPPool{
		network: network,
		mask:    mask,

		start: start,
		end:   end,

		next: start,
		used: make(map[uint32]struct{}),
	}
}

func (p *IPPool) Next() (uint32, error) {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	// XXX this is a particularly stupid algorithm, but should be OK in practice
	current := p.next

	for {
		next := current + 1
		if next > p.end {
			next = p.start
		}
		_, present := p.used[current]
		if !present {
			p.used[current] = struct{}{}
			p.next = next
			return current, nil
		}
		if next == p.next {
			break
		}
		current = next
	}

	return 0, fmt.Errorf("no more IP addresses available")
}

func (p *IPPool) AcquireFromOffset(offset int) (uint32, error) {
	var key uint32
	if offset < 0 {
		key = uint32(int(p.end) + offset + 1)
	} else {
		key = uint32(int(p.start) + offset)
	}

	ip := IntToIP(key)
	return key, p.acquireRaw(ip, key)
}

func (p *IPPool) AcquireFromString(ip string) (uint32, error) {
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return 0, fmt.Errorf("invalid IP address: %s", ip)
	}

	return p.Acquire(parsed)
}

func (p *IPPool) Acquire(ip net.IP) (uint32, error) {
	requested := ip.To4()
	if requested == nil {
		return 0, fmt.Errorf("invalid IP address: %s", ip)
	}

	return p.acquireIP(requested)
}

func (p *IPPool) acquireIP(ip net.IP) (uint32, error) {
	key := IPv4ToInt(ip)
	return key, p.acquireRaw(ip, key)
}

func (p *IPPool) acquireRaw(ip net.IP, key uint32) error {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	if key < p.start || key > p.end {
		return fmt.Errorf("not in pool range: %s", ip)
	}

	_, present := p.used[key]
	if present {
		return fmt.Errorf("already allocated IP %s", ip)
	}
	p.used[key] = struct{}{}
	return nil
}

func (p *IPPool) Release(ip uint32) {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	delete(p.used, ip)
}

func (p *IPPool) FormatIP(ip uint32) string {
	return FormatIPWithMask(ip, net.IPMask(IntToIPv4(p.mask)))
}
