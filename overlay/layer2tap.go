package overlay

import (
	"log"
	"net"
	"time"

	"github.com/songgao/water"
)

type L2Tap struct {
	Name string

	peer net.Conn
	tap  *water.Interface
}

func NewL2Tap(peer net.Conn) (*L2Tap, error) {
	tap, err := water.NewTAP("sfl2.tap%d")
	if err != nil {
		return nil, err
	}

	name := tap.Name()
	log.Printf("created layer 2 tap %s\n", name)

	return &L2Tap{
		Name: name,

		peer: peer,
		tap:  tap,
	}, nil
}

func (*L2Tap) Forward() {
	// TODO
	for {
		time.Sleep(time.Second)
	}
}
