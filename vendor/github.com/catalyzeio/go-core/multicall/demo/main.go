package main

import (
	"flag"
	"fmt"

	"github.com/catalyzeio/go-core/multicall"
)

func cmd1() {
	f := flag.String("flag1", "", "test flag 1")
	flag.Parse()
	fmt.Printf("-flag1 was %s\n", *f)
}

func cmd2() {
	f := flag.String("flag2", "", "test flag 2")
	flag.Parse()
	fmt.Printf("-flag2 was %s\n", *f)
}

func main() {
	multicall.Start("demo", multicall.Commands{
		"cmd1": cmd1,
		"cmd2": cmd2,
	})
}
