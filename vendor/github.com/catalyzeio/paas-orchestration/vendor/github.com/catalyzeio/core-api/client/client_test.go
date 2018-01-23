package client

import (
	"flag"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/catalyzeio/core-api/config"
	"github.com/catalyzeio/core-api/server"
)

const publicKey = `-----BEGIN PUBLIC KEY-----
MIICIjANBgkqhkiG9w0BAQEFAAOCAg8AMIICCgKCAgEA0x9mgRfi4VTthdAcCUxg
Qclyo7jlXehBljy2eXSwaW7u2wyym/7swPL3gMOpw7kVj72BEB3oB6ANxxmFTHCx
ST4mrFp83F7dCD0bkZvwlNgDAtdjRWZlb8XfHjUESd5pUoXCDqsw9b6YHbLaHIYV
LT7nKBXhreqS12KbAXq3cIR+gobVKG7x8gmsE/qUxSrjMFpkRvoK2BSD0alR4Wp7
910gE/FjpYuR490YSKjPUkFHd5DRDp39yJML/tS6AugUt5QF0WKyX1iJGLhSwNvs
lEFnYwRz/vh/BtL8dgxeqL9PKhJYeToTPunfBXmxAWlxf2VGleEwA1YVM6cbfMZV
h2GoY0Fc2+hcZAFXl0pgw5H3zvw2UvaUxK25lLd/n3wi2+N0pUtHLj6DPa+CAOsS
ych9U6n982og8cjoWTwUs/k12zWD76EkRo2nIG03ebn+WHs6FpG5Je53iWpR787P
kWoDqN79K3BBdQG/ypyVap9McMsIdPRRPrr/nSyR5pXu54LBbql1q76m/PGSwsei
GrqT8l0u4SRhUX/rOLBF3AyHhiSdiz19MbNcU0t82GlMgVgYpx5E9e46ZX6eK0/s
ZOhzYoTb2GYqKxhM9DiVG+t6d2oOGHxdpLkK34nTOrK0PscsPMjc6OmbTPkt1OkL
0/7Zdq0jtOaAKRIL7Z1wyCkCAwEAAQ==
-----END PUBLIC KEY-----
`

var escapedPublicKey = strings.Replace(publicKey, "\n", "\\n", -1)

const privateKey = `-----BEGIN RSA PRIVATE KEY-----
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
-----END RSA PRIVATE KEY-----
`

func TestMain(m *testing.M) {
	flag.Parse()
	if !testing.Short() {
		go runTests(m)
		server.TestServer()
	}
}

func runTests(m *testing.M) {
	defer server.Kill()
	defer doTeardown()
	time.Sleep(time.Duration(5) * time.Second)
	doSetup()
	m.Run()
}

func makeDebugRequest(verb, route string, body *[]byte) error {
	req, _, _, err := buildRequest(verb, fmt.Sprintf("http://%s%s", config.C.AdminServeAddress, route), body)
	if err != nil {
		return err
	}
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	return transformResponse(resp, nil)
}

func makeCheckedDebugRequest(verb, route string, body *[]byte) {
	err := makeDebugRequest(verb, route, body)
	if err != nil {
		fmt.Printf("WARNING: Error happened: %v\n", err)
	}
}

func doSetup() {
	body := []byte(fmt.Sprintf(`{"name": "client-test", "publicKey": "%s"}`, escapedPublicKey))
	makeCheckedDebugRequest("POST", "/debug/access-key", &body)
	makeCheckedDebugRequest("PUT", "/admin/access-keys/reload", nil)
}

func doTeardown() {
	makeCheckedDebugRequest("DELETE", "/debug/access-key/client-test", nil)
	makeCheckedDebugRequest("PUT", "/admin/access-keys/reload", nil)
}

func TestIdentify(t *testing.T) {
	client := NewAppAuth(config.C.HostAddress, privateKey, publicKey)
	name, err := client.IdentifyApplication()
	if err != nil {
		t.Fatal(err)
	}
	if name != "client-test" {
		t.Fatalf("Expected name to be 'client-test' but was %s", name)
	}
}
