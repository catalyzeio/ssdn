package proto

import (
	"bufio"
	"crypto/tls"
	"net"
	"time"
)

type SyncCaller interface {
	SyncCall(msg []byte) ([]byte, error)
}

type SyncClient struct {
	Handshaker  func(caller SyncCaller) error
	IdleHandler func(caller SyncCaller) error

	client      *ReconnectClient
	requests    chan *syncReq
	idleTimeout time.Duration
}

type directCaller struct {
	conn   net.Conn
	reader *bufio.Reader
}

type syncReq struct {
	msg    []byte
	result chan *syncResp
}

type syncResp struct {
	msg []byte
	err error
}

const (
	separator = '\n'
	bufSize   = 1 << 18 // 64 KiB
)

func NewSyncClient(host string, port int, config *tls.Config, idleTimeout time.Duration) *SyncClient {
	s := SyncClient{
		requests:    make(chan *syncReq, 1),
		idleTimeout: idleTimeout,
	}
	s.client = NewClient(s.connHandler, host, port, config)
	return &s
}

func (c *SyncClient) Start() {
	c.client.Start()
}

func (c *SyncClient) Disconnect() {
	c.client.Disconnect()
}

func (c *SyncClient) Stop() {
	c.client.Stop()
}

func (c *SyncClient) SyncCall(msg []byte) ([]byte, error) {
	req := syncReq{msg, make(chan *syncResp, 1)}
	c.requests <- &req
	resp := <-req.result
	return resp.msg, resp.err
}

func (c *SyncClient) connHandler(conn net.Conn, abort <-chan struct{}) error {
	r := bufio.NewReaderSize(conn, bufSize)

	dc := directCaller{conn, r}
	if c.Handshaker != nil {
		if err := c.Handshaker(&dc); err != nil {
			return err
		}
	}

	for {
		var timeout <-chan time.Time
		if c.idleTimeout > 0 && c.IdleHandler != nil {
			timeout = time.After(c.idleTimeout)
		}

		// TODO try to detect disconnections immediately

		select {
		case <-abort:
			return nil
		case <-timeout:
			if err := c.IdleHandler(&dc); err != nil {
				return err
			}
		case request := <-c.requests:
			msg, err := dc.SyncCall(request.msg)
			request.result <- &syncResp{msg, err}
			if err != nil {
				return err
			}
		}
	}
}

func (dc *directCaller) SyncCall(reqMsg []byte) ([]byte, error) {
	// send request
	if log.IsTraceEnabled() {
		log.Trace("-> %s", reqMsg)
	}
	if _, err := dc.conn.Write(append(reqMsg, separator)); err != nil {
		return nil, err
	}
	// read result
	respMsg, err := dc.reader.ReadBytes(separator)
	if err != nil {
		return nil, err
	}
	if log.IsTraceEnabled() {
		log.Trace("<- %s", respMsg)
	}
	return respMsg, err
}
