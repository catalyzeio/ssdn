package main

import (
	"github.com/catalyzeio/go-core/multicall"

	"github.com/catalyzeio/paas-orchestration/cmd"
)

func main() {
	multicall.Start("PaaS Orchestration", multicall.Commands{
		"regcli":      cmd.RegCLI,
		"regserver":   cmd.RegServer,
		"regwatch":    cmd.RegWatch,
		"agentcli":    cmd.AgentCLI,
		"agentserver": cmd.AgentServer,
		"schedserver": cmd.SchedServer,
	})
}
