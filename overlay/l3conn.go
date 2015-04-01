package overlay

import (
	"bufio"
	"time"
)

type L3Conn struct {
	url    string
	subnet *IPv4Route

	peers *L3Peers
}

func NewL3Conn(url string, subnet *IPv4Route, peers *L3Peers) *L3Conn {
	return &L3Conn{
		url:    url,
		subnet: subnet,

		peers: peers,
	}
}

func (c *L3Conn) Stop() {
	// TODO
}

func (c *L3Conn) Route(r *bufio.Reader, w *bufio.Writer) {
	// TODO

	for {
		time.Sleep(time.Hour)
	}
}
