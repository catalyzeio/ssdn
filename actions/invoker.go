package actions

import (
	"fmt"
	"os/exec"
	"path"
)

type Invoker struct {
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

func NewInvoker(dir string) *Invoker {
	return &Invoker{
		dir:      dir,
		requests: make(chan *actionReq),
		command:  make(chan bool),
	}
}

func (a *Invoker) Start() {
	go a.run()
}

func (a *Invoker) Stop() {
	a.command <- true
}

func (a *Invoker) Execute(action string, args ...string) ([]byte, error) {
	req := actionReq{action, args, make(chan *actionResp, 1)}
	a.requests <- &req
	resp := <-req.result
	return resp.output, resp.err
}

func (a *Invoker) run() {
	for {
		select {
		case <-a.command:
			return
		case req := <-a.requests:
			a.invoke(req)
		}
	}
}

func (a *Invoker) invoke(req *actionReq) {
	target := path.Join(a.dir, req.action)
	cmd := exec.Command(target, req.args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		err = fmt.Errorf("{%s %s} failed (%s): %s", target, req.args, err, string(output))
	} else {
		log.Debug("Command {%s %s} succeeded: %s", target, req.args, string(output))
	}
	req.result <- &actionResp{output, err}
}
