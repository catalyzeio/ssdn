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

const (
	ErrorPrefix = "Error: "
)

type CommandHandler func(args ...string) (string, error)

type CommandListener struct {
	dsPath   string
	handlers map[string]*entry

	start   time.Time
	command chan bool
}

type entry struct {
	command     string
	usage       string
	description string
	minArgs     int
	maxArgs     int
	handler     CommandHandler
}

const (
	bufSize = 1 << 18 // 64 KiB
	delim   = '\n'
)

func NewServer(baseDir, tenant string) *CommandListener {
	c := CommandListener{
		dsPath:   path.Join(baseDir, tenant),
		handlers: make(map[string]*entry),
		command:  make(chan bool),
	}
	c.Register("uptime", "", "Displays process uptime", 0, 0, c.uptime)
	c.Register("help", "[command]", "Shows help on available commands", 0, 1, c.help)
	return &c
}

func (c *CommandListener) Register(command, usage, description string, minArgs, maxArgs int, handler CommandHandler) {
	c.handlers[command] = &entry{command, usage, description, minArgs, maxArgs, handler}
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
			response, err = c.dispatch(args[0], args[1:])
			if err != nil {
				response = ErrorPrefix + err.Error()
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

func (c *CommandListener) dispatch(cmd string, args []string) (string, error) {
	entry, err := c.disambiguate(cmd)
	if err != nil {
		return "", err
	}

	n := len(args)
	if n < entry.minArgs || (entry.maxArgs >= 0 && n > entry.maxArgs) {
		return "", fmt.Errorf("invalid number of arguments to command '%s'", entry.command)
	}
	return entry.handler(args...)
}

func (c *CommandListener) disambiguate(cmd string) (*entry, error) {
	match, present := c.handlers[cmd]
	if present {
		return match, nil
	}

	var candidate *entry
	for _, e := range c.handlers {
		if strings.HasPrefix(e.command, cmd) {
			if candidate != nil {
				return nil, fmt.Errorf("ambiguous command '%s'", cmd)
			}
			candidate = e
		}
	}
	if candidate == nil {
		return nil, fmt.Errorf("unknown command '%s'", cmd)
	}
	return candidate, nil
}

func (c *CommandListener) uptime(args ...string) (string, error) {
	return time.Now().Sub(c.start).String(), nil
}

func (c *CommandListener) help(args ...string) (string, error) {
	if len(args) > 0 {
		entry, err := c.disambiguate(args[0])
		if err != nil {
			return "", err
		}
		usage := entry.usage
		if len(usage) > 0 {
			usage = " " + usage
		}
		return fmt.Sprintf("%s%s: %s", entry.command, usage, entry.description), nil
	}

	msg := []string{"Available commands:"}
	for k := range c.handlers {
		msg = append(msg, k)
	}
	return strings.Join(msg, " "), nil
}