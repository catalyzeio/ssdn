package overlay

import (
	"bufio"
	"crypto/tls"
	"fmt"
	"net"

	"github.com/catalyzeio/shadowfax/proto"
)

type L2Uplink struct {
	client *proto.ReconnectClient
	tap    *L2Tap
}

func NewL2Uplink(addr *proto.Address, config *tls.Config) (*L2Uplink, error) {
	if !addr.TLS() {
		config = nil
	} else if config == nil {
		return nil, fmt.Errorf("uplink %s requires TLS configuration", addr)
	}

	u := L2Uplink{}
	u.client = proto.NewClient(u.connHandler, addr.Host(), addr.Port(), config)
	return &u, nil
}

func (u *L2Uplink) Start(tap *L2Tap) {
	u.tap = tap
	u.client.Start()
}

func (u *L2Uplink) Stop() {
	u.client.Stop()
	u.tap.Close()
}

func (u *L2Uplink) Name() string {
	return u.tap.Name()
}

func (u *L2Uplink) connHandler(conn net.Conn, abort <-chan struct{}) error {
	r, w, err := L2Handshake(conn)
	if err != nil {
		return err
	}
	u.tap.Forward(r, w, abort)
	return nil
}

func L2Handshake(conn net.Conn) (*bufio.Reader, *bufio.Writer, error) {
	return Handshake(conn, "SFL2 1.0")
}

func Handshake(conn net.Conn, hello string) (*bufio.Reader, *bufio.Writer, error) {
	const delim = '\n'
	message := hello + string(delim)

	r := bufio.NewReaderSize(conn, bufSize)
	w := bufio.NewWriterSize(conn, bufSize)

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
		return nil, nil, fmt.Errorf("remote at %s sent invalid handshake", conn.RemoteAddr())
	}

	return r, w, nil
}
