package main

import (
	"flag"
	"fmt"
	"io"
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
	buffer := make([]byte, 9000+2)
	for {
		n, err := io.ReadAtLeast(conn, buffer, 2)
		if err != nil {
			fmt.Printf("failed to read inbound data: %v\n", err)
			break
		}
		fmt.Printf("read %d bytes\n", n)
		payload_len := int(buffer[0])&0x1F<<8 | int(buffer[1])
		payload_rem := 2 + payload_len - n
		if payload_rem > 0 {
			fmt.Printf("reading %d more bytes\n", payload_rem)
			_, err = io.ReadFull(conn, buffer[n:n+payload_rem])
			if err != nil {
				fmt.Printf("failed to read inbound data: %v\n", err)
				break
			}
		}
		fmt.Printf("read %d inbound bytes\n", payload_len)
		payload := buffer[2:2+payload_len]
		logpacket(payload, "received")
		_, err = tif.Write(payload)
		if err != nil {
			fmt.Printf("failed to write inbound data: %v\n", err)
		}
	}
}

func accept(tif *water.Interface, l net.Listener) {
	for {
		conn, err := l.Accept()
		if err != nil {
			fmt.Printf("failed to accept inbound connection: %v\n", err)
			break
		}
		go service(tif, conn)
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
	buffer := make([]byte, 9000+2)
	payload := buffer[2:]
	for {
		n, err := tif.Read(payload)
		if err != nil {
			fmt.Printf("failed to read outbound data: %v\n", err)
			break
		}
		fmt.Printf("read %d outbound bytes\n", n)
		buffer[0] = byte((n >> 8) & 0x1F)
		buffer[1] = byte(n)
		logpacket(payload, "sending")
		_, err = out.Write(buffer[:n+2])
		if err != nil {
			fmt.Printf("failed to send outbound data: %v\n", err)
		}
	}
}
