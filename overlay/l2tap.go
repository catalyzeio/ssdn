package overlay

import (
	"bufio"
	"io"
	"log"
	"net"

	"github.com/songgao/water"
)

const (
	MaxPacketSize = 1 << 15 // 32 KiB
)

type L2Tap struct {
	name string
	tap  *water.Interface
}

const (
	bufSize = 1 << 18 // 64 KiB
)

func NewL2Tap() (*L2Tap, error) {
	tap, err := water.NewTAP("sfl2.tap%d")
	if err != nil {
		return nil, err
	}

	name := tap.Name()
	log.Printf("created layer 2 tap %s\n", name)

	return &L2Tap{
		name: name,
		tap:  tap,
	}, nil
}

func (lt *L2Tap) Name() string {
	return lt.name
}

func (lt *L2Tap) Close() {
	// TODO add close method to water library
}

func (lt *L2Tap) Forward(peer net.Conn) {
	done := make(chan bool, 2)

	go lt.connReader(peer, done)
	go lt.connWriter(peer, done)

	<-done
}

func (lt *L2Tap) connReader(peer net.Conn, done chan<- bool) {
	defer func() {
		done <- true
	}()

	header := make([]byte, 2)
	msgBuffer := make([]byte, MaxPacketSize)

	r := bufio.NewReaderSize(peer, bufSize)
	for {
		// read header
		_, err := io.ReadFull(r, header)
		if err == io.EOF {
			return
		}
		if err != nil {
			log.Printf("Error reading message header: %s", err)
			return
		}
		// check message type
		discriminator := int(header[0] >> 7)
		len := int(header[0])&0x7F<<8 | int(header[1])
		// read message
		message := msgBuffer[:len]
		_, err = io.ReadFull(r, message)
		if err != nil {
			log.Printf("Error reading message: %s", err)
			return
		}
		// process message
		if discriminator == 0 {
			// forwarded packet; write to tap
			_, err = lt.tap.Write(message)
			if err != nil {
				log.Printf("Error relaying message to tap: %s", err)
				return
			}
		} else {
			// control message; ignore for now
		}
	}
}

func (lt *L2Tap) connWriter(peer net.Conn, done chan<- bool) {
	defer func() {
		done <- true
	}()

	// TODO periodic ping messages for broken TLS connections?

	header := make([]byte, 2)
	msgBuffer := make([]byte, MaxPacketSize)
	w := bufio.NewWriterSize(peer, bufSize)

	for {
		// read whole packet from tap
		len, err := lt.tap.Read(msgBuffer)
		if err != nil {
			log.Printf("Error reading from tap: %s", err)
			return
		}
		// send header with packet discriminator
		header[0] = byte(len >> 8 & 0x7F)
		header[1] = byte(len)
		_, err = w.Write(header)
		if err != nil {
			log.Printf("Error writing message header: %s", err)
			return
		}
		// send packet as message
		message := msgBuffer[:len]
		_, err = w.Write(message)
		if err != nil {
			log.Printf("Error writing message: %s", err)
			return
		}
		// TODO batch flush operations
		err = w.Flush()
		if err != nil {
			log.Printf("Error flushing message: %s", err)
			return
		}
	}
}
