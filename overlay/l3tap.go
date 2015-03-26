package overlay

import (
	"crypto/rand"
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

	free chan *PacketBuffer
	out  chan *PacketBuffer

	arpTracker *ARPTracker
}

func NewL3Tap(gwIP net.IP, mtu uint16, bridge *L3Bridge) (*L3Tap, error) {
	var gwMAC []byte
	gwMAC, err := RandomMAC()
	if err != nil {
		return nil, err
	}
	log.Printf("Virtual gateway: %s at %s", gwIP, net.HardwareAddr(gwMAC))

	// TODO determine an appropriate number here
	const numPackets = 2
	free := make(chan *PacketBuffer, numPackets)
	out := make(chan *PacketBuffer, numPackets)
	for _, v := range NewPacketBuffers(numPackets, int(mtu)) {
		free <- &v
	}

	return &L3Tap{
		gwIP:  gwIP,
		gwMAC: gwMAC,

		bridge: bridge,

		free: free,
		out:  out,
	}, nil
}

func (lt *L3Tap) Start(cli *cli.Listener) error {
	tap, iface, err := lt.createLinkedTap()
	if err != nil {
		return err
	}

	tracker := NewARPTracker(lt.gwIP, lt.gwMAC)
	tracker.Start()
	lt.arpTracker = tracker

	go lt.service(tap, iface)

	return nil
}

func (lt *L3Tap) createLinkedTap() (*taptun.Interface, *net.Interface, error) {
	log.Printf("Creating new tap")

	tap, err := taptun.NewTAP(tapNameTemplate)
	if err != nil {
		return nil, nil, err
	}

	name := tap.Name()
	log.Printf("Created layer 3 tap %s", name)

	err = lt.bridge.link(name)
	if err != nil {
		tap.Close()
		return nil, nil, err
	}

	iface, err := net.InterfaceByName(name)
	if err != nil {
		tap.Close()
		return nil, nil, err
	}

	return tap, iface, err
}

func (lt *L3Tap) service(tap *taptun.Interface, iface *net.Interface) {
	for {
		lt.forward(tap, iface)

		for {
			time.Sleep(time.Second)
			newTap, newIface, err := lt.createLinkedTap()
			if err == nil {
				tap = newTap
				iface = newIface
				break
			}
			log.Printf("Error creating tap: %s", err)
		}
	}
}

func (lt *L3Tap) forward(tap *taptun.Interface, iface *net.Interface) {
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
	out := lt.out
	arpTracker := lt.arpTracker

	for {
		// grab a free packet
		p := <-free

		// read whole packet from tap
		n, err := tap.Read(p.Data)
		if err != nil {
			log.Printf("Error reading from tap: %s", err)
			return
		}
		log.Printf("Read %d bytes", n)
		p.Length = n

		// process any ARP traffic
		switch arpTracker.Process(p, free) {
		case ARPReply:
			// tracker responded to an ARP query; send to output
			out <- p
			continue
		case ARPProcessing:
			// tracker is processing and will return buffer when done
			continue
		case ARPUnsupported:
			// ignore and continue
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
	defer func() {
		done <- true
	}()

	out := lt.out
	free := lt.free

	for {
		// grab next outgoing packet
		p := <-out

		// write next outgoing packet
		message := p.Data[:p.Length]
		n, err := tap.Write(message)
		if err != nil {
			log.Printf("Error relaying message to tap: %s", err)
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
