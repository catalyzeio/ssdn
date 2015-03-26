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
	bridge *L3Bridge
	mac    []byte
}

func NewL3Tap(bridge *L3Bridge) *L3Tap {
	return &L3Tap{
		bridge: bridge,
	}
}

func (lt *L3Tap) Start(cli *cli.Listener) error {
	tap, iface, err := lt.createLinkedTap()
	if err != nil {
		return err
	}

	mac, err := RandomMAC()
	if err != nil {
		return err
	}
	log.Printf("Layer 3 tap gateway MAC: %s", mac)
	lt.mac = mac

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

	buff := make([]byte, MaxPacketSize)

	for {
		// read whole packet from tap
		len, err := tap.Read(buff)
		if err != nil {
			log.Printf("Error reading from tap: %s", err)
			return
		}
		log.Printf("Read %d bytes", len)

		// XXX the following code assumes frames have no 802.1q tagging

		// check for ARP request
		if len >= 42 && buff[12] == 0x08 && buff[13] == 0x06 {
			// TODO ARP request/response handlers
			log.Printf("Got an arp")
		}
	}
}

func (lt *L3Tap) tapWriter(tap *taptun.Interface, done chan<- bool) {
	defer func() {
		done <- true
	}()

	for {
		// TODO
		time.Sleep(time.Hour)
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
