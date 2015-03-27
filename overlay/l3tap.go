package overlay

import (
	"crypto/rand"
	"fmt"
	"log"
	"net"
	"time"

	"github.com/catalyzeio/shadowfax/cli"
	"github.com/catalyzeio/taptun"
)

type L3Tap struct {
	gwIP  net.IP
	gwMAC net.HardwareAddr

	bridge *L3Bridge

	free       chan *PacketBuffer
	outFrames  chan *PacketBuffer
	outPackets chan *PacketBuffer

	arpTracker *ARPTracker
}

func NewL3Tap(gwIP net.IP, bridge *L3Bridge) (*L3Tap, error) {
	var gwMAC []byte
	gwMAC, err := RandomMAC()
	if err != nil {
		return nil, err
	}
	log.Printf("Virtual gateway: %s at %s", gwIP, net.HardwareAddr(gwMAC))

	// add just enough packets for local traffic (ARP, etc)
	const numPackets = 16
	free := make(chan *PacketBuffer, numPackets)
	outFrames := make(chan *PacketBuffer, numPackets)
	outPackets := make(chan *PacketBuffer, numPackets)
	for _, v := range NewPacketBuffers(numPackets, MaxPacketSize) {
		free <- &v
	}

	return &L3Tap{
		gwIP:  gwIP,
		gwMAC: gwMAC,

		bridge: bridge,

		free:       free,
		outFrames:  outFrames,
		outPackets: outPackets,
	}, nil
}

func (lt *L3Tap) Start(cli *cli.Listener) error {
	tap, err := lt.createLinkedTap()
	if err != nil {
		return err
	}

	arpTracker := NewARPTracker(lt.gwIP, lt.gwMAC)
	arpTracker.Start()
	lt.arpTracker = arpTracker

	cli.Register("arp", "", "Shows current ARP table", 0, 0, lt.cliARPTable)
	cli.Register("resolve", "", "Forces IP to MAC address resolution", 1, 1, lt.cliResolve)

	go lt.service(tap)

	return nil
}

func (lt *L3Tap) cliARPTable(args ...string) (string, error) {
	table := lt.arpTracker.Snapshot()
	return fmt.Sprintf("ARP table: %s", mapValues(table.StringMap())), nil
}

func (lt *L3Tap) cliResolve(args ...string) (string, error) {
	ipString := args[0]

	ip := net.ParseIP(ipString)
	if ip == nil {
		return "", fmt.Errorf("invalid IP address: %s", ipString)
	}

	mac, err := lt.Resolve(ip)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s is at %s", ip, mac), nil
}

func (lt *L3Tap) Resolve(ip net.IP) (net.HardwareAddr, error) {
	ip = ip.To4()
	if ip == nil {
		return nil, fmt.Errorf("can only resolve IPv4 addresses")
	}

	arpTracker := lt.arpTracker

	resolved := make(chan []byte, 1)
	if !arpTracker.TrackQuery(ip, resolved) {
		return nil, fmt.Errorf("already resolving %s", ip)
	}
	defer arpTracker.UntrackQuery(ip)

	free := lt.free
	outFrames := lt.outFrames

	for i := 0; i < 3; i++ {
		// grab a free packet
		p := <-free
		// generate the request
		err := arpTracker.GenerateQuery(p, ip)
		if err != nil {
			return nil, err
		}
		// send the request
		outFrames <- p
		log.Printf("Sent ARP request for %s", ip)
		// wait up to a second for the response
		select {
		case response := <-resolved:
			return net.HardwareAddr(response), nil
		case <-time.After(time.Second):
			// resend
		}
	}

	return nil, fmt.Errorf("failed to resolve %s", ip)
}

func (lt *L3Tap) createLinkedTap() (*taptun.Interface, error) {
	log.Printf("Creating new tap")

	tap, err := taptun.NewTAP(tapNameTemplate)
	if err != nil {
		return nil, err
	}

	name := tap.Name()
	log.Printf("Created layer 3 tap %s", name)

	err = lt.bridge.link(name)
	if err != nil {
		tap.Close()
		return nil, err
	}

	return tap, err
}

func (lt *L3Tap) service(tap *taptun.Interface) {
	for {
		lt.forward(tap)

		for {
			time.Sleep(time.Second)
			newTap, err := lt.createLinkedTap()
			if err == nil {
				tap = newTap
				break
			}
			log.Printf("Error creating tap: %s", err)
		}
	}
}

func (lt *L3Tap) forward(tap *taptun.Interface) {
	defer func() {
		tap.Close()
		log.Printf("Closed tap %s", tap.Name())
	}()

	done := make(chan bool, 2)

	go lt.tapReader(tap, done)
	go lt.tapWriter(tap, done)

	<-done
}

func (lt *L3Tap) tapReader(tap *taptun.Interface, done chan<- bool) {
	defer func() {
		done <- true
	}()

	free := lt.free
	outFrames := lt.outFrames
	arpTracker := lt.arpTracker

	for {
		// grab a free packet
		p := <-free

		// read whole packet from tap
		n, err := tap.Read(p.Data)
		if err != nil {
			log.Printf("Error reading from tap: %s", err)
			// bail on error, but return packet to free queue first
			free <- p
			return
		}
		log.Printf("Read %d bytes", n)
		p.Length = n

		// process any ARP traffic
		switch arpTracker.Process(p, free) {
		case ARPReply:
			// tracker responded to an ARP query; send to output
			outFrames <- p
			continue
		case ARPIsProcessing:
			// tracker is processing and will return buffer when done
			continue
		case ARPUnsupported:
			// ignore, return packet, and continue
			free <- p
			continue
		case NotARP:
			// process packet normally
		}

		// TODO packet routing

		// return packet to free queue
		free <- p
	}
}

func (lt *L3Tap) tapWriter(tap *taptun.Interface, done chan<- bool) {
	var macChanges chan ARPTable
	defer func() {
		done <- true
		if macChanges != nil {
			lt.arpTracker.RemoveListener(macChanges)
		}
	}()

	var macTable ARPTable
	macChanges = make(chan ARPTable, 8)
	lt.arpTracker.AddListener(macChanges)

	outFrames := lt.outFrames
	outPackets := lt.outPackets
	free := lt.free

	for {
		// grab next outgoing packet
		var p *PacketBuffer
		select {
		case macTable = <-macChanges:
			// continue with new MAC lookup table
			continue
		case p = <-outPackets:
			// attach MAC addresses based on destination IP
			if macTable == nil || !macTable.SetDestinationMAC(p, lt.gwMAC) {
				free <- p
				continue
			}
			// send adjusted frame
		case p = <-outFrames:
			// send frame as-is
		}

		// write next outgoing packet
		message := p.Data[:p.Length]
		n, err := tap.Write(message)
		if err != nil {
			log.Printf("Error relaying message to tap: %s", err)
			// bail on error, but return packet to free queue first
			free <- p
			return
		}
		log.Printf("Wrote %d bytes", n)

		// return packet to free queue
		free <- p
	}
}

func RandomMAC() (net.HardwareAddr, error) {
	address := make([]byte, 6)
	_, err := rand.Read(address)
	if err != nil {
		return nil, err
	}
	// clear multicast and set local assignment bits
	address[0] &= 0xFE
	address[0] |= 0x02
	return net.HardwareAddr(address), nil
}
