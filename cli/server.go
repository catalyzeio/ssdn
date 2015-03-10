package cli

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"path"
	"strings"
	"time"
)

type CommandHandler func(args ...string) (string, error)

type CommandListener struct {
	dsPath  string
	handler CommandHandler

	start   time.Time
	command chan bool
}

const (
	bufSize = 1 << 18 // 64 KiB
	delim   = '\n'
)

func NewServer(baseDir string, tenant string, handler CommandHandler) *CommandListener {
	c := CommandListener{
		dsPath:  path.Join(baseDir, tenant),
		handler: handler,
		command: make(chan bool),
	}
	return &c
}

func (c *CommandListener) Start() error {
	// remove any existing domain socket
	_, err := os.Stat(c.dsPath)
	if err == nil {
		err := os.Remove(c.dsPath)
		if err != nil {
			return err
		}
		log.Printf("Removed existing socket at %s", c.dsPath)
	}
	// create new socket and start up listener
	l, err := net.Listen("unix", c.dsPath)
	if err != nil {
		return err
	}
	go c.listen(l)
	return nil
}

func (c *CommandListener) Stop() {
	c.command <- true
}

func (c *CommandListener) listen(l net.Listener) {
	defer l.Close()

	go c.accept(l)
	for {
		select {
		case <-c.command:
			log.Printf("CLI shutting down")
			return
		}
	}
}

func (c *CommandListener) accept(l net.Listener) {
	log.Printf("CLI accepting commands at %s", c.dsPath)
	c.start = time.Now()
	for {
		conn, err := l.Accept()
		if err != nil {
			log.Printf("Error accepting connection: %s", err)
			return
		}
		go c.service(conn)
	}
}

func (c *CommandListener) service(conn net.Conn) {
	defer conn.Close()

	r := bufio.NewReaderSize(conn, bufSize)
	for {
		request, err := r.ReadString(delim)
		if err == io.EOF {
			return
		}
		if err != nil {
			log.Printf("Error receiving request: %s", err)
			return
		}
		log.Printf("CLI <- %s", request)

		args := strings.Fields(request)
		response := ""
		if len(args) > 0 {
			if args[0] == "uptime" {
				response = time.Now().Sub(c.start).String()
			} else {
				response, err = c.handler(args...)
				if err != nil {
					response = fmt.Sprintf("Error: %s", err)
				}
			}
		}
		log.Printf("CLI -> %s", response)

		_, err = conn.Write(append([]byte(response), delim))
		if err != nil {
			log.Printf("Error sending response: %s", err)
			return
		}
	}
}
