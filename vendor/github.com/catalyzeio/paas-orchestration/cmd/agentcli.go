package cmd

import (
	"encoding/json"
	"flag"
	"fmt"

	"github.com/catalyzeio/go-core/comm"
	"github.com/catalyzeio/go-core/simplelog"
	"github.com/pborman/uuid"

	"github.com/catalyzeio/paas-orchestration/agent"
)

func AgentCLI() {
	simplelog.AddFlags()
	comm.AddTLSFlags()
	agent.AddFlags(true)
	killJobFlag := flag.String("kill", "", "kill a job")
	stopJobFlag := flag.String("stop", "", "stop a job")
	bidJobFlag := flag.String("bid", "", "bid on a job")
	offerJobFlag := flag.String("offer", "", "offer job to agent")
	listJobFlag := flag.String("job", "", "list a specific job on the agent")
	listJobsFlag := flag.Bool("jobs", false, "list jobs running on the agent")
	getUsageFlag := flag.Bool("usage", false, "report agent usage")
	getModeFlag := flag.Bool("mode", false, "report agent mode")
	setModeFlag := flag.String("set-mode", "", "set agent mode")
	flag.Parse()

	config, err := comm.GenerateTLSConfig(false)
	if err != nil {
		fail("Invalid TLS config: %s\n", err)
	}

	client, err := agent.GenerateClient(config)
	if err != nil {
		fail("Failed to start agent client: %s\n", err)
	}
	if client == nil {
		fail("Invalid agent config: -agent is required\n")
	}
	client.Start()

	killJobID := *killJobFlag
	if len(killJobID) > 0 {
		err := client.Kill(killJobID)
		if err != nil {
			fail("Request failed: %s\n", err)
		}
		fmt.Printf("Killed job %s\n", killJobID)
	}

	stopJobID := *stopJobFlag
	if len(stopJobID) > 0 {
		err := client.Stop(stopJobID)
		if err != nil {
			fail("Request failed: %s\n", err)
		}
		fmt.Printf("Stopped job %s\n", killJobID)
	}

	bidJob := *bidJobFlag
	if len(bidJob) > 0 {
		jr, err := loadJob(bidJob)
		if err != nil {
			fail("Invalid job: %s\n", err)
		}
		jr.ID = uuid.New()
		bid, err := client.Bid(jr)
		if err != nil {
			fail("Request failed: %s\n", err)
		}
		if bid != nil {
			fmt.Printf("Bid: %f\n", *bid)
		} else {
			fmt.Printf("Bid: <none>\n")
		}
	}

	offerJob := *offerJobFlag
	if len(offerJob) > 0 {
		jr, err := loadJob(offerJob)
		if err != nil {
			fail("Invalid job: %s\n", err)
		}
		jr.ID = uuid.New()
		info, err := client.Offer(jr)
		if err != nil {
			fail("Request failed: %s\n", err)
		}
		data, err := json.MarshalIndent(info, "", "  ")
		if err != nil {
			fail("Invalid response: %s\n", err)
		}
		fmt.Printf("%s\n", data)
	}

	listJob := *listJobFlag
	if len(listJob) > 0 {
		jobs, err := client.ListJob(listJob)
		if err != nil {
			fail("Request failed: %s\n", err)
		}
		data, err := json.MarshalIndent(jobs, "", "  ")
		if err != nil {
			fail("Invalid response: %s\n", err)
		}
		fmt.Printf("%s\n", data)
	}

	if *listJobsFlag {
		jobs, err := client.ListJobs()
		if err != nil {
			fail("Request failed: %s\n", err)
		}
		data, err := json.MarshalIndent(jobs, "", "  ")
		if err != nil {
			fail("Invalid response: %s\n", err)
		}
		fmt.Printf("%s\n", data)
	}

	if *getUsageFlag {
		usage, err := client.GetUsage()
		if err != nil {
			fail("Request failed: %s\n", err)
		}
		fmt.Printf("Usage: %f of %f\n", usage.Used, usage.Available)
	}

	setMode := *setModeFlag
	if len(setMode) > 0 {
		err := client.SetMode(setMode)
		if err != nil {
			fail("Request failed: %s\n", err)
		}
		fmt.Printf("Set agent mode to %s\n", setMode)
	}

	if *getModeFlag {
		mode, err := client.GetMode()
		if err != nil {
			fail("Request failed: %s\n", err)
		}
		fmt.Printf("Agent mode: %s\n", mode)
	}
}
