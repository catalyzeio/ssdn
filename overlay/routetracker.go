package overlay

import (
	"sync"
)

type IPv4Route struct {
	Network uint32
	Mask    uint32

	Out PacketQueue
}

type RouteListener chan []IPv4Route

type RouteTracker struct {
	mutex sync.Mutex
	// Registered listeners. Copied and replaced when any modifications are made.
	listeners map[RouteListener]interface{}
	// Registered routes. Copied and replaced when any modifications are made.
	routes []IPv4Route
}

func NewRouteTracker() *RouteTracker {
	return &RouteTracker{
		listeners: make(map[RouteListener]interface{}),
		routes:    make([]IPv4Route, 0),
	}
}

func (rt *RouteTracker) AddListener(listener RouteListener) {
	listener <- rt.addListener(listener)
}

func (rt *RouteTracker) addListener(listener RouteListener) []IPv4Route {
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

func (rt *RouteTracker) AddRoute(route IPv4Route) {
	notifyRouteListeners(rt.addRoute(route))
}

func (rt *RouteTracker) addRoute(route IPv4Route) (map[RouteListener]interface{}, []IPv4Route) {
	rt.mutex.Lock()
	defer rt.mutex.Unlock()

	oldRoutes := rt.routes

	newLen := len(oldRoutes) + 1
	newRoutes := make([]IPv4Route, newLen)
	copy(newRoutes, oldRoutes)
	newRoutes[newLen-1] = route

	// TODO sort routes by netmask for longest-prefix matching

	rt.routes = newRoutes
	return rt.listeners, newRoutes
}

func (rt *RouteTracker) RemoveRoute(route IPv4Route) {
	notifyRouteListeners(rt.removeRoute(route))
}

func (rt *RouteTracker) removeRoute(route IPv4Route) (map[RouteListener]interface{}, []IPv4Route) {
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
	newRoutes := make([]IPv4Route, newLen)
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

func notifyRouteListeners(listeners map[RouteListener]interface{}, routes []IPv4Route) {
	if listeners != nil {
		for listener, _ := range listeners {
			listener <- routes
		}
	}
}
