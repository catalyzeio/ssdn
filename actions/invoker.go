package actions

import (
	"fmt"
	"log"
	"os/exec"
	"path"
)

// TODO execute requests serially

type ActionInvoker struct {
	dir string
}

func NewInvoker(dir string) *ActionInvoker {
	return &ActionInvoker{dir}
}

func (a *ActionInvoker) Execute(action string, args ...string) error {
	target := path.Join(a.dir, action)
	cmd := exec.Command(target, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("command {%s %s} failed (%s): %s", target, args, err, string(output))
	}
	log.Printf("Command {%s %s} succeeded: %s", target, args, string(output))
	return nil
}
