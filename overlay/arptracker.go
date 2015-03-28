package overlay

import (
	"bytes"
	"fmt"
	"net"
	"sync"
)

type ARPResult int

const (
	NotARP ARPResult = iota
	ARPUnsupported
	ARPReply
	ARPIsProcessing
)

type ARPTable map[uint32][]byte

type ARPTracker struct {
	localIP  []byte
	localMAC []byte

	trackersMutex sync.Mutex
	trackers      map[uint32]chan []byte

	control chan *atRequest
}

type atRequest struct {
	snapshot chan ARPTable

	listener chan ARPTable
	add      bool

	entryIP  []byte
	entryMAC []byte

	arp *PacketBuffer
}

func NewARPTracker(localIP []byte, localMAC []byte) *ARPTracker {
	return &ARPTracker{
		localIP:  localIP,
		localMAC: localMAC,

		trackers: make(map[uint32]chan []byte),

		control: make(chan *atRequest),
	}
}

func (at *ARPTracker) Start() {
	go at.service()
}

func (at *ARPTracker) Stop() {
	at.control <- nil
}

func (at *ARPTracker) Snapshot() ARPTable {
	snapshot := make(chan ARPTable, 1)
	at.control <- &atRequest{
		snapshot: snapshot,
	}
	return <-snapshot
}

func (at *ARPTracker) AddListener(listener chan ARPTable) {
	at.control <- &atRequest{
		listener: listener,
		add:      true,
	}
}

func (at *ARPTracker) RemoveListener(listener chan ARPTable) {
	at.control <- &atRequest{
		listener: listener,
		add:      false,
	}
}

func (at *ARPTracker) Seed(ip net.IP, mac []byte) {
	at.control <- &atRequest{
		entryIP:  ip,
		entryMAC: mac,
	}
}

func (at *ARPTracker) TrackQuery(ip net.IP, resolved chan []byte) bool {
	key := IPv4ToInt(ip)

	at.trackersMutex.Lock()
	defer at.trackersMutex.Unlock()

	_, present := at.trackers[key]
	if present {
		return false
	}
	at.trackers[key] = resolved
	return true
}

func (at *ARPTracker) UntrackQuery(ip net.IP) {
	key := IPv4ToInt(ip)

	at.trackersMutex.Lock()
	defer at.trackersMutex.Unlock()

	delete(at.trackers, key)
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

func (at *ARPTracker) Process(packet *PacketBuffer) ARPResult {
	// XXX assumes frames have no 802.1q tagging
	buff := packet.Data

	// ignore non-ARP packets
	if packet.Length < 42 || buff[12] != 0x08 || buff[13] != 0x06 {
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
			arp: packet,
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
	// copied and replaced when any changes are made
	table := make(ARPTable)
	listeners := make(map[chan ARPTable]interface{})

	// TODO initialize ARP table with broadcast entries

	for {
		req := <-at.control
		if req == nil {
			return
		}

		// process snapshot requests
		snapshot := req.snapshot
		if snapshot != nil {
			snapshot <- table
		}

		// process listener requests
		listener := req.listener
		if listener != nil {
			if req.add {
				listeners[listener] = nil
				listener <- table
			} else {
				delete(listeners, listener)
			}
		}

		// process seed requests
		entryIP := req.entryIP
		if entryIP != nil {
			table = table.modify(listeners, IPv4ToInt(entryIP), req.entryMAC)
		}

		// process ARP responses
		arp := req.arp
		if arp != nil {
			buff := arp.Data
			senderMAC := make([]byte, 6) // explicit copy is necessary due to buffer reuse
			copy(senderMAC, buff[22:28])
			senderIP := buff[28:32]
			if log.IsDebugEnabled() {
				log.Debug("ARP response: %s is at %s", net.IP(senderIP), net.HardwareAddr(senderMAC))
			}

			ipKey := IPv4ToInt(senderIP)
			if at.isTracking(ipKey, senderMAC) {
				table = table.modify(listeners, ipKey, senderMAC)
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

// This method requires a 4-byte IP address to function properly.
// Use ip.To4() if the IPv4 address may have been encoded with 16 bytes.
func IPv4ToInt(ip []byte) uint32 {
	return uint32(ip[0])<<24 | uint32(ip[1])<<16 | uint32(ip[2])<<8 | uint32(ip[3])
}

// Reverse operation of IPv4ToInt.
func IntToIPv4(ip uint32) []byte {
	return []byte{byte(ip >> 24), byte(ip >> 16), byte(ip >> 8), byte(ip)}
}

func (t ARPTable) SetDestinationMAC(packet *PacketBuffer, srcMAC []byte) bool {
	// XXX assumes frames have no 802.1q tagging
	buff := packet.Data

	// ignore non-IPv4 packets
	if packet.Length < 34 || buff[12] != 0x08 || buff[13] != 0x00 {
		return false
	}

	// look up destination MAC based on destination IP
	destIP := buff[30:34]
	key := IPv4ToInt(destIP)
	destMAC, present := t[key]
	if present {
		copy(buff[0:6], destMAC)
		copy(buff[6:12], srcMAC)
		return true
	}
	return false
}

func (t ARPTable) StringMap() map[string]string {
	sm := make(map[string]string)
	for k, v := range t {
		ip := net.IP(IntToIPv4(k))
		mac := net.HardwareAddr(v)
		sm[ip.String()] = mac.String()
	}
	return sm
}

func (t ARPTable) modify(listeners map[chan ARPTable]interface{}, newKey uint32, newMAC []byte) ARPTable {
	// copy existing table and response into new table
	newTable := make(ARPTable)
	for k, v := range t {
		newTable[k] = v
	}
	newTable[newKey] = newMAC

	// fire off notifications for updated table
	for k, _ := range listeners {
		k <- newTable
	}
	return newTable
}
