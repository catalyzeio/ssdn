package overlay

import (
	"fmt"
	"net"
	"sync"
)

type IPPool struct {
	subnet *IPv4Route

	start uint32
	end   uint32

	mutex sync.Mutex
	next  uint32
	used  map[uint32]struct{}
}

func NewIPPool(subnet *IPv4Route) *IPPool {
	start := subnet.Network&subnet.Mask + 1
	end := subnet.Network | ^subnet.Mask - 1

	return &IPPool{
		subnet: subnet,

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

func (p *IPPool) Acquire(ip net.IP) error {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	key := IPv4ToInt(ip)
	_, present := p.used[key]
	if present {
		return fmt.Errorf("already allocated IP %s", ip)
	}
	p.used[key] = struct{}{}
	return nil
}

func (p *IPPool) Free(ip uint32) {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	delete(p.used, ip)
}

func (p *IPPool) FormatIP(ip uint32) string {
	return FormatIPWithMask(ip, net.IPMask(IntToIPv4(p.subnet.Mask)))
}

func FormatIPWithMask(ip uint32, mask net.IPMask) string {
	net := net.IPNet{
		IP:   net.IP(IntToIPv4(ip)),
		Mask: mask,
	}
	return net.String()
}
