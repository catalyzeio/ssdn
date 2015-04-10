package cli

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"os"
	"path"
	"sort"
	"strings"
	"time"
)

const (
	ErrorPrefix = "Error: "
)

type Handler func(args ...string) (string, error)

type Listener struct {
	dsPath   string
	handlers map[string]*entry

	start   time.Time
	control chan struct{}
}

type entry struct {
	command     string
	usage       string
	description string
	minArgs     int
	maxArgs     int
	handler     Handler
}

const (
	bufSize = 1 << 18 // 64 KiB
	delim   = '\n'
)

func NewServer(baseDir, name string) *Listener {
	s := Listener{
		dsPath:   path.Join(baseDir, name),
		handlers: make(map[string]*entry),
		control:  make(chan struct{}),
	}
	s.Register("uptime", "", "Displays process uptime", 0, 0, s.uptime)
	s.Register("help", "[command]", "Shows help on available commands", 0, 1, s.help)
	return &s
}

func (s *Listener) Register(command, usage, description string, minArgs, maxArgs int, handler Handler) {
	s.handlers[command] = &entry{command, usage, description, minArgs, maxArgs, handler}
}

func (s *Listener) Start() error {
	// check for existing domain socket
	_, err := os.Stat(s.dsPath)
	if err == nil {
		// check if existing socket is live
		conn, err := net.Dial("unix", s.dsPath)
		if err == nil {
			conn.Close()
			return fmt.Errorf("%s exists and is accepting connections; is there another instance running?", s.dsPath)
		}
		// remove the existing domain socket
		if err := os.Remove(s.dsPath); err != nil {
			return err
		}
		log.Warn("Removed existing socket at %s", s.dsPath)
	}
	// create new socket and start up listener
	l, err := net.Listen("unix", s.dsPath)
	if err != nil {
		return err
	}
	go s.listen(l)
	return nil
}

func (s *Listener) Stop() {
	s.control <- struct{}{}
}

func (s *Listener) listen(l net.Listener) {
	defer l.Close()

	go s.accept(l)

	for {
		select {
		case <-s.control:
			log.Info("Shutting down")
			return
		}
	}
}

func (s *Listener) accept(l net.Listener) {
	log.Info("Accepting commands at %s", s.dsPath)
	s.start = time.Now()
	for {
		conn, err := l.Accept()
		if err != nil {
			log.Warn("Failed to accept connection: %s", err)
			return
		}
		go s.service(conn)
	}
}

func (s *Listener) service(conn net.Conn) {
	defer conn.Close()

	r := bufio.NewReaderSize(conn, bufSize)
	for {
		request, err := r.ReadString(delim)
		if err == io.EOF {
			return
		}
		if err != nil {
			log.Warn("Faild to receive request: %s", err)
			return
		}
		if log.IsDebugEnabled() {
			log.Debug("<- %s", request)
		}

		args := strings.Fields(request)
		response := ""
		if len(args) > 0 {
			response, err = s.dispatch(args[0], args[1:])
			if err != nil {
				response = ErrorPrefix + err.Error()
			}
		}
		response = strings.Replace(response, string(delim), "; ", -1)
		if log.IsDebugEnabled() {
			log.Debug("-> %s", response)
		}

		_, err = conn.Write(append([]byte(response), delim))
		if err != nil {
			log.Warn("Failed to send response: %s", err)
			return
		}
	}
}

func (s *Listener) dispatch(cmd string, args []string) (string, error) {
	entry, err := s.disambiguate(cmd)
	if err != nil {
		return "", err
	}

	n := len(args)
	if n < entry.minArgs || (entry.maxArgs >= 0 && n > entry.maxArgs) {
		return "", fmt.Errorf("invalid number of arguments to command '%s'", entry.command)
	}
	return entry.handler(args...)
}

func (s *Listener) disambiguate(cmd string) (*entry, error) {
	match, present := s.handlers[cmd]
	if present {
		return match, nil
	}

	var candidate *entry
	for _, e := range s.handlers {
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

func (s *Listener) uptime(args ...string) (string, error) {
	return time.Now().Sub(s.start).String(), nil
}

func (s *Listener) help(args ...string) (string, error) {
	if len(args) > 0 {
		entry, err := s.disambiguate(args[0])
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
	for k := range s.handlers {
		msg = append(msg, k)
	}
	sort.Strings(msg[1:])
	return strings.Join(msg, " "), nil
}
