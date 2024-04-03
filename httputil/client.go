// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2018-2020 Canonical Ltd
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License version 3 as
 * published by the Free Software Foundation.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with this program.  If not, see <http://www.gnu.org/licenses/>.
 *
 */

package httputil

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
)

// CertData contains the raw data of a certificate and the origin of
// the cert, this is usually a file path on disk and is just used
// for error reporting.
type CertData struct {
	Raw    []byte
	Origin string
}

// ExtraSSLCerts is an interface that provides a way to add extra
// SSL certificates to the httputil.Client
type ExtraSSLCerts interface {
	Certs() ([]*CertData, error)
}

// ExtraSSLCertsFromDir implements ExtraSSLCerts and provides all the
// pem encoded certs from the given directory.
type ExtraSSLCertsFromDir struct {
	Dir string
}

// Certs returns a slice CertData or an error.
func (e *ExtraSSLCertsFromDir) Certs() ([]*CertData, error) {
	extraCertFiles, err := filepath.Glob(filepath.Join(e.Dir, "*.pem"))
	if err != nil {
		return nil, err
	}
	extraCerts := make([]*CertData, 0, len(extraCertFiles))
	for _, p := range extraCertFiles {
		cert, err := os.ReadFile(p)
		if err != nil {
			return nil, fmt.Errorf("cannot read certificate: %v", err)
		}
		extraCerts = append(extraCerts, &CertData{
			Raw:    cert,
			Origin: p,
		})
	}
	return extraCerts, nil
}

// dialTLS holds a tls.Config that is used by the dialTLS.dialTLS()
// function.
type dialTLS struct {
	conf          *tls.Config
	extraSSLCerts ExtraSSLCerts
}

// dialTLS will use it's tls.Config and use that to do a tls connection.
func (d *dialTLS) dialTLS(network, addr string) (net.Conn, error) {
	if d.conf == nil {
		// c.f. go source: crypto/tls/common.go
		var emptyConfig tls.Config
		d.conf = &emptyConfig
	}

	// ensure we never use anything lower than TLS v1.2, see
	// https://github.com/snapcore/snapd/pull/8100/files#r384046667
	if d.conf.MinVersion < tls.VersionTLS12 {
		d.conf.MinVersion = tls.VersionTLS12
	}

	// add extraSSLCerts if needed
	if err := d.addLocalSSLCertificates(); err != nil {
		logger.Noticef("cannot add local ssl certificates: %v", err)
	}

	return tls.Dial(network, addr, d.conf)
}

// addLocalSSLCertificates() is an internal helper that is called by
// dialTLS to add an extra certificates.
func (d *dialTLS) addLocalSSLCertificates() (err error) {
	if d.extraSSLCerts == nil {
		// nothing to add
		return nil
	}

	var allCAs *x509.CertPool
	// start with all our current certs
	if d.conf.RootCAs != nil {
		allCAs = d.conf.RootCAs
	} else {
		allCAs, err = x509.SystemCertPool()
		if err != nil {
			return fmt.Errorf("cannot read system certificates: %v", err)
		}
	}
	if allCAs == nil {
		return fmt.Errorf("cannot use empty certificate pool")
	}

	// and now collect any new ones
	extraCerts, err := d.extraSSLCerts.Certs()
	if err != nil {
		return err
	}
	for _, cert := range extraCerts {
		if ok := allCAs.AppendCertsFromPEM(cert.Raw); !ok {
			logger.Noticef("cannot load ssl certificate: %v", cert.Origin)
		}
	}

	// and add them
	d.conf.RootCAs = allCAs
	return nil
}

type ClientOptions struct {
	Timeout    time.Duration
	TLSConfig  *tls.Config
	MayLogBody bool

	Proxy              func(*http.Request) (*url.URL, error)
	ProxyConnectHeader http.Header

	ExtraSSLCerts ExtraSSLCerts
}

// NewHTTPClient returns a new http.Client with a LoggedTransport, a
// Timeout and preservation of range requests across redirects
func NewHTTPClient(opts *ClientOptions) *http.Client {
	if opts == nil {
		opts = &ClientOptions{}
	}

	transport := newDefaultTransport()
	if opts.Proxy != nil {
		transport.Proxy = opts.Proxy
	}
	transport.ProxyConnectHeader = opts.ProxyConnectHeader
	// Remember the original ClientOptions.TLSConfig when making
	// tls connection.
	// Note that we only set TLSClientConfig here because it's extracted
	// by the cmd/snap-repair/runner_test.go
	transport.TLSClientConfig = opts.TLSConfig
	dialTLS := &dialTLS{
		conf:          opts.TLSConfig,
		extraSSLCerts: opts.ExtraSSLCerts,
	}
	transport.DialTLS = dialTLS.dialTLS

	return &http.Client{
		Transport: &LoggedTransport{
			Transport:  transport,
			Key:        "SNAPD_DEBUG_HTTP",
			MayLogBody: opts.MayLogBody,
		},
		Timeout:       opts.Timeout,
		CheckRedirect: checkRedirect,
	}
}

func MockResponseHeaderTimeout(d time.Duration) (restore func()) {
	osutil.MustBeTestBinary("cannot mock ResponseHeaderTimeout outside of tests")
	old := responseHeaderTimeout
	responseHeaderTimeout = d
	return func() {
		responseHeaderTimeout = old
	}
}
