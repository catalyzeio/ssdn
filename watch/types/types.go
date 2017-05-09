package types

import (
	"time"
)

const (
	TenantLabel   = "io.catalyze.ssdn.tenant"   // plain string
	IPLabel       = "io.catalyze.ssdn.ip"       // plain string
	ServicesLabel = "io.catalyze.ssdn.services" // []Service json
)

const (
	StableWatchInterval   = time.Second * 7
	UnstableWatchInterval = time.Second * 2
)

type Service struct {
	Name     string `json:"name"`
	Location string `json:"location"`
}

const (
	DockerRetryInterval = 15 * time.Second

	RegistryRetryInterval = 5 * time.Second
	RegistryPollInterval  = 30 * time.Second
)
