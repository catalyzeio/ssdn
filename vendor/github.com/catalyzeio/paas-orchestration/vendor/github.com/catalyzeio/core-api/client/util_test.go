package client

import "testing"

func TestParsePrivateKey(t *testing.T) {
	key, err := parsePrivateKey(privateKey)
	if err != nil {
		t.Fatal(err)
	}
	if key == nil {
		t.Fatal("Key was null!")
	}
}
