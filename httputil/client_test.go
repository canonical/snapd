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

package httputil_test

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"io"
	"io/ioutil"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/httputil"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/testutil"
)

type clientSuite struct{}

var _ = check.Suite(&clientSuite{})

func mustParse(c *check.C, rawurl string) *url.URL {
	url, err := url.Parse(rawurl)
	c.Assert(err, check.IsNil)
	return url
}

type proxyProvider struct {
	proxy *url.URL
}

func (p *proxyProvider) proxyCallback(*http.Request) (*url.URL, error) {
	return p.proxy, nil
}

func (s *clientSuite) TestClientOptionsWithProxy(c *check.C) {
	pp := proxyProvider{proxy: mustParse(c, "http://some-proxy:3128")}
	cli := httputil.NewHTTPClient(&httputil.ClientOptions{
		Proxy: pp.proxyCallback,
	})
	c.Assert(cli, check.NotNil)

	trans := cli.Transport.(*httputil.LoggedTransport).Transport.(*http.Transport)
	req, err := http.NewRequest("GET", "http://example.com", nil)
	c.Check(err, check.IsNil)
	url, err := trans.Proxy(req)
	c.Check(err, check.IsNil)
	c.Check(url.String(), check.Equals, "http://some-proxy:3128")
}

func (s *clientSuite) TestClientProxySetsUserAgent(c *check.C) {
	myUserAgent := "snapd yadda yadda"

	defer httputil.MockUserAgent(myUserAgent)()

	called := false
	proxyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.UserAgent(), check.Equals, myUserAgent)
		called = true
	}))
	defer proxyServer.Close()
	cli := httputil.NewHTTPClient(&httputil.ClientOptions{
		Proxy: func(*http.Request) (*url.URL, error) {
			return mustParse(c, proxyServer.URL), nil
		},
	})
	_, err := cli.Get("https://localhost:9999")
	c.Check(err, check.NotNil) // because we didn't do anything in the handler

	c.Assert(called, check.Equals, true)
}

func (s *clientSuite) TestClientExtraSSLCertInvalidCertWarns(c *check.C) {
	logbuf, restore := logger.MockLogger()
	defer restore()

	tmpdir := c.MkDir()
	dirs.SetRootDir(tmpdir)
	err := os.MkdirAll(dirs.SnapdExtraSslCertsDir, 0755)
	c.Assert(err, check.IsNil)

	err = ioutil.WriteFile(filepath.Join(dirs.SnapdExtraSslCertsDir, "garbage.pem"), []byte("garbage"), 0644)
	c.Assert(err, check.IsNil)

	cli := httputil.NewHTTPClient(nil)
	c.Assert(cli, check.NotNil)
	c.Assert(logbuf.String(), check.Matches, "(?m).* cannot add local ssl certificates: cannot append extra ssl certificate: .*/var/lib/snapd/ssl/snapd-only/garbage.pem")
}

// see crypto/tls/generate_cert.go
func generateTestCert(c *check.C, certpath, keypath string) {
	priv, err := rsa.GenerateKey(rand.Reader, 768)
	c.Assert(err, check.IsNil)

	template := x509.Certificate{
		SerialNumber: big.NewInt(123456789),
		Subject: pkix.Name{
			Organization: []string{"Snapd testers"},
		},
		NotBefore:   time.Now(),
		NotAfter:    time.Now().Add(24 * time.Hour),
		IPAddresses: []net.IP{net.IPv4(127, 0, 0, 1)},
		DNSNames:    []string{"localhost"},
		IsCA:        true,
		KeyUsage:    x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
	}
	derBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	c.Assert(err, check.IsNil)

	certOut, err := os.Create(certpath)
	c.Assert(err, check.IsNil)
	err = pem.Encode(certOut, &pem.Block{Type: "CERTIFICATE", Bytes: derBytes})
	c.Assert(err, check.IsNil)
	err = certOut.Close()
	c.Assert(err, check.IsNil)

	if keypath != "" {
		keyOut, err := os.Create(keypath)
		c.Assert(err, check.IsNil)
		privBytes, err := x509.MarshalPKCS8PrivateKey(priv)
		c.Assert(err, check.IsNil)
		err = pem.Encode(keyOut, &pem.Block{Type: "PRIVATE KEY", Bytes: privBytes})
		c.Assert(err, check.IsNil)
		err = keyOut.Close()
		c.Assert(err, check.IsNil)
	}
}

func (s *clientSuite) TestClientExtraSSLCertLoaded(c *check.C) {
	logbuf, restore := logger.MockLogger()
	defer restore()

	tmpdir := c.MkDir()
	dirs.SetRootDir(tmpdir)
	err := os.MkdirAll(dirs.SnapdExtraSslCertsDir, 0755)
	c.Assert(err, check.IsNil)

	certpath := filepath.Join(dirs.SnapdExtraSslCertsDir, "good.pem")
	generateTestCert(c, certpath, "")

	// create a client, it should pick up our test cert
	cli := httputil.NewHTTPClient(nil)
	c.Assert(cli, check.NotNil)
	c.Assert(logbuf.String(), check.Equals, "")

	// check that our cert got added to the transport cert pool
	systemCerts, err := x509.SystemCertPool()
	c.Assert(err, check.IsNil)
	// peel the onion
	lg := cli.Transport.(*httputil.LoggedTransport)
	tr := lg.Transport.(*http.Transport)
	transportCerts := tr.TLSClientConfig.RootCAs.Subjects()
	c.Assert(len(transportCerts), check.Equals, len(systemCerts.Subjects())+1)
	// XXX: should we properly decode the cert?
	c.Assert(string(transportCerts[len(transportCerts)-1]), testutil.Contains, "Snapd testers")
}

func (s *clientSuite) TestClientExtraSSLCertIntegration(c *check.C) {
	logbuf, restore := logger.MockLogger()
	defer restore()

	tmpdir := c.MkDir()
	dirs.SetRootDir(tmpdir)
	err := os.MkdirAll(dirs.SnapdExtraSslCertsDir, 0755)
	c.Assert(err, check.IsNil)

	certpath := filepath.Join(dirs.SnapdExtraSslCertsDir, "good.pem")
	keypath := filepath.Join(c.MkDir(), "key.pem")
	generateTestCert(c, certpath, keypath)

	// create a server that uses our certs
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `all good`)
	}))
	cert, err := tls.LoadX509KeyPair(certpath, keypath)
	c.Assert(err, check.IsNil)
	srv.TLS = &tls.Config{Certificates: []tls.Certificate{cert}}
	srv.StartTLS()
	defer srv.Close()

	// create a client that will load our cert
	cli := httputil.NewHTTPClient(nil)
	c.Assert(cli, check.NotNil)
	c.Assert(logbuf.String(), check.Equals, "")
	res, err := cli.Get(srv.URL)
	c.Assert(err, check.IsNil)
	c.Assert(res.StatusCode, check.Equals, 200)
}
