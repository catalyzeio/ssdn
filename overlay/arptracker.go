package overlay

import (
	"bytes"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"unsafe"

	"github.com/catalyzeio/go-core/comm"
)

type ARPResult int

const (
	NotARP ARPResult = iota
	ARPUnsupported
	ARPReply
	ARPIsProcessing
)

type ARPTable map[uint32][]byte

func (t ARPTable) StringMap() map[string]string {
	sm := make(map[string]string)
	for k, v := range t {
		ip := net.IP(comm.IntToIPv4(k))
		mac := net.HardwareAddr(v)
		sm[ip.String()] = mac.String()
	}
	return sm
}

type ARPTracker struct {
	localIP  []byte
	localMAC []byte

	trackersMutex sync.Mutex
	trackers      map[uint32]chan []byte

	table unsafe.Pointer // *ARPTable

	control chan *atRequest
}

type atRequest struct {
	arp *PacketBuffer
}

func NewARPTracker(localIP []byte, localMAC []byte) *ARPTracker {
	initTable := make(ARPTable)

	return &ARPTracker{
		localIP:  localIP,
		localMAC: localMAC,

		trackers: make(map[uint32]chan []byte),

		table: unsafe.Pointer(&initTable),

		control: make(chan *atRequest),
	}
}

func (at *ARPTracker) Start() {
	go at.service()
}

func (at *ARPTracker) Stop() {
	at.control <- nil
}

func (at *ARPTracker) Get() ARPTable {
	pointer := &at.table
	p := (*ARPTable)(atomic.LoadPointer(pointer))
	return *p
}

func (at *ARPTracker) TrackQuery(ip uint32, resolved chan []byte) bool {
	at.trackersMutex.Lock()
	defer at.trackersMutex.Unlock()

	_, present := at.trackers[ip]
	if present {
		return false
	}
	at.trackers[ip] = resolved
	return true
}

func (at *ARPTracker) UntrackQuery(ip uint32) {
	at.trackersMutex.Lock()
	defer at.trackersMutex.Unlock()

	delete(at.trackers, ip)
}

// XXX ARP packet encoding and decoding are done manually to avoid reflection overheads

func (at *ARPTracker) GenerateQuery(packet *PacketBuffer, ip net.IP) error {
	ip = ip.To4()
	if ip == nil {
		return fmt.Errorf("can only generate IPv4 ARP requests")
	}

	// XXX assumes frames have no 802.1q tagging
	targetMAC := []byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF}

	// dest, src
	buff := packet.Data
	copy(buff[0:6], targetMAC)
	copy(buff[6:12], at.localMAC)
	// ethertype
	buff[12] = 0x08
	buff[13] = 0x06

	// hardware type: ethernet
	buff[14] = 0x00
	buff[15] = 0x01
	// protocol type: IPv4
	buff[16] = 0x08
	buff[17] = 0x00
	// lengths
	buff[18] = 0x06
	buff[19] = 0x04
	// opcode: who-has
	buff[20] = 0x00
	buff[21] = 0x01
	// sender addresses
	copy(buff[22:28], at.localMAC)
	copy(buff[28:32], at.localIP)
	// target addresses
	copy(buff[32:38], targetMAC)
	copy(buff[38:42], ip)

	packet.Length = 42

	return nil
}

func (at *ARPTracker) SetDestinationMAC(packet *PacketBuffer, srcMAC []byte) bool {
	trace := log.IsTraceEnabled()

	// XXX assumes frames have no 802.1q tagging
	buff := packet.Data

	// ignore non-IPv4 packets
	if packet.Length < 34 || buff[12] != 0x08 || buff[13] != 0x00 {
		if trace {
			log.Trace("Cannot set destination MAC for non-IPv4 packet")
		}
		return false
	}

	// look up destination MAC based on destination IP
	destIP := buff[30:34]
	key := comm.IPv4ToInt(destIP)
	destMAC, present := at.Get()[key]
	if present {
		copy(buff[0:6], destMAC)
		copy(buff[6:12], srcMAC)
		if trace {
			log.Trace("Destination MAC for %s: %s", net.IP(destIP), net.HardwareAddr(destMAC))
		}
		return true
	}

	if trace {
		log.Trace("Failed to resolve destination MAC for %s", net.IP(destIP))
	}
	return false
}

