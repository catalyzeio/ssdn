package udocker

import (
	"flag"
	"fmt"
	"os"

	"github.com/catalyzeio/go-core/comm"
)

var dockerHostFlag *string

func AddFlags(description string) {
	dockerDefault := os.Getenv("DOCKER_HOST")
	if len(dockerDefault) == 0 {
		dockerDefault = "unix:///var/run/docker.sock"
	}
	if len(description) == 0 {
		description = "Docker host URL"
	}
	dockerHostFlag = flag.String("docker-host", dockerDefault, description)
}

func GetFlag() string {
	return *dockerHostFlag
}

func GenerateClient(required bool) (*Client, error) {
	dockerHost := GetFlag()
	if len(dockerHost) == 0 {
		if required {
			return nil, fmt.Errorf("-docker-host is required")
		}
		return nil, nil
	}
	return ClientFromURL(dockerHost)
}

func ClientFromURL(urlString string) (*Client, error) {
	c, base, err := comm.HTTPClientFromURL(urlString)
	if err != nil {
		return nil, err
	}
	return NewClient(base, c), nil
}
