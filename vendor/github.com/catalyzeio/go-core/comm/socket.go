package comm

import (
	"fmt"
	"net"
	"os"
)

func DomainSocketListener(path string) (net.Listener, error) {
	_, err := os.Stat(path)
	if err == nil {
		conn, err := net.Dial("unix", path)
		if err == nil {
			conn.Close()
			return nil, fmt.Errorf("%s exists and is accepting connections; is there another instance running?", path)
		}
		if err := os.Remove(path); err != nil {
			return nil, err
		}
		log.Warn("Removed existing domain socket at %s", path)
	}
	return net.Listen("unix", path)
}
