package proto

import (
	"bufio"
	"crypto/tls"
	"fmt"
	"log"
	"math/rand"
	"net"
	"time"
)

type ConnReader interface {
	ReadConn(conn net.Conn, dest <-chan []byte)
}

type ConnWriter interface {
	WriteConn(conn net.Conn, src <-chan []byte, abort <-chan bool)
}

type Event int

const (
	maxReconnectDelay = 5 * time.Second
)

const (
	Connected Event = iota
	Disconnected
)

type request int

const (
	disconnect request = iota
	stop
)

type ReconnectClient struct {
	In     chan []byte
	Out    chan []byte
	Events chan Event

	Reader ConnReader
	Writer ConnWriter

	host   string
	port   int
	config *tls.Config

	control chan request
}

const chanSize = 64

func NewClient(host string, port int) *ReconnectClient {
	return NewTLSClient(host, port, nil)
}

func NewTLSClient(host string, port int, config *tls.Config) *ReconnectClient {
	return &ReconnectClient{
		In:      make(chan []byte, chanSize),
		Out:     make(chan []byte, chanSize),
		Events:  make(chan Event, chanSize),
		host:    host,
		port:    port,
		config:  config,
		control: make(chan request),
	}
}

func (p *ReconnectClient) Start() {
	go p.run()
}

func (p *ReconnectClient) Disconnect() {
	p.control <- disconnect
}

func (p *ReconnectClient) Stop() {
	p.control <- stop
}

func (p *ReconnectClient) run() {
	target := fmt.Sprintf("%s:%d", p.host, p.port)
	initDelay := false
	for {
		if p.connect(target, initDelay) {
			return
		}
		log.Printf("Reconnecting to %s", target)
		initDelay = true
	}
}

func (p *ReconnectClient) connect(target string, initDelay bool) bool {
	abort := make(chan bool)

	// connect to remote host
	conn := p.dial(target, initDelay)
	if conn == nil {
		return true
	}

	// schedule cleanup and and trigger connected event
	defer func() {
		conn.Close()
		abort <- true
		log.Printf("Disconnected from %s", target)
		p.Events <- Disconnected
	}()
	log.Printf("Connected to %s", target)
	p.Events <- Connected

	// set up connection
	tcpConn := conn.(*net.TCPConn)
	tcpConn.SetKeepAlive(true)
	tcpConn.SetKeepAlivePeriod(15 * time.Second)

	// service inbound and outbound channels
	done := make(chan bool, 2)
	go doRead(target, done, p.Reader, conn, p.In)
	go doWrite(target, done, p.Writer, conn, p.Out, abort)

	// continue until control signal or reader/writer finish or fail
	result := false
	select {
	case <-done:
		// allow reconnect
	case msg := <-p.control:
		switch msg {
		case stop:
			// inhibit reconnect
			result = true
		default:
			// allow reconnect
		}
	}
	return result
}

func (p *ReconnectClient) dial(target string, initDelay bool) net.Conn {
	var delay time.Duration
	if initDelay {
		delay = time.Second
	}

	for {
		select {
		case cmsg := <-p.control:
			switch cmsg {
			case disconnect:
				log.Printf("Not connected to %s; ignoring disconnection request", target)
				// XXX this causes an extra connection delay that is mostly harmless
				continue
			case stop:
				log.Printf("Aborting connection with %s", target)
				return nil
			}
		case <-time.After(delay):
			// continue connection attempts
		}

		log.Printf("Connecting to %s", target)
		conn, err := net.Dial("tcp", target)
		if err == nil {
			return conn
		}

		delay += time.Duration(500+rand.Intn(500)) * time.Millisecond
		if delay > maxReconnectDelay {
			delay = maxReconnectDelay
		}
		log.Printf("Error connecting to %s: %s; retrying in %s", target, err, delay)
	}
}

func doRead(target string, done chan<- bool, reader ConnReader, conn net.Conn, dest chan []byte) {
	defer func() {
		done <- true
	}()

	if reader != nil {
		reader.ReadConn(conn, dest)
		return
	}

	const readBufferSize = 1 << 18 // 64 KiB
	b := bufio.NewReaderSize(conn, readBufferSize)
	for {
		msg, err := b.ReadBytes('\n')
		if err != nil {
			log.Printf("Failed to read from %s: %s", target, err)
			break
		}
		dest <- msg
	}
}

func doWrite(target string, done chan<- bool, writer ConnWriter, conn net.Conn, src chan []byte, abort <-chan bool) {
	defer func() {
		done <- true
	}()

	if writer != nil {
		writer.WriteConn(conn, src, abort)
		return
	}

	for {
		select {
		case <-abort:
			log.Printf("Aborting writes to %s", target)
			return
		case msg := <-src:
			_, err := conn.Write(msg)
			if err != nil {
				log.Printf("Failed to send to %s: %s", target, err)
				return
			}
		}
	}
}
