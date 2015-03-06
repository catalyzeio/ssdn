package proto

import (
	"bufio"
	"crypto/tls"
	"log"
	"net"
)

// TODO hook for connection handshake
// TODO timeout trigger for idle connections

type SyncClient struct {
	client   *ReconnectClient
	requests chan *syncReq
}

type syncReq struct {
	msg    []byte
	result chan *syncResp
}

type syncResp struct {
	msg []byte
	err error
}

func NewSyncClient(host string, port int, config *tls.Config) *SyncClient {
	s := SyncClient{
		requests: make(chan *syncReq, 1),
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

func (c *SyncClient) Call(msg []byte) ([]byte, error) {
	req := syncReq{msg, make(chan *syncResp, 1)}
	c.requests <- &req
	resp := <-req.result
	return resp.msg, resp.err
}

func (c *SyncClient) syncHandler(conn net.Conn, abort <-chan bool) error {
	const bufferSize = 1 << 18 // 64 KiB
	r := bufio.NewReaderSize(conn, bufferSize)
	for {
		select {
		case <-abort:
			return nil
		case request := <-c.requests:
			result := syncResp{}
			// send request
			log.Printf(" -> %s", request.msg)
			_, result.err = conn.Write(request.msg)
			if result.err != nil {
				request.result <- &result
				return result.err
			}
			// read result
			result.msg, result.err = r.ReadBytes('\n')
			if result.err != nil {
				request.result <- &result
				return result.err
			}
			log.Printf(" <- %s", result.msg)
			request.result <- &result
		}
	}
}
