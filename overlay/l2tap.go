package overlay

import (
	"bufio"
	"io"

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
	bufSize = 1 << 18 // 64 KiB
)

func NewL2Tap() (*L2Tap, error) {
	const tapNameTemplate = "sf2.tap%d"
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

func (t *L2Tap) Name() string {
	return t.name
}

func (t *L2Tap) Close() error {
	return t.tap.Close()
}

func (t *L2Tap) Forward(bridge *L2Bridge, r *bufio.Reader, w *bufio.Writer, abort <-chan struct{}) error {
	if err := bridge.link(t.name); err != nil {
		return err
	}

	done := make(chan struct{}, 2)

	go t.connReader(r, done)
	go t.connWriter(w, done)

	for {
		select {
		case <-abort:
			return nil
		case <-done:
			return nil
		}
	}
}

func (t *L2Tap) connReader(r *bufio.Reader, done chan<- struct{}) {
	defer func() {
		done <- struct{}{}
	}()

	header := make([]byte, 2)
	msgBuffer := make([]byte, ethernetHeaderSize+MaxPacketSize)

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
		if _, err := io.ReadFull(r, message); err != nil {
			log.Warn("Failed to read message: %s", err)
			return
		}

		// process message
		if discriminator == 0 {
			// forwarded packet; write to tap
			if _, err := t.tap.Write(message); err != nil {
				log.Warn("Failed to relay message to tap: %s", err)
				return
			}
		} else {
			// control message; ignore for now
		}
	}
}

func (t *L2Tap) connWriter(w *bufio.Writer, done chan<- struct{}) {
	defer func() {
		done <- struct{}{}
	}()

	header := make([]byte, 2)
	msgBuffer := make([]byte, ethernetHeaderSize+MaxPacketSize)

	for {
		// read whole packet from tap
		len, err := t.tap.Read(msgBuffer)
		if err != nil {
			log.Warn("Failed to read from tap: %s", err)
			return
		}

		// send header with packet discriminator
		header[0] = byte(len >> 8 & 0x7F)
		header[1] = byte(len)
		if _, err := w.Write(header); err != nil {
			log.Warn("Failed to write message header: %s", err)
			return
		}

		// send packet as message
		message := msgBuffer[:len]
		if _, err := w.Write(message); err != nil {
			log.Warn("Failed to write message: %s", err)
			return
		}

		// flush queued outgoing data
		if err := w.Flush(); err != nil {
			log.Warn("Failed to flush message: %s", err)
			return
		}
	}
}
