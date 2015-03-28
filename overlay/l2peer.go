package overlay

import (
	"bufio"
	"crypto/tls"
	"fmt"
	"net"

	"github.com/catalyzeio/shadowfax/proto"
)

type L2Peer struct {
	client *proto.ReconnectClient
	tap    *L2Tap
}

func NewL2Peer(addr *proto.Address, config *tls.Config) (*L2Peer, error) {
	if !addr.TLS() {
		config = nil
	} else if config == nil {
		return nil, fmt.Errorf("peer %s requires TLS configuration", addr)
	}

	p := L2Peer{}
	p.client = proto.NewClient(p.connHandler, addr.Host(), addr.Port(), config)
	return &p, nil
}

func (p *L2Peer) Start(tap *L2Tap) {
	p.tap = tap
	p.client.Start()
}

func (p *L2Peer) Stop() {
	p.client.Stop()
	p.tap.Close()
}

func (p *L2Peer) Name() string {
	return p.tap.Name()
}

func (p *L2Peer) connHandler(conn net.Conn, abort <-chan bool) error {
	r, w, err := L2Handshake(conn)
	if err != nil {
		return err
	}
	p.tap.Forward(r, w)
	return nil
}

func L2Handshake(peer net.Conn) (*bufio.Reader, *bufio.Writer, error) {
	return Handshake(peer, "SFL2 1.0")
}

func Handshake(peer net.Conn, hello string) (*bufio.Reader, *bufio.Writer, error) {
	const delim = '\n'
	message := hello + string(delim)

	r := bufio.NewReaderSize(peer, bufSize)
	w := bufio.NewWriterSize(peer, bufSize)

	_, err := w.WriteString(message)
	if err != nil {
		return nil, nil, err
	}
	err = w.Flush()
	if err != nil {
		return nil, nil, err
	}
	resp, err := r.ReadString(delim)
	if err != nil {
		return nil, nil, err
	}
	if resp != message {
		return nil, nil, fmt.Errorf("peer sent invalid handshake")
	}

	return r, w, nil
}
