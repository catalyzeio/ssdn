package agent

import (
	"os/exec"
	"strconv"
	"strings"
)

func availableMemoryMiB() int64 {
	bytes, err := exec.Command("sysctl", "-n", "hw.memsize").Output()
	if err != nil {
		log.Warn("Could not determine available system memory: %s", err)
		return defaultMemoryValue
	}
	value, err := strconv.ParseInt(strings.TrimSpace(string(bytes)), 0, 64)
	if err != nil {
		log.Warn("Could not determine available system memory: %s", err)
		return defaultMemoryValue
	}
	return value / 1024 / 1024
}
