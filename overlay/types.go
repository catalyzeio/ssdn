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

type PeerState int

const (
	_ PeerState = iota
	Connecting
	Connected
	Inbound
)

func (p PeerState) String() string {
	switch p {
	case Connecting:
		return "connecting"
	case Connected:
		return "connected"
	case Inbound:
		return "inbound"
	default:
		return "unknown"
	}
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

type RegistryConsumer interface {
	UpdatePeers(peerURLs map[string]struct{})
}
