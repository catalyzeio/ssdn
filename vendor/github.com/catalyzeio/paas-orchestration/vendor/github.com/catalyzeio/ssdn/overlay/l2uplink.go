package overlay

import (
	"bufio"
	"crypto/tls"
	"fmt"
	"net"
	"sync"

	"github.com/catalyzeio/go-core/comm"
)

type L2Uplink struct {
	bridge *L2Bridge

	client *comm.ReconnectClient

	nameMutex sync.Mutex
	name      string
}

func NewL2Uplink(bridge *L2Bridge, addr *comm.Address, config *tls.Config) (*L2Uplink, error) {
	if !addr.TLS() {
		config = nil
	} else if config == nil {
		return nil, fmt.Errorf("uplink %s requires TLS configuration", addr)
	}

	u := L2Uplink{
		bridge: bridge,
	}
	u.client = comm.NewClient(u.connHandler, addr.Host(), addr.Port(), config)
	return &u, nil
}

func (u *L2Uplink) Start() {
	u.client.Start()
}

func (u *L2Uplink) Connected() bool {
	return u.client.Connected()
}

func (u *L2Uplink) Stop() {
	u.client.Stop()
}

func (u *L2Uplink) Name() string {
	u.nameMutex.Lock()
	defer u.nameMutex.Unlock()

	return u.name
}

func (u *L2Uplink) connHandler(conn net.Conn, abort <-chan struct{}) error {
	r, w, err := L2Handshake(conn)
	if err != nil {
		return err
	}

	tap, err := NewL2Tap()
	if err != nil {
		return err
	}
	defer func() {
		if err := tap.Close(); err != nil {
			log.Warn("Failed to close uplink tap: %s", err)
		} else {
			log.Info("Closed uplink tap %s", tap.Name())
		}
	}()

	u.updateName(tap.Name())
	defer u.updateName("")

	return tap.Forward(u.bridge, r, w, abort)
}

func (u *L2Uplink) updateName(name string) {
	u.nameMutex.Lock()
	defer u.nameMutex.Unlock()

	u.name = name
}

func L2Handshake(conn net.Conn) (*bufio.Reader, *bufio.Writer, error) {
	return Handshake(conn, "SSDN-L2 1.0")
}

func Handshake(conn net.Conn, hello string) (*bufio.Reader, *bufio.Writer, error) {
	const delim = '\n'
	message := hello + string(delim)

	r := bufio.NewReaderSize(conn, bufSize)
	w := bufio.NewWriterSize(conn, bufSize)

	if _, err := w.WriteString(message); err != nil {
		return nil, nil, err
	}
	if err := w.Flush(); err != nil {
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
