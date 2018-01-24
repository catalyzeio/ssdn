package ping

import (
	"fmt"
	"net"
	"sync"
	"time"

	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
)

const (
	protocolICMP = 1

	chanBufSize = 256
)

type Pinger struct {
	conn *icmp.PacketConn
	reqs chan net.Addr

	mutex   sync.Mutex
	pending map[string]chan struct{}
}

func NewPinger(address string) (*Pinger, error) {
	conn, err := icmp.ListenPacket("ip4:icmp", address)
	if err != nil {
		return nil, err
	}
	p := &Pinger{
		conn: conn,
		reqs: make(chan net.Addr, chanBufSize),

		pending: make(map[string]chan struct{}),
	}
	go p.send()
	go p.receive()
	return p, nil
}

func (p *Pinger) Close() error {
	return p.conn.Close()
}

func (p *Pinger) Ping(addr net.IP) <-chan struct{} {
	resp := make(chan struct{}, 1)
	p.register(addr.String(), resp)
	p.reqs <- &net.IPAddr{IP: addr, Zone: ""}
	return resp
}

func (p *Pinger) PingSync(addr net.IP, timeout time.Duration) error {
	resp := p.Ping(addr)
	deadline := time.After(timeout)
	select {
	case <-resp:
		return nil
	case <-deadline:
		return fmt.Errorf("no response from %s", addr)
	}
}

func (p *Pinger) send() {
	seq := 0
	for {
		req := <-p.reqs
		wm := icmp.Message{
			Type: ipv4.ICMPTypeEcho,
			Code: 0,
			Body: &icmp.Echo{
				ID:   4242,
				Seq:  seq,
				Data: []byte("1234567890"),
			},
		}
		seq += 1
		buf, err := wm.Marshal(nil)
		if err != nil {
			log.Warn("Failed to generate ping packet: %s", err)
			continue
		}
		if _, err := p.conn.WriteTo(buf, req); err != nil {
			log.Warn("Failed to send ping packet: %s", err)
			continue
		}
		if log.IsTraceEnabled() {
			log.Trace("Sent ping to %s", req)
		}
	}
}

func (p *Pinger) receive() {
	buf := make([]byte, 1500)
	for {
		n, peer, err := p.conn.ReadFrom(buf)
		if err != nil {
			log.Warn("Failed to receive ping data: %s", err)
		}
		resp, err := icmp.ParseMessage(protocolICMP, buf[:n])
		if err != nil {
			log.Warn("Failed to parse ping message: %s", err)
			continue
		}
		switch resp.Type {
		case ipv4.ICMPTypeEchoReply:
			if log.IsTraceEnabled() {
				log.Trace("Received ICMP ping response from %s", peer)
			}
			p.notify(peer.String())
		}
	}
}

func (p *Pinger) register(ip string, res chan struct{}) {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	p.pending[ip] = res
}

func (p *Pinger) notify(ip string) {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	res := p.pending[ip]
	if res != nil {
		select {
		case res <- struct{}{}:
		}
		delete(p.pending, ip)
	}
}
