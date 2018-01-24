package registry

import (
	"bufio"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"time"

	"github.com/catalyzeio/go-core/comm"
)

const (
	DefaultPort = 7411

	delimiter   = '\n'
	idleTimeout = 2 * time.Minute
)

type Listener struct {
	address *comm.Address
	config  *tls.Config

	backend Backend
}

func NewListener(address *comm.Address, config *tls.Config, backend Backend) *Listener {
	return &Listener{address, config, backend}
}

func (l *Listener) Listen() error {
	listener, err := l.address.Listen(l.config)
	if err != nil {
		return err
	}
	l.accept(listener)

	return nil
}

func (l *Listener) accept(listener net.Listener) {
	defer listener.Close()
	defer l.backend.Close()

	log.Info("Listening on %s", listener.Addr())
	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Warn("Failed to accept incoming connection: %s", err)
			return
		}
		go l.service(conn)
	}
}

func (l *Listener) service(conn net.Conn) {
	remoteAddr := conn.RemoteAddr()
	defer func() {
		conn.Close()
		log.Info("Client disconnected: %s", remoteAddr)
	}()

	log.Info("Inbound connection: %s", remoteAddr)

	if err := l.handle(conn, remoteAddr); err != nil {
		if err != io.EOF {
			log.Warn("Failed to handle inbound connection %s: %s", remoteAddr, err)
		}
	}
}

func (l *Listener) handle(conn net.Conn, remoteAddr net.Addr) error {
	io := comm.WrapO(conn, messageWriter, 1, idleTimeout)
	defer io.Stop()

	authenticated := false
	var tenant string

	backend := l.backend

	advertised := false
	ads := Message{}
	defer func() {
		if advertised {
			if err := backend.Unadvertise(tenant, &ads, true); err != nil {
				log.Warn("Failed to clean up advertisements for %s: %s", remoteAddr, err)
			}
		}
	}()

	out, done := io.Out, io.Done

	registered := false
	defer func() {
		if registered {
			if err := backend.Unregister(tenant, out, nil); err != nil {
				log.Warn("Failed to clean up registrations for %s: %s", remoteAddr, err)
			}
		}
	}()

	reader := newMessageReader(conn)
	for {
		req, err := reader(conn)
		if err != nil {
			return err
		}

		resp := Message{}
		if !authenticated {
			// first request must be to authenticate
			if req.Type == "authenticate" {
				authTenant, err := l.authenticate(req, &resp)
				if err != nil {
					return err
				}
				resp.Version = "1.1"
				tenant = authTenant
				authenticated = true
				log.Info("Authenticated %s on %s", tenant, remoteAddr)
			} else {
				return fmt.Errorf("unexpected request type %s", req.Type)
			}
		} else {
			// authenticated; handle other requests
			if req.Type == "pong" {
				// no-op
				continue
			} else if req.Type == "ping" {
				// respond to ping request
				resp.Type = "pong"
			} else if req.Type == "advertise" {
				// unadvertise any current advertisements
				if advertised {
					if err := backend.Unadvertise(tenant, &ads, false); err != nil {
						return err
					}
				}
				// forward request to backend
				if err := backend.Advertise(tenant, req, &resp); err != nil {
					return err
				}
				// record latest advertisements
				advertised = true
				ads = *req
			} else if req.Type == "query" {
				// forward request to backend
				if err := backend.Query(tenant, req, &resp); err != nil {
					return err
				}
			} else if req.Type == "queryAll" {
				// forward request to backend
				if err := backend.QueryAll(tenant, req, &resp); err != nil {
					return err
				}
			} else if req.Type == "enumerate" {
				// forward request to backend
				if err := backend.Enumerate(tenant, req, &resp); err != nil {
					return err
				}
			} else if req.Type == "register" {
				// forward request to backend
				if err := backend.Register(tenant, out, &resp); err != nil {
					return err
				}
				registered = true
			} else if req.Type == "unregister" {
				// forward request to backend
				if err := backend.Unregister(tenant, out, &resp); err != nil {
					return err
				}
				registered = false
			} else {
				// invalid request
				resp.SetError(fmt.Sprintf("unexpected request type %s", req.Type))
			}
		}

		select {
		case <-done:
			return nil
		case out <- &resp:
		}
	}
}

func (l *Listener) authenticate(req *Message, resp *Message) (string, error) {
	tenant := req.Tenant
	if len(tenant) == 0 || req.Token == nil {
		return "", fmt.Errorf("bad auth request")
	}

	// TODO pluggable authentication
	resp.Type = "authenticated"
	return tenant, nil
}

func newMessageReader(conn net.Conn) func(conn net.Conn) (*Message, error) {
	r := bufio.NewReader(conn)
	return func(conn net.Conn) (*Message, error) {
		if err := conn.SetReadDeadline(time.Now().Add(idleTimeout)); err != nil {
			return nil, err
		}
		data, err := r.ReadBytes(delimiter)
		if err != nil {
			return nil, err
		}
		if log.IsTraceEnabled() {
			log.Trace("<- %s", data)
		}
		msg := &Message{}
		if err := json.Unmarshal(data, msg); err != nil {
			return nil, err
		}
		return msg, nil
	}
}

func messageWriter(conn net.Conn, msg interface{}) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	if log.IsTraceEnabled() {
		log.Trace("-> %s", data)
	}
	_, err = conn.Write(append(data, delimiter))
	return err
}