func (at *ARPTracker) Process(p *PacketBuffer) ARPResult {
	// XXX assumes frames have no 802.1q tagging
	buff := p.Data

	// ignore non-ARP packets
	if p.Length < 42 || buff[12] != 0x08 || buff[13] != 0x06 {
		return NotARP
	}

	// proto: IPv4, 6-byte MAC, 4-byte IP
	if buff[16] != 0x08 || buff[17] != 0x00 || buff[18] != 0x06 || buff[19] != 0x04 {
		return ARPUnsupported
	}

	// opcode
	op1, op2 := buff[20], buff[21]
	if op1 != 0x00 {
		return ARPUnsupported
	}

	// request
	if op2 == 0x01 {
		if log.IsTraceEnabled() {
			log.Trace("Received ARP who-has")
		}
		return at.handleRequest(buff)
	}

	// response
	if op2 == 0x02 {
		if log.IsTraceEnabled() {
			log.Trace("Received ARP is-at")
		}
		at.control <- &atRequest{
			arp: p,
		}
		return ARPIsProcessing
	}

	// unsupported op
	return ARPUnsupported
}

func (at *ARPTracker) handleRequest(buff []byte) ARPResult {
	// check if it is for the local IP
	targetIP := buff[38:42]
	if !bytes.Equal(targetIP, at.localIP) {
		// requests for other IPs are not supported
		return ARPUnsupported
	}
	if log.IsDebugEnabled() {
		log.Debug("Responding to ARP request for IP %s", net.IP(targetIP))
	}

	// transform packet into response
	buff[21] = 0x02

	destMAC := buff[0:6]
	srcMAC := buff[6:12]
	copy(destMAC, srcMAC)
	copy(srcMAC, at.localMAC)

	senderMAC := buff[22:28]
	senderIP := buff[28:32]
	targetMAC := buff[32:38]
	copy(targetIP, senderIP)
	copy(targetMAC, senderMAC)
	copy(senderMAC, at.localMAC)
	copy(senderIP, at.localIP)

	return ARPReply
}

func (at *ARPTracker) service() {
	for {
		req := <-at.control
		if req == nil {
			return
		}

		arp := req.arp
		if arp != nil {
			buff := arp.Data
			senderMAC := make([]byte, 6) // explicit copy is necessary due to buffer reuse
			copy(senderMAC, buff[22:28])
			senderIP := buff[28:32]
			if log.IsDebugEnabled() {
				log.Debug("ARP response: %s is at %s", net.IP(senderIP), net.HardwareAddr(senderMAC))
			}

			ipKey := comm.IPv4ToInt(senderIP)
			if at.isTracking(ipKey, senderMAC) {
				at.set(ipKey, senderMAC)
			}

			arp.Queue <- arp
		}
	}
}

func (at *ARPTracker) isTracking(ipKey uint32, result []byte) bool {
	at.trackersMutex.Lock()
	defer at.trackersMutex.Unlock()

	resolved, present := at.trackers[ipKey]
	if present {
		resolved <- result
		return true
	}
	return false
}

func (at *ARPTracker) set(ipKey uint32, mac []byte) {
	pointer := &at.table
	for {
		// grab current table
		old := atomic.LoadPointer(pointer)
		current := (*ARPTable)(old)

		// copy existing table into new table and add entry
		oldTable := *current

		newTable := make(ARPTable)
		for k, v := range oldTable {
			newTable[k] = v
		}
		newTable[ipKey] = mac

		// replace current table with new table
		new := unsafe.Pointer(&newTable)
		if atomic.CompareAndSwapPointer(pointer, old, new) {
			if log.IsDebugEnabled() {
				log.Debug("New ARP table: %s", newTable.StringMap())
			}
			return
		}
	}
}

func (at *ARPTracker) unset(ipKey uint32) {
	pointer := &at.table
	for {
		// grab current table
		old := atomic.LoadPointer(pointer)
		current := (*ARPTable)(old)

		// copy existing table into new table and skip entry
		oldTable := *current

		newTable := make(ARPTable)
		for k, v := range oldTable {
			if k != ipKey {
				newTable[k] = v
			}
		}

		// replace current table with new table
		new := unsafe.Pointer(&newTable)
		if atomic.CompareAndSwapPointer(pointer, old, new) {
			if log.IsDebugEnabled() {
				log.Debug("New ARP table: %s", newTable.StringMap())
			}
			return
		}
	}
}
