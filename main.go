package main

import (
	"fmt"
	"os"

	"github.com/catalyzeio/ssdn/cmd"
)

func main() {
	fmt.Printf("%+v\n", os.Args)
	/*multicall.Start("Secure SDN", multicall.Commands{
		"ssdn":     cmd.StartSSDN,
		"cdns":     cmd.StartCDNS,
		"l2link":   cmd.StartL2Link,
		"l3bridge": cmd.StartL3Bridge,
		"l3direct": cmd.StartL3Direct,
		"l3node":   cmd.StartL3Node,
	})*/
	cmd.StartL3Node()
}
