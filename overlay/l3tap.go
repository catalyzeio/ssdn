package overlay

import (
	"crypto/rand"
	"fmt"
	"net"
	"time"

	"github.com/catalyzeio/shadowfax/cli"
	"github.com/catalyzeio/taptun"
)

type L3Tap struct {
	gwIP  net.IP
	gwMAC net.HardwareAddr

	bridge     *L3Bridge
	routes     *RouteTracker
	arpTracker *ARPTracker

	free PacketQueue
	out  PacketQueue

	freeARP PacketQueue
	outARP  PacketQueue
}

const (
	tapQueueSize = 256
)

func NewL3Tap(gwIP net.IP, mtu uint16, bridge *L3Bridge, routes *RouteTracker) (*L3Tap, error) {
	var gwMAC []byte
	gwMAC, err := RandomMAC()
	if err != nil {
		return nil, err
	}
	log.Info("Virtual gateway: %s at %s", gwIP, net.HardwareAddr(gwMAC))

	free := AllocatePacketQueue(tapQueueSize, ethernetHeaderSize+int(mtu))
	out := make(PacketQueue, tapQueueSize)

	const arpQueueSize = 16
	freeARP := AllocatePacketQueue(arpQueueSize, ethernetHeaderSize+int(mtu))
	outARP := make(PacketQueue, arpQueueSize)

	return &L3Tap{
		gwIP:  gwIP,
		gwMAC: gwMAC,

		bridge: bridge,

		free: free,
		out:  out,

		freeARP: freeARP,
		outARP:  outARP,

		routes: routes,
	}, nil
}

func (t *L3Tap) Start(cli *cli.Listener) error {
	tap, err := t.createLinkedTap()
	if err != nil {
		return err
	}

	arpTracker := NewARPTracker(t.gwIP, t.gwMAC)
	arpTracker.Start()
	t.arpTracker = arpTracker

	cli.Register("arp", "", "Shows current ARP table", 0, 0, t.cliARPTable)
	cli.Register("resolve", "", "Forces IP to MAC address resolution", 1, 1, t.cliResolve)

	go t.service(tap)

	return nil
}

func (t *L3Tap) cliARPTable(args ...string) (string, error) {
	table := t.arpTracker.Get()
	return fmt.Sprintf("ARP table: %s", mapValues(table.StringMap())), nil
}

func (t *L3Tap) cliResolve(args ...string) (string, error) {
	ipString := args[0]

	ip := net.ParseIP(ipString)
	if ip == nil {
		return "", fmt.Errorf("invalid IP address: %s", ipString)
	}

	mac, err := t.Resolve(ip)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s is at %s", ip, mac), nil
}

func (t *L3Tap) SeedMAC(ip uint32, mac net.HardwareAddr) {
	t.arpTracker.set(ip, mac)
}

func (t *L3Tap) UnseedMAC(ip uint32) {
	t.arpTracker.unset(ip)
}

func (t *L3Tap) Resolve(ip net.IP) (net.HardwareAddr, error) {
	ip = ip.To4()
	if ip == nil {
		return nil, fmt.Errorf("can only resolve IPv4 addresses")
	}

	arpTracker := t.arpTracker

	resolved := make(chan []byte, 1)
	if !arpTracker.TrackQuery(ip, resolved) {
		return nil, fmt.Errorf("already resolving %s", ip)
	}
	defer arpTracker.UntrackQuery(ip)

	freeARP := t.freeARP
	outARP := t.outARP

	for i := 0; i < 3; i++ {
		// grab a free packet
		p := <-freeARP
		// generate the request
		err := arpTracker.GenerateQuery(p, ip)
		if err != nil {
			p.Queue <- p
			return nil, err
		}
		// send the request
		outARP <- p
		if log.IsDebugEnabled() {
			log.Debug("Sent ARP request for %s", ip)
		}
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

func (t *L3Tap) InboundHandler(packet *PacketBuffer) error {
	t.out <- packet
	return nil
}

func (t *L3Tap) createLinkedTap() (*taptun.Interface, error) {
	if log.IsDebugEnabled() {
		log.Debug("Creating new tap")
	}

	const tapNameTemplate = "sf3.tap%d"
	tap, err := taptun.NewTAP(tapNameTemplate)
	if err != nil {
		return nil, err
	}

	name := tap.Name()
	log.Info("Created layer 3 tap %s", name)

	err = t.bridge.link(name)
	if err != nil {
		tap.Close()
		return nil, err
	}

	return tap, err
}

func (t *L3Tap) service(tap *taptun.Interface) {
	for {
		t.forward(tap)

		for {
			time.Sleep(time.Second)
			newTap, err := t.createLinkedTap()
			if err == nil {
				tap = newTap
				break
			}
			log.Warn("Failed to create tap: %s", err)
		}
	}
}

func (t *L3Tap) forward(tap *taptun.Interface) {
	defer func() {
		tap.Close()
		log.Info("Closed tap %s", tap.Name())
	}()

	done := make(chan struct{}, 2)

	go t.tapReader(tap, done)
	go t.tapWriter(tap, done)

	<-done
}

func (t *L3Tap) tapReader(tap *taptun.Interface, done chan<- struct{}) {
	defer func() {
		done <- struct{}{}
	}()

	trace := log.IsTraceEnabled()

	free := t.free
	outARP := t.outARP
	arpTracker := t.arpTracker
	routes := t.routes

	for {
		// grab a free packet
		p := <-free

		// read whole packet from tap
		n, err := tap.Read(p.Data)
		if err != nil {
			log.Warn("Failed to read from tap: %s", err)
			p.Queue <- p
			return
		}
		if trace {
			log.Trace("Read %d bytes from tap", n)
		}
		p.Length = n

		// process any ARP traffic
		switch arpTracker.Process(p) {
		case ARPReply:
			// tracker responded to an ARP query; send to output
			outARP <- p
			continue
		case ARPIsProcessing:
			// tracker is processing and will return buffer when done
			continue
		case ARPUnsupported:
			// ignore, return packet, and continue
			p.Queue <- p
			continue
		case NotARP:
			// process packet normally
		}

		// TODO reply to ICMP traffic

		// route packet to its destination queue
		routes.RoutePacket(p)
	}
}

func (t *L3Tap) tapWriter(tap *taptun.Interface, done chan<- struct{}) {
	defer func() {
		done <- struct{}{}
	}()

	trace := log.IsTraceEnabled()

	arpTracker := t.arpTracker
	out := t.out
	outARP := t.outARP

	for {
		// grab next outgoing packet
		var p *PacketBuffer
		select {
		case p = <-out:
			// attach MAC addresses based on destination IP
			if !arpTracker.SetDestinationMAC(p, t.gwMAC) {
				p.Queue <- p
				continue
			}
			// send adjusted frame
		case p = <-outARP:
			// send ARP as-is
		}

		// write next outgoing packet
		frame := p.Data[:p.Length]
		n, err := tap.Write(frame)
		if err != nil {
			log.Warn("Failed to relay packet to tap: %s", err)
			p.Queue <- p
			return
		}
		if trace {
			log.Trace("Wrote %d bytes to tap", n)
		}

		// return packet to its owner
		p.Queue <- p
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
