package client

import (
	"bytes"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"
)

func generateNonce() (string, error) {
	buf := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, buf); err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(buf), nil
}

func generateTimestamp() string {
	return strconv.Itoa(int(time.Now().Unix()))
}

func hashPublicKey(publicKey string) string {
	hash := sha256.Sum256([]byte(publicKey))
	return base64.StdEncoding.EncodeToString(hash[:])
}

func parsePrivateKey(privateKey string) (*rsa.PrivateKey, error) {
	var privateKeyBlock *pem.Block
	restBytes := []byte(privateKey)
	for block, rest := pem.Decode(restBytes); privateKeyBlock == nil; {
		if len(rest) == len(restBytes) {
			return nil, errors.New("no private key found in the provided pem-encoded data")
		}
		if block.Type == "RSA PRIVATE KEY" {
			privateKeyBlock = block
		}
	}
	return x509.ParsePKCS1PrivateKey(privateKeyBlock.Bytes)
}

func makeSignature(verb, uri, nonce, timestamp string, privateKey string) (string, error) {
	toSign := sha256.Sum256([]byte(fmt.Sprintf("%s&%s&%s&%s", nonce, verb, uri, timestamp)))
	key, err := parsePrivateKey(privateKey)
	if err != nil {
		return "", err
	}
	signed, err := rsa.SignPKCS1v15(nil, key, crypto.SHA256, toSign[:])
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(signed), nil
}

func buildRequest(verb, url string, body *[]byte) (*http.Request, string, string, error) {
	var payload io.Reader
	if body != nil {
		payload = bytes.NewBuffer(*body)
	}
	req, err := http.NewRequest(verb, url, payload)
	if err != nil {
		return nil, "", "", err
	}
	if body != nil {
		req.Header.Add("Content-Type", "application/json;charset=utf-8")
	}
	nonce, err := generateNonce()
	if err != nil {
		return nil, "", "", err
	}
	req.Header.Add("X-Request-Nonce", nonce)
	timestamp := generateTimestamp()
	req.Header.Add("X-Request-Timestamp", timestamp)
	return req, nonce, timestamp, nil
}

func transformResponse(resp *http.Response, responseType interface{}) error {
	if resp.StatusCode >= 300 {
		coreErr := &coreError{}
		err := json.NewDecoder(resp.Body).Decode(coreErr)
		if err != nil {
			return err
		}
		return fmt.Errorf("core API responded with error: %d - %s", coreErr.ID, coreErr.Message)
	}
	if responseType != nil {
		return json.NewDecoder(resp.Body).Decode(responseType)
	}
	return nil
}
