package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"

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

func main() {
	tenantFlag := flag.String("tenant", "", "tenant identifier (required)")
	cliDirFlag := flag.String("rundir", "/var/run/shadowfax", "server socket directory")
	flag.Parse()

	if len(*tenantFlag) == 0 {
		fail("Missing -tenant argument\n")
	}

	client := cli.NewClient(*cliDirFlag, *tenantFlag)
	err := client.Connect()
	if err != nil {
		fail("Could not connect to server: %s\n", err)
	}

	defer client.Close()

	args := flag.Args()
	if len(args) > 0 {
		show(client.CallWithArgs(args...))
		return
	}

	s := bufio.NewScanner(os.Stdin)
	print("> ")
	for s.Scan() {
		show(client.Call(s.Text()))
		print("> ")
	}
}
