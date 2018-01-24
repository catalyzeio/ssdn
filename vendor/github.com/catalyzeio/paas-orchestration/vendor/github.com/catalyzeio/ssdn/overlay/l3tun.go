package overlay

import (
	"io"
	"time"

	"github.com/catalyzeio/taptun"
)

type L3Tun struct {
	container string
	ip        uint32
	route     *IPv4Route

	tuns *L3Tuns

	free PacketQueue
	out  PacketQueue

	control chan struct{}
}

func NewL3Tun(container string, ip uint32, tuns *L3Tuns) *L3Tun {
	const tunQueueSize = tapQueueSize
	free := AllocatePacketQueue(tunQueueSize, ethernetHeaderSize+int(tuns.mtu))
	out := make(PacketQueue, tunQueueSize)

	return &L3Tun{
		container: container,
		ip:        ip,
		route:     &IPv4Route{ip, 0xFFFFFFFF, out},

		tuns: tuns,

		free: free,
		out:  out,

		control: make(chan struct{}, 1),
	}
}

func (t *L3Tun) Start() error {
	tun, err := t.createTun()
	if err != nil {
		return err
	}

	go t.service(tun)

	return nil
}

func (t *L3Tun) Stop() {
	t.control <- struct{}{}
}

func (t *L3Tun) createTun() (*taptun.Interface, error) {
	if log.IsDebugEnabled() {
		log.Debug("Creating new tun")
	}

	const tunNameTemplate = "sl3.tun%d"
	tun, err := taptun.NewTUN(tunNameTemplate)
	if err != nil {
		return nil, err
	}

	name := tun.Name()
	log.Info("Created layer 3 tun %s", name)

	if err := t.tuns.inject(t.container, name, t.ip); err != nil {
		tun.Close()
		return nil, err
	}

	return tun, nil
}

func (t *L3Tun) service(tun *taptun.Interface) {
	for {
		if !ForwardL3Tun(tun, t.route, t.tuns.routes, t.free, t.out, t.control) {
			return
		}

		for {
			select {
			case <-t.control:
				return
			case <-time.After(5 * time.Second):
			}
			newTun, err := t.createTun()
			if err == nil {
				tun = newTun
				break
			}
			log.Warn("Failed to create tun: %s", err)
		}
	}
}

func ForwardL3Tun(tun *taptun.Interface, route *IPv4Route, routes *RouteTracker, free PacketQueue, out PacketQueue, abort <-chan struct{}) bool {
	defer func() {
		if err := tun.Close(); err != nil {
			log.Warn("Failed to close tun: %s", err)
		} else {
			log.Info("Closed tun %s", tun.Name())
		}
	}()

	routes.Add(route)
	defer routes.Remove(route)

	acc, err := tun.Accessor()
	if err != nil {
		log.Warn("Failed to initialize tun: %s", err)
		return true
	}
	defer acc.Stop()

	cancel := make(chan struct{})
	defer close(cancel)

	done := make(chan struct{}, 2)

	go tunReader(acc, free, routes, done, cancel)
	go tunWriter(acc, out, done, cancel)

	for {
		select {
		case <-done:
			return true
		case <-abort:
			return false
		}
	}
}

/*
XXX The packet buffers below leave space for an Ethernet frame, even
though tun devices do not include layer 2 framing. The additional
(empty) offsets are for compatibility with the existing L3Relay code.
*/

func tunReader(acc taptun.Accessor, free PacketQueue, routes *RouteTracker, done chan<- struct{}, cancel <-chan struct{}) {
	defer func() {
		done <- struct{}{}
	}()

	trace := log.IsTraceEnabled()

	for {
		// grab a free packet
		var p *PacketBuffer
		select {
		case <-cancel:
			return
		case p = <-free:
		}

		// read whole packet from tun (skipping ethernet header)
		buff := p.Data
		n, err := acc.Read(buff[ethernetHeaderSize:])
		if err == io.EOF {
			p.Queue <- p
			return
		}
		if err != nil {
			log.Warn("Failed to read from tun: %s", err)
			p.Queue <- p
			return
		}
		if trace {
			log.Trace("Read %d bytes from tun", n)
		}

		// set length to indicate a blank MAC frame with an IPv4 ethertype
		p.Length = ethernetHeaderSize + n
		buff[12] = 0x08
		buff[13] = 0x00

		// route packet to its destination queue
		routes.RoutePacket(p)
	}
}

func tunWriter(acc taptun.Accessor, out PacketQueue, done chan<- struct{}, cancel <-chan struct{}) {
	defer func() {
		done <- struct{}{}
	}()

	trace := log.IsTraceEnabled()

	for {
		// grab next outgoing packet
		var p *PacketBuffer
		select {
		case <-cancel:
			return
		case p = <-out:
		}

		// write next outgoing packet (skipping ethernet header)
		payload := p.Data[ethernetHeaderSize:p.Length]
		n, err := acc.Write(payload)
		if err == io.EOF {
			p.Queue <- p
			return
		}
		if err != nil {
			log.Warn("Failed to relay packet to tun: %s", err)
			p.Queue <- p
			return
		}
		if trace {
			log.Trace("Wrote %d bytes to tun", n)
		}

		// return packet to its owner
		p.Queue <- p
	}
}
