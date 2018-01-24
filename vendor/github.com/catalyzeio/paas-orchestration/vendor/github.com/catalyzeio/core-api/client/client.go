package client

import (
	"fmt"
	"net/http"
)

type appAuth struct {
	coreAPIHost string
	privateKey  string
	publicKey   string
	client      *http.Client
}

// NewAppAuth builds a new CoreAppAuth instance.
func NewAppAuth(coreAPIHost, privateKey, publicKey string) CoreAppAuth {
	return &appAuth{
		coreAPIHost: coreAPIHost,
		privateKey:  privateKey,
		publicKey:   publicKey,
		client:      &http.Client{},
	}
}

func (a *appAuth) makeRequest(verb, route string, body *[]byte) (*http.Response, error) {
	req, nonce, timestamp, err := buildRequest(verb, a.coreAPIHost+route, body)
	if err != nil {
		return nil, err
	}

	keyHash := hashPublicKey(a.publicKey)
	signature, err := makeSignature(verb, route, nonce, timestamp, a.privateKey)
	if err != nil {
		return nil, err
	}

	req.Header.Add("Authorization", fmt.Sprintf("Application %s %s", keyHash, signature))

	resp, err := a.client.Do(req)
	return resp, err
}

func (a *appAuth) IdentifyApplication() (string, error) {
	resp, err := a.makeRequest("GET", "/auth/application", nil)
	if err != nil {
		return "", err
	}
	identifyResponse := &struct {
		Name string `json:"name"`
	}{}
	err = transformResponse(resp, identifyResponse)
	if err != nil {
		return "", err
	}
	return identifyResponse.Name, nil
}

func (a *appAuth) GetDockerToken(service, scope, account string) (*DockerToken, error) {
	resp, err := a.makeRequest("GET",
		fmt.Sprintf("/docker/token?service=%s&scope=%s&account=%s", service, scope, account),
		nil)
	if err != nil {
		return nil, err
	}
	token := &DockerToken{}
	err = transformResponse(resp, token)
	if err != nil {
		return nil, err
	}
	return token, nil
}
