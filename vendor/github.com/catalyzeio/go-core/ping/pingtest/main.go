package main

import (
	"flag"
	"fmt"
	"net"
	"os"
	"time"

	"github.com/catalyzeio/go-core/ping"
	"github.com/catalyzeio/go-core/simplelog"
)

func fail(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, format, args...)
	os.Exit(1)
}

func main() {
	simplelog.AddFlags()
	addressFlag := flag.String("address", "0.0.0.0", "listen address")
	flag.Parse()

	p, err := ping.NewPinger(*addressFlag)
	if err != nil {
		fail("Failed to create pinger: %s\n", err)
	}

	for {
		for _, v := range flag.Args() {
			ip := net.ParseIP(v)
			if ip == nil {
				fail("Invalid IP address: %s\n", v)
			}
			start := time.Now()
			if err := p.PingSync(ip, 1*time.Second); err != nil {
				fmt.Printf("Failed to ping %s: %s\n", ip, err)
			} else {
				fmt.Printf("Received response from %s in %s\n", ip, time.Since(start))
			}
			time.Sleep(1 * time.Second)
		}
	}
}
