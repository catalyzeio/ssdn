package cmd

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/catalyzeio/go-core/simplelog"

	"github.com/catalyzeio/ssdn/cli"
	"github.com/catalyzeio/ssdn/overlay"
)

func StartSSDN() {
	simplelog.AddFlags()
	overlay.AddTenantFlags()
	overlay.AddDirFlags()
	flag.Parse()

	tenant, _, err := overlay.GetTenantFlags()
	if err != nil {
		fail("Invalid tenant config: %s\n", err)
	}

	runDir, _, err := overlay.GetDirFlags()
	if err != nil {
		fail("Invalid directory config: %s\n", err)
	}

	client, err := overlay.NewClient(tenant, runDir)
	if err != nil {
		fail("Invalid client config: %s\n", err)
	}

	cmd := cli.NewCLI(client)

	args := flag.Args()
	if len(args) > 0 {
		checkCall(cmd, args, true)
		return
	}

	s := bufio.NewScanner(os.Stdin)
	fmt.Printf("> ")
	for s.Scan() {
		args := strings.Split(s.Text(), " ")
		checkCall(cmd, args, false)
		fmt.Printf("> ")
	}
}

func checkCall(cmd *cli.CLI, args []string, die bool) {
	res, err := cmd.Call(args...)
	if err != nil {
		if die {
			fail("Operation failed: %s\n", err)
		}
		fmt.Printf("Operation failed: %s\n", err)
	}
	if len(res) > 0 {
		fmt.Println(res)
	}
}
