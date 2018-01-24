package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/catalyzeio/go-core/simplelog"
	"github.com/catalyzeio/go-core/udocker"
)

func main() {
	simplelog.AddFlags()
	udocker.AddFlags("")
	pingFlag := flag.Bool("ping", false, "ping the server")
	listFlag := flag.Bool("list", false, "list containers")
	inspectFlag := flag.String("inspect", "", "container to inspect")
	createFlag := flag.String("create", "", "container to create")
	startFlag := flag.String("start", "", "container to start")
	stopFlag := flag.String("stop", "", "container to stop")
	killFlag := flag.String("kill", "", "container to kill")
	attachFlag := flag.String("attach", "", "container to attach to")
	waitFlag := flag.String("wait", "", "container to wait for")
	commitFlag := flag.String("commit", "", "container to commit")
	pullFlag := flag.String("pull", "", "image to pull")
	tagFlag := flag.String("tag", "", "tag to add to image")
	tagRepoFlag := flag.String("tag-repo", "", "target repo for tag")
	tagForceFlag := flag.Bool("tag-force", false, "whether to force the tag operation")
	pushFlag := flag.String("push", "", "image to push")
	inspectImageFlag := flag.String("inspect-image", "", "image to inspect")
	buildFlag := flag.String("build", "", "tar file containing Dockerfile to build")
	eventsFlag := flag.Bool("events", false, "subscribe to event stream")
	execFlag := flag.String("exec", "", "container to exec")
	logFlag := flag.String("log", "", "container to get logs")
	copyFlag := flag.String("copy", "", "container to copy")
	flag.Parse()

	c, err := udocker.GenerateClient(true)
	if err != nil {
		fmt.Printf("Invalid Docker configuration: %s\n", err)
		os.Exit(1)
	}

	if *pingFlag {
		if err := c.Ping(); err != nil {
			fmt.Printf("Failed to ping server: %s\n", err)
		} else {
			fmt.Printf("Ping operation successful\n")
		}
	}

	if *listFlag {
		l, err := c.ListContainers(true)
		if err != nil {
			fmt.Printf("Failed to list containers: %s\n", err)
		} else {
			fmt.Printf("%+v\n", l)
		}
	}

	inspect := *inspectFlag
	if len(inspect) > 0 {
		l, err := c.InspectContainer(inspect)
		if err != nil {
			fmt.Printf("Failed to inspect container: %s\n", err)
		} else {
			fmt.Printf("%+v\n", l)
		}
	}

	create := *createFlag
	if len(create) > 0 {
		def, err := loadContainerDef(create)
		if err != nil {
			fmt.Printf("Failed to load container definition: %s\n", err)
		} else {
			res, err := c.CreateContainer("", def)
			if err != nil {
				fmt.Printf("Failed to create container: %s\n", err)
			} else {
				fmt.Printf("%+v\n", res)
			}
		}
	}

	start := *startFlag
	if len(start) > 0 {
		err := c.StartContainer(start, nil)
		if err != nil {
			fmt.Printf("Failed to start container: %s\n", err)
		} else {
			fmt.Printf("Started %s\n", start)
		}
	}

	stop := *stopFlag
	if len(stop) > 0 {
		err := c.StopContainer(stop, 6)
		if err != nil {
			fmt.Printf("Failed to stop container: %s\n", err)
		} else {
			fmt.Printf("Stopped %s\n", start)
		}
	}

	kill := *killFlag
	if len(kill) > 0 {
		err := c.KillContainer(kill)
		if err != nil {
			fmt.Printf("Failed to kill container: %s\n", err)
		} else {
			fmt.Printf("Killed %s\n", kill)
		}
	}

	attach := *attachFlag
	if len(attach) > 0 {
		err := c.AttachContainer(attach)
		if err != nil {
			fmt.Printf("Failed to attach to container: %s\n", err)
		} else {
			fmt.Printf("Disconnected from container %s\n", attach)
		}
	}

	wait := *waitFlag
	if len(wait) > 0 {
		res, err := c.WaitContainer(wait)
		if err != nil {
			fmt.Printf("Failed to wait for container: %s\n", err)
		} else {
			fmt.Printf("Exit code: %d\n", res.StatusCode)
		}
	}

	commit := *commitFlag
	if len(commit) > 0 {
		res, err := c.CommitContainer(commit, "", "", nil)
		if err != nil {
			fmt.Printf("Failed to commit container: %s\n", err)
		} else {
			fmt.Printf("Committed container: %s\n", res.Id)
		}
	}

	pull := *pullFlag
	if len(pull) > 0 {
		err := c.PullImage(pull)
		if err != nil {
			fmt.Printf("Failed to pull image: %s\n", err)
		} else {
			fmt.Printf("Pulled image %s\n", pull)
		}
	}

	image, tag := extractTag(*tagFlag)
	if len(image) > 0 {
		repo, repoTag := extractTag(*tagRepoFlag)
		err := c.TagImage(image, tag, repo, repoTag, *tagForceFlag)
		if err != nil {
			fmt.Printf("Failed to tag image: %s\n", err)
		} else {
			fmt.Printf("Tagged image %s\n", tag)
		}
	}

	push := *pushFlag
	if len(push) > 0 {
		image, tag := extractTag(push)
		err := c.PushImage(image, tag)
		if err != nil {
			fmt.Printf("Failed to push image: %s\n", err)
		} else {
			fmt.Printf("Pushed image %s\n", push)
		}
	}

	inspectImage := *inspectImageFlag
	if len(inspectImage) > 0 {
		l, err := c.InspectImage(inspectImage)
		if err != nil {
			fmt.Printf("Failed to inspect image: %s\n", err)
		} else {
			fmt.Printf("%+v\n", l)
		}
	}

	build := *buildFlag
	if len(build) > 0 {
		tarFile, err := os.Open(build)
		if err != nil {
			fmt.Printf("Invalid build file: %s\n", err)
		} else {
			defer tarFile.Close()
			err := c.BuildImage(tarFile, "udocker", false)
			if err != nil {
				fmt.Printf("Failed to build image: %s\n", err)
			} else {
				fmt.Printf("Built image from %s\n", build)
			}
		}
	}

	if *eventsFlag {
		handler := func(msg *udocker.EventMessage) {
			fmt.Printf("Event: %s %s %s\n", msg.Status, msg.ID, msg.From)
		}
		if err := c.Events(handler); err != nil {
			fmt.Printf("Failed to subscribe to events: %s\n", err)
		}
	}

	exec := *execFlag
	if len(exec) > 0 {
		stdout, stderr, statusCode, err := c.ExecContainer([]string{"echo", "hello"}, exec)
		if err != nil {
			fmt.Printf("Failed to exec to container: %s\n", err)
		} else if statusCode > 0 {
			fmt.Printf("Exec container command had non-zero exit code: %d %s\n", statusCode, stderr)
		} else {
			fmt.Printf("Exec container command echo ran: %s", stdout)
		}
	}

	logID := *logFlag
	if len(logID) > 0 {
		or, err := c.ContainerLogs(logID, nil)
		if err != nil {
			fmt.Printf("Failed to get container logs: %s\n", err)
		}
		for i := 0; i < 10; i++ {
			_, err := or.Read()
			if err != nil {
				fmt.Printf("Failed to read container logs: %s\n", err)
				break
			}
		}
	}

	copyID := *copyFlag
	if len(copyID) > 0 {
		err := c.CopyFileTo(copyID, "/", "test", 0x644, 5, 1000, 1000, bytes.NewBuffer([]byte("hello")))
		if err != nil {
			fmt.Printf("Failed to copy file into container: %s\n", err)
		}
		_, _, err = c.CopyFileFrom(copyID, "/test")
		if err != nil {
			fmt.Printf("Failed to copy file from container: %s\n", err)
		}
	}
}

func loadContainerDef(jsonFile string) (*udocker.ContainerDefinition, error) {
	f, err := os.Open(jsonFile)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	def := udocker.ContainerDefinition{}
	err = json.NewDecoder(f).Decode(&def)
	return &def, err
}

func extractTag(image string) (string, string) {
	args := strings.SplitN(image, ":", 2)
	if args == nil {
		return "", ""
	}
	if len(args) == 1 {
		return image, ""
	}
	return args[0], args[1]
}
