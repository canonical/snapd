// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2018 Canonical Ltd
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
	"io/ioutil"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"time"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/logger"
)

type ClientOptions struct {
	Timeout    time.Duration
	TLSConfig  *tls.Config
	MayLogBody bool
	Proxy      func(*http.Request) (*url.URL, error)
}

func addLocalSslCertificates(conf *tls.Config) (allCAs *x509.CertPool, err error) {
	if conf != nil && conf.RootCAs != nil {
		allCAs = conf.RootCAs
	} else {
		allCAs, err = x509.SystemCertPool()
		if err != nil {
			return nil, fmt.Errorf("cannot read system certificates: %v", err)
		}
	}
	if allCAs == nil {
		return nil, fmt.Errorf("cannot use empty certificate pool")
	}
	extraCertFiles, err := filepath.Glob(filepath.Join(dirs.SnapdExtraSslCertsDir, "*.pem"))
	if err != nil {
		return nil, err
	}
	for _, p := range extraCertFiles {
		extraCert, err := ioutil.ReadFile(p)
		if err != nil {
			return nil, fmt.Errorf("cannot read extra certificate: %v", err)
		}
		// XXX: continue here trying to add others?
		if ok := allCAs.AppendCertsFromPEM(extraCert); !ok {
			return nil, fmt.Errorf("cannot append extra ssl certificate: %v", p)
		}
	}
	return allCAs, nil
}

// dialTLS holds a tls.Config that is used by the dialTLS.dialTLS()
// function.
type dialTLS struct {
	conf *tls.Config
}

// dialTLS will use it's tls.Config and use that to do a tls connection.
func (d *dialTLS) dialTLS(network, addr string) (net.Conn, error) {
	if d.conf == nil {
		// c.f. go source: crypto/tls/common.go
		var emptyConfig tls.Config
		d.conf = &emptyConfig
	}
	certs, err := addLocalSslCertificates(d.conf)
	if err != nil {
		logger.Noticef("cannot add local ssl certificates: %v", err)
	}
	if certs != nil {
		d.conf.RootCAs = certs
	}
	return tls.Dial(network, addr, d.conf)
}

// NewHTTPCLient returns a new http.Client with a LoggedTransport, a
// Timeout and preservation of range requests across redirects
func NewHTTPClient(opts *ClientOptions) *http.Client {
	if opts == nil {
		opts = &ClientOptions{}
	}

	transport := newDefaultTransport()
	// remember the original ClientOptions.TLSConfig when making
	// tls connection.
	transport.DialTLS = (&dialTLS{opts.TLSConfig}).dialTLS
	if opts.Proxy != nil {
		transport.Proxy = opts.Proxy
	}
	transport.ProxyConnectHeader = http.Header{"User-Agent": []string{UserAgent()}}

	return &http.Client{
		Transport: &LoggedTransport{
			Transport: transport,
			Key:       "SNAPD_DEBUG_HTTP",
			body:      opts.MayLogBody,
		},
		Timeout:       opts.Timeout,
		CheckRedirect: checkRedirect,
	}
}
