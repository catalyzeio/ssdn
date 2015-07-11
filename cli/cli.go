package cli

import (
	"fmt"
	"sort"
	"strings"

	"github.com/catalyzeio/ssdn/overlay"
)

type Handler func(args ...string) (string, error)

type CLI struct {
	client *overlay.Client

	handlers map[string]*entry
}

type entry struct {
	command     string
	usage       string
	description string
	minArgs     int
	maxArgs     int
	handler     Handler
}

func NewCLI(client *overlay.Client) *CLI {
	c := &CLI{client, make(map[string]*entry)}

	c.Register("help", "[command]", "Shows help on available commands", 0, 1, c.help)
	c.Register("status", "", "Displays process status", 0, 0, c.status)

	c.Register("attach", "[container]", "Attaches the given container to this overlay network", 1, 1, c.attach)
	c.Register("detach", "[container]", "Detaches the given container from this overlay network", 1, 1, c.detach)

	return c
}

func (c *CLI) Register(command, usage, description string, minArgs, maxArgs int, handler Handler) {
	c.handlers[command] = &entry{command, usage, description, minArgs, maxArgs, handler}
}

func (c *CLI) Call(args ...string) (string, error) {
	if len(args) == 0 {
		return "", nil
	}
	return c.dispatch(args[0], args[1:])
}

func (c *CLI) dispatch(cmd string, args []string) (string, error) {
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

func (c *CLI) disambiguate(cmd string) (*entry, error) {
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

func (c *CLI) help(args ...string) (string, error) {
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
	sort.Strings(msg[1:])
	return strings.Join(msg, " "), nil
}

func (c *CLI) status(args ...string) (string, error) {
	res, err := c.client.Status()
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("Uptime: %s", res["uptime"]), nil
}

func (c *CLI) attach(args ...string) (string, error) {
	container := args[0]
	req := &overlay.AttachRequest{Container: container}
	err := c.client.Attach(req)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("Attached to %s", container), nil
}

func (c *CLI) detach(args ...string) (string, error) {
	container := args[0]
	err := c.client.Detach(container)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("Detached from %s", container), nil
}
