package overlay

import (
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

	const tunNameTemplate = "sf3.tun%d"
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
		if !t.forward(tun) {
			return
		}

		for {
			time.Sleep(time.Second)
			newTun, err := t.createTun()
			if err == nil {
				tun = newTun
				break
			}
			log.Warn("Failed to create tun: %s", err)
		}
	}
}

func (t *L3Tun) forward(tun *taptun.Interface) bool {
	defer func() {
		if err := tun.Close(); err != nil {
			log.Warn("Failed to close tun: %s", err)
		} else {
			log.Info("Closed tun %s", tun.Name())
		}
	}()

	routes := t.tuns.routes
	routes.Add(t.route)
	defer routes.Remove(t.route)

	done := make(chan struct{}, 2)

	go t.tunReader(tun, done)
	go t.tunWriter(tun, done)

	for {
		select {
		case <-done:
			return true
		case <-t.control:
			return false
		}
	}
}

// XXX The packet buffers below leave space for an Ethernet frame, even though
// tun devices do not include layer 2 framing. The additional (empty) offsets
// are for compatibility with the existing L3Relay code.

func (t *L3Tun) tunReader(tun *taptun.Interface, done chan<- struct{}) {
	defer func() {
		done <- struct{}{}
	}()

	trace := log.IsTraceEnabled()

	free := t.free
	routes := t.tuns.routes

	for {
		// grab a free packet
		p := <-free

		// XXX On some Linux kernel versions, the following read call will stay
		// blocked even if the underlying tun file descriptor is closed. The call
		// will eventually return the next time a packet is written to the interface.

		// read whole packet from tun (skipping ethernet header)
		buff := p.Data
		n, err := tun.Read(buff[ethernetHeaderSize:])
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

func (t *L3Tun) tunWriter(tun *taptun.Interface, done chan<- struct{}) {
	defer func() {
		done <- struct{}{}
	}()

	trace := log.IsTraceEnabled()

	out := t.out

	for {
		// grab next outgoing packet
		p := <-out

		// write next outgoing packet (skipping ethernet header)
		payload := p.Data[ethernetHeaderSize:p.Length]
		n, err := tun.Write(payload)
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
