package overlay

import (
	"flag"
	"fmt"
	"regexp"
)

const (
	tenantIDLength = 6
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
	tenantID := tenant
	if len(tenantID) > tenantIDLength {
		tenantID = tenantID[:tenantIDLength]
	}

	return tenant, tenantID, nil
}
