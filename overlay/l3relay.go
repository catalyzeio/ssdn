package overlay

import (
	"bufio"
	"io"
	"time"
)

// Handler for inbound packets.
// If the return value is non-nil, the packet will be returned to its queue.
type InboundHandler func(packet *PacketBuffer) error

type L3Relay struct {
	peers *L3Peers

	handler InboundHandler

	free PacketQueue
	out  PacketQueue

	ping chan struct{}

	control chan struct{}
}

const (
	pingInterval = 60 * time.Second

	// XXX assumes frames have no 802.1q tagging
	ethernetHeaderSize = 14
)

func NewL3Relay(peers *L3Peers) *L3Relay {
	free := AllocatePacketQueue(tapQueueSize, ethernetHeaderSize+int(peers.mtu))
	out := make(PacketQueue, tapQueueSize)

	return NewL3RelayWithQueues(peers, free, out)
}

func NewL3RelayWithQueues(peers *L3Peers, free, out PacketQueue) *L3Relay {
	return &L3Relay{
		peers: peers,

		free: free,
		out:  out,

		ping: make(chan struct{}, 1),

		control: make(chan struct{}, 1),
	}
}

/*
For the L3Peer interface.

If this type is used as an instance of L3Peer that means it represents
an inbound connection. Relays for inbound connections are only
referenced while clients are connected, so this method should only
return true.
*/
func (rl *L3Relay) Connected() bool {
	return true
}

func (rl *L3Relay) Stop() {
	rl.control <- struct{}{}
}

func (rl *L3Relay) Forward(remoteSubnet *IPv4Route, r *bufio.Reader, w *bufio.Writer, abort <-chan struct{}) {
	routes := rl.peers.routes
	remoteSubnet.Queue = rl.out
	routes.Add(remoteSubnet)
	defer routes.Remove(remoteSubnet)

	done := make(chan struct{}, 2)

	go rl.connReader(r, done)
	go rl.connWriter(w, done)

	for {
		select {
		case <-abort:
			return
		case <-done:
			return
		case <-rl.control:
			return
		case <-time.After(pingInterval):
			rl.ping <- struct{}{}
		}
	}
}

func (rl *L3Relay) connReader(r *bufio.Reader, done chan<- struct{}) {
	defer func() {
		done <- struct{}{}
	}()

	trace := log.IsTraceEnabled()

	free := rl.free
	header := make([]byte, 2)

	peers := rl.peers
	mtu := int(peers.mtu)
	handler := peers.handler

	for {
		// grab packet
		p := <-free
		buff := p.Data

		// read header
		_, err := io.ReadFull(r, header)
		if err == io.EOF {
			p.Queue <- p
			return
		}
		if err != nil {
			log.Warn("Failed to read message header: %s", err)
			p.Queue <- p
			return
		}

		// check message type
		discriminator := int(header[0] >> 7)
		len := int(header[0])&0x7F<<8 | int(header[1])

		// bail if packet length is too large for local MTU
		if len > mtu {
			log.Warn("Incoming message is too large: %d", len)
			p.Queue <- p
			return
		}

		// read message (skipping ethernet header)
		message := buff[ethernetHeaderSize : ethernetHeaderSize+len]
		if _, err := io.ReadFull(r, message); err != nil {
			log.Warn("Failed to read message: %s", err)
			p.Queue <- p
			return
		}
		if trace {
			log.Trace("Read inbound message of size %d", len)
		}

		// process message
		if discriminator == 0 {
			// forwarded IPv4 packet
			p.Length = ethernetHeaderSize + len
			buff[12] = 0x08
			buff[13] = 0x00
			if handler != nil {
				// send to handler
				if err := handler(p); err != nil {
					log.Warn("Failed to process incoming packet message: %s", err)
					p.Queue <- p
					return
				}
			} else {
				// no handler
				p.Queue <- p
			}
		} else {
			// control message; ignore for now
			if trace {
				log.Trace("Received control message")
			}
			p.Queue <- p
		}
	}
}

func (rl *L3Relay) connWriter(w *bufio.Writer, done chan<- struct{}) {
	defer func() {
		done <- struct{}{}
	}()

	trace := log.IsTraceEnabled()

	ping := rl.ping
	out := rl.out
	header := make([]byte, 2)

	for {
		// grab next outgoing packet
		var p *PacketBuffer
		select {
		case <-ping:
			// send header with control discriminator
			header[0] = 0x80
			header[1] = 0x01
			if _, err := w.Write(header); err != nil {
				log.Warn("Failed to write control message header: %s", err)
				return
			}
			if err := w.WriteByte(0); err != nil {
				log.Warn("Failed to write control message: %s", err)
				return
			}
			if err := w.Flush(); err != nil {
				log.Warn("Failed to flush control message: %s", err)
				return
			}
			if trace {
				log.Trace("Sent ping control message")
			}
			continue
		case p = <-out:
			break
		}

		len := p.Length - ethernetHeaderSize
		buff := p.Data

		// send header with packet discriminator
		header[0] = byte(len >> 8 & 0x7F)
		header[1] = byte(len)
		if _, err := w.Write(header); err != nil {
			log.Warn("Failed to write packet message header: %s", err)
			p.Queue <- p
			return
		}

		// send packet (skipping ethernet header) as message
		message := buff[ethernetHeaderSize:p.Length]
		if _, err := w.Write(message); err != nil {
			log.Warn("Failed to write packet message: %s", err)
			p.Queue <- p
			return
		}

		// flush queued outgoing data
		if err := w.Flush(); err != nil {
			log.Warn("Failed to flush packet message: %s", err)
			p.Queue <- p
			return
		}
		if trace {
			log.Trace("Sent outbound packet message of size %d", len)
		}

		p.Queue <- p
	}
}
