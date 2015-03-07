package main

import (
	"flag"
	"fmt"
	"math/rand"
	"os"
	"time"

	"github.com/catalyzeio/shadowfax/proto"
	"github.com/catalyzeio/shadowfax/registry"
)

func main() {
	flag.Parse()
	config, err := proto.GenerateTLSConfig()
	if err != nil {
		fmt.Printf("Invalid TLS settings: %s\n", err)
		os.Exit(1)
	}

	r := registry.NewRegistry("orchestration", "localhost", 7411, config)
	ads := make([]registry.Advertisement, 2)
	for i := 0; i < len(ads); i++ {
		ads[i] = registry.Advertisement{
			Name:     fmt.Sprintf("key%d", i),
			Location: fmt.Sprintf("val%d", rand.Intn(100)),
		}
	}
	r.Start(ads)

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
