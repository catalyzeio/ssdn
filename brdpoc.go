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
	numBuffers    = 1024
)

type PacketBuffer struct {
	length int
	buffer []byte
	header []byte
	data   []byte
}

func makeBuffers() (in chan *PacketBuffer, out chan *PacketBuffer) {
	in = make(chan *PacketBuffer, numBuffers)
	out = make(chan *PacketBuffer, numBuffers)
	for i := 0; i < numBuffers; i++ {
		pkt := PacketBuffer{}
		pkt.buffer = make([]byte, bufferSize)
		pkt.header = pkt.buffer[:headerSize]
		pkt.data = pkt.buffer[headerSize:]
		in <- &pkt
	}
	return
}

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

func logPacket(buffer []byte, direction string) {
	fmt.Printf("%s packet: src=%s dest=%s\n",
		direction, waterutil.IPv4Source(buffer), waterutil.IPv4Destination(buffer))
}

func forwardIn(tif *water.Interface, read chan *PacketBuffer, written chan *PacketBuffer) {
	for {
		pkt := <-read
		_, err := tif.Write(pkt.data)
		if err != nil {
			fmt.Printf("failed to write inbound data: %v\n", err)
			break
		}
		written <- pkt
	}
}

func service(verbose bool, tif *water.Interface, conn net.Conn) {
	in, out := makeBuffers()
	go forwardIn(tif, out, in)
	for {
		pkt := <-in
		_, err := io.ReadFull(conn, pkt.header)
		if err != nil {
			fmt.Printf("failed to read inbound header: %v\n", err)
			break
		}
		n := int(pkt.header[0])&0x1F<<8 | int(pkt.header[1])
		pkt.data = pkt.buffer[headerSize : headerSize+n]
		_, err = io.ReadFull(conn, pkt.data)
		if err != nil {
			fmt.Printf("failed to read inbound payload: %v\n", err)
			break
		}
		if verbose {
			fmt.Printf("read %d inbound bytes\n", n)
			logPacket(pkt.data, "received")
		}
		pkt.length = n
		out <- pkt
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

func forwardOut(loc *string, tlsConfig *tls.Config, read chan *PacketBuffer, sent chan *PacketBuffer) {
	out := dial(*loc, tlsConfig)
	fmt.Printf("connected to %s\n", *loc)
	for {
		pkt := <-read
		n := pkt.length
		pkt.header[0] = byte((n >> 8) & 0x1F)
		pkt.header[1] = byte(n)
		_, err := out.Write(pkt.buffer[:headerSize+n])
		if err != nil {
			fmt.Printf("failed to send outbound data: %v\n", err)
			break
		}
		sent <- pkt
	}
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
			Certificates:       []tls.Certificate{keyPair},
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

	in, out := makeBuffers()
	go forwardOut(loc, tlsConfig, out, in)
	for {
		pkt := <-in
		n, err := tif.Read(pkt.data)
		if err != nil {
			fmt.Printf("failed to read outbound data: %v\n", err)
			break
		}
		if *verbose {
			fmt.Printf("read %d outbound bytes\n", n)
			logPacket(pkt.data, "sending")
		}
		pkt.length = n
		out <- pkt
	}
}
