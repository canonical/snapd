// Copyright (c) Abstract Machines
// SPDX-License-Identifier: Apache-2.0

package tls

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"os"
)

var (
	errLoadCerts    = errors.New("failed to load certificates")
	errLoadServerCA = errors.New("failed to load Server CA")
	errAppendCA     = errors.New("failed to append root ca tls.Config")
)

// Load return a TLS configuration that can be used in TLS servers.
func Load(c *Config) (*tls.Config, error) {
	if c.CertFile == "" || c.KeyFile == "" {
		return nil, nil
	}

	tlsConfig := &tls.Config{}

	certificate, err := tls.LoadX509KeyPair(c.CertFile, c.KeyFile)
	if err != nil {
		return nil, errors.Join(errLoadCerts, err)
	}
	tlsConfig = &tls.Config{
		Certificates: []tls.Certificate{certificate},
	}

	// Loading Server CA file
	rootCA, err := loadCertFile(c.ServerCAFile)
	if err != nil {
		return nil, errors.Join(errLoadServerCA, err)
	}
	if len(rootCA) > 0 {
		if tlsConfig.RootCAs == nil {
			tlsConfig.RootCAs = x509.NewCertPool()
		}
		if !tlsConfig.RootCAs.AppendCertsFromPEM(rootCA) {
			return nil, errAppendCA
		}
	}
	return tlsConfig, nil
}

// Load return a TLS configuration that can be used in TLS clients (for rest endpoints).
func LoadClient(c *Config) (*tls.Config, error) {
	certpool := x509.NewCertPool()
	// Loading Server CA file
	rootCA, err := loadCertFile(c.ServerCAFile)
	if err != nil {
		return nil, errors.Join(errLoadServerCA, err)
	}

	certpool.AppendCertsFromPEM(rootCA)
	return &tls.Config{
		RootCAs: certpool,
		// Set to true only for testing/self-signed certs:
		InsecureSkipVerify: false,
	}, nil
}

// SecurityStatus returns log message from TLS config.
func SecurityStatus(c *tls.Config) string {
	if c == nil {
		return "no TLS"
	}
	ret := "TLS"
	// It is possible to establish TLS with client certificates only.
	if len(c.Certificates) == 0 {
		ret = "no server certificates"
	}
	return ret
}

func loadCertFile(certFile string) ([]byte, error) {
	if certFile != "" {
		return os.ReadFile(certFile)
	}
	return []byte{}, nil
}
