package overlay

import (
	"net"
)

type Status struct {
	Uptime string `json:"uptime"`
}

type AttachRequest struct {
	Container string `json:"container"`
}

type ConnectionDetails struct {
	Interface string `json:"interface,omitempty"`

	IP  string `json:"ip,omitempty"`
	MAC string `json:"mac,omitempty"`
}

type Connector interface {
	Attach(string) error
	Detach(string) error
	ListConnections() map[string]*ConnectionDetails
}

type PeerDetails struct {
	Type      string `json:"type"`
	Interface string `json:"interface,omitempty"`
}

type PeerManager interface {
	AddPeer(string) error
	DeletePeer(string) error
	ListPeers() map[string]*PeerDetails
}

type Resolver interface {
	ARPTable() map[string]string
	Resolve(ip net.IP) (net.HardwareAddr, error)
}
