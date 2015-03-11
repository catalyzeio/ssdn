package actions

import (
	"fmt"
	"log"
	"os/exec"
	"path"
)

type ActionInvoker struct {
	dir      string
	requests chan *actionReq
	command  chan bool
}

type actionReq struct {
	action string
	args   []string
	result chan *actionResp
}

type actionResp struct {
	output []byte
	err    error
}

func NewInvoker(dir string) *ActionInvoker {
	return &ActionInvoker{
		dir:      dir,
		requests: make(chan *actionReq),
		command:  make(chan bool),
	}
}

func (a *ActionInvoker) Start() {
	go a.run()
}

func (a *ActionInvoker) Stop() {
	a.command <- true
}

func (a *ActionInvoker) Execute(action string, args ...string) ([]byte, error) {
	req := actionReq{action, args, make(chan *actionResp, 1)}
	a.requests <- &req
	resp := <-req.result
	return resp.output, resp.err
}

func (a *ActionInvoker) run() {
	for {
		select {
		case <-a.command:
			return
		case req := <-a.requests:
			a.invoke(req)
		}
	}
}

func (a *ActionInvoker) invoke(req *actionReq) {
	target := path.Join(a.dir, req.action)
	cmd := exec.Command(target, req.args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		err = fmt.Errorf("{%s %s} failed (%s): %s", target, req.args, err, string(output))
	} else {
		log.Printf("Command {%s %s} succeeded: %s", target, req.args, string(output))
	}
	req.result <- &actionResp{output, err}
}
