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

package assemblestate

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"time"

	"gopkg.in/check.v1"

	"github.com/snapcore/snapd/testutil"
)

type ClusterSuite struct{}

var _ = check.Suite(&ClusterSuite{})

// committer tracks commit calls and the session data that was committed
type committer struct {
	commits []AssembleSession
}

func (c *committer) commit(as AssembleSession) {
	c.commits = append(c.commits, as)
}

type selector struct {
	AddAuthoritativeRouteFunc func(r DeviceToken, via string)
	RecordRoutesFunc          func(r DeviceToken, ro Routes) (int, int, error)
	VerifyRoutesFunc          func()
	SelectFunc                func(to DeviceToken, count int) (routes Routes, ack func(), ok bool)
	RoutesFunc                func() Routes
}

func (s *selector) AddAuthoritativeRoute(r DeviceToken, via string) {
	if s.AddAuthoritativeRouteFunc == nil {
		panic("unexpected call")
	}
	s.AddAuthoritativeRouteFunc(r, via)
}

func (s *selector) RecordRoutes(r DeviceToken, ro Routes) (int, int, error) {
	if s.RecordRoutesFunc == nil {
		panic("unexpected call")
	}
	return s.RecordRoutesFunc(r, ro)
}

func (s *selector) VerifyRoutes() {
	if s.VerifyRoutesFunc == nil {
		panic("unexpected call")
	}
	s.VerifyRoutesFunc()
}

func (s *selector) Select(to DeviceToken, count int) (Routes, func(), bool) {
	if s.SelectFunc == nil {
		panic("unexpected call")
	}
	return s.SelectFunc(to, count)
}

func (s *selector) Routes() Routes {
	if s.RoutesFunc == nil {
		panic("unexpected call")
	}
	return s.RoutesFunc()
}

type testClient struct {
	TrustedFunc   func(ctx context.Context, addr string, cert []byte, kind string, message any) error
	UntrustedFunc func(ctx context.Context, addr string, kind string, message any) (cert []byte, err error)
}

func (m *testClient) Trusted(ctx context.Context, addr string, cert []byte, kind string, msg any) error {
	if m.TrustedFunc == nil {
		panic("unexpected call")
	}
	return m.TrustedFunc(ctx, addr, cert, kind, msg)
}

func (m *testClient) Untrusted(ctx context.Context, addr, kind string, msg any) ([]byte, error) {
	if m.UntrustedFunc == nil {
		panic("unexpected call")
	}
	return m.UntrustedFunc(ctx, addr, kind, msg)
}

type testTransport struct {
	ServeFunc     func(context.Context, string, tls.Certificate, *AssembleState) error
	NewClientFunc func(tls.Certificate) Client
	StatsFunc     func() (int64, int64, int64, int64)
}

func (t *testTransport) Serve(ctx context.Context, addr string, cert tls.Certificate, as *AssembleState) error {
	if t.ServeFunc == nil {
		panic("unexpected call")
	}
	return t.ServeFunc(ctx, addr, cert, as)
}

func (t *testTransport) NewClient(cert tls.Certificate) Client {
	if t.NewClientFunc == nil {
		panic("unexpected call")
	}
	return t.NewClientFunc(cert)
}

func (t *testTransport) Stats() (int64, int64, int64, int64) {
	if t.StatsFunc == nil {
		return 0, 0, 0, 0
	}
	return t.StatsFunc()
}

func createTestCertAndKey(c *check.C, ip net.IP) (certPEM []byte, keyPEM []byte) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	c.Assert(err, check.IsNil)

	serial, err := rand.Int(rand.Reader, big.NewInt(1<<62))
	c.Assert(err, check.IsNil)

	// TODO: rotation, renewal? don't worry about it? for now make it last until
	// the next century, when i'll be gone
	now := time.Now()
	template := x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: "localhost-ed25519"},
		NotBefore:    now,
		NotAfter:     now.AddDate(100, 0, 0),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses:  []net.IP{ip},
	}

	cert, err := x509.CreateCertificate(rand.Reader, &template, &template, pub, priv)
	c.Assert(err, check.IsNil)

	der, err := x509.MarshalPKCS8PrivateKey(priv)
	c.Assert(err, check.IsNil)

	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert})
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})

	return certPEM, keyPEM
}

func assembleStateWithTestKeys(c *check.C, sel *selector, cfg AssembleConfig) (*AssembleState, *committer, tls.Certificate) {
	certPEM, keyPEM := createTestCertAndKey(c, cfg.IP)

	cfg.TLSCert = certPEM
	cfg.TLSKey = keyPEM

	cm := &committer{}
	as, err := NewAssembleState(cfg, AssembleSession{}, func(DeviceToken, Identifier) (RouteSelector, error) {
		return sel, nil
	}, cm.commit)
	c.Assert(err, check.IsNil)

	cert, err := tls.X509KeyPair([]byte(cfg.TLSCert), []byte(cfg.TLSKey))
	c.Assert(err, check.IsNil)

	return as, cm, cert
}

func statelessSelector() *selector {
	return &selector{
		AddAuthoritativeRouteFunc: func(r DeviceToken, via string) {},
		RecordRoutesFunc: func(r DeviceToken, ro Routes) (int, int, error) {
			return 0, 0, nil
		},
		VerifyRoutesFunc: func() {},
		SelectFunc: func(to DeviceToken, count int) (Routes, func(), bool) {
			return Routes{}, nil, false
		},
		RoutesFunc: func() Routes { return Routes{} },
	}
}

