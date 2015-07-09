package main

import (
	"github.com/catalyzeio/go-core/multicall"

	"github.com/catalyzeio/ssdn/cmd"
)

func main() {
	multicall.Start("Secure SDN", multicall.Commands{
		"cdns":     cmd.StartCDNS,
		"l2link":   cmd.StartL2Link,
		"l3bridge": cmd.StartL3Bridge,
		"l3direct": cmd.StartL3Direct,
	})
}
