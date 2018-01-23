package agent

import (
	"io/ioutil"
	"regexp"
	"strconv"
)

func availableMemoryMiB() int64 {
	bytes, err := ioutil.ReadFile("/proc/meminfo")
	if err != nil {
		log.Warn("Could not determine available system memory: %s", err)
		return defaultMemoryValue
	}
	re := regexp.MustCompile(`MemTotal:[\s]+([0-9]+)[\s]+kB`)
	match := re.FindSubmatch(bytes)
	if match == nil {
		log.Warn("Could not determine available system memory: could not find MemTotal value")
		return defaultMemoryValue
	}
	value, err := strconv.ParseInt(string(match[1]), 0, 64)
	if err != nil {
		log.Warn("Could not determine available system memory: %s", err)
		return defaultMemoryValue
	}
	return value / 1024
}