func (s *ClusterSuite) TestPublishAuthAndCommit(c *check.C) {
	as, cm, tlsCert := assembleStateWithTestKeys(c, statelessSelector(), AssembleConfig{
		Secret: "secret",
		RDT:    "rdt",
		IP:     net.IPv4(127, 0, 0, 1),
		Port:   8001,
	})

	var called int
	client := testClient{
		UntrustedFunc: func(ctx context.Context, addr, kind string, message any) (cert []byte, err error) {
			called++

			c.Assert(addr, check.Equals, "127.0.0.1:8002")
			c.Assert(kind, check.Equals, "auth")

			auth := message.(Auth)

			expectedHMAC := CalculateHMAC("rdt", CalculateFP(tlsCert.Certificate[0]), "secret")
			c.Assert(auth.HMAC, check.DeepEquals, expectedHMAC)
			c.Assert(auth.RDT, check.Equals, DeviceToken("rdt"))

			return []byte("peer-certificate"), nil
		},
	}

	err := as.publishAuthAndCommit(context.Background(), []string{"127.0.0.1:8002"}, &client)
	c.Assert(err, check.IsNil)
	c.Assert(called, check.Equals, 1)

	c.Assert(len(cm.commits), check.Equals, 1)
	c.Assert(cm.commits[0].Addresses, check.DeepEquals, map[string]string{
		encodeCertAsFP([]byte("peer-certificate")): "127.0.0.1:8002",
	})
	c.Assert(cm.commits[0].Discovered, check.DeepEquals, []string{"127.0.0.1:8002"})

	// the second time around we shouldn't publish anything, since we already
	// have delivered an auth message to this peer
	called = 0
	err = as.publishAuthAndCommit(context.Background(), []string{"127.0.0.1:8002"}, &client)
	c.Assert(err, check.IsNil)
	c.Assert(called, check.Equals, 0)

	c.Assert(len(cm.commits), check.Equals, 2)
	c.Assert(cm.commits[1].Addresses, check.DeepEquals, map[string]string{
		encodeCertAsFP([]byte("peer-certificate")): "127.0.0.1:8002",
	})
	c.Assert(cm.commits[1].Discovered, check.DeepEquals, []string{"127.0.0.1:8002"})
}

func (s *ClusterSuite) TestPublishAuthAndCommitCertificateAddressMismatch(c *check.C) {
	as, cm, cert := assembleStateWithTestKeys(c, statelessSelector(), AssembleConfig{
		Secret: "secret",
		RDT:    "rdt",
		IP:     net.IPv4(127, 0, 0, 1),
		Port:   8001,
	})

	var calls int
	client := testClient{
		UntrustedFunc: func(ctx context.Context, addr, kind string, message any) ([]byte, error) {
			calls++

			c.Assert(kind, check.Equals, "auth")

			auth := message.(Auth)
			expectedHMAC := CalculateHMAC("rdt", CalculateFP(cert.Certificate[0]), "secret")
			c.Assert(auth.HMAC, check.DeepEquals, expectedHMAC)
			c.Assert(auth.RDT, check.Equals, DeviceToken("rdt"))

			// return the same certificate regardless of address
			return []byte("peer-certificate"), nil
		},
	}

	// first call should succeed and register the certificate with first address
	err := as.publishAuthAndCommit(context.Background(), []string{"127.0.0.1:8001"}, &client)
	c.Assert(err, check.IsNil)
	c.Assert(calls, check.Equals, 1)

	c.Assert(len(cm.commits), check.Equals, 1)
	c.Assert(cm.commits[0].Addresses, check.DeepEquals, map[string]string{
		encodeCertAsFP([]byte("peer-certificate")): "127.0.0.1:8001",
	})
	c.Assert(cm.commits[0].Discovered, check.DeepEquals, []string{"127.0.0.1:8001"})

	// second call with same certificate but different address should fail
	err = as.publishAuthAndCommit(context.Background(), []string{"127.0.0.1:8002"}, &client)
	c.Assert(err, check.ErrorMatches, "found new address 127.0.0.1:8002 using same certificate as other address 127.0.0.1:8001")
	c.Assert(calls, check.Equals, 2)

	c.Assert(len(cm.commits), check.Equals, 1)
}

func (s *ClusterSuite) TestAuthenticate(c *check.C) {
	as, cm, _ := assembleStateWithTestKeys(c, statelessSelector(), AssembleConfig{
		Secret: "secret",
		RDT:    "rdt",
		IP:     net.IPv4(127, 0, 0, 1),
		Port:   8001,
	})

	peerCert := []byte("peer-certificate")
	peerFP := CalculateFP(peerCert)
	peerRDT := DeviceToken("peer-rdt")

	// wrong RDT in HMAC
	auth := Auth{
		HMAC: CalculateHMAC("wrong-rdt", peerFP, "secret"),
		RDT:  peerRDT,
	}
	err := as.authenticateAndCommit(auth, peerCert)
	c.Assert(err, check.ErrorMatches, "received invalid HMAC from peer")
	c.Assert(len(cm.commits), check.Equals, 0, check.Commentf("commit should not be called on authentication failure"))

	// wrong RDT in message
	auth = Auth{
		HMAC: CalculateHMAC(peerRDT, peerFP, "secret"),
		RDT:  "wrong-rdt",
	}
	err = as.authenticateAndCommit(auth, peerCert)
	c.Assert(err, check.ErrorMatches, "received invalid HMAC from peer")
	c.Assert(len(cm.commits), check.Equals, 0, check.Commentf("commit should not be called on authentication failure"))

	// wrong FP in HMAC
	auth = Auth{
		HMAC: CalculateHMAC(peerRDT, CalculateFP([]byte("wrong-cert")), "secret"),
		RDT:  peerRDT,
	}
	err = as.authenticateAndCommit(auth, peerCert)
	c.Assert(err, check.ErrorMatches, "received invalid HMAC from peer")
	c.Assert(len(cm.commits), check.Equals, 0, check.Commentf("commit should not be called on authentication failure"))

	// wrong cert from transport layer
	auth = Auth{
		HMAC: CalculateHMAC(peerRDT, peerFP, "secret"),
		RDT:  peerRDT,
	}
	err = as.authenticateAndCommit(auth, []byte("wrong-cert"))
	c.Assert(err, check.ErrorMatches, "received invalid HMAC from peer")
	c.Assert(len(cm.commits), check.Equals, 0, check.Commentf("commit should not be called on authentication failure"))

	// wrong secret
	auth = Auth{
		HMAC: CalculateHMAC(peerRDT, peerFP, "wrong-secret"),
		RDT:  peerRDT,
	}
	err = as.authenticateAndCommit(auth, peerCert)
	c.Assert(err, check.ErrorMatches, "received invalid HMAC from peer")
	c.Assert(len(cm.commits), check.Equals, 0, check.Commentf("commit should not be called on authentication failure"))

	// valid case
	auth = Auth{
		HMAC: CalculateHMAC(peerRDT, peerFP, "secret"),
		RDT:  peerRDT,
	}
	err = as.authenticateAndCommit(auth, peerCert)
	c.Assert(err, check.IsNil)

	c.Assert(len(cm.commits), check.Equals, 1)
	c.Assert(cm.commits[0].Trusted, check.DeepEquals, map[string]Peer{
		encodeCertAsFP(peerCert): {
			RDT:  peerRDT,
			Cert: peerCert,
		},
	})
}

func (s *ClusterSuite) TestAuthenticateFingerprintMismatch(c *check.C) {
	as, cm, _ := assembleStateWithTestKeys(c, statelessSelector(), AssembleConfig{
		Secret: "secret",
		RDT:    "rdt",
		IP:     net.IPv4(127, 0, 0, 1),
		Port:   8001,
	})

	peerRDT := DeviceToken("peer-rdt")

	// first, add a device identity with a specific fingerprint
	correctCert := []byte("correct-certificate")
	correctFP := CalculateFP(correctCert)
	as.devices.RecordIdentity(Identity{
		RDT: peerRDT,
		FP:  correctFP,
	})

	// now try to authenticate with the same RDT but different certificate
	wrongCert := []byte("wrong-certificate")
	wrongFP := CalculateFP(wrongCert)

	auth := Auth{
		HMAC: CalculateHMAC(peerRDT, wrongFP, "secret"),
		RDT:  peerRDT,
	}

	err := as.authenticateAndCommit(auth, wrongCert)
	c.Assert(err, check.ErrorMatches, "fingerprint mismatch for device peer-rdt")

	// verify commit was not called on fingerprint mismatch
	c.Assert(len(cm.commits), check.Equals, 0, check.Commentf("commit should not be called on fingerprint mismatch"))
}

func (s *ClusterSuite) TestAuthenticateCertificateReuse(c *check.C) {
	as, cm, _ := assembleStateWithTestKeys(c, statelessSelector(), AssembleConfig{
		Secret: "secret",
		RDT:    "rdt",
		IP:     net.IPv4(127, 0, 0, 1),
		Port:   8001,
	})

	cert := []byte("certificate")
	fp := CalculateFP(cert)

	// first peer authenticates successfully
	err := as.authenticateAndCommit(Auth{
		HMAC: CalculateHMAC("peer-one", fp, "secret"),
		RDT:  "peer-one",
	}, cert)
	c.Assert(err, check.IsNil)

	c.Assert(len(cm.commits), check.Equals, 1)
	c.Assert(cm.commits[0].Trusted, check.DeepEquals, map[string]Peer{
		encodeCertAsFP(cert): {
			RDT:  "peer-one",
			Cert: cert,
		},
	})

	// second peer tries to use the same certificate - should fail
	err = as.authenticateAndCommit(Auth{
		HMAC: CalculateHMAC("peer-two", fp, "secret"),
		RDT:  "peer-two",
	}, cert)
	c.Assert(err, check.ErrorMatches, `peer "peer-one" and "peer-two" are using the same TLS certificate`)

	c.Assert(len(cm.commits), check.Equals, 1)
}

func (s *ClusterSuite) TestAuthenticateCertificateConsistency(c *check.C) {
	as, cm, _ := assembleStateWithTestKeys(c, statelessSelector(), AssembleConfig{
		Secret: "secret",
		RDT:    "rdt",
		IP:     net.IPv4(127, 0, 0, 1),
		Port:   8001,
	})

	// first authentication with first certificate
	cert := []byte("certificate-one")
	fp := CalculateFP(cert)
	err := as.authenticateAndCommit(Auth{
		HMAC: CalculateHMAC("peer", fp, "secret"),
		RDT:  "peer",
	}, cert)
	c.Assert(err, check.IsNil)

	c.Assert(len(cm.commits), check.Equals, 1)
	c.Assert(cm.commits[0].Trusted, check.DeepEquals, map[string]Peer{
		encodeCertAsFP(cert): {
			RDT:  "peer",
			Cert: cert,
		},
	})

	// second authentication with different certificate - should fail
	cert = []byte("certificate-two")
	fp = CalculateFP(cert)
	err = as.authenticateAndCommit(Auth{
		HMAC: CalculateHMAC("peer", fp, "secret"),
		RDT:  "peer",
	}, cert)
	c.Assert(err, check.ErrorMatches, `peer "peer" is using a new TLS certificate`)

	c.Assert(len(cm.commits), check.Equals, 1)
}

func (s *ClusterSuite) TestAuthenticateWithKnownAddress(c *check.C) {
	var authoritative []struct {
		rdt DeviceToken
		via string
	}
	sel := &selector{
		AddAuthoritativeRouteFunc: func(rdt DeviceToken, via string) {
			authoritative = append(authoritative, struct {
				rdt DeviceToken
				via string
			}{rdt, via})
		},
		RecordRoutesFunc: func(r DeviceToken, ro Routes) (int, int, error) {
			return 0, 0, nil
		},
		VerifyRoutesFunc: func() {},
		SelectFunc: func(to DeviceToken, count int) (Routes, func(), bool) {
			return Routes{}, nil, false
		},
		RoutesFunc: func() Routes { return Routes{} },
	}

	as, cm, _ := assembleStateWithTestKeys(c, sel, AssembleConfig{
		Secret: "secret",
		RDT:    "rdt",
		IP:     net.IPv4(127, 0, 0, 1),
		Port:   8001,
	})

	const peerRDT = DeviceToken("peer")
	const peerAddr = "127.0.0.1:8002"
	peerCert := []byte("peer-certificate")
	peerFP := CalculateFP(peerCert)

	// first, use publishAuth to discover the peer's address
	client := &testClient{
		UntrustedFunc: func(ctx context.Context, addr, kind string, message any) ([]byte, error) {
			c.Assert(addr, check.Equals, peerAddr)
			c.Assert(kind, check.Equals, "auth")
			return peerCert, nil
		},
	}

	err := as.publishAuthAndCommit(context.Background(), []string{peerAddr}, client)
	c.Assert(err, check.IsNil)

	c.Assert(len(cm.commits), check.Equals, 1)
	c.Assert(cm.commits[0].Addresses, check.DeepEquals, map[string]string{
		encodeCertAsFP(peerCert): peerAddr,
	})
	c.Assert(cm.commits[0].Discovered, check.DeepEquals, []string{peerAddr})

	// verify no authoritative routes added yet
	c.Assert(len(authoritative), check.Equals, 0)

	// now authenticate the peer
	auth := Auth{
		HMAC: CalculateHMAC(peerRDT, peerFP, "secret"),
		RDT:  peerRDT,
	}

	err = as.authenticateAndCommit(auth, peerCert)
	c.Assert(err, check.IsNil)

	c.Assert(len(cm.commits), check.Equals, 2)
	c.Assert(cm.commits[1].Trusted, check.DeepEquals, map[string]Peer{
		encodeCertAsFP(peerCert): {
			RDT:  peerRDT,
			Cert: peerCert,
		},
	})
	c.Assert(cm.commits[1].Addresses, check.DeepEquals, map[string]string{
		encodeCertAsFP(peerCert): peerAddr,
	})

	// since we have discovered the route from us to the peer and the peer has
	// authenticated, then AddAuthoritativeRoute should have been called
	c.Assert(len(authoritative), check.Equals, 1)
	c.Assert(authoritative[0].rdt, check.Equals, peerRDT)
	c.Assert(authoritative[0].via, check.Equals, peerAddr)
}

func (s *ClusterSuite) TestVerifyPeer(c *check.C) {
	as, _, _ := assembleStateWithTestKeys(c, statelessSelector(), AssembleConfig{
		Secret: "secret",
		RDT:    "rdt",
		IP:     net.IPv4(127, 0, 0, 1),
		Port:   8001,
	})

	peerCert := []byte("peer-certificate")
	peerFP := CalculateFP(peerCert)
	peerRDT := DeviceToken("peer-rdt")

	err := as.authenticateAndCommit(Auth{
		HMAC: CalculateHMAC(peerRDT, peerFP, "secret"),
		RDT:  peerRDT,
	}, peerCert)
	c.Assert(err, check.IsNil)

	handle, err := as.verifyPeer(peerCert)
	c.Assert(err, check.IsNil)

	c.Assert(handle.RDT(), check.Equals, peerRDT)
}

func (s *ClusterSuite) TestVerifyPeerUntrustedCert(c *check.C) {
	as, _, _ := assembleStateWithTestKeys(c, statelessSelector(), AssembleConfig{
		Secret: "secret",
		RDT:    "rdt",
		IP:     net.IPv4(127, 0, 0, 1),
		Port:   8001,
	})

	// try to verify a certificate that was never authenticated
	handle, err := as.verifyPeer([]byte("untrusted-certificate"))
	c.Assert(err, check.ErrorMatches, "given TLS certificate is not associated with a trusted RDT")
	c.Assert(handle, check.IsNil)
}

func trustedAndDiscoveredPeer(c *check.C, as *AssembleState, rdt DeviceToken) (h *peerHandle, address string, cert []byte) {
	peerCert := []byte(fmt.Sprintf("%s-certificate", rdt))
	peerFP := CalculateFP(peerCert)

	err := as.authenticateAndCommit(Auth{
		HMAC: CalculateHMAC(rdt, peerFP, "secret"),
		RDT:  rdt,
	}, peerCert)
	c.Assert(err, check.IsNil)

	handle, err := as.verifyPeer(peerCert)
	c.Assert(err, check.IsNil)

	c.Assert(handle.RDT(), check.Equals, rdt)

	peerAddr := fmt.Sprintf("%s-addr", rdt)
	client := testClient{
		UntrustedFunc: func(ctx context.Context, addr, kind string, message any) (cert []byte, err error) {
			c.Assert(addr, check.Equals, peerAddr)
			c.Assert(kind, check.Equals, "auth")
			return peerCert, nil
		},
	}

	err = as.publishAuthAndCommit(context.Background(), []string{peerAddr}, &client)
	c.Assert(err, check.IsNil)

	return handle, peerAddr, peerCert
}

func trustedPeer(c *check.C, as *AssembleState, rdt DeviceToken) (h *peerHandle, cert []byte) {
	peerCert := []byte(fmt.Sprintf("%s-certificate", rdt))
	peerFP := CalculateFP(peerCert)

	err := as.authenticateAndCommit(Auth{
		HMAC: CalculateHMAC(rdt, peerFP, "secret"),
		RDT:  rdt,
	}, peerCert)
	c.Assert(err, check.IsNil)

	handle, err := as.verifyPeer(peerCert)
	c.Assert(err, check.IsNil)

	c.Assert(handle.RDT(), check.Equals, rdt)

	return handle, peerCert
}

func (s *ClusterSuite) TestPublishDeviceQueries(c *check.C) {
	as, cm, _ := assembleStateWithTestKeys(c, statelessSelector(), AssembleConfig{
		Secret: "secret",
		RDT:    "rdt",
		IP:     net.IPv4(127, 0, 0, 1),
		Port:   8001,
	})

	peerRDT := DeviceToken("peer")
	peer, peerAddr, peerCert := trustedAndDiscoveredPeer(c, as, peerRDT)

	// this tells us that this peer has knowledge of one and two.
	err := peer.CommitRoutes(Routes{
		Devices: []DeviceToken{"one", "two"},
	})
	c.Assert(err, check.IsNil)

	client := testClient{
		TrustedFunc: func(ctx context.Context, addr string, cert []byte, kind string, message any) error {
			c.Assert(addr, check.Equals, peerAddr)
			c.Assert(cert, check.DeepEquals, peerCert)
			c.Assert(kind, check.Equals, "unknown")

			unknown := message.(UnknownDevices)
			c.Assert(unknown.Devices, testutil.DeepUnsortedMatches, []DeviceToken{"one", "two"})
			return nil
		},
	}
	baseline := len(cm.commits)
	as.publishDeviceQueries(context.Background(), &client)
	// publishing device queries does not commit
	c.Assert(len(cm.commits), check.Equals, baseline)

	// act as if the peer responded for only one of the devices
	err = peer.CommitDevices(Devices{
		Devices: []Identity{{
			RDT: "one",
			FP:  CalculateFP([]byte("one-certificate")),
		}},
	})
	c.Assert(err, check.IsNil)

	// now, we should expect to see a query for just "two"
	client.TrustedFunc = func(ctx context.Context, addr string, cert []byte, kind string, message any) error {
		c.Assert(addr, check.Equals, peerAddr)
		c.Assert(cert, check.DeepEquals, peerCert)
		c.Assert(kind, check.Equals, "unknown")

		unknown := message.(UnknownDevices)
		c.Assert(unknown.Devices, testutil.DeepUnsortedMatches, []DeviceToken{"two"})
		return nil
	}
	baseline = len(cm.commits)
	as.publishDeviceQueries(context.Background(), &client)
	// publishing device queries does not commit
	c.Assert(len(cm.commits), check.Equals, baseline)
}

func (s *ClusterSuite) TestPublishDevicesAndCommit(c *check.C) {
	as, cm, _ := assembleStateWithTestKeys(c, statelessSelector(), AssembleConfig{
		Secret: "secret",
		RDT:    "rdt",
		IP:     net.IPv4(127, 0, 0, 1),
		Port:   8001,
	})

	oneRDT := DeviceToken("one")
	one, _, _ := trustedAndDiscoveredPeer(c, as, oneRDT)

	// inform us of devices one and two
	err := one.CommitDevices(Devices{
		Devices: []Identity{
			{
				RDT: "one",
				FP:  CalculateFP([]byte("one-certificate")),
			},
			{
				RDT: "two",
				FP:  CalculateFP([]byte("two-certificate")),
			},
		},
	})
	c.Assert(err, check.IsNil)

	threeRDT := DeviceToken("three")
	three, threeAddr, threeCert := trustedAndDiscoveredPeer(c, as, threeRDT)

	baseline := len(cm.commits)
	as.publishDevicesAndCommit(context.Background(), &testClient{})
	c.Assert(len(cm.commits), check.Equals, baseline+1)

	// three asks us for information about two
	three.CommitDeviceQueries(UnknownDevices{
		Devices: []DeviceToken{"two"},
	})

	var called int
	client := testClient{
		TrustedFunc: func(ctx context.Context, addr string, cert []byte, kind string, message any) error {
			called++
			c.Assert(addr, check.Equals, threeAddr)
			c.Assert(cert, check.DeepEquals, threeCert)
			c.Assert(kind, check.Equals, "devices")

			devices := message.(Devices)
			c.Assert(devices.Devices, testutil.DeepUnsortedMatches, []Identity{{
				RDT: "two",
				FP:  CalculateFP([]byte("two-certificate")),
			}})
			return nil
		},
	}
	baseline = len(cm.commits)
	as.publishDevicesAndCommit(context.Background(), &client)
	c.Assert(called, check.Equals, 1)
	c.Assert(len(cm.commits), check.Equals, baseline+1)

	// since we successfully published the response to the query, we don't send
	// anything
	baseline = len(cm.commits)
	as.publishDevicesAndCommit(context.Background(), &testClient{})
	c.Assert(len(cm.commits), check.Equals, baseline+1)
}

func (s *ClusterSuite) TestCommitDevicesFingerprintMismatch(c *check.C) {
	as, cm, _ := assembleStateWithTestKeys(c, statelessSelector(), AssembleConfig{
		Secret: "secret",
		RDT:    "rdt",
		IP:     net.IPv4(127, 0, 0, 1),
		Port:   8001,
	})

	peerRDT := DeviceToken("peer-rdt")
	peer, _, _ := trustedAndDiscoveredPeer(c, as, peerRDT)

	baseline := len(cm.commits)

	// try to add a device identity with the peer's RDT but wrong fingerprint
	wrongFP := CalculateFP([]byte("wrong-certificate"))

	err := peer.CommitDevices(Devices{
		Devices: []Identity{{
			RDT: peerRDT,
			FP:  wrongFP,
		}},
	})

	c.Assert(err, check.ErrorMatches, "fingerprint mismatch for device peer-rdt")
	c.Assert(len(cm.commits), check.Equals, baseline, check.Commentf("commit should not be called on fingerprint mismatch"))
}

func (s *ClusterSuite) TestCommitDevicesInconsistentIdentity(c *check.C) {
	as, cm, _ := assembleStateWithTestKeys(c, statelessSelector(), AssembleConfig{
		Secret: "secret",
		RDT:    "rdt",
		IP:     net.IPv4(127, 0, 0, 1),
		Port:   8001,
	})

	one, _, _ := trustedAndDiscoveredPeer(c, as, "peer-one")
	two, _, _ := trustedAndDiscoveredPeer(c, as, "peer-two")

	// first peer records an identity for a third device
	id := Identity{
		RDT: "peer-three",
		FP:  CalculateFP([]byte("original-certificate")),
	}

	err := one.CommitDevices(Devices{
		Devices: []Identity{id},
	})
	c.Assert(err, check.IsNil)

	baseline := len(cm.commits)

	// second peer tries to record a different identity for the same device
	conflicting := Identity{
		RDT: "peer-three",
		FP:  CalculateFP([]byte("different-certificate")),
	}

	err = two.CommitDevices(Devices{
		Devices: []Identity{conflicting},
	})

	c.Assert(err, check.ErrorMatches, "got inconsistent device identity")
	c.Assert(len(cm.commits), check.Equals, baseline, check.Commentf("commit should not be called on identity inconsistency"))
}

func (s *ClusterSuite) TestPublishRoutes(c *check.C) {
	selector := statelessSelector()
	as, cm, _ := assembleStateWithTestKeys(c, selector, AssembleConfig{
		Secret: "secret",
		RDT:    "rdt",
		IP:     net.IPv4(127, 0, 0, 1),
		Port:   8001,
	})

	oneRDT := DeviceToken("one")
	_, oneAddr, oneCert := trustedAndDiscoveredPeer(c, as, oneRDT)

	twoRDT := DeviceToken("two")
	_, twoAddr, twoCert := trustedAndDiscoveredPeer(c, as, twoRDT)

	threeRDT := DeviceToken("three")
	trustedPeer(c, as, threeRDT)

	baseline := len(cm.commits)

	var msg testClient
	var called int
	acked := make(map[DeviceToken]int)

	selector.SelectFunc = func(to DeviceToken, count int) (Routes, func(), bool) {
		called++
		return Routes{}, func() {
			acked[to]++
		}, true
	}

	msg.TrustedFunc = func(ctx context.Context, addr string, cert []byte, kind string, message any) error {
		switch addr {
		case oneAddr:
			c.Assert(cert, check.DeepEquals, oneCert)
		case twoAddr:
			c.Assert(cert, check.DeepEquals, twoCert)
		default:
			c.Fatalf("unexpected address: %s", addr)
		}
		c.Assert(kind, check.Equals, "routes")
		_ = message.(Routes)
		return nil
	}

	as.publishRoutes(context.Background(), &msg, 5, 100)
	c.Assert(called, check.Equals, 2)

	// since peer three isn't discovered, we should have only acked our
	// publications to peer one and two (each called once)
	c.Assert(acked, check.DeepEquals, map[DeviceToken]int{
		oneRDT: 1,
		twoRDT: 1,
	})

	// publishing routes doesn't commit anything
	c.Assert(len(cm.commits), check.Equals, baseline)
}

func (s *ClusterSuite) TestNewAssembleStateTimeout(c *check.C) {
	ip := net.IPv4(127, 0, 0, 1)
	certPEM, keyPEM := createTestCertAndKey(c, ip)

	// use a fixed time for testing
	now := time.Now()
	clock := func() time.Time {
		return now
	}

	cfg := AssembleConfig{
		Secret:  "secret",
		RDT:     "rdt",
		IP:      ip,
		Port:    8001,
		TLSCert: certPEM,
		TLSKey:  keyPEM,
		Clock:   clock,
	}

	commit := func(AssembleSession) {}

	// test with no initiated time (new session)
	_, err := NewAssembleState(cfg, AssembleSession{}, func(DeviceToken, Identifier) (RouteSelector, error) {
		return statelessSelector(), nil
	}, commit)
	c.Assert(err, check.IsNil)

	// test with recent session still within the timeout (30 minutes old)
	recent := AssembleSession{
		Initiated: now.Add(-30 * time.Minute),
	}
	_, err = NewAssembleState(cfg, recent, func(DeviceToken, Identifier) (RouteSelector, error) {
		return statelessSelector(), nil
	}, commit)
	c.Assert(err, check.IsNil)

	// test with expired session (2 hours old)
	expired := AssembleSession{
		Initiated: now.Add(-2 * time.Hour),
	}
	_, err = NewAssembleState(cfg, expired, func(DeviceToken, Identifier) (RouteSelector, error) {
		return statelessSelector(), nil
	}, commit)
	c.Assert(err, check.ErrorMatches, "invalid session data: cannot resume an assembly session that began more than an hour ago")
}

