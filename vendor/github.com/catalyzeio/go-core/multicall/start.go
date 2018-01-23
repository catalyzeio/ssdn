package multicall

import (
	"fmt"
	"os"
	"path"
	"strings"
)

type Commands map[string]func()

func Start(name string, avail Commands) {
	args := os.Args
	if args == nil || len(args) < 1 {
		Usage(name, avail)
	}

	cmd := path.Base(args[0])
	f, present := avail[cmd]
	if present {
		f()
		return
	}

	if len(args) > 1 {
		cmd = args[1]
		f, present := avail[cmd]
		if present {
			os.Args = args[1:]
			f()
			return
		}
		fmt.Printf("Error: Unrecognized command %s\n", cmd)
	}

	Usage(name, avail)
}

func Usage(name string, avail Commands) {
	fmt.Printf("%s multi-call binary\n\n", name)
	keys := make([]string, 0, len(avail))
	for k := range avail {
		keys = append(keys, k)
	}
	fmt.Printf("Available commands: %s\n", strings.Join(keys, ", "))
	os.Exit(1)
}
