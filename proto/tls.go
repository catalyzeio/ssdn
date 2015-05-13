package proto

import (
	"crypto/tls"
	"crypto/x509"
	"flag"
	"fmt"
	"io/ioutil"
)

var useTLSFlag *bool
var certFileFlag *string
var keyFileFlag *string
var caFileFlag *string

func AddTLSFlags() {
	useTLSFlag = flag.Bool("tls", false, "whether to listen using TLS")
	certFileFlag = flag.String("cert", "", "certificate to use in TLS mode")
	keyFileFlag = flag.String("key", "", "certificate key to use in TLS mode")
	caFileFlag = flag.String("ca", "", "CA certificate(s) to use in TLS mode")
}

func GenerateTLSConfig() (*tls.Config, error) {
	return NewTLSConfig(*useTLSFlag, *certFileFlag, *keyFileFlag, *caFileFlag)
}

func NewTLSConfig(useTLS bool, certFile, keyFile, caFile string) (*tls.Config, error) {
	if !useTLS {
		return nil, nil
	}

	var certs []tls.Certificate
	if len(certFile) > 0 {
		keyPair, err := tls.LoadX509KeyPair(certFile, keyFile)
		if err != nil {
			return nil, err
		}
		certs = append(certs, keyPair)
	}

	var rootCAs *x509.CertPool
	if len(caFile) > 0 {
		caBytes, err := ioutil.ReadFile(caFile)
		if err != nil {
			return nil, err
		}
		rootCAs = x509.NewCertPool()
		if !rootCAs.AppendCertsFromPEM(caBytes) {
			return nil, fmt.Errorf("failed to load CA file: %s", caFile)
		}
	}

	// TODO client certificate validation

	config := tls.Config{
		Certificates:             certs,
		RootCAs:                  rootCAs,
		PreferServerCipherSuites: true,
		SessionTicketsDisabled:   true,
		MinVersion:               tls.VersionTLS12,
		CipherSuites:             []uint16{tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256},
		InsecureSkipVerify:       true,
	}
	return &config, nil
}
