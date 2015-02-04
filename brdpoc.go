package main

import (
	"crypto/tls"
	"flag"
	"fmt"
	"io"
	"net"
	"time"

	"github.com/songgao/water"
	"github.com/songgao/water/waterutil"
)

const (
	headerSize    = 2
	maxPacketSize = 9000
	bufferSize    = headerSize + maxPacketSize
)

func dial(loc string, tlsConfig *tls.Config) (valid_conn net.Conn) {
	for {
		var conn net.Conn
		var err error
		if tlsConfig != nil {
			conn, err = tls.Dial("tcp", loc, tlsConfig)
		} else {
			conn, err = net.Dial("tcp", loc)
		}
		if err != nil {
			fmt.Printf("failed to connect to %s: %v\n", loc, err)
		} else {
			valid_conn = conn
			return
		}
		time.Sleep(1 * time.Second)
	}
}

func service(verbose bool, tif *water.Interface, conn net.Conn) {
	buffer := make([]byte, bufferSize)
	header := buffer[:headerSize]
	data := buffer[headerSize:]
	for {
		_, err := io.ReadFull(conn, header)
		if err != nil {
			fmt.Printf("failed to read inbound header: %v\n", err)
			break
		}
		payload_len := int(header[0])&0x1F<<8 | int(header[1])
		payload := data[:payload_len]
		_, err = io.ReadFull(conn, payload)
		if err != nil {
			fmt.Printf("failed to read inbound payload: %v\n", err)
			break
		}
		if verbose {
			fmt.Printf("read %d inbound bytes\n", payload_len)
			logpacket(payload, "received")
		}
		_, err = tif.Write(payload)
		if err != nil {
			fmt.Printf("failed to write inbound data: %v\n", err)
		}
	}
}

func accept(verbose bool, tif *water.Interface, l net.Listener) {
	for {
		conn, err := l.Accept()
		if err != nil {
			fmt.Printf("failed to accept inbound connection: %v\n", err)
			break
		}
		go service(verbose, tif, conn)
	}
}

func logpacket(buffer []byte, direction string) {
	fmt.Printf("%s packet: src=%s dest=%s\n",
		direction, waterutil.IPv4Source(buffer), waterutil.IPv4Destination(buffer))
}

func main() {
	port := flag.Int("port", 5050, "listen port")
	loc := flag.String("dest", "127.0.0.1:5051", "forwarding destination")
	verbose := flag.Bool("verbose", false, "verbose logging")
	cert := flag.String("cert", "", "TLS certificate")
	key := flag.String("key", "", "TLS private key")
	flag.Parse()
	fmt.Printf("listening on %d, sending to %s\n", *port, *loc)

	tif, err := water.NewTUN("tun%d")
	if err != nil {
		fmt.Printf("failed to create interface: %v\n", err)
		return
	}
	fmt.Printf("created %v\n", tif)

	var tlsConfig *tls.Config = nil
	if len(*cert) > 0 {
		fmt.Printf("using TLS certificate, key: %s, %s\n", *cert, *key)
		keyPair, err := tls.LoadX509KeyPair(*cert, *key)
		if err != nil {
			fmt.Printf("failed to load key pair: %v\n", err)
			return
		}
		tlsConfig = &tls.Config{
			Certificates: []tls.Certificate{keyPair},
			InsecureSkipVerify: true,
		}
	}

	var l net.Listener
	if tlsConfig != nil {
		l, err = tls.Listen("tcp", fmt.Sprintf("0.0.0.0:%d", *port), tlsConfig)
	} else {
		l, err = net.Listen("tcp", fmt.Sprintf("0.0.0.0:%d", *port))
	}
	if err != nil {
		fmt.Printf("failed to listen: %v\n", err)
		return
	}
	go accept(*verbose, tif, l)

	out := dial(*loc, tlsConfig)
	fmt.Printf("connected to %s\n", *loc)
	buffer := make([]byte, bufferSize)
	header := buffer[:headerSize]
	data := buffer[headerSize:]
	for {
		n, err := tif.Read(data)
		if err != nil {
			fmt.Printf("failed to read outbound data: %v\n", err)
			break
		}
		header[0] = byte((n >> 8) & 0x1F)
		header[1] = byte(n)
		if *verbose {
			fmt.Printf("read %d outbound bytes\n", n)
			logpacket(data, "sending")
		}
		_, err = out.Write(buffer[:headerSize+n])
		if err != nil {
			fmt.Printf("failed to send outbound data: %v\n", err)
		}
	}
}
