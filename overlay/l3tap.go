package overlay

import (
	"log"
	"time"

	"github.com/catalyzeio/shadowfax/cli"
	"github.com/catalyzeio/taptun"
)

type L3Tap struct {
	bridge *L3Bridge
}

func NewL3Tap(bridge *L3Bridge) *L3Tap {
	return &L3Tap{
		bridge: bridge,
	}
}

func (lt *L3Tap) Start(cli *cli.Listener) error {
	tap, err := lt.createLinkedTap()
	if err != nil {
		return err
	}

	go lt.service(tap)

	return nil
}

func (lt *L3Tap) createLinkedTap() (*taptun.Interface, error) {
	log.Printf("Creating new tap")

	tap, err := taptun.NewTAP(tapNameTemplate)
	if err != nil {
		return nil, err
	}

	name := tap.Name()
	log.Printf("Created layer 3 tap %s\n", name)

	err = lt.bridge.link(name)
	if err != nil {
		tap.Close()
		return nil, err
	}

	return tap, nil
}

func (lt *L3Tap) service(tap *taptun.Interface) {
	for {
		lt.forward(tap)

		for {
			newTap, err := lt.createLinkedTap()
			if err == nil {
				tap = newTap
				break
			}
			log.Printf("Error creating tap: %s; retrying in 1 second\n", err)
			time.Sleep(time.Second)
		}
	}
}

func (lt *L3Tap) forward(tap *taptun.Interface) {
	defer func() {
		tap.Close()
		log.Printf("Closed tap %s\n", tap.Name())
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

	msgBuffer := make([]byte, MaxPacketSize)

	for {
		// read whole packet from tap
		len, err := tap.Read(msgBuffer)
		if err != nil {
			log.Printf("Error reading from tap: %s", err)
			return
		}
		log.Printf("Read %d bytes\n", len)
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
