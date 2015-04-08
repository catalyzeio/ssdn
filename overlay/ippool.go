package overlay

import (
	"fmt"
	"net"
	"sync"
)

type IPPool struct {
	network *net.IPNet
	subnet  *IPv4Route
	gwIP    net.IP

	start uint32
	end   uint32

	mutex sync.Mutex
	next  uint32
	used  map[uint32]struct{}
}

func NewIPPool(network *net.IPNet, subnet *IPv4Route, gwIP net.IP) *IPPool {
	start := subnet.Network&subnet.Mask + 1
	end := subnet.Network | ^subnet.Mask - 1

	used := make(map[uint32]struct{})
	used[IPv4ToInt(gwIP)] = struct{}{}

	return &IPPool{
		network: network,
		subnet:  subnet,
		gwIP:    gwIP,

		start: start,
		end:   end,

		next: start,
		used: used,
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

	return 0, fmt.Errorf("No more IP addresses available")
}

func (p *IPPool) Free(ip uint32) {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	delete(p.used, ip)
}

func (p *IPPool) FormatIP(ip uint32) string {
	ipString := net.IP(IntToIPv4(ip)).String()
	maskString := net.IP(IntToIPv4(p.subnet.Mask)).String()
	return ipString + "/" + maskString
}

func (p *IPPool) FormatNetwork() string {
	return p.network.String()
}

func (p *IPPool) FormatGatewayIP() string {
	return p.gwIP.String()
}
