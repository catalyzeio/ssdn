package cmd

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/catalyzeio/paas-orchestration/agent"
)

func fail(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, format, args...)
	os.Exit(1)
}

func loadJob(path string) (*agent.JobRequest, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	jr := &agent.JobRequest{}
	err = json.NewDecoder(file).Decode(jr)
	return jr, err
}
