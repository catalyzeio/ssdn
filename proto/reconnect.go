package proto

import (
	"crypto/tls"
	"fmt"
	"log"
	"math/rand"
	"net"
	"time"
)

type ConnHandler func(conn net.Conn, abort <-chan bool) error

type ReconnectClient struct {
	Handler ConnHandler

	host   string
	port   int
	config *tls.Config

	control chan clientRequest
}

type clientRequest int

const (
	disconnect clientRequest = iota
	stop
)

const (
	maxReconnectDelay = 5 * time.Second
)

func NewClient(handler ConnHandler, host string, port int, config *tls.Config) *ReconnectClient {
	return &ReconnectClient{
		Handler: handler,

		host:   host,
		port:   port,
		config: config,

		control: make(chan clientRequest, 1),
	}
}

func (c *ReconnectClient) Start() {
	go c.run()
}

func (c *ReconnectClient) Disconnect() {
	c.control <- disconnect
}

func (c *ReconnectClient) Stop() {
	c.control <- stop
}

func (c *ReconnectClient) run() {
	target := fmt.Sprintf("%s:%d", c.host, c.port)
	initDelay := false
	for {
		if c.connect(target, initDelay) {
			return
		}
		log.Printf("Reconnecting to %s", target)
		initDelay = true
	}
}

func (c *ReconnectClient) connect(target string, initDelay bool) bool {
	abort := make(chan bool, 1)

	// connect to remote host
	conn := c.dial(target, initDelay)
	if conn == nil {
		return true
	}

	// schedule cleanup
	defer func() {
		conn.Close()
		abort <- true
		log.Printf("Disconnected from %s", target)
	}()
	log.Printf("Connected to %s", target)

	// set up connection
	tcpConn := conn.(*net.TCPConn)
	tcpConn.SetKeepAlive(true)
	tcpConn.SetKeepAlivePeriod(15 * time.Second)

	// run handler
	failed := make(chan bool, 1)
	if c.Handler != nil {
		go func() {
			err := c.Handler(conn, abort)
			if err != nil {
				log.Printf("Error in connection handler: %s", err)
				failed <- true
			}
		}()
	}

	// continue until control signal or handler failure
	result := false
	select {
	case <-failed:
		// allow reconnect
	case msg := <-c.control:
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

func (c *ReconnectClient) dial(target string, initDelay bool) net.Conn {
	var delay time.Duration
	if initDelay {
		delay = time.Second
	}

	for {
		select {
		case cmsg := <-c.control:
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
		// TODO TLS dial
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