// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2025 Canonical Ltd
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

package assemblestate_test

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"time"

	"gopkg.in/check.v1"

	"github.com/snapcore/snapd/cluster/assemblestate"
)

type transportSuite struct{}

var _ = check.Suite(&transportSuite{})

var (
	testClientCert    tls.Certificate
	testServerCert    tls.Certificate
	testServerCertDER []byte
	testOtherCert     tls.Certificate
	testOtherCertDER  []byte
)

func init() {
	testClientCert, _ = generateTestCert()
	testServerCert, testServerCertDER = generateTestCert()
	testOtherCert, testOtherCertDER = generateTestCert()
}

func generateTestCert() (tls.Certificate, []byte) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		panic(err)
	}

	serial, err := rand.Int(rand.Reader, big.NewInt(1<<62))
	if err != nil {
		panic(err)
	}

	template := x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			Organization: []string{"Test"},
		},
		NotBefore:   time.Now(),
		NotAfter:    time.Now().Add(time.Hour),
		KeyUsage:    x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}

	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, pub, priv)
	if err != nil {
		panic(err)
	}

	cert := tls.Certificate{
		Certificate: [][]byte{certDER},
		PrivateKey:  priv,
	}

	return cert, certDER
}

func (s *transportSuite) TestTrustedSuccess(c *check.C) {
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.Assert(r.URL.Path, check.Equals, "/assemble/routes")
		c.Assert(r.Method, check.Equals, "POST")

		var routes assemblestate.Routes
		err := json.NewDecoder(r.Body).Decode(&routes)
		c.Assert(err, check.IsNil)

		w.WriteHeader(200)
	}))

	server.TLS = &tls.Config{
		Certificates: []tls.Certificate{testServerCert},
		ClientAuth:   tls.RequireAnyClientCert,
	}
	server.StartTLS()
	defer server.Close()

	stats := assemblestate.TransportStats{}
	client := assemblestate.NewHTTPSClient(testClientCert, &stats, nil)

	routes := assemblestate.Routes{
		Devices:   []assemblestate.DeviceToken{"device"},
		Addresses: []string{"addr"},
		Routes:    []int{1, 2, 3},
	}

	err := client.Trusted(context.Background(), server.Listener.Addr().String(), testServerCertDER, "routes", routes)
	c.Assert(err, check.IsNil)

	c.Assert(stats.Sent, check.Equals, int64(1))
	c.Assert(stats.Tx > 0, check.Equals, true)
}

func (s *transportSuite) TestTrustedCertificateMismatch(c *check.C) {
	// need a different cert to test mismatch
	_, wrongCertDER := generateTestCert()

	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))

	// suppress TLS errors in test output
	server.Config.ErrorLog = log.New(io.Discard, "", 0)

	server.TLS = &tls.Config{
		Certificates: []tls.Certificate{testServerCert},
		ClientAuth:   tls.RequireAnyClientCert,
	}
	server.StartTLS()
	defer server.Close()

	stats := assemblestate.TransportStats{}
	client := assemblestate.NewHTTPSClient(testClientCert, &stats, nil)

	routes := assemblestate.Routes{
		Devices:   []assemblestate.DeviceToken{"device1"},
		Addresses: []string{"addr1"},
		Routes:    []int{1},
	}

	err := client.Trusted(context.Background(), server.Listener.Addr().String(), wrongCertDER, "routes", routes)
	c.Assert(err, check.NotNil)
	c.Assert(err, check.ErrorMatches, ".*refusing to communicate with unexpected peer certificate")
	c.Assert(stats.Sent, check.Equals, int64(0))
}

func (s *transportSuite) TestTrustedNonSuccessStatus(c *check.C) {
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(400)
	}))

	server.TLS = &tls.Config{
		Certificates: []tls.Certificate{testServerCert},
		ClientAuth:   tls.RequireAnyClientCert,
	}
	server.StartTLS()
	defer server.Close()

	stats := assemblestate.TransportStats{}
	client := assemblestate.NewHTTPSClient(testClientCert, &stats, nil)

	routes := assemblestate.Routes{
		Devices:   []assemblestate.DeviceToken{"device"},
		Addresses: []string{"addr"},
		Routes:    []int{1, 2, 3},
	}

	err := client.Trusted(context.Background(), server.Listener.Addr().String(), testServerCertDER, "routes", routes)
	c.Assert(err, check.NotNil)

	c.Assert(err, check.ErrorMatches, "response to 'routes' message contains status code 400")
	// counters should not be incremented when send fails due to non-200 status
	c.Assert(stats.Sent, check.Equals, int64(0))
}

func (s *transportSuite) TestTrustedMultipleServerCertificates(c *check.C) {
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	server.Config.ErrorLog = log.New(io.Discard, "", 0)

	// create a second certificate for the server to present
	secondCert, _ := generateTestCert()
	server.TLS = &tls.Config{
		Certificates: []tls.Certificate{
			{
				Certificate: [][]byte{
					testServerCert.Certificate[0],
					secondCert.Certificate[0],
				},
				PrivateKey: testServerCert.PrivateKey,
			},
		},
		ClientAuth: tls.RequireAnyClientCert,
	}
	server.StartTLS()
	defer server.Close()

	stats := assemblestate.TransportStats{}
	client := assemblestate.NewHTTPSClient(testClientCert, &stats, nil)

	routes := assemblestate.Routes{
		Devices:   []assemblestate.DeviceToken{"device1"},
		Addresses: []string{"addr1"},
		Routes:    []int{1},
	}

	err := client.Trusted(context.Background(), server.Listener.Addr().String(), testServerCertDER, "routes", routes)
	c.Assert(err, check.NotNil)
	c.Assert(err, check.ErrorMatches, ".*exactly one peer certificate expected, got 2")
	c.Assert(stats.Sent, check.Equals, int64(0))
}

func (s *transportSuite) TestUntrustedSuccess(c *check.C) {
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.Assert(r.URL.Path, check.Equals, "/assemble/auth")
		c.Assert(r.Method, check.Equals, "POST")

		var auth assemblestate.Auth
		err := json.NewDecoder(r.Body).Decode(&auth)
		c.Assert(err, check.IsNil)

		w.WriteHeader(200)
	}))

	server.TLS = &tls.Config{
		Certificates: []tls.Certificate{testServerCert},
		ClientAuth:   tls.RequireAnyClientCert,
	}
	server.StartTLS()
	defer server.Close()

	stats := assemblestate.TransportStats{}
	client := assemblestate.NewHTTPSClient(testClientCert, &stats, nil)

	auth := assemblestate.Auth{
		HMAC: []byte("test-hmac"),
		RDT:  assemblestate.DeviceToken("test-device"),
	}

	cert, err := client.Untrusted(context.Background(), server.Listener.Addr().String(), "auth", auth)
	c.Assert(err, check.IsNil)

	c.Assert(string(cert), check.Equals, string(testServerCertDER))

	c.Assert(stats.Sent, check.Equals, int64(1))
	c.Assert(stats.Tx > 0, check.Equals, true)
}

func (s *transportSuite) TestUntrustedNonSuccessStatus(c *check.C) {
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(403)
	}))

	server.TLS = &tls.Config{
		Certificates: []tls.Certificate{testServerCert},
		ClientAuth:   tls.RequireAnyClientCert,
	}
	server.StartTLS()
	defer server.Close()

	stats := assemblestate.TransportStats{}
	client := assemblestate.NewHTTPSClient(testClientCert, &stats, nil)

	auth := assemblestate.Auth{
		HMAC: []byte("test-hmac"),
		RDT:  assemblestate.DeviceToken("test-device"),
	}

	_, err := client.Untrusted(context.Background(), server.Listener.Addr().String(), "auth", auth)
	c.Assert(err, check.NotNil)

	c.Assert(err, check.ErrorMatches, "got non-200 status code in response to auth message: 403")
	// message was sent even though response was error
	c.Assert(stats.Sent, check.Equals, int64(1))
}

func (s *transportSuite) TestUntrustedNoTLS(c *check.C) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	defer server.Close()

	client := assemblestate.NewHTTPSClient(testClientCert, nil, nil)

	auth := assemblestate.Auth{
		HMAC: []byte("test-hmac"),
		RDT:  assemblestate.DeviceToken("test-device"),
	}

	// client tries TLS but server doesn't support it
	_, err := client.Untrusted(context.Background(), server.Listener.Addr().String(), "auth", auth)
	c.Assert(err, check.ErrorMatches, ".*server gave HTTP response to HTTPS client")
}

func (s *transportSuite) TestUntrustedMultipleServerCertificates(c *check.C) {
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	server.Config.ErrorLog = log.New(io.Discard, "", 0)

	// create a second certificate for the server to present
	secondCert, _ := generateTestCert()
	server.TLS = &tls.Config{
		Certificates: []tls.Certificate{
			{
				Certificate: [][]byte{
					testServerCert.Certificate[0],
					secondCert.Certificate[0],
				},
				PrivateKey: testServerCert.PrivateKey,
			},
		},
		ClientAuth: tls.RequireAnyClientCert,
	}
	server.StartTLS()
	defer server.Close()

	stats := assemblestate.TransportStats{}
	client := assemblestate.NewHTTPSClient(testClientCert, &stats, nil)

	auth := assemblestate.Auth{
		HMAC: []byte("test-hmac"),
		RDT:  assemblestate.DeviceToken("test-device"),
	}

	_, err := client.Untrusted(context.Background(), server.Listener.Addr().String(), "auth", auth)
	c.Assert(err, check.NotNil)
	c.Assert(err, check.ErrorMatches, ".*exactly one peer certificate expected, got 2")

	// message was sent but failed on certificate validation after response
	c.Assert(stats.Sent, check.Equals, int64(1))
}

func (s *transportSuite) TestHTTPSClientRejectsRedirects(c *check.C) {
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", "https://example.com/redirected")
		w.WriteHeader(302)
	}))
	server.Config.ErrorLog = log.New(io.Discard, "", 0)

	server.TLS = &tls.Config{
		Certificates: []tls.Certificate{testServerCert},
		ClientAuth:   tls.RequireAnyClientCert,
	}
	server.StartTLS()
	defer server.Close()

	ctx := context.Background()
	addr := server.Listener.Addr().String()

	stats := assemblestate.TransportStats{}
	client := assemblestate.NewHTTPSClient(testClientCert, &stats, nil)

	err := client.Trusted(ctx, addr, testServerCertDER, "routes", nil)
	c.Assert(err, check.ErrorMatches, ".*redirects are not expected")
	c.Assert(stats.Sent, check.Equals, int64(0))

	_, err = client.Untrusted(ctx, addr, "auth", nil)
	c.Assert(err, check.ErrorMatches, ".*redirects are not expected")
	c.Assert(stats.Sent, check.Equals, int64(0))
}

func (s *transportSuite) TestTrustedNoTLS(c *check.C) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	defer server.Close()

	client := assemblestate.NewHTTPSClient(testClientCert, nil, nil)

	routes := assemblestate.Routes{
		Devices:   []assemblestate.DeviceToken{"device"},
		Addresses: []string{"addr"},
		Routes:    []int{1},
	}

	// client tries TLS but server doesn't support it
	err := client.Trusted(context.Background(), server.Listener.Addr().String(), testServerCertDER, "routes", routes)
	c.Assert(err, check.ErrorMatches, ".*server gave HTTP response to HTTPS client")
}

type testPeerAuthenticator struct {
	AuthenticateAndCommitFunc func(auth assemblestate.Auth, cert []byte) error
	VerifyPeerFunc            func(cert []byte) (assemblestate.VerifiedPeer, error)
}

func (m *testPeerAuthenticator) AuthenticateAndCommit(auth assemblestate.Auth, cert []byte) error {
	if m.AuthenticateAndCommitFunc == nil {
		panic("unexpected call to AuthenticateAndCommit")
	}
	return m.AuthenticateAndCommitFunc(auth, cert)
}

func (m *testPeerAuthenticator) VerifyPeer(cert []byte) (assemblestate.VerifiedPeer, error) {
	if m.VerifyPeerFunc == nil {
		panic("unexpected call to VerifyPeer")
	}
	return m.VerifyPeerFunc(cert)
}

type testVerifiedPeer struct {
	CommitDevicesFunc       func(devices assemblestate.Devices) error
	CommitDeviceQueriesFunc func(unknown assemblestate.UnknownDevices) error
	CommitRoutesFunc        func(routes assemblestate.Routes) error
	RDTFunc                 func() assemblestate.DeviceToken
}

func (m *testVerifiedPeer) CommitDevices(devices assemblestate.Devices) error {
	if m.CommitDevicesFunc == nil {
		panic("unexpected call to CommitDevices")
	}
	return m.CommitDevicesFunc(devices)
}

func (m *testVerifiedPeer) CommitDeviceQueries(unknown assemblestate.UnknownDevices) error {
	if m.CommitDeviceQueriesFunc == nil {
		panic("unexpected call to CommitDeviceQueries")
	}
	return m.CommitDeviceQueriesFunc(unknown)
}

func (m *testVerifiedPeer) CommitRoutes(routes assemblestate.Routes) error {
	if m.CommitRoutesFunc == nil {
		panic("unexpected call to CommitRoutes")
	}
	return m.CommitRoutesFunc(routes)
}

func (s *transportSuite) TestHTTPSTransportServeAuthRoute(c *check.C) {
	var auths []assemblestate.Auth
	var certs [][]byte
	pa := &testPeerAuthenticator{
		AuthenticateAndCommitFunc: func(auth assemblestate.Auth, cert []byte) error {
			auths = append(auths, auth)
			certs = append(certs, cert)
			return nil
		},
	}

	transport := assemblestate.NewHTTPSTransport()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	c.Assert(err, check.IsNil)
	defer ln.Close()

	addr := ln.Addr().String()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = transport.Serve(ctx, ln, testServerCert, pa)
	}()

	auth := assemblestate.Auth{
		HMAC: []byte("test-hmac-data"),
		RDT:  assemblestate.DeviceToken("test-rdt"),
	}

	client := transport.NewClient(testClientCert)
	cert, err := client.Untrusted(ctx, addr, "auth", auth)
	c.Assert(err, check.IsNil)
	c.Assert(cert, check.DeepEquals, testServerCert.Certificate[0])

	c.Assert(auths, check.HasLen, 1)
	c.Assert(auths[0].HMAC, check.DeepEquals, auth.HMAC)
	c.Assert(auths[0].RDT, check.Equals, auth.RDT)

	c.Assert(certs, check.HasLen, 1)
	c.Assert(certs[0], check.DeepEquals, testClientCert.Certificate[0])

	stats := transport.Stats()
	c.Assert(stats.Received, check.Equals, int64(1))
	c.Assert(stats.Rx > 0, check.Equals, true)

	cancel()
	wg.Wait()
}

func (s *transportSuite) TestHTTPSTransportServeDevicesRoute(c *check.C) {
	var devices []assemblestate.Devices
	var peerCerts [][]byte
	pa := &testPeerAuthenticator{
		VerifyPeerFunc: func(cert []byte) (assemblestate.VerifiedPeer, error) {
			peerCerts = append(peerCerts, cert)
			return &testVerifiedPeer{
				CommitDevicesFunc: func(d assemblestate.Devices) error {
					devices = append(devices, d)
					return nil
				},
				RDTFunc: func() assemblestate.DeviceToken {
					return assemblestate.DeviceToken("peer-rdt")
				},
			}, nil
		},
	}

	transport := assemblestate.NewHTTPSTransport()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	c.Assert(err, check.IsNil)
	defer ln.Close()

	addr := ln.Addr().String()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = transport.Serve(ctx, ln, testServerCert, pa)
	}()

	msg := assemblestate.Devices{
		Devices: []assemblestate.Identity{
			{
				RDT:    assemblestate.DeviceToken("device-1"),
				FP:     assemblestate.CalculateFP([]byte("fingerprint-1")),
				Serial: "serial1",
			},
			{
				RDT:    assemblestate.DeviceToken("device-2"),
				FP:     assemblestate.CalculateFP([]byte("fingerprint-2")),
				Serial: "serial2",
			},
		},
	}

	client := transport.NewClient(testClientCert)
	err = client.Trusted(ctx, addr, testServerCert.Certificate[0], "devices", msg)
	c.Assert(err, check.IsNil)

	c.Assert(devices, check.HasLen, 1)
	c.Assert(devices[0], check.DeepEquals, msg)

	c.Assert(peerCerts, check.HasLen, 1)
	c.Assert(peerCerts[0], check.DeepEquals, testClientCert.Certificate[0])

	stats := transport.Stats()
	c.Assert(stats.Received, check.Equals, int64(1))
	c.Assert(stats.Rx > 0, check.Equals, true)

	cancel()
	wg.Wait()
}

func (s *transportSuite) TestHTTPSTransportServeRoutesRoute(c *check.C) {
	var routes []assemblestate.Routes
	var peerCerts [][]byte
	pa := &testPeerAuthenticator{
		VerifyPeerFunc: func(cert []byte) (assemblestate.VerifiedPeer, error) {
			peerCerts = append(peerCerts, cert)
			return &testVerifiedPeer{
				CommitRoutesFunc: func(r assemblestate.Routes) error {
					routes = append(routes, r)
					return nil
				},
				RDTFunc: func() assemblestate.DeviceToken {
					return assemblestate.DeviceToken("peer-rdt")
				},
			}, nil
		},
	}

	transport := assemblestate.NewHTTPSTransport()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	c.Assert(err, check.IsNil)
	defer ln.Close()

	addr := ln.Addr().String()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = transport.Serve(ctx, ln, testServerCert, pa)
	}()

	msg := assemblestate.Routes{
		Devices: []assemblestate.DeviceToken{
			assemblestate.DeviceToken("device-1"),
			assemblestate.DeviceToken("device-2"),
			assemblestate.DeviceToken("device-3"),
		},
		Addresses: []string{
			"192.168.1.1:8000",
			"192.168.1.2:8000",
			"192.168.1.3:8000",
		},
		Routes: []int{0, 1, 0, 1, 2, 1, 2, 0, 2},
	}

	client := transport.NewClient(testClientCert)
	err = client.Trusted(ctx, addr, testServerCert.Certificate[0], "routes", msg)
	c.Assert(err, check.IsNil)

	c.Assert(routes, check.HasLen, 1)
	c.Assert(routes[0], check.DeepEquals, msg)

	c.Assert(peerCerts, check.HasLen, 1)
	c.Assert(peerCerts[0], check.DeepEquals, testClientCert.Certificate[0])

	stats := transport.Stats()
	c.Assert(stats.Received, check.Equals, int64(1))
	c.Assert(stats.Rx > 0, check.Equals, true)

	cancel()
	wg.Wait()
}

func (s *transportSuite) TestHTTPSTransportServeUnknownRoute(c *check.C) {
	var unknownDevices []assemblestate.UnknownDevices
	var peerCerts [][]byte
	pa := &testPeerAuthenticator{
		VerifyPeerFunc: func(cert []byte) (assemblestate.VerifiedPeer, error) {
			peerCerts = append(peerCerts, cert)
			return &testVerifiedPeer{
				CommitDeviceQueriesFunc: func(u assemblestate.UnknownDevices) error {
					unknownDevices = append(unknownDevices, u)
					return nil
				},
				RDTFunc: func() assemblestate.DeviceToken {
					return assemblestate.DeviceToken("peer-rdt")
				},
			}, nil
		},
	}

	transport := assemblestate.NewHTTPSTransport()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	c.Assert(err, check.IsNil)
	defer ln.Close()

	addr := ln.Addr().String()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = transport.Serve(ctx, ln, testServerCert, pa)
	}()

	msg := assemblestate.UnknownDevices{
		Devices: []assemblestate.DeviceToken{
			assemblestate.DeviceToken("unknown-device-1"),
			assemblestate.DeviceToken("unknown-device-2"),
			assemblestate.DeviceToken("unknown-device-3"),
		},
	}

	client := transport.NewClient(testClientCert)
	err = client.Trusted(ctx, addr, testServerCert.Certificate[0], "unknown", msg)
	c.Assert(err, check.IsNil)

	c.Assert(unknownDevices, check.HasLen, 1)
	c.Assert(unknownDevices[0], check.DeepEquals, msg)

	c.Assert(peerCerts, check.HasLen, 1)
	c.Assert(peerCerts[0], check.DeepEquals, testClientCert.Certificate[0])

	stats := transport.Stats()
	c.Assert(stats.Received, check.Equals, int64(1))
	c.Assert(stats.Rx > 0, check.Equals, true)

	cancel()
	wg.Wait()
}

func (s *transportSuite) TestHTTPSTransportTrustedHandlerRejectsUnverifiedPeers(c *check.C) {
	pa := &testPeerAuthenticator{
		VerifyPeerFunc: func(cert []byte) (assemblestate.VerifiedPeer, error) {
			return nil, errors.New("peer verification failed")
		},
	}

	transport := assemblestate.NewHTTPSTransport()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	c.Assert(err, check.IsNil)
	defer ln.Close()

	addr := ln.Addr().String()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = transport.Serve(ctx, ln, testServerCert, pa)
	}()

	client := transport.NewClient(testClientCert)
	for _, endpoint := range []string{"routes", "unknown", "devices"} {
		err := client.Trusted(ctx, addr, testServerCert.Certificate[0], endpoint, []string{"some", "json"})
		c.Assert(err, check.NotNil)
		c.Assert(err, check.ErrorMatches, fmt.Sprintf("response to '%s' message contains status code 403", endpoint))
	}

	stats := transport.Stats()
	c.Assert(stats.Received, check.Equals, int64(3))

	cancel()
	wg.Wait()
}

func (s *transportSuite) TestHTTPSTransportServeAuthRejectsFailedAuthentication(c *check.C) {
	pa := &testPeerAuthenticator{
		AuthenticateAndCommitFunc: func(auth assemblestate.Auth, cert []byte) error {
			return errors.New("authentication failed")
		},
	}

	transport := assemblestate.NewHTTPSTransport()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	c.Assert(err, check.IsNil)
	defer ln.Close()

	addr := ln.Addr().String()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = transport.Serve(ctx, ln, testServerCert, pa)
	}()

	auth := assemblestate.Auth{
		HMAC: []byte("test-hmac-data"),
		RDT:  assemblestate.DeviceToken("test-rdt"),
	}

	client := transport.NewClient(testClientCert)
	_, err = client.Untrusted(ctx, addr, "auth", auth)
	c.Assert(err, check.NotNil)
	c.Assert(err.Error(), check.Equals, "got non-200 status code in response to auth message: 403")

	stats := transport.Stats()
	c.Assert(stats.Received, check.Equals, int64(1))

	cancel()
	wg.Wait()
}

func (s *transportSuite) TestHTTPSTransportTrustedHandlerRejectsMultipleCertificates(c *check.C) {
	pa := &testPeerAuthenticator{
		VerifyPeerFunc: func(cert []byte) (assemblestate.VerifiedPeer, error) {
			c.Fatal("should not be called when multiple certificates are provided")
			return nil, nil
		},
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	c.Assert(err, check.IsNil)
	defer ln.Close()

	addr := ln.Addr().String()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	transport := assemblestate.NewHTTPSTransport()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = transport.Serve(ctx, ln, testServerCert, pa)
	}()

	// create a second certificate to present alongside the first
	secondCert, _ := generateTestCert()
	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true,
				Certificates: []tls.Certificate{
					{
						Certificate: [][]byte{
							testClientCert.Certificate[0],
							secondCert.Certificate[0],
						},
						PrivateKey: testClientCert.PrivateKey,
					},
				},
			},
		},
		Timeout: time.Second * 5,
	}

	endpoints := []struct {
		path string
		data any
	}{
		{
			path: "auth",
			data: assemblestate.Auth{
				HMAC: []byte("test-hmac-data"),
				RDT:  assemblestate.DeviceToken("test-rdt"),
			},
		},
		{
			path: "routes",
			data: assemblestate.Routes{
				Devices:   []assemblestate.DeviceToken{"device1"},
				Addresses: []string{"addr1"},
				Routes:    []int{0},
			},
		},
		{
			path: "devices",
			data: assemblestate.Devices{
				Devices: []assemblestate.Identity{
					{
						RDT:    assemblestate.DeviceToken("device-1"),
						FP:     assemblestate.CalculateFP([]byte("fp")),
						Serial: "serial1",
					},
				},
			},
		},
		{
			path: "unknown",
			data: assemblestate.UnknownDevices{
				Devices: []assemblestate.DeviceToken{"unknown-device"},
			},
		},
	}

	for _, endpoint := range endpoints {
		payload, err := json.Marshal(endpoint.data)
		c.Assert(err, check.IsNil)

		url := fmt.Sprintf("https://%s/assemble/%s", addr, endpoint.path)
		req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(payload))
		c.Assert(err, check.IsNil)

		resp, err := client.Do(req)
		c.Assert(err, check.IsNil)
		defer resp.Body.Close()

		c.Assert(resp.StatusCode, check.Equals, 403)
	}

	stats := transport.Stats()
	c.Assert(stats.Received, check.Equals, int64(4))

	cancel()
	wg.Wait()
}

func (s *transportSuite) TestHTTPSTransportRejectsNonPOSTRequests(c *check.C) {
	pa := &testPeerAuthenticator{
		AuthenticateAndCommitFunc: func(auth assemblestate.Auth, cert []byte) error {
			c.Fatal("should not be called for non-POST requests")
			return nil
		},
		VerifyPeerFunc: func(cert []byte) (assemblestate.VerifiedPeer, error) {
			return &testVerifiedPeer{
				CommitDevicesFunc: func(devices assemblestate.Devices) error {
					c.Fatal("should not be called for non-POST requests")
					return nil
				},
				CommitDeviceQueriesFunc: func(unknown assemblestate.UnknownDevices) error {
					c.Fatal("should not be called for non-POST requests")
					return nil
				},
				CommitRoutesFunc: func(routes assemblestate.Routes) error {
					c.Fatal("should not be called for non-POST requests")
					return nil
				},
			}, nil
		},
	}

	transport := assemblestate.NewHTTPSTransport()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	c.Assert(err, check.IsNil)
	defer ln.Close()

	addr := ln.Addr().String()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = transport.Serve(ctx, ln, testServerCert, pa)
	}()

	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true,
				Certificates:       []tls.Certificate{testClientCert},
			},
		},
		Timeout: time.Second * 5,
	}

	for _, endpoint := range []string{"auth", "routes", "devices", "unknown"} {
		url := fmt.Sprintf("https://%s/assemble/%s", addr, endpoint)
		req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
		c.Assert(err, check.IsNil)

		resp, err := client.Do(req)
		c.Assert(err, check.IsNil)
		defer resp.Body.Close()

		c.Assert(resp.StatusCode, check.Equals, 405)
	}

	stats := transport.Stats()
	c.Assert(stats.Received, check.Equals, int64(4))

	cancel()
	wg.Wait()
}

