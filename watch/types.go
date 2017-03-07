package watch

import (
	"time"
)

const (
	TenantLabel   = "io.catalyze.ssdn.tenant"   // plain string
	IPLabel       = "io.catalyze.ssdn.ip"       // plain string
	ServicesLabel = "io.catalyze.ssdn.services" // []Service json
	JobIDLabel    = "io.catalyze.paas.job"      // plain string
)

type Service struct {
	Name     string `json:"name"`
	Location string `json:"location"`
}

const (
	dockerRetryInterval = 15 * time.Second

	registryRetryInterval = 5 * time.Second
	registryPollInterval  = 30 * time.Second
)
