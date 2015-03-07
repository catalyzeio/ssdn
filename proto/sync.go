package proto

import (
	"bufio"
	"crypto/tls"
	"log"
	"net"
	"time"
)

type SyncCaller interface {
	SyncCall(msg []byte) ([]byte, error)
}

type SyncClient struct {
	Handshaker     func(caller SyncCaller) error
	TimeoutHandler func()

	client   *ReconnectClient
	requests chan *syncReq
	timeout  time.Duration
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
)

func NewSyncClient(host string, port int, config *tls.Config, timeout time.Duration) *SyncClient {
	s := SyncClient{
		requests: make(chan *syncReq, 1),
		timeout:  timeout,
	}
	s.client = NewClient(s.syncHandler, host, port, config)
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

func (c *SyncClient) syncHandler(conn net.Conn, abort <-chan bool) error {
	const bufferSize = 1 << 18 // 64 KiB
	r := bufio.NewReaderSize(conn, bufferSize)

	dc := directCaller{conn, r}
	if c.Handshaker != nil {
		err := c.Handshaker(&dc)
		if err != nil {
			return err
		}
	}

	for {
		select {
		case <-abort:
			return nil
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
	log.Printf(" -> %s", reqMsg)
	_, err := dc.conn.Write(append(reqMsg, separator))
	if err != nil {
		return nil, err
	}
	// read result
	respMsg, err := dc.reader.ReadBytes(separator)
	if err != nil {
		return nil, err
	}
	log.Printf(" <- %s", respMsg)
	return respMsg, err
}
