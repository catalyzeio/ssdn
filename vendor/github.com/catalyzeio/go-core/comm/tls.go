package comm

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
var verifyFlag *bool

func AddTLSFlags() {
	useTLSFlag = flag.Bool("tls", false, "whether to listen using TLS")
	certFileFlag = flag.String("tls-cert", "", "certificate to use in TLS mode")
	keyFileFlag = flag.String("tls-key", "", "certificate key to use in TLS mode")
	caFileFlag = flag.String("tls-ca", "", "CA certificate(s) to use in TLS mode")
	verifyFlag = flag.Bool("tls-verify", true, "whether to verify TLS certificates")
}

func TLSCertFile() string {
	return *certFileFlag
}

func TLSKeyFile() string {
	return *keyFileFlag
}

func TLSCAFile() string {
	return *caFileFlag
}

func TLSVerify() bool {
	return *verifyFlag
}

func GenerateTLSConfig(auth bool) (*tls.Config, error) {
	return NewTLSConfig(*useTLSFlag, *certFileFlag, *keyFileFlag, *caFileFlag, *verifyFlag, auth)
}

func GenerateAltTLSConfig(certFile, keyFile string, auth bool) (*tls.Config, error) {
	if len(certFile) == 0 {
		certFile = *certFileFlag
	}
	if len(keyFile) == 0 {
		keyFile = *keyFileFlag
	}
	return NewTLSConfig(*useTLSFlag, certFile, keyFile, *caFileFlag, *verifyFlag, auth)
}

func NewTLSConfig(useTLS bool, certFile, keyFile, caFile string, verify, auth bool) (*tls.Config, error) {
	return NewTLSConfigWithCiphers(useTLS, certFile, keyFile, caFile, verify, auth, []uint16{tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256})
}

func NewTLSConfigWithCiphers(useTLS bool, certFile, keyFile, caFile string, verify, auth bool, ciphers []uint16) (*tls.Config, error) {
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

	config := tls.Config{
		Certificates:             certs,
		RootCAs:                  rootCAs,
		PreferServerCipherSuites: true,
		SessionTicketsDisabled:   true,
		MinVersion:               tls.VersionTLS12,
	}
	if ciphers != nil {
		log.Debug("Restricting allowed cipher suites")
		config.CipherSuites = ciphers
	}

	if !verify {
		log.Warn("Disabling TLS certificate validation")
		config.InsecureSkipVerify = true
	}

	if auth {
		config.ClientCAs = rootCAs
		if config.InsecureSkipVerify {
			config.ClientAuth = tls.RequireAnyClientCert
		} else {
			config.ClientAuth = tls.RequireAndVerifyClientCert
		}
	}

	return &config, nil
}
