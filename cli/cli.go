package cli

import (
	"bytes"
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
	c.Register("connections", "", "Lists all containers attached to this overlay network", 0, 0, c.connections)

	c.Register("addpeer", "[proto://host:port]", "Adds a peer at the specified address", 1, 1, c.addPeer)
	c.Register("delpeer", "[proto://host:port]", "Deletes the peer at the specified address", 1, 1, c.delPeer)
	c.Register("peers", "", "List all active peers", 0, 0, c.peers)

	c.Register("routes", "", "List all available routes", 0, 0, c.routes)

	// TODO arp, resolve

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
	return fmt.Sprintf("Status\n  Uptime: %s", res.Uptime), nil
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

func (c *CLI) connections(args ...string) (string, error) {
	connections, err := c.client.ListConnections()
	if err != nil {
		return "", err
	}
	res := []string{"Connections:"}
	for k, v := range connections {
		var buf bytes.Buffer
		fmt.Fprintf(&buf, "  %s", k)
		if len(v.Interface) > 0 {
			fmt.Fprintf(&buf, " via %s", v.Interface)
		}
		ip, mac := len(v.IP) > 0, len(v.MAC) > 0
		if ip && mac {
			fmt.Fprintf(&buf, " (%s/%s)", v.IP, v.MAC)
		} else if ip {
			fmt.Fprintf(&buf, " (%s)", v.IP)
		} else if mac {
			fmt.Fprintf(&buf, " (%s)", v.MAC)
		}
		res = append(res, buf.String())
	}
	return strings.Join(res, "\n"), nil
}

func (c *CLI) addPeer(args ...string) (string, error) {
	peer := args[0]
	err := c.client.AddPeer(peer)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("Added peer %s", peer), nil
}

func (c *CLI) delPeer(args ...string) (string, error) {
	peer := args[0]
	err := c.client.DeletePeer(peer)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("Deleted peer %s", peer), nil
}

func (c *CLI) peers(args ...string) (string, error) {
	peers, err := c.client.ListPeers()
	if err != nil {
		return "", err
	}
	res := []string{"Peers:"}
	for k, v := range peers {
		res = append(res, fmt.Sprintf("  %s (%s)", k, v.Type))
	}
	return strings.Join(res, "\n"), nil
}

func (c *CLI) routes(args ...string) (string, error) {
	routes, err := c.client.ListRoutes()
	if err != nil {
		return "", err
	}
	res := []string{"Routes:"}
	for _, v := range routes {
		res = append(res, fmt.Sprintf("  %s", v))
	}
	return strings.Join(res, "\n"), nil
}