func (s *ClusterSuite) TestNewAssembleStateWithSessionImport(c *check.C) {
	type peer struct {
		rdt  DeviceToken
		cert []byte
		fp   Fingerprint
	}

	ip := net.IPv4(127, 0, 0, 1)
	certPEM, keyPEM := createTestCertAndKey(c, ip)
	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	c.Assert(err, check.IsNil)

	local := peer{
		rdt:  "local-rdt",
		cert: certPEM,
		fp:   CalculateFP(cert.Certificate[0]),
	}

	var peers []peer
	for i, rdt := range []string{"peer-one-rdt", "peer-two-rdt", "peer-three-rdt"} {
		certPEM, keyPEM := createTestCertAndKey(c, net.IPv4(127, 0, 0, byte(2+i)))
		cert, err := tls.X509KeyPair(certPEM, keyPEM)
		c.Assert(err, check.IsNil)

		peers = append(peers, peer{
			rdt:  DeviceToken(rdt),
			cert: certPEM,
			fp:   CalculateFP(cert.Certificate[0]),
		})
	}

	now := time.Now()
	clock := func() time.Time {
		return now
	}

	// create pre-populated session with various data
	session := AssembleSession{
		Initiated: now.Add(-30 * time.Minute),
		Trusted: map[string]Peer{
			base64.StdEncoding.EncodeToString(peers[0].fp[:]): {
				RDT:  peers[0].rdt,
				Cert: peers[0].cert,
			},
			base64.StdEncoding.EncodeToString(peers[1].fp[:]): {
				RDT:  peers[1].rdt,
				Cert: peers[1].cert,
			},
		},
		Addresses: map[string]string{
			base64.StdEncoding.EncodeToString(peers[0].fp[:]): "127.0.0.2:8001",
			base64.StdEncoding.EncodeToString(peers[1].fp[:]): "127.0.0.3:8001",
			base64.StdEncoding.EncodeToString(peers[2].fp[:]): "127.0.0.4:8001",
		},
		Discovered: []string{
			"127.0.0.2:8001",
		},
		Routes: Routes{
			Devices:   []DeviceToken{local.rdt, peers[0].rdt, peers[1].rdt},
			Addresses: []string{"127.0.0.1:8001", "127.0.0.2:8001", "127.0.0.3:8001"},
			Routes: []int{
				0, 1, 1, // local->peer-one via addr[1]
				0, 2, 2, // local->peer-two via addr[2]
			},
		},
		Devices: DeviceQueryTrackerData{
			IDs: []Identity{
				{
					RDT: peers[0].rdt,
					FP:  peers[0].fp,
				},
				{
					RDT: peers[1].rdt,
					FP:  peers[1].fp,
				},
			},
			Queries: map[DeviceToken][]DeviceToken{
				peers[0].rdt: {local.rdt},
			},
			Known: map[DeviceToken][]DeviceToken{
				peers[0].rdt: {"unknown-rdt"},
			},
		},
	}

	// track selector calls
	var routes Routes
	var authoritative []struct {
		to  DeviceToken
		via string
	}

	selector := &selector{
		AddAuthoritativeRouteFunc: func(to DeviceToken, via string) {
			authoritative = append(authoritative, struct {
				to  DeviceToken
				via string
			}{to, via})
		},
		RecordRoutesFunc: func(from DeviceToken, r Routes) (int, int, error) {
			routes = r
			return len(r.Routes) / 3, len(r.Routes) / 3, nil
		},
		VerifyRoutesFunc: func() {},
		SelectFunc: func(to DeviceToken, count int) (Routes, func(), bool) {
			return Routes{
				Devices:   []DeviceToken{local.rdt, to},
				Addresses: []string{"127.0.0.1:8001"},
				Routes:    []int{0, 1, 0},
			}, func() {}, true
		},
		RoutesFunc: func() Routes {
			return routes
		},
	}

	cfg := AssembleConfig{
		Secret:  "secret",
		RDT:     local.rdt,
		IP:      ip,
		Port:    8001,
		TLSCert: certPEM,
		TLSKey:  keyPEM,
		Clock:   clock,
	}

	// create AssembleState with imported session
	as, err := NewAssembleState(cfg, session, func(self DeviceToken, identified func(DeviceToken) bool) (RouteSelector, error) {
		c.Assert(self, check.Equals, local.rdt)
		return selector, nil
	}, func(AssembleSession) {})
	c.Assert(err, check.IsNil)

	// verify imported routes were recorded in selector
	c.Assert(routes, check.DeepEquals, session.Routes)

	// verify authoritative routes were added for trusted peers with addresses
	c.Assert(len(authoritative), check.Equals, 2) // peer-one and peer-two
	found := make(map[DeviceToken]bool)
	for _, route := range authoritative {
		found[route.to] = true
		switch route.to {
		case peers[0].rdt:
			c.Assert(route.via, check.Equals, "127.0.0.2:8001")
		case peers[1].rdt:
			c.Assert(route.via, check.Equals, "127.0.0.3:8001")
		}
	}
	c.Assert(found[peers[0].rdt], check.Equals, true)
	c.Assert(found[peers[1].rdt], check.Equals, true)

	// test publishing auth messages; should skip already discovered address
	var publications []string
	client := &testClient{
		UntrustedFunc: func(ctx context.Context, addr, kind string, message any) ([]byte, error) {
			publications = append(publications, addr)
			switch addr {
			case "127.0.0.3:8001":
				return peers[1].cert, nil
			case "127.0.0.5:8001":
				return []byte("new-peer-cert"), nil
			}
			return nil, errors.New("unexpected address")
		},
	}

	// try to publish auth to discovered and undiscovered addresses
	err = as.publishAuthAndCommit(context.Background(), []string{
		"127.0.0.2:8001", // already discovered, should skip
		"127.0.0.3:8001", // not discovered yet
		"127.0.0.5:8001", // completely new
	}, client)
	c.Assert(err, check.IsNil)
	c.Assert(publications, check.DeepEquals, []string{"127.0.0.3:8001", "127.0.0.5:8001"})

	// test publishing routes - should only send to trusted peers with addresses
	publications = nil
	client = &testClient{
		TrustedFunc: func(ctx context.Context, addr string, cert []byte, kind string, message any) error {
			publications = append(publications, addr)
			// verify we're using the correct certificate
			switch addr {
			case "127.0.0.2:8001":
				c.Assert(cert, check.DeepEquals, peers[0].cert)
			case "127.0.0.3:8001":
				c.Assert(cert, check.DeepEquals, peers[1].cert)
			}
			return nil
		},
	}

	as.publishRoutes(context.Background(), client, 10, 100)
	c.Assert(publications, testutil.DeepUnsortedMatches, []string{"127.0.0.2:8001", "127.0.0.3:8001"})

	// test publishing devices - respond to device queries that were imported
	publications = nil
	client = &testClient{
		TrustedFunc: func(ctx context.Context, addr string, cert []byte, kind string, message any) error {
			publications = append(publications, addr)
			devices := message.(Devices)
			c.Check(devices.Devices, check.DeepEquals, []Identity{{
				RDT: local.rdt,
				FP:  local.fp,
			}})
			return nil
		},
	}

	as.publishDevicesAndCommit(context.Background(), client)
	c.Assert(publications, check.DeepEquals, []string{"127.0.0.2:8001"})

	publications = nil
	client = &testClient{
		TrustedFunc: func(ctx context.Context, addr string, cert []byte, kind string, message any) error {
			publications = append(publications, addr)
			unknowns := message.(UnknownDevices)
			c.Assert(unknowns.Devices, check.DeepEquals, []DeviceToken{"unknown-rdt"})
			return nil
		},
	}

	as.publishDeviceQueries(context.Background(), client)
	c.Assert(publications, check.DeepEquals, []string{"127.0.0.2:8001"})
}

