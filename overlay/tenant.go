package overlay

import (
	"flag"
	"fmt"
	"regexp"
)

const (
	tenantIdLength = 6
)

var (
	tenantPattern = regexp.MustCompile("^[-0-9A-Za-z_]+$")
	tenantFlag    *string
)

func AddTenantFlags() {
	tenantFlag = flag.String("tenant", "", "tenant name (required)")
}

func GetTenantFlags() (string, string, error) {
	// validate tenant
	tenant := *tenantFlag
	tlen := len(tenant)
	if tlen == 0 {
		return "", "", fmt.Errorf("-tenant is required")
	}
	if !tenantPattern.MatchString(tenant) {
		return "", "", fmt.Errorf("invalid -tenant value")
	}

	// extract tenant ID
	tenID := tenant
	if len(tenID) > tenantIdLength {
		tenID = tenID[:tenantIdLength]
	}

	return tenant, tenID, nil
}