func (s *transportSuite) TestHTTPSTransportVerifiedPeerErrors(c *check.C) {
	pa := &testPeerAuthenticator{
		VerifyPeerFunc: func(cert []byte) (assemblestate.VerifiedPeer, error) {
			return &testVerifiedPeer{
				CommitDevicesFunc: func(devices assemblestate.Devices) error {
					return errors.New("commit devices failed")
				},
				CommitDeviceQueriesFunc: func(unknown assemblestate.UnknownDevices) error {
					return errors.New("commit device queries failed")
				},
				CommitRoutesFunc: func(routes assemblestate.Routes) error {
					return errors.New("commit routes failed")
				},
			}, nil
		},
	}

	transport := assemblestate.NewHTTPSTransport()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	c.Assert(err, check.IsNil)
	defer ln.Close()

	addr := ln.Addr().String()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = transport.Serve(ctx, ln, testServerCert, pa)
	}()

	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true,
				Certificates:       []tls.Certificate{testClientCert},
			},
		},
		Timeout: time.Second * 5,
	}

	endpoints := []struct {
		path string
		data any
	}{
		{
			path: "routes",
			data: assemblestate.Routes{
				Devices:   []assemblestate.DeviceToken{"device1"},
				Addresses: []string{"addr1"},
				Routes:    []int{0},
			},
		},
		{
			path: "devices",
			data: assemblestate.Devices{
				Devices: []assemblestate.Identity{
					{
						RDT:    assemblestate.DeviceToken("device-1"),
						FP:     assemblestate.CalculateFP([]byte("fp")),
						Serial: "serial1",
					},
				},
			},
		},
		{
			path: "unknown",
			data: assemblestate.UnknownDevices{
				Devices: []assemblestate.DeviceToken{"unknown-device"},
			},
		},
	}

	for _, endpoint := range endpoints {
		payload, err := json.Marshal(endpoint.data)
		c.Assert(err, check.IsNil)

		url := fmt.Sprintf("https://%s/assemble/%s", addr, endpoint.path)
		req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(payload))
		c.Assert(err, check.IsNil)

		resp, err := client.Do(req)
		c.Assert(err, check.IsNil)
		defer resp.Body.Close()

		c.Assert(resp.StatusCode, check.Equals, 400)
	}

	stats := transport.Stats()
	c.Assert(stats.Received, check.Equals, int64(3))

	cancel()
	wg.Wait()
}

func (s *transportSuite) TestHTTPSTransportAuthenticateAndCommitError(c *check.C) {
	pa := &testPeerAuthenticator{
		AuthenticateAndCommitFunc: func(auth assemblestate.Auth, cert []byte) error {
			return errors.New("authentication failed")
		},
	}

	transport := assemblestate.NewHTTPSTransport()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	c.Assert(err, check.IsNil)
	defer ln.Close()

	addr := ln.Addr().String()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = transport.Serve(ctx, ln, testServerCert, pa)
	}()

	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true,
				Certificates:       []tls.Certificate{testClientCert},
			},
		},
		Timeout: time.Second * 5,
	}

	auth := assemblestate.Auth{
		HMAC: []byte("test-hmac-data"),
		RDT:  assemblestate.DeviceToken("test-rdt"),
	}

	payload, err := json.Marshal(auth)
	c.Assert(err, check.IsNil)

	url := fmt.Sprintf("https://%s/assemble/auth", addr)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(payload))
	c.Assert(err, check.IsNil)

	resp, err := client.Do(req)
	c.Assert(err, check.IsNil)
	defer resp.Body.Close()

	c.Assert(resp.StatusCode, check.Equals, 403)

	stats := transport.Stats()
	c.Assert(stats.Received, check.Equals, int64(1))

	cancel()
	wg.Wait()
}
