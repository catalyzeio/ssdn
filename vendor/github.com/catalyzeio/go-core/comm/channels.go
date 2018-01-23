package comm

import (
	"io"
	"net"
	"sync/atomic"
	"time"
)

type MessageReader func(conn net.Conn) (interface{}, error)
type MessageWriter func(conn net.Conn, msg interface{}) error

type IOChannels struct {
	In   <-chan interface{}
	Out  chan<- interface{}
	Done <-chan struct{}

	conn   net.Conn
	reader MessageReader
	writer MessageWriter

	doneClosed int32 // atomic int32

	halt       chan struct{}
	haltClosed int32 // atomic int32
}

func WrapIO(conn net.Conn, reader MessageReader, writer MessageWriter, queueSize int, deadline time.Duration) *IOChannels {
	return newIOChannels(conn, reader, writer, queueSize, deadline)
}

func WrapI(conn net.Conn, reader MessageReader, queueSize int, deadline time.Duration) *IOChannels {
	return newIOChannels(conn, reader, nil, queueSize, deadline)
}

func WrapO(conn net.Conn, writer MessageWriter, queueSize int, deadline time.Duration) *IOChannels {
	return newIOChannels(conn, nil, writer, queueSize, deadline)
}

func newIOChannels(conn net.Conn, reader MessageReader, writer MessageWriter, queueSize int, deadline time.Duration) *IOChannels {
	done := make(chan struct{})
	halt := make(chan struct{})
	io := IOChannels{
		Done:   done,
		conn:   conn,
		reader: reader,
		writer: writer,
		halt:   halt,
	}
	if reader != nil {
		in := make(chan interface{}, queueSize)
		io.In = in
		go io.read(in, deadline, done)
	}
	if writer != nil {
		out := make(chan interface{}, queueSize)
		io.Out = out
		go io.write(out, deadline, done)
	}
	return &io
}

func (io *IOChannels) Stop() {
	if atomic.CompareAndSwapInt32(&io.haltClosed, 0, 1) {
		close(io.halt)
	}
}

func (io *IOChannels) finished(done chan struct{}) {
	if atomic.CompareAndSwapInt32(&io.doneClosed, 0, 1) {
		close(done)
	}
}

func (io *IOChannels) read(in chan<- interface{}, deadline time.Duration, done chan struct{}) {
	defer io.finished(done)

	halt, conn, reader := io.halt, io.conn, io.reader

	for {
		if deadline > 0 {
			if err := conn.SetReadDeadline(time.Now().Add(deadline)); err != nil {
				if shouldLog(err, halt) {
					log.Warn("Failed to set read deadline for %s: %s", conn.RemoteAddr(), err)
				}
				return
			}
		}
		msg, err := reader(conn)
		if err != nil {
			if shouldLog(err, halt) {
				log.Warn("Failed to receive message from %s: %s", conn.RemoteAddr(), err)
			}
			return
		}
		if msg == nil {
			continue
		}

		select {
		case <-halt:
			return
		case in <- msg:
		}
	}
}

func (io *IOChannels) write(out <-chan interface{}, deadline time.Duration, done chan struct{}) {
	defer io.finished(done)

	halt, conn, writer := io.halt, io.conn, io.writer

	for {
		var msg interface{}
		select {
		case <-halt:
			return
		case msg = <-out:
		}

		if deadline > 0 {
			if err := conn.SetWriteDeadline(time.Now().Add(deadline)); err != nil {
				if shouldLog(err, halt) {
					log.Warn("Failed to set write deadline for %s: %s", conn.RemoteAddr(), err)
				}
				return
			}
		}
		err := writer(conn, msg)
		if err != nil {
			if shouldLog(err, halt) {
				log.Warn("Failed to receive message from %s: %s", conn.RemoteAddr(), err)
			}
			return
		}
	}
}

func shouldLog(err error, halt <-chan struct{}) bool {
	// disconnections are not enough to complain about
	if err == io.EOF {
		return false
	}
	// ignore any errors that occur after a stop request
	select {
	case <-halt:
		return false
	default:
	}
	// everything else gets logged
	return true
}
