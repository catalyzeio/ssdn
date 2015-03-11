package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/catalyzeio/shadowfax/cli"
)

func fail(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, format, args...)
	os.Exit(1)
}

func show(resp *string, err error) {
	if err != nil {
		fail("Request failed: %s\n", err)
	}
	if resp != nil {
		print(*resp)
	}
}

func check(resp *string, err error) {
	if err == nil && resp != nil {
		s := *resp
		if strings.HasPrefix(s, cli.ErrorPrefix) {
			fail(s)
		}
	}
	show(resp, err)
}

func main() {
	tenantFlag := flag.String("tenant", "", "tenant identifier (required)")
	runDirFlag := flag.String("rundir", "/var/run/shadowfax", "server socket directory")
	flag.Parse()

	if len(*tenantFlag) == 0 {
		fail("Missing -tenant argument\n")
	}

	client := cli.NewClient(*runDirFlag, *tenantFlag)
	err := client.Connect()
	if err != nil {
		fail("Could not connect to server: %s\n", err)
	}

	defer client.Close()

	args := flag.Args()
	if len(args) > 0 {
		check(client.CallWithArgs(args...))
		return
	}

	s := bufio.NewScanner(os.Stdin)
	print("> ")
	for s.Scan() {
		show(client.Call(s.Text()))
		print("> ")
	}
}
