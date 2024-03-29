package overlay

import (
	"fmt"
	"io"
	"net"
	"time"

	"github.com/catalyzeio/go-core/comm"
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

func (t *L3Tap) Start() error {
	tap, err := t.createLinkedTap()
	if err != nil {
		return err
	}

	arpTracker := NewARPTracker(t.gwIP, t.gwMAC)
	arpTracker.Start()
	t.arpTracker = arpTracker

	go t.service(tap)

	return nil
}

func (t *L3Tap) ARPTable() map[string]string {
	table := t.arpTracker.Get()
	return table.StringMap()
}

func (t *L3Tap) SeedMAC(ip uint32, mac net.HardwareAddr) {
	t.arpTracker.set(ip, mac)
}

func (t *L3Tap) UnseedMAC(ip uint32) {
	t.arpTracker.unset(ip)
}

func (t *L3Tap) Resolve(ip net.IP) (net.HardwareAddr, error) {
	ipVal, err := comm.IPToInt(ip)
	if err != nil {
		return nil, err
	}

	arpTracker := t.arpTracker

	resolved := make(chan []byte, 1)
	if !arpTracker.TrackQuery(ipVal, resolved) {
		return nil, fmt.Errorf("already resolving %s", ip)
	}
	defer arpTracker.UntrackQuery(ipVal)

	freeARP := t.freeARP
	outARP := t.outARP

	for i := 0; i < 3; i++ {
		// grab a free packet
		p := <-freeARP

		// generate the request
		if err := arpTracker.GenerateQuery(p, ip); err != nil {
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

	const tapNameTemplate = "sl3.tap%d"
	tap, err := taptun.NewTAP(tapNameTemplate)
	if err != nil {
		return nil, err
	}

	name := tap.Name()
	log.Info("Created layer 3 tap %s", name)

	if err := t.bridge.link(name); err != nil {
		tap.Close()
		return nil, err
	}

	return tap, err
}

func (t *L3Tap) service(tap *taptun.Interface) {
	for {
		t.forward(tap)

		for {
			time.Sleep(5 * time.Second)
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
		if err := tap.Close(); err != nil {
			log.Warn("Failed to close tap: %s", err)
		} else {
			log.Info("Closed tap %s", tap.Name())
		}
	}()

	acc, err := tap.Accessor()
	if err != nil {
		log.Warn("Failed to initialize tap: %s", err)
		return
	}
	defer acc.Stop()

	cancel := make(chan struct{})
	defer close(cancel)

	done := make(chan struct{}, 2)

	go t.tapReader(acc, done, cancel)
	go t.tapWriter(acc, done, cancel)

	<-done
}

func (t *L3Tap) tapReader(acc taptun.Accessor, done chan<- struct{}, cancel <-chan struct{}) {
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
		var p *PacketBuffer
		select {
		case <-cancel:
			return
		case p = <-free:
		}

		// read whole packet from tap
		n, err := acc.Read(p.Data)
		if err == io.EOF {
			p.Queue <- p
			return
		}
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

func (t *L3Tap) tapWriter(acc taptun.Accessor, done chan<- struct{}, cancel <-chan struct{}) {
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
		case <-cancel:
			return
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
		n, err := acc.Write(frame)
		if err == io.EOF {
			p.Queue <- p
			return
		}
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
