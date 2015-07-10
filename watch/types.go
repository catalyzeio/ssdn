package watch

const (
	TenantLabel   = "io.catalyze.ssdn.tenant"   // plain string
	ServicesLabel = "io.catalyze.ssdn.services" // []Service json
)

type Service struct {
	Name     string `json:"name"`
	Location string `json:"location"`
}