func (s *ClusterSuite) TestNewAssembleStateInvalidSessionData(c *check.C) {
	ip := net.IPv4(127, 0, 0, 1)
	cert, key := createTestCertAndKey(c, ip)

	cfg := AssembleConfig{
		Secret:  "secret",
		RDT:     "local-rdt",
		IP:      ip,
		Port:    8001,
		TLSCert: cert,
		TLSKey:  key,
	}

	commit := func(AssembleSession) {}

	testCases := []struct {
		name    string
		session AssembleSession
		err     string
	}{
		{
			name: "invalid base64 fingerprint in trusted peers",
			session: AssembleSession{
				Trusted: map[string]Peer{
					"not-valid-base64!!!": {
						RDT:  "peer-rdt",
						Cert: []byte("cert"),
					},
				},
			},
			err: "invalid session data: .*illegal base64.*",
		},
		{
			name: "invalid base64 fingerprint in addresses",
			session: AssembleSession{
				Addresses: map[string]string{
					"not-valid-base64!!!": "127.0.0.2:8001",
				},
			},
			err: "invalid session data: .*illegal base64.*",
		},
		{
			name: "wrong size fingerprint in trusted peers",
			session: AssembleSession{
				Trusted: map[string]Peer{
					base64.StdEncoding.EncodeToString([]byte("too-short")): {
						RDT:  "peer-rdt",
						Cert: []byte("cert"),
					},
				},
			},
			err: "invalid session data: invalid fingerprint in trusted peers: certificate fingerprint expected to be 64 bytes",
		},
		{
			name: "wrong size fingerprint in addresses",
			session: AssembleSession{
				Addresses: map[string]string{
					base64.StdEncoding.EncodeToString([]byte("too-short")): "127.0.0.2:8001",
				},
			},
			err: "invalid session data: invalid fingerprint in addresses: certificate fingerprint expected to be 64 bytes",
		},
		{
			name: "routes array not multiple of 3",
			session: AssembleSession{
				Routes: Routes{
					Devices:   []DeviceToken{"device1", "device2"},
					Addresses: []string{"addr1", "addr2"},
					Routes:    []int{0, 1}, // incomplete route
				},
			},
			err: "invalid session data: routes array length must be multiple of 3",
		},
		{
			name: "invalid source device index in routes",
			session: AssembleSession{
				Routes: Routes{
					Devices:   []DeviceToken{"device1"},
					Addresses: []string{"addr1"},
					Routes:    []int{5, 0, 0}, // device index 5 doesn't exist
				},
			},
			err: "invalid session data: invalid source device index 5 in routes",
		},
		{
			name: "invalid destination device index in routes",
			session: AssembleSession{
				Routes: Routes{
					Devices:   []DeviceToken{"device1"},
					Addresses: []string{"addr1"},
					Routes:    []int{0, 5, 0}, // device index 5 doesn't exist
				},
			},
			err: "invalid session data: invalid destination device index 5 in routes",
		},
		{
			name: "invalid address index in routes",
			session: AssembleSession{
				Routes: Routes{
					Devices:   []DeviceToken{"device1", "device2"},
					Addresses: []string{"addr1"},
					Routes:    []int{0, 1, 5}, // address index 5 doesn't exist
				},
			},
			err: "invalid session data: invalid address index 5 in routes",
		},
		{
			name: "discovered address not in addresses map",
			session: func() AssembleSession {
				fp := CalculateFP([]byte("cert"))
				return AssembleSession{
					Addresses: map[string]string{
						base64.StdEncoding.EncodeToString(fp[:]): "127.0.0.1:8001",
					},
					Discovered: []string{
						"127.0.0.2:8001", // not in addresses
					},
				}
			}(),
			err: "invalid session data: discovered address \"127.0.0.2:8001\" not found in addresses map",
		},
		{
			name: "local device fingerprint mismatch",
			session: func() AssembleSession {
				// use a different fingerprint than what the test cert would generate
				wrongFP := CalculateFP([]byte("wrong-cert"))
				return AssembleSession{
					Devices: DeviceQueryTrackerData{
						IDs: []Identity{
							{
								RDT: "local-rdt", // matches cfg.RDT in the test
								FP:  wrongFP,     // different from actual cert fingerprint
							},
						},
					},
				}
			}(),
			err: "fingerprint mismatch for local device.*",
		},
	}

	for _, tc := range testCases {
		_, err := NewAssembleState(cfg, tc.session, func(DeviceToken, func(DeviceToken) bool) (RouteSelector, error) {
			return statelessSelector(), nil
		}, commit)

		c.Assert(err, check.NotNil, check.Commentf("test case %q", tc.name))
		c.Assert(err, check.ErrorMatches, tc.err, check.Commentf("test case %q", tc.name))
	}
}

func (s *ClusterSuite) TestRunTimeout(c *check.C) {
	ip := net.IPv4(127, 0, 0, 1)
	certPEM, keyPEM := createTestCertAndKey(c, ip)

	// mock clock that advances time on each call
	started := time.Now()
	called := false
	clock := func() time.Time {
		// first call during NewAssembleState: return current time
		if !called {
			called = true
			return started
		}

		// subsequent calls: return time that's past the 1-hour limit
		return started.Add(time.Hour + time.Second)
	}

	cfg := AssembleConfig{
		Secret:  "secret",
		RDT:     "rdt",
		IP:      ip,
		Port:    8001,
		TLSCert: certPEM,
		TLSKey:  keyPEM,
		Clock:   clock,
	}

	commit := func(AssembleSession) {}

	transport := &testTransport{
		ServeFunc: func(ctx context.Context, addr string, cert tls.Certificate, as *AssembleState) error {
			<-ctx.Done()
			return ctx.Err()
		},
		NewClientFunc: func(cert tls.Certificate) Client {
			return &testClient{}
		},
	}

	discover := func(ctx context.Context) ([]string, error) {
		return []string{}, nil
	}

	session := AssembleSession{
		Initiated: started,
	}
	as, err := NewAssembleState(cfg, session, func(DeviceToken, Identifier) (RouteSelector, error) {
		return statelessSelector(), nil
	}, commit)
	c.Assert(err, check.IsNil)

	// when Run is called, the clock will return a time past the 1-hour limit
	_, err = as.Run(context.Background(), transport, discover)
	c.Assert(err, check.ErrorMatches, "cannot resume an assembly session that began more than an hour ago")
}

func (s *ClusterSuite) TestRunServerError(c *check.C) {
	ip := net.IPv4(127, 0, 0, 1)
	certPEM, keyPEM := createTestCertAndKey(c, ip)

	cfg := AssembleConfig{
		Secret:  "secret",
		RDT:     "rdt",
		IP:      ip,
		Port:    8001,
		TLSCert: certPEM,
		TLSKey:  keyPEM,
	}

	commit := func(AssembleSession) {}

	// create a transport that returns a non-context.Canceled error
	serverError := errors.New("bind failed")
	transport := &testTransport{
		ServeFunc: func(ctx context.Context, addr string, cert tls.Certificate, as *AssembleState) error {
			return serverError
		},
		NewClientFunc: func(cert tls.Certificate) Client {
			return &testClient{}
		},
	}

	discover := func(ctx context.Context) ([]string, error) {
		return []string{}, nil
	}

	as, err := NewAssembleState(cfg, AssembleSession{}, func(DeviceToken, Identifier) (RouteSelector, error) {
		return statelessSelector(), nil
	}, commit)
	c.Assert(err, check.IsNil)

	// run should return the server error wrapped with "server failed: "
	_, err = as.Run(context.Background(), transport, discover)
	c.Assert(err, testutil.ErrorIs, serverError)
}

func encodeCertAsFP(fingerprint []byte) string {
	fp := CalculateFP(fingerprint)
	return base64.StdEncoding.EncodeToString(fp[:])
}
