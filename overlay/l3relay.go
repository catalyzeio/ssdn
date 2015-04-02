package overlay

import (
	"bufio"
	"io"
)

// Handler for inbound packets.
// If the return value is non-nil, the packet will be returned to its queue.
type InboundHandler func(packet *PacketBuffer) error

type L3Relay struct {
	peers *L3Peers

	handler InboundHandler

	free PacketQueue
	out  PacketQueue
}

const (
	// XXX assumes frames have no 802.1q tagging
	ipPayloadOffset = 14
)

func NewL3Relay(peers *L3Peers) *L3Relay {
	free := AllocatePacketQueue(peerQueueSize, int(peers.mtu))
	out := make(PacketQueue, peerQueueSize)

	return NewL3RelayWithQueues(peers, free, out)
}

func NewL3RelayWithQueues(peers *L3Peers, free, out PacketQueue) *L3Relay {
	return &L3Relay{
		peers: peers,

		free: free,
		out:  out,
	}
}

func (rl *L3Relay) Stop() {
	// TODO
}

func (rl *L3Relay) Forward(remoteSubnet *IPv4Route, r *bufio.Reader, w *bufio.Writer) {
	routes := rl.peers.routes
	remoteSubnet.Queue = rl.out
	routes.AddRoute(remoteSubnet)
	defer routes.RemoveRoute(remoteSubnet)

	done := make(chan bool, 2)

	go rl.connReader(r, done)
	go rl.connWriter(w, done)

	<-done
}

func (rl *L3Relay) connReader(r *bufio.Reader, done chan<- bool) {
	defer func() {
		done <- true
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
		if ipPayloadOffset+len > mtu {
			log.Warn("Incoming message is too large: %d", len)
			p.Queue <- p
			return
		}

		// read message
		message := buff[ipPayloadOffset:len]
		_, err = io.ReadFull(r, message)
		if err != nil {
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
			p.Length = ipPayloadOffset + len
			buff[12] = 0x80
			buff[13] = 0x00
			if handler != nil {
				// send to handler
				err = handler(p)
				if err != nil {
					log.Warn("Failed to process incoming message: %s", err)
					p.Queue <- p
					return
				}
			} else {
				// no handler
				p.Queue <- p
			}
		} else {
			// control message; ignore for now
			p.Queue <- p
		}
	}
}

func (rl *L3Relay) connWriter(w *bufio.Writer, done chan<- bool) {
	defer func() {
		done <- true
	}()

	trace := log.IsTraceEnabled()

	out := rl.out
	header := make([]byte, 2)

	for {
		// grab next outgoing packet
		p := <-out

		len := p.Length - ipPayloadOffset
		buff := p.Data

		// send header with packet discriminator
		header[0] = byte(len >> 8 & 0x7F)
		header[1] = byte(len)
		_, err := w.Write(header)
		if err != nil {
			log.Warn("Failed to write message header: %s", err)
			p.Queue <- p
			return
		}

		// send packet as message
		message := buff[ipPayloadOffset:len]
		_, err = w.Write(message)
		if err != nil {
			log.Warn("Failed to write message: %s", err)
			p.Queue <- p
			return
		}

		// flush queued outgoing data
		err = w.Flush()
		if err != nil {
			log.Warn("Failed to flush message: %s", err)
			p.Queue <- p
			return
		}
		if trace {
			log.Trace("Sent outbound message of size %d", len)
		}

		p.Queue <- p
	}
}