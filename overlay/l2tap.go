package overlay

import (
	"bufio"
	"fmt"
	"io"
	"net"

	"github.com/catalyzeio/taptun"
)

const (
	MaxPacketSize = 1<<15 - 1 // just under 32 KiB
)

type L2Tap struct {
	name string
	tap  *taptun.Interface
}

const (
	bufSize         = 1 << 18 // 64 KiB
	tapNameTemplate = "sf2.tap%d"
)

func NewL2Tap() (*L2Tap, error) {
	tap, err := taptun.NewTAP(tapNameTemplate)
	if err != nil {
		return nil, err
	}

	name := tap.Name()
	log.Info("Created layer 2 tap %s", name)

	return &L2Tap{
		name: name,
		tap:  tap,
	}, nil
}

func (lt *L2Tap) Name() string {
	return lt.name
}

func (lt *L2Tap) Close() error {
	log.Info("Closing layer 2 tap %s", lt.name)
	return lt.tap.Close()
}

func (lt *L2Tap) Forward(peer net.Conn) {
	done := make(chan bool, 2)

	r, w, err := Handshake(peer, "SFL2 1.0")
	if err != nil {
		log.Warn("Failed to initialize connection to %s: %s", peer.RemoteAddr(), err)
		return
	}

	go lt.connReader(r, done)
	go lt.connWriter(w, done)

	<-done
}

func (lt *L2Tap) connReader(r *bufio.Reader, done chan<- bool) {
	defer func() {
		done <- true
	}()

	header := make([]byte, 2)
	msgBuffer := make([]byte, MaxPacketSize)

	for {
		// read header
		_, err := io.ReadFull(r, header)
		if err == io.EOF {
			return
		}
		if err != nil {
			log.Warn("Failed to read message header: %s", err)
			return
		}
		// check message type
		discriminator := int(header[0] >> 7)
		len := int(header[0])&0x7F<<8 | int(header[1])
		// read message
		message := msgBuffer[:len]
		_, err = io.ReadFull(r, message)
		if err != nil {
			log.Warn("Failed to read message: %s", err)
			return
		}
		// process message
		if discriminator == 0 {
			// forwarded packet; write to tap
			_, err = lt.tap.Write(message)
			if err != nil {
				log.Warn("Failed to relay message to tap: %s", err)
				return
			}
		} else {
			// control message; ignore for now
		}
	}
}

func (lt *L2Tap) connWriter(w *bufio.Writer, done chan<- bool) {
	defer func() {
		done <- true
	}()

	header := make([]byte, 2)
	msgBuffer := make([]byte, MaxPacketSize)

	for {
		// read whole packet from tap
		len, err := lt.tap.Read(msgBuffer)
		if err != nil {
			log.Warn("Failed to read from tap: %s", err)
			return
		}
		// send header with packet discriminator
		header[0] = byte(len >> 8 & 0x7F)
		header[1] = byte(len)
		_, err = w.Write(header)
		if err != nil {
			log.Warn("Failed to write message header: %s", err)
			return
		}
		// send packet as message
		message := msgBuffer[:len]
		_, err = w.Write(message)
		if err != nil {
			log.Warn("Failed to write message: %s", err)
			return
		}
		err = w.Flush()
		if err != nil {
			log.Warn("Failed to flush message: %s", err)
			return
		}
	}
}

func Handshake(peer net.Conn, hello string) (*bufio.Reader, *bufio.Writer, error) {
	const delim = '\n'
	message := hello + string(delim)

	r := bufio.NewReaderSize(peer, bufSize)
	w := bufio.NewWriterSize(peer, bufSize)

	_, err := w.WriteString(message)
	if err != nil {
		return nil, nil, err
	}
	err = w.Flush()
	if err != nil {
		return nil, nil, err
	}
	resp, err := r.ReadString(delim)
	if err != nil {
		return nil, nil, err
	}
	if resp != message {
		return nil, nil, fmt.Errorf("peer sent invalid handshake")
	}

	return r, w, nil
}
