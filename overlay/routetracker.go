package overlay

import (
	"fmt"
	"io"
	"net"
	"sort"
	"strings"
	"sync"

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
	_, err := w.Write(IntToIPv4(r.Network))
	if err != nil {
		return err
	}
	_, err = w.Write(IntToIPv4(r.Mask))
	return err
}

func ReadIPv4Route(r io.Reader) (*IPv4Route, error) {
	netBytes := make([]byte, 4)
	_, err := io.ReadFull(r, netBytes)
	if err != nil {
		return nil, err
	}
	netMask := make([]byte, 4)
	_, err = io.ReadFull(r, netMask)
	if err != nil {
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

type RouteListener chan RouteList

type RouteTracker struct {
	mutex sync.Mutex
	// Registered listeners. Copied and replaced when any modifications are made.
	listeners map[RouteListener]interface{}
	// Registered routes. Copied and replaced when any modifications are made.
	routes RouteList
}

func NewRouteTracker() *RouteTracker {
	return &RouteTracker{
		listeners: make(map[RouteListener]interface{}),
		routes:    make(RouteList, 0),
	}
}

func (rt *RouteTracker) Start(cli *cli.Listener) {
	cli.Register("routes", "", "List all available routes", 0, 0, rt.cliRoutes)
}

func (rt *RouteTracker) cliRoutes(args ...string) (string, error) {
	routes := rt.Routes()
	routeStrings := make([]string, len(routes))
	for i, v := range routes {
		routeStrings[i] = v.String()
	}
	return fmt.Sprintf("Routes: %s", strings.Join(routeStrings, ", ")), nil
}

func (rt *RouteTracker) Routes() RouteList {
	rt.mutex.Lock()
	defer rt.mutex.Unlock()

	return rt.routes
}

func (rt *RouteTracker) AddListener(listener RouteListener) {
	listener <- rt.addListener(listener)
}

func (rt *RouteTracker) addListener(listener RouteListener) RouteList {
	rt.mutex.Lock()
	defer rt.mutex.Unlock()

	newListeners := make(map[RouteListener]interface{}, len(rt.listeners)+1)
	for k, v := range rt.listeners {
		newListeners[k] = v
	}
	newListeners[listener] = nil

	rt.listeners = newListeners
	return rt.routes
}

func (rt *RouteTracker) RemoveListener(listener RouteListener) {
	rt.mutex.Lock()
	defer rt.mutex.Unlock()

	newListeners := make(map[RouteListener]interface{}, len(rt.listeners)-1)
	for k, v := range rt.listeners {
		if k != listener {
			newListeners[k] = v
		}
	}

	rt.listeners = newListeners
}

func (rt *RouteTracker) AddRoute(route *IPv4Route) {
	notifyRouteListeners(rt.addRoute(route))
}

type ByMask RouteList

func (m ByMask) Len() int           { return len(m) }
func (m ByMask) Swap(i, j int)      { m[i], m[j] = m[j], m[i] }
func (m ByMask) Less(i, j int) bool { return ^m[i].Mask < ^m[j].Mask }

func (rt *RouteTracker) addRoute(route *IPv4Route) (map[RouteListener]interface{}, RouteList) {
	rt.mutex.Lock()
	defer rt.mutex.Unlock()

	oldRoutes := rt.routes

	newLen := len(oldRoutes) + 1
	newRoutes := make(RouteList, newLen)
	copy(newRoutes, oldRoutes)
	newRoutes[newLen-1] = route

	// sort routes by netmask for longest-prefix matching
	sort.Sort(ByMask(newRoutes))

	rt.routes = newRoutes
	return rt.listeners, newRoutes
}

func (rt *RouteTracker) RemoveRoute(route *IPv4Route) {
	notifyRouteListeners(rt.removeRoute(route))
}

func (rt *RouteTracker) removeRoute(route *IPv4Route) (map[RouteListener]interface{}, RouteList) {
	rt.mutex.Lock()
	defer rt.mutex.Unlock()

	oldRoutes := rt.routes

	match := -1
	for i, v := range oldRoutes {
		if route == v {
			match = i
			break
		}
	}
	if match < 0 {
		return nil, nil
	}

	newLen := len(oldRoutes) - 1
	newRoutes := make(RouteList, newLen)
	offset := 0
	for i, v := range oldRoutes {
		if i != match {
			newRoutes[offset] = v
			offset++
		}
	}

	rt.routes = newRoutes
	return rt.listeners, newRoutes
}

// TODO replace with atomic pointer?
func notifyRouteListeners(listeners map[RouteListener]interface{}, routes RouteList) {
	if listeners != nil {
		for listener, _ := range listeners {
			listener <- routes
		}
	}
}
