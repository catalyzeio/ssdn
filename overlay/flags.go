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
	tenantIDFlag  *string

	mtuFlag *int
)

func AddTenantFlags() {
	tenantFlag = flag.String("tenant", "", "tenant name (required)")
	tenantIDFlag = flag.String("tenantID", "", "tenant identifier (optional)")
}

func GetTenantFlags() (string, string, error) {
	// validate tenant
	tenant := *tenantFlag
	tlen := len(tenant)
	if tlen == 0 {
		return "", "", fmt.Errorf("-tenant is required")
	}
	if !tenantPattern.MatchString(tenant) {
		return "", "", fmt.Errorf("invalid -tenant value '%s'", tenant)
	}

	// use provided tenant ID, or default to shorthand tenant name
	tenantID := *tenantIDFlag
	idlen := len(tenantID)
	if idlen > 0 {
		if idlen > tenantIDLength {
			return "", "", fmt.Errorf("tenant ID too long (max: %d characters)", tenantIDLength)
		}
		if !tenantPattern.MatchString(tenantID) {
			return "", "", fmt.Errorf("invalid -tenantID value '%s'", tenantID)
		}
	} else {
		tenantID = tenant
		if len(tenantID) > tenantIDLength {
			tenantID = tenantID[:tenantIDLength]
		}
	}

	return tenant, tenantID, nil
}

func AddMTUFlag() {
	mtuFlag = flag.Int("mtu", 9000, "MTU to use for virtual interfaces")
}

func GetMTUFlag() (uint16, error) {
	mtuVal := *mtuFlag
	if mtuVal < 0x400 || mtuVal > MaxPacketSize {
		return 0, fmt.Errorf("invalid MTU: %d", mtuVal)
	}
	return uint16(mtuVal), nil
}
