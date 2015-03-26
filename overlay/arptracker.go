package overlay

import (
	"net"
	"sync"
)

type ARPResult int

const (
	NotARP ARPResult = iota
	ARPUnsupported
	ARPReply
	ARPProcessing
)

type ARPTable map[int][]byte

type ARPTracker struct {
	localIP  []byte
	localMAC []byte

	listenersMutex sync.Mutex
	listeners      map[chan ARPTable]interface{}

	trackersMutex sync.Mutex
	trackers      map[int]chan bool

	control chan *arpMessage
}

type arpMessage struct {
	arp       *PacketBuffer
	processed chan *PacketBuffer
}

func NewARPTracker(localIP []byte, localMAC []byte) *ARPTracker {
	return &ARPTracker{
		localIP:  localIP,
		localMAC: localMAC,

		listeners: make(map[chan ARPTable]interface{}),

		control: make(chan *arpMessage),
	}
}

func (a *ARPTracker) Start() {
	go a.service()
}

func (a *ARPTracker) Stop() {
	a.control <- nil
}

func (a *ARPTracker) AddListener(listener chan ARPTable) {
	a.listenersMutex.Lock()
	defer a.listenersMutex.Unlock()

	a.listeners[listener] = nil
}

func (a *ARPTracker) RemoveListener(listener chan ARPTable) {
	a.listenersMutex.Lock()
	defer a.listenersMutex.Unlock()

	delete(a.listeners, listener)
}

func (a *ARPTracker) TrackQuery(ip net.IP, resolved chan bool) bool {
	key := IPv4ToInt(ip)

	a.trackersMutex.Lock()
	defer a.trackersMutex.Unlock()

	_, present := a.trackers[key]
	if present {
		return false
	}
	a.trackers[key] = resolved
	return true
}

func (a *ARPTracker) UntrackQuery(ip net.IP) {
	key := IPv4ToInt(ip)

	a.trackersMutex.Lock()
	defer a.trackersMutex.Unlock()

	delete(a.trackers, key)
}

func (a *ARPTracker) Process(packet *PacketBuffer, processed chan *PacketBuffer) ARPResult {
	// XXX assumes frames have no 802.1q tagging
	buff := packet.Data

	// TODO reply to ICMP traffic

	// ignore non-ARP packets
	if packet.Length < 42 || buff[12] != 0x08 || buff[13] != 0x06 {
		return NotARP
	}

	// TODO
	return ARPUnsupported
}

func (a *ARPTracker) service() {
	for {
		req := <-a.control
		if req == nil {
			return
		}

		arp := req.arp
		// TODO process ARP
		req.processed <- arp
	}
}

func IPv4ToInt(ip net.IP) int {
	return int(ip[0])<<24 | int(ip[1])<<16 | int(ip[2])<<8 | int(ip[3])
}
