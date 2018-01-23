package registry

import (
	"sync"
)

type ServiceLocationDetails struct {
	Serial  uint `json:"serial"`
	Running bool `json:"running"`
}

type ServiceLocations struct {
	locations map[string]ServiceLocationDetails
}

func NewServiceLocations() *ServiceLocations {
	return &ServiceLocations{
		make(map[string]ServiceLocationDetails),
	}
}

func (l *ServiceLocations) Query(resp *Message) {
	// Go already returns keys in arbitrary order; no need to randomize the result
	for k := range l.locations {
		resp.Location = k
		return
	}
}

func (l *ServiceLocations) QueryAll(resp *Message) {
	resp.Locations = l.Locations()
}

func (l *ServiceLocations) Add(details ServiceLocationDetails, location string) {
	l.locations[location] = details
}

func (l *ServiceLocations) Remove(serial uint, location string) {
	current, present := l.locations[location]
	if !present || current.Serial != serial {
		log.Warn("Ignoring obsolete unadvertise request for %s", location)
		return
	}
	delete(l.locations, location)
}

func (l *ServiceLocations) Locations() []WeightedLocation {
	n := len(l.locations)
	if n == 0 {
		return nil
	}
	weight := 1.0 / float32(n)
	result := make([]WeightedLocation, 0, n)
	for k, s := range l.locations {
		result = append(result, WeightedLocation{k, weight, s.Running})
	}
	return result
}

func (l *ServiceLocations) Empty() bool {
	return len(l.locations) == 0
}

func (l *ServiceLocations) RemoveSeed() {
	for loc, sld := range l.locations {
		if sld.Serial == 0 {
			l.Remove(0, loc)
		}
	}
}

type Services struct {
	provides  map[string]*ServiceLocations
	publishes map[string]*ServiceLocations
}

func NewServices() *Services {
	return &Services{
		make(map[string]*ServiceLocations),
		make(map[string]*ServiceLocations),
	}
}

func (s *Services) Advertise(req *Message) {
	s.advertise(req.serial, req.Provides, req.Publishes)
}

func (s *Services) Unadvertise(ads *Message) {
	serial := ads.serial
	s.remove(serial, ads.Provides, true)
	s.remove(serial, ads.Publishes, false)
}

func (s *Services) Query(requires string, resp *Message) {
	locations := s.provides[requires]
	if locations != nil {
		locations.Query(resp)
	}
}

func (s *Services) QueryAll(requires string, resp *Message) {
	locations := s.provides[requires]
	if locations != nil {
		locations.QueryAll(resp)
	}
}

func (s *Services) Enumerate(resp *Message) {
	resp.Registry = s.GetEnumeration()
}

func (s *Services) GetEnumeration() *Enumeration {
	e := NewEnumeration()
	for k, v := range s.provides {
		e.Provides[k] = v.Locations()
	}
	for k, v := range s.publishes {
		e.Publishes[k] = v.Locations()
	}
	return e
}

func (s *Services) Empty() bool {
	return len(s.provides) == 0 && len(s.publishes) == 0
}

func (s *Services) RemoveSeed() {
	for _, sl := range s.provides {
		sl.RemoveSeed()
	}
	for _, sl := range s.publishes {
		sl.RemoveSeed()
	}
}

func (s *Services) advertise(serial uint, provides, publishes []Advertisement) {
	s.add(serial, provides, true)
	s.add(serial, publishes, false)
}

func (s *Services) add(serial uint, list []Advertisement, provides bool) {
	if list != nil {
		for _, v := range list {
			m := s.getMap(provides)
			service := v.Name
			locations := m[service]
			if locations == nil {
				locations = NewServiceLocations()
				m[service] = locations
			}
			locations.Add(ServiceLocationDetails{serial, v.Running}, v.Location)
		}
	}
}

func (s *Services) remove(serial uint, list []Advertisement, provides bool) {
	if list != nil {
		for _, v := range list {
			m := s.getMap(provides)
			service := v.Name
			locations := m[service]
			if locations != nil {
				locations.Remove(serial, v.Location)
				if locations.Empty() {
					delete(m, service)
				}
			}
		}
	}
}

func (s *Services) getMap(provides bool) map[string]*ServiceLocations {
	if provides {
		return s.provides
	}
	return s.publishes
}

type ChangeListeners struct {
	targets map[chan<- interface{}]struct{}
}

func NewChangeListeners() *ChangeListeners {
	return &ChangeListeners{
		make(map[chan<- interface{}]struct{}),
	}
}

func (l *ChangeListeners) Empty() bool {
	return len(l.targets) == 0
}

func (l *ChangeListeners) Add(listener chan<- interface{}) {
	l.targets[listener] = struct{}{}
}

func (l *ChangeListeners) Remove(listener chan<- interface{}) {
	delete(l.targets, listener)
}

