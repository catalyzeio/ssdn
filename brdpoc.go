package main

import (
	"flag"
	"fmt"
	"net"
	"time"

	"github.com/songgao/water"
	"github.com/songgao/water/waterutil"
)

func dial(loc string) (valid_conn net.Conn) {
	for {
		conn, err := net.Dial("tcp", loc)
		if err != nil {
			fmt.Printf("failed to connect to %s: %v\n", loc, err)
		} else {
			valid_conn = conn
			return
		}
		time.Sleep(1 * time.Second)
	}
}

func service(tif *water.Interface, conn net.Conn) {
	buffer := make([]byte, 9000)
	for {
		n, err := conn.Read(buffer)
		if err != nil {
			fmt.Printf("failed to read inbound data: %v\n", err)
			break
		} else {
			fmt.Printf("read %d inbound bytes\n", n)
		}
		logpacket(buffer, "received")
		tif.Write(buffer[:n])
	}
}

func accept(tif *water.Interface, l net.Listener) {
	for {
		conn, err := l.Accept()
		if err != nil {
			fmt.Printf("failed to accept inbound connection: %v\n", err)
		} else {
			go service(tif, conn)
		}
	}
}

func logpacket(buffer []byte, direction string) {
	fmt.Printf("%s packet: src=%s dest=%s\n",
		direction, waterutil.IPv4Source(buffer), waterutil.IPv4Destination(buffer))
}

func main() {
	var port = flag.Int("port", 5050, "listen port")
	var loc = flag.String("dest", "127.0.0.1:5051", "forwarding destination")
	flag.Parse()
	fmt.Printf("listening on %d, sending to %s\n", *port, *loc)

	tif, err := water.NewTUN("tun%d")
	if err != nil {
		fmt.Printf("failed to create interface: %v\n", err)
		return
	}
	fmt.Printf("created %v\n", tif)

	l, err := net.Listen("tcp", fmt.Sprintf("0.0.0.0:%d", *port))
	if err != nil {
		fmt.Printf("failed to listen: %v\n", err)
		return
	}
	go accept(tif, l)

	out := dial(*loc)
	fmt.Printf("connected to %s\n", *loc)
	buffer := make([]byte, 9000)
	for {
		n, err := tif.Read(buffer)
		if err != nil {
			fmt.Printf("failed to read outbound data: %v\n", err)
			break
		} else {
			fmt.Printf("read %d outbound bytes\n", n)
		}
		logpacket(buffer, "sending")
		out.Write(buffer[:n])
	}
}
