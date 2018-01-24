package client

// CoreAuth allows authenticated interaction with the Core API.
type CoreAuth interface {
	GetDockerToken(service, scope, account string) (*DockerToken, error)
}

// CoreAppAuth allows functionality from external applications to the core API, using access keys.
type CoreAppAuth interface {
	CoreAuth
	IdentifyApplication() (string, error)
}

type coreError struct {
	ID      int    `json:"id"`
	Message string `json:"message"`
}

// DockerToken is a docker-registry-issued token.
type DockerToken struct {
	Token   string `json:"token"`
	Expires int64  `json:"expires_in"`
	Issued  string `json:"issued_at"`
}
