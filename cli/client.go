package cli

import (
	"bufio"
	"net"
	"path"
	"strings"
)

type Client struct {
	dsPath string

	conn   net.Conn
	reader *bufio.Reader
}

func NewClient(baseDir string, name string) *Client {
	c := Client{
		dsPath: path.Join(baseDir, name),
	}
	return &c
}

func (c *Client) Connect() error {
	conn, err := net.Dial("unix", c.dsPath)
	if err != nil {
		return err
	}
	c.conn = conn
	c.reader = bufio.NewReaderSize(conn, bufSize)
	return nil
}

func (c *Client) Close() error {
	if c.conn == nil {
		return nil
	}
	return c.conn.Close()
}

func (c *Client) CallWithArgs(args ...string) (*string, error) {
	return c.Call(strings.Join(args, " "))
}

func (c *Client) Call(request string) (*string, error) {
	_, err := c.conn.Write(append([]byte(request), delim))
	if err != nil {
		return nil, err
	}
	result, err := c.reader.ReadString(delim)
	if err != nil {
		return nil, err
	}
	return &result, nil
}
