package comm

import (
	"crypto/tls"
	"fmt"
	"net"
	"strings"
	"sync/atomic"
	"time"
)

type ConnHandler func(conn net.Conn, abort <-chan struct{}) error

type ReconnectClient struct {
	Handler ConnHandler

	Timeout time.Duration
	Failed  func()

	proto string

	// unix socket
	dsPath string

	// tcp socket
	host   string
	port   int
	config *tls.Config

	control chan clientRequest

	state int32 // atomic int32
}

const (
	connectionTimeout = 60 * time.Second
	keepAlivePeriod   = 30 * time.Second
)

type clientRequest int

const (
	disconnect clientRequest = iota
	stop
)

const (
	disconnected = 0
	connected    = 1
)

func NewClient(handler ConnHandler, host string, port int, config *tls.Config) *ReconnectClient {
	proto := "tcp"
	if config != nil {
		proto = "tcps"
	}
	return &ReconnectClient{
		Handler: handler,

		proto: proto,

		host:   host,
		port:   port,
		config: config,

		control: make(chan clientRequest, 1),
	}
}

func NewSocketClient(handler ConnHandler, dsPath string) *ReconnectClient {
	return &ReconnectClient{
		Handler: handler,

		proto: "unix",

		dsPath: dsPath,

		control: make(chan clientRequest, 1),
	}
}

func (c *ReconnectClient) TargetURL() string {
	if c.proto == "unix" {
		return fmt.Sprintf("unix://%s", c.dsPath)
	}
	return fmt.Sprintf("%s://%s:%d", c.proto, c.host, c.port)
}

func (c *ReconnectClient) Start() {
	go c.run()
}

func (c *ReconnectClient) Disconnect() {
	c.control <- disconnect
}

func (c *ReconnectClient) Connected() bool {
	return atomic.LoadInt32(&c.state) == connected
}

func (c *ReconnectClient) Stop() {
	c.control <- stop
}

func (c *ReconnectClient) run() {
	target := strings.SplitN(c.TargetURL(), "://", 2)[1]
	initDelay := false
	for {
		if c.connect(target, initDelay) {
			return
		}
		log.Info("Reconnecting to %s", target)
		initDelay = true
	}
}

func (c *ReconnectClient) connect(target string, initDelay bool) bool {
	abort := make(chan struct{})

	// connect to remote host
	conn := c.dial(target, initDelay)
	if conn == nil {
		return true
	}

	// schedule cleanup
	defer func() {
		conn.Close()
		close(abort)
		atomic.StoreInt32(&c.state, disconnected)
		log.Info("Disconnected from %s", target)
	}()
	atomic.StoreInt32(&c.state, connected)
	log.Info("Connected to %s", target)

	// run handler
	finished := make(chan struct{}, 1)
	if c.Handler != nil {
		go func() {
			if err := c.Handler(conn, abort); err != nil {
				log.Warn("Failed to handle connection to %s: %s", target, err)
			}
			finished <- struct{}{}
		}()
	}

	// continue until control signal or handler finishes
	result := false
	select {
	case <-finished:
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
	b := NewDefaultBackoff()
	if initDelay {
		b.Init()
	}

	var timeout <-chan time.Time
	if c.Timeout > 0 {
		timeout = time.After(c.Timeout)
	}

	for {
		select {
		case cmsg := <-c.control:
			switch cmsg {
			case disconnect:
				if log.IsDebugEnabled() {
					log.Debug("Not connected to %s; ignoring disconnection request", target)
				}
				// XXX this causes an extra connection delay that is mostly harmless
				continue
			case stop:
				if log.IsDebugEnabled() {
					log.Debug("Aborting connection with %s", target)
				}
				return nil
			}
		case <-timeout:
			log.Debug("Connection to %s timed out", target)
			if c.Failed != nil {
				c.Failed()
			}
			return nil
		case <-b.After():
			// continue connection attempts
		}

		if log.IsDebugEnabled() {
			log.Debug("Connecting to %s", target)
		}
		dialer := &net.Dialer{
			Timeout:   connectionTimeout,
			KeepAlive: keepAlivePeriod,
		}
		var conn net.Conn
		var err error
		if c.proto == "unix" {
			conn, err = dialer.Dial(c.proto, target)
		} else if c.config != nil {
			conn, err = tls.DialWithDialer(dialer, "tcp", target, c.config)
		} else {
			conn, err = dialer.Dial("tcp", target)
		}
		if err == nil {
			return conn
		}

		b.Fail()
		log.Warn("Failed to connect to %s: %s; retrying in %s", target, err, b.Delay)
	}
}
