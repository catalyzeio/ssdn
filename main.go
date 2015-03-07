package main

import (
	"fmt"
	"time"

	"github.com/catalyzeio/shadowfax/registry"
)

func main() {
	r := registry.NewRegistry("orchestration", "localhost", 7411, nil)
	r.Start(nil)
	for i := 0; i < 3; i++ {
		fmt.Printf("querying\n")
		a, err := r.QueryAll("orchestration_agent")
		if err == nil {
			fmt.Printf("query result: %v\n", a)
		} else {
			fmt.Printf("query failed: %v\n", err)
		}
		time.Sleep(3 * time.Second)
	}
	time.Sleep(1 * time.Hour)
}
