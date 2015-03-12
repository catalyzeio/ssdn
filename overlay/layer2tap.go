package overlay

import (
	"bufio"
	"io"
	"log"
	"net"

	"github.com/songgao/water"
)

type L2Tap struct {
	Name string

	peer net.Conn
	tap  *water.Interface
}

const (
	bufSize       = 1 << 18 // 64 KiB
	maxPacketSize = 1 << 15 // 32 KiB
)

func NewL2Tap(peer net.Conn) (*L2Tap, error) {
	tap, err := water.NewTAP("sfl2.tap%d")
	if err != nil {
		return nil, err
	}

	name := tap.Name()
	log.Printf("created layer 2 tap %s\n", name)

	return &L2Tap{
		Name: name,

		peer: peer,
		tap:  tap,
	}, nil
}

func (lt *L2Tap) Close() {
	// TODO add close method to water library
}

func (lt *L2Tap) Forward() {
	done := make(chan bool, 2)

	go lt.connReader(done)
	go lt.connWriter(done)

	<-done
}

func (lt *L2Tap) connReader(done chan<- bool) {
	defer func() {
		done <- true
	}()

	header := make([]byte, 2)
	msgBuffer := make([]byte, maxPacketSize)

	r := bufio.NewReaderSize(lt.peer, bufSize)
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

func (lt *L2Tap) connWriter(done chan<- bool) {
	defer func() {
		done <- true
	}()

	// TODO periodic ping messages for broken TLS connections?

	header := make([]byte, 2)
	msgBuffer := make([]byte, maxPacketSize)
	w := bufio.NewWriterSize(lt.peer, bufSize)

	for {
		// read whole packet from tap
		len, err := lt.tap.Read(msgBuffer)
		if err != nil {
			log.Printf("Error reading from tap: %s", err)
			return
		}
		// update header and message, use packet discriminator
		header[0] = byte(len >> 8 & 0x7F)
		header[1] = byte(len)
		message := msgBuffer[:len]
		// write to connection
		_, err = w.Write(header)
		if err != nil {
			log.Printf("Error writing message header: %s", err)
			return
		}
		_, err = w.Write(message)
		if err != nil {
			log.Printf("Error writing message: %s", err)
			return
		}
	}
}
