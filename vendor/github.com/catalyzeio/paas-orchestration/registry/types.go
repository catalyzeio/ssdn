package registry

type Advertisement struct {
	Name     string `json:"name"`
	Location string `json:"location"`
	Running  bool   `json:"running"`
}

type WeightedLocation struct {
	Location string  `json:"location"`
	Weight   float32 `json:"weight"`
	Running  bool    `json:"running"`
}

type Enumeration struct {
	Provides  map[string][]WeightedLocation `json:"provides,omitempty"`
	Publishes map[string][]WeightedLocation `json:"publishes,omitempty"`
}

func NewEnumeration() *Enumeration {
	return &Enumeration{
		make(map[string][]WeightedLocation),
		make(map[string][]WeightedLocation),
	}
}

func (enum *Enumeration) ToAdvertisements() ([]Advertisement, []Advertisement) {
	var provides []Advertisement
	var publishes []Advertisement
	for k, v := range enum.Provides {
		for _, wl := range v {
			provides = append(provides, Advertisement{k, wl.Location, wl.Running})
		}
	}
	for k, v := range enum.Publishes {
		for _, wl := range v {
			publishes = append(publishes, Advertisement{k, wl.Location, wl.Running})
		}
	}
	return provides, publishes
}

type Message struct {
	Type string `json:"type"`

	// authenticate request
	Tenant string  `json:"tenant,omitempty"`
	Token  *string `json:"token,omitempty"` // send if empty, omit if null

	// authenticate response
	Version string `json:"version,omitempty"`

	// advertise request
	Provides  []Advertisement `json:"provides,omitempty"`
	Publishes []Advertisement `json:"publishes,omitempty"`
	serial    uint

	// query, queryAll request
	Requires string `json:"requires,omitempty"`

	// query response
	Location string `json:"location,omitempty"`

	// queryAll response
	Locations []WeightedLocation `json:"locations,omitempty"`

	// enumerate response
	Registry *Enumeration `json:"registry,omitempty"`

	// error responses
	Message string `json:"message,omitempty"`
}

func (m *Message) SetError(errorMessage string) {
	m.Type = "error"
	m.Message = errorMessage
}

type Backend interface {
	Advertise(tenant string, req *Message, resp *Message) error
	Unadvertise(tenant string, ads *Message, notify bool) error
	Query(tenant string, req *Message, resp *Message) error
	QueryAll(tenant string, req *Message, resp *Message) error
	Enumerate(tenant string, req *Message, resp *Message) error
	Register(tenant string, notifications chan<- interface{}, resp *Message) error
	Unregister(tenant string, notifications chan<- interface{}, resp *Message) error
	EnumerateAll() (map[string]*Enumeration, error)
	RemoveSeed() error
	Close()
}
