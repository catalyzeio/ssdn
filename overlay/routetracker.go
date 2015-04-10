package overlay

import (
	"fmt"
	"io"
	"net"
	"sort"
	"strings"
	"sync/atomic"
	"unsafe"

	"github.com/catalyzeio/shadowfax/cli"
)

type IPv4Route struct {
	Network uint32
	Mask    uint32

	Queue PacketQueue
}

func NewIPv4Route(network *net.IPNet) (*IPv4Route, error) {
	ip := network.IP.To4()
	if ip == nil {
		return nil, fmt.Errorf("network must be IPv4")
	}

	_, bits := network.Mask.Size()
	if bits != 32 {
		return nil, fmt.Errorf("netmask must be IPv4")
	}

	return &IPv4Route{
		Network: IPv4ToInt(ip),
		Mask:    IPv4ToInt(network.Mask),
	}, nil
}

func (r *IPv4Route) Write(w io.Writer) error {
	if _, err := w.Write(IntToIPv4(r.Network)); err != nil {
		return err
	}
	_, err := w.Write(IntToIPv4(r.Mask))
	return err
}

func ReadIPv4Route(r io.Reader) (*IPv4Route, error) {
	netBytes := make([]byte, 4)
	if _, err := io.ReadFull(r, netBytes); err != nil {
		return nil, err
	}
	netMask := make([]byte, 4)
	if _, err := io.ReadFull(r, netMask); err != nil {
		return nil, err
	}
	return &IPv4Route{
		Network: IPv4ToInt(netBytes),
		Mask:    IPv4ToInt(netMask),
	}, nil
}

func (r *IPv4Route) String() string {
	mask := net.IPMask(IntToIPv4(r.Mask))
	maskBits, _ := mask.Size()
	return fmt.Sprintf("%s/%d", net.IP(IntToIPv4(r.Network)), maskBits)
}

type RouteList []*IPv4Route

type RouteTracker struct {
	list unsafe.Pointer // *RouteList
}

func NewRouteTracker() *RouteTracker {
	emptyList := make(RouteList, 0)
	return &RouteTracker{
		list: unsafe.Pointer(&emptyList),
	}
}

func (rt *RouteTracker) Start(cli *cli.Listener) {
	rt.StartAs(cli, "routes", "List all available routes")
}

func (rt *RouteTracker) StartAs(cli *cli.Listener, command, description string) {
	cli.Register(command, "", description, 0, 0, rt.cliRoutes)
}

func (rt *RouteTracker) cliRoutes(args ...string) (string, error) {
	routes := rt.Get()
	routeStrings := make([]string, len(routes))
	for i, v := range routes {
		routeStrings[i] = v.String()
	}
	return fmt.Sprintf("Routes: %s", strings.Join(routeStrings, ", ")), nil
}

func (rt *RouteTracker) Get() RouteList {
	pointer := &rt.list
	p := (*RouteList)(atomic.LoadPointer(pointer))
	return *p
}

type ByMask RouteList

func (m ByMask) Len() int           { return len(m) }
func (m ByMask) Swap(i, j int)      { m[i], m[j] = m[j], m[i] }
func (m ByMask) Less(i, j int) bool { return ^m[i].Mask < ^m[j].Mask }

func (rt *RouteTracker) Add(route *IPv4Route) {
	pointer := &rt.list
	for {
		// grab current list
		old := atomic.LoadPointer(pointer)
		current := (*RouteList)(old)

		// add new entry to list
		oldRoutes := *current

		newLen := len(oldRoutes) + 1
		newRoutes := make(RouteList, newLen)
		copy(newRoutes, oldRoutes)
		newRoutes[newLen-1] = route

		// sort routes by netmask for longest-prefix matching
		sort.Sort(ByMask(newRoutes))

		// replace current list with new list
		new := unsafe.Pointer(&newRoutes)
		if atomic.CompareAndSwapPointer(pointer, old, new) {
			if log.IsDebugEnabled() {
				log.Debug("Updated routing table: %s", newRoutes)
			}
			return
		}
	}
}

func (rt *RouteTracker) Remove(route *IPv4Route) {
	pointer := &rt.list
	for {
		// grab current list
		old := atomic.LoadPointer(pointer)
		current := (*RouteList)(old)

		// look up position in existing list (bail if not in list)
		oldRoutes := *current

		match := -1
		for i, v := range oldRoutes {
			if route == v {
				match = i
				break
			}
		}
		if match < 0 {
			return
		}

		// create new list, skipping matched position
		newLen := len(oldRoutes) - 1
		newRoutes := make(RouteList, newLen)
		offset := 0
		for i, v := range oldRoutes {
			if i != match {
				newRoutes[offset] = v
				offset++
			}
		}

		// replace current list with new list
		new := unsafe.Pointer(&newRoutes)
		if atomic.CompareAndSwapPointer(pointer, old, new) {
			if log.IsDebugEnabled() {
				log.Debug("Updated routing table: %s", newRoutes)
			}
			return
		}
	}
}

func (rt *RouteTracker) RoutePacket(p *PacketBuffer) {
	trace := log.IsTraceEnabled()

	// XXX assumes frames have no 802.1q tagging

	// ignore non-IPv4 packets
	buff := p.Data
	if p.Length < 34 || buff[12] != 0x08 || buff[13] != 0x00 {
		if log.IsTraceEnabled() {
			log.Trace("Dropped non-IPv4 packet")
		}
		p.Queue <- p
		return
	}

	// pull out destination IP
	destIPBytes := buff[30:34]
	destIP := IPv4ToInt(destIPBytes)

	// look up destination based on available routes
	for _, r := range rt.Get() {
		if destIP&r.Mask == r.Network {
			if trace {
				log.Trace("Found match for destination IP %d", destIP)
			}
			r.Queue <- p
			return
		}
	}

	// no route available; return to owner
	if trace {
		log.Trace("No match for destination IP %d", destIP)
	}
	p.Queue <- p
}
