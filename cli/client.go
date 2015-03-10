package cli

import (
	"bufio"
	"net"
	"path"
	"strings"
)

type CommandClient struct {
	dsPath string

	conn   net.Conn
	reader *bufio.Reader
}

func NewClient(baseDir string, tenant string) *CommandClient {
	c := CommandClient{
		dsPath: path.Join(baseDir, tenant),
	}
	return &c
}

func (c *CommandClient) Connect() error {
	conn, err := net.Dial("unix", c.dsPath)
	if err != nil {
		return err
	}
	c.conn = conn
	c.reader = bufio.NewReaderSize(conn, bufSize)
	return nil
}

func (c *CommandClient) Close() error {
	if c.conn == nil {
		return nil
	}
	return c.conn.Close()
}

func (c *CommandClient) CallWithArgs(args ...string) (*string, error) {
	return c.Call(strings.Join(args, " "))
}

func (c *CommandClient) Call(request string) (*string, error) {
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