func (l *ChangeListeners) Notify() {
	msg := Message{Type: "modified"}
	for listener := range l.targets {
		select {
		case listener <- &msg:
		default:
			// drop notification if queue is full
		}
	}
}

type MemoryBackend struct {
	mutex   sync.RWMutex
	tenants map[string]*Services
	// 0 means the data was seeded
	serial    uint
	listeners map[string]*ChangeListeners
}

func NewMemoryBackend(seed *map[string]*Enumeration) *MemoryBackend {
	m := &MemoryBackend{
		tenants:   make(map[string]*Services),
		serial:    1,
		listeners: make(map[string]*ChangeListeners),
	}
	if seed != nil {
		for k, v := range *seed {
			services := m.tenants[k]
			if services == nil {
				services = NewServices()
				m.tenants[k] = services
			}
			provides, publishes := v.ToAdvertisements()
			services.advertise(0, provides, publishes)
		}
	}
	return m
}

func (m *MemoryBackend) Advertise(tenant string, req *Message, resp *Message) error {
	if log.IsDebugEnabled() {
		log.Debug("Advertising provides=%v publishes=%v for %s", req.Provides, req.Publishes, tenant)
	}

	m.mutex.Lock()
	defer m.mutex.Unlock()

	serial := m.serial
	m.serial++
	req.serial = serial

	services := m.tenants[tenant]
	if services == nil {
		services = NewServices()
		m.tenants[tenant] = services
	}
	services.Advertise(req)
	m.notifyListeners(tenant)
	resp.Type = "advertised"
	return nil
}

func (m *MemoryBackend) Unadvertise(tenant string, ads *Message, notify bool) error {
	if log.IsDebugEnabled() {
		log.Debug("Unadvertising provides=%v publishes=%v for %s", ads.Provides, ads.Publishes, tenant)
	}

	m.mutex.Lock()
	defer m.mutex.Unlock()

	services := m.tenants[tenant]
	if services != nil {
		services.Unadvertise(ads)
		if services.Empty() {
			delete(m.tenants, tenant)
		}
	}
	if notify {
		m.notifyListeners(tenant)
	}
	return nil
}

func (m *MemoryBackend) notifyListeners(tenant string) {
	listeners := m.listeners[tenant]
	if listeners != nil {
		listeners.Notify()
	}
}

func (m *MemoryBackend) Query(tenant string, req *Message, resp *Message) error {
	requires := req.Requires
	if len(requires) == 0 {
		resp.SetError("query missing required service")
		return nil
	}

	m.mutex.RLock()
	defer m.mutex.RUnlock()

	services := m.tenants[tenant]
	if services != nil {
		services.Query(requires, resp)
	}
	resp.Type = "answer"
	return nil
}

func (m *MemoryBackend) QueryAll(tenant string, req *Message, resp *Message) error {
	requires := req.Requires
	if len(requires) == 0 {
		resp.SetError("query missing required service")
		return nil
	}

	m.mutex.RLock()
	defer m.mutex.RUnlock()

	services := m.tenants[tenant]
	if services != nil {
		services.QueryAll(requires, resp)
	}
	resp.Type = "answer"
	return nil
}

func (m *MemoryBackend) Enumerate(tenant string, req *Message, resp *Message) error {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	services := m.tenants[tenant]
	if services != nil {
		services.Enumerate(resp)
	}
	resp.Type = "answer"
	return nil
}

func (m *MemoryBackend) Register(tenant string, notifications chan<- interface{}, resp *Message) error {
	if log.IsDebugEnabled() {
		log.Debug("Registering listener for %s", tenant)
	}

	m.mutex.Lock()
	defer m.mutex.Unlock()

	listeners := m.listeners[tenant]
	if listeners == nil {
		listeners = NewChangeListeners()
		m.listeners[tenant] = listeners
	}
	listeners.Add(notifications)
	resp.Type = "registered"
	return nil
}

func (m *MemoryBackend) Unregister(tenant string, notifications chan<- interface{}, resp *Message) error {
	if log.IsDebugEnabled() {
		log.Debug("Unregistering listener for %s", tenant)
	}

	m.mutex.Lock()
	defer m.mutex.Unlock()

	listeners := m.listeners[tenant]
	if listeners != nil {
		listeners.Remove(notifications)
		if listeners.Empty() {
			delete(m.listeners, tenant)
		}
	}
	if resp != nil {
		resp.Type = "unregistered"
	}
	return nil
}

func (m *MemoryBackend) EnumerateAll() (map[string]*Enumeration, error) {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	enum := make(map[string]*Enumeration)
	for t, s := range m.tenants {
		enum[t] = s.GetEnumeration()
	}
	return enum, nil
}

func (m *MemoryBackend) RemoveSeed() error {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	for _, s := range m.tenants {
		s.RemoveSeed()
	}
	return nil
}

func (m *MemoryBackend) Close() {}
