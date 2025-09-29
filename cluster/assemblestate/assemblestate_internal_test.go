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
	"strings"
	"time"

	"gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
	"github.com/snapcore/snapd/randutil"
	"github.com/snapcore/snapd/testutil"
)

type clusterSuite struct{}

var _ = check.Suite(&clusterSuite{})

func createTestIdentity(
	c *check.C,
	signing *assertstest.StoreStack,
	rdt DeviceToken,
	fp Fingerprint,
	secret string,
) Identity {
	_, bundle, key := createTestSerialBundle(c, signing)

	// create SerialProof using the same device key
	hmac := CalculateHMAC(rdt, fp, secret)
	proof, err := asserts.RawSignWithKey(hmac, key)
	c.Assert(err, check.IsNil)

	return Identity{
		RDT:          rdt,
		FP:           fp,
		SerialBundle: bundle,
		SerialProof:  proof,
	}
}

func createTestSerial(
	c *check.C,
	signing assertstest.SignerDB,
) (*asserts.Serial, asserts.PrivateKey) {
	// create a device key for the serial assertion
	key, _ := assertstest.GenerateKey(752)
	pubkey, err := asserts.EncodePublicKey(key.PublicKey())
	c.Assert(err, check.IsNil)

	headers := map[string]any{
		"authority-id":        "canonical",
		"brand-id":            "canonical",
		"model":               "test-model",
		"serial":              randutil.RandomString(10),
		"device-key":          string(pubkey),
		"device-key-sha3-384": key.PublicKey().ID(),
		"timestamp":           time.Now().Format(time.RFC3339),
	}

	assertion, err := signing.Sign(asserts.SerialType, headers, nil, "")
	c.Assert(err, check.IsNil)

	s, ok := assertion.(*asserts.Serial)
	c.Assert(ok, check.Equals, true)
	return s, key
}

func createTestSerialBundle(
	c *check.C,
	signing *assertstest.StoreStack,
) (*asserts.Serial, string, asserts.PrivateKey) {
	serial, key := createTestSerial(c, signing)
	bundle, err := buildSerialBundle(serial, signing.Database)
	c.Assert(err, check.Equals, nil)
	return serial, bundle, key
}

func createTestAssembleConfig(c *check.C, signing *assertstest.StoreStack, secret, rdt string) AssembleConfig {
	certPEM, keyPEM := createTestCertAndKey(c)
	serial, _, deviceKey := createTestSerialBundle(c, signing)
	return AssembleConfig{
		Secret:  secret,
		RDT:     DeviceToken(rdt),
		TLSCert: certPEM,
		TLSKey:  keyPEM,
		Serial:  serial,
		Signer:  privateKeySigner(deviceKey),
		Clock:   time.Now,
	}
}

func mockAssertDB(c *check.C) (*asserts.Database, *assertstest.StoreStack) {
	signing := assertstest.NewStoreStack("canonical", nil)
	db, err := asserts.OpenDatabase(&asserts.DatabaseConfig{
		Backstore: asserts.NewMemoryBackstore(),
		Trusted:   signing.Trusted,
	})
	c.Assert(err, check.IsNil)

	err = db.Add(signing.StoreAccountKey(""))
	c.Assert(err, check.IsNil)

	return db, signing
}

func privateKeySigner(pk asserts.PrivateKey) func([]byte) ([]byte, error) {
	return func(data []byte) ([]byte, error) {
		return asserts.RawSignWithKey(data, pk)
	}
}

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
	CompleteFunc              func(size int) (bool, error)
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

func (s *selector) Complete(size int) (bool, error) {
	if s.CompleteFunc == nil {
		panic("unexpected call")
	}
	return s.CompleteFunc(size)
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

// addr implements net.Addr.
type addr struct {
	address string
}

func (m *addr) Network() string {
	return "test"
}

func (m *addr) String() string {
	return m.address
}

// listener implements net.Listener.
type listener struct {
	addr net.Addr
}

func (m *listener) Accept() (net.Conn, error) {
	panic("unexpected call")
}

func (m *listener) Close() error {
	panic("unexpected call")
}

func (m *listener) Addr() net.Addr {
	return m.addr
}

// testListener creates a new mock listener with the given address.
func testListener(address string) net.Listener {
	return &listener{
		addr: &addr{address: address},
	}
}

type testTransport struct {
	ServeFunc     func(context.Context, net.Listener, tls.Certificate, PeerAuthenticator) error
	NewClientFunc func(tls.Certificate) Client
}

func (t *testTransport) Serve(ctx context.Context, ln net.Listener, cert tls.Certificate, pv PeerAuthenticator) error {
	if t.ServeFunc == nil {
		panic("unexpected call")
	}
	return t.ServeFunc(ctx, ln, cert, pv)
}

func (t *testTransport) NewClient(cert tls.Certificate) Client {
	if t.NewClientFunc == nil {
		panic("unexpected call")
	}
	return t.NewClientFunc(cert)
}

func (t *testTransport) Stats() TransportStats {
	return TransportStats{}
}

func createTestCertAndKey(c *check.C) (certPEM []byte, keyPEM []byte) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	c.Assert(err, check.IsNil)

	serial, err := rand.Int(rand.Reader, big.NewInt(1<<62))
	c.Assert(err, check.IsNil)

	now := time.Now()
	template := x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: "localhost-ed25519"},
		NotBefore:    now,
		NotAfter:     now.AddDate(100, 0, 0),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses:  []net.IP{net.IPv4(127, 0, 0, 1)},
	}

	cert, err := x509.CreateCertificate(rand.Reader, &template, &template, pub, priv)
	c.Assert(err, check.IsNil)

	der, err := x509.MarshalPKCS8PrivateKey(priv)
	c.Assert(err, check.IsNil)

	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert})
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})

	return certPEM, keyPEM
}

func newAssembleStateWithTestKeys(c *check.C, sel *selector, cfg AssembleConfig) (*AssembleState, *committer, tls.Certificate, asserts.PrivateKey, *assertstest.StoreStack) {
	certPEM, keyPEM := createTestCertAndKey(c)

	cfg.TLSCert = certPEM
	cfg.TLSKey = keyPEM

	db, signing := mockAssertDB(c)
	serial, key := createTestSerial(c, signing)
	cfg.Serial = serial
	cfg.Signer = privateKeySigner(key)

	cm := &committer{}
	as, err := NewAssembleState(cfg, AssembleSession{}, func(DeviceToken, Identifier) (RouteSelector, error) {
		return sel, nil
	}, cm.commit, db)
	c.Assert(err, check.IsNil)

	cert, err := tls.X509KeyPair([]byte(cfg.TLSCert), []byte(cfg.TLSKey))
	c.Assert(err, check.IsNil)

	return as, cm, cert, key, signing
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
		CompleteFunc: func(size int) (bool, error) {
			return false, nil
		},
	}
}

func (s *clusterSuite) TestPublishAuthAndCommit(c *check.C) {
	as, cm, cert, _, _ := newAssembleStateWithTestKeys(c, statelessSelector(), AssembleConfig{
		Secret: "secret",
		RDT:    "rdt",
	})

	var called int
	client := testClient{
		UntrustedFunc: func(ctx context.Context, addr, kind string, message any) ([]byte, error) {
			called++

			c.Assert(addr, check.Equals, "127.0.0.1:8002")
			c.Assert(kind, check.Equals, "auth")

			auth := message.(Auth)

			expectedHMAC := CalculateHMAC("rdt", CalculateFP(cert.Certificate[0]), "secret")
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

func (s *clusterSuite) TestPublishAuthAndCommitCertificateAddressMismatch(c *check.C) {
	as, cm, cert, _, _ := newAssembleStateWithTestKeys(c, statelessSelector(), AssembleConfig{
		Secret: "secret",
		RDT:    "rdt",
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

func (s *clusterSuite) TestAuthenticate(c *check.C) {
	as, cm, _, _, _ := newAssembleStateWithTestKeys(c, statelessSelector(), AssembleConfig{
		Secret: "secret",
		RDT:    "rdt",
	})

	peerCert := []byte("peer-certificate")
	peerFP := CalculateFP(peerCert)
	peerRDT := DeviceToken("peer-rdt")

	// valid case
	auth := Auth{
		HMAC: CalculateHMAC(peerRDT, peerFP, "secret"),
		RDT:  peerRDT,
	}
	err := as.AuthenticateAndCommit(auth, peerCert)
	c.Assert(err, check.IsNil)

	c.Assert(len(cm.commits), check.Equals, 1)
	c.Assert(cm.commits[0].Trusted, check.DeepEquals, map[string]Peer{
		encodeCertAsFP(peerCert): {
			RDT:  peerRDT,
			Cert: peerCert,
		},
	})
}

func (s *clusterSuite) TestAuthenticateErrorCases(c *check.C) {
	as, cm, _, _, _ := newAssembleStateWithTestKeys(c, statelessSelector(), AssembleConfig{
		Secret: "secret",
		RDT:    "rdt",
	})

	peerCert := []byte("peer-certificate")
	peerFP := CalculateFP(peerCert)
	peerRDT := DeviceToken("peer-rdt")

	cases := []struct {
		name string
		auth Auth
		cert []byte
		err  string
	}{
		{
			name: "wrong RDT in HMAC",
			auth: Auth{
				HMAC: CalculateHMAC("wrong-rdt", peerFP, "secret"),
				RDT:  peerRDT,
			},
			cert: peerCert,
			err:  "received invalid HMAC from peer",
		},
		{
			name: "wrong RDT in message",
			auth: Auth{
				HMAC: CalculateHMAC(peerRDT, peerFP, "secret"),
				RDT:  "wrong-rdt",
			},
			cert: peerCert,
			err:  "received invalid HMAC from peer",
		},
		{
			name: "wrong FP in HMAC",
			auth: Auth{
				HMAC: CalculateHMAC(peerRDT, CalculateFP([]byte("wrong-cert")), "secret"),
				RDT:  peerRDT,
			},
			cert: peerCert,
			err:  "received invalid HMAC from peer",
		},
		{
			name: "wrong cert from transport layer",
			auth: Auth{
				HMAC: CalculateHMAC(peerRDT, peerFP, "secret"),
				RDT:  peerRDT,
			},
			cert: []byte("wrong-cert"),
			err:  "received invalid HMAC from peer",
		},
		{
			name: "wrong secret",
			auth: Auth{
				HMAC: CalculateHMAC(peerRDT, peerFP, "wrong-secret"),
				RDT:  peerRDT,
			},
			cert: peerCert,
			err:  "received invalid HMAC from peer",
		},
	}

	for _, tc := range cases {
		err := as.AuthenticateAndCommit(tc.auth, tc.cert)
		c.Assert(err, check.NotNil, check.Commentf("test case %q", tc.name))
		c.Assert(err, check.ErrorMatches, tc.err, check.Commentf("test case %q", tc.name))
		c.Assert(cm.commits, check.HasLen, 0)
	}
}

func (s *clusterSuite) TestAuthenticateFingerprintMismatch(c *check.C) {
	as, cm, _, _, signing := newAssembleStateWithTestKeys(c, statelessSelector(), AssembleConfig{
		Secret: "secret",
		RDT:    "rdt",
	})

	peerRDT := DeviceToken("peer-rdt")

	// first, add a device identity with a specific fingerprint
	correctCert := []byte("correct-certificate")
	correctFP := CalculateFP(correctCert)
	identity := createTestIdentity(c, signing, peerRDT, correctFP, "secret")
	err := as.devices.RecordIdentity(identity)
	c.Assert(err, check.IsNil)

	// now try to authenticate with the same RDT but different certificate
	wrongCert := []byte("wrong-certificate")
	wrongFP := CalculateFP(wrongCert)

	auth := Auth{
		HMAC: CalculateHMAC(peerRDT, wrongFP, "secret"),
		RDT:  peerRDT,
	}

	err = as.AuthenticateAndCommit(auth, wrongCert)
	c.Assert(err, check.ErrorMatches, "fingerprint mismatch for device peer-rdt")

	// verify commit was not called on fingerprint mismatch
	c.Assert(len(cm.commits), check.Equals, 0, check.Commentf("commit should not be called on fingerprint mismatch"))
}

func (s *clusterSuite) TestAuthenticateCertificateReuse(c *check.C) {
	as, cm, _, _, _ := newAssembleStateWithTestKeys(c, statelessSelector(), AssembleConfig{
		Secret: "secret",
		RDT:    "rdt",
	})

	cert := []byte("certificate")
	fp := CalculateFP(cert)

	// first peer authenticates successfully
	err := as.AuthenticateAndCommit(Auth{
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
	err = as.AuthenticateAndCommit(Auth{
		HMAC: CalculateHMAC("peer-two", fp, "secret"),
		RDT:  "peer-two",
	}, cert)
	c.Assert(err, check.ErrorMatches, `peer "peer-one" and "peer-two" are using the same TLS certificate`)

	c.Assert(len(cm.commits), check.Equals, 1)
}

func (s *clusterSuite) TestAuthenticateCertificateConsistency(c *check.C) {
	as, cm, _, _, _ := newAssembleStateWithTestKeys(c, statelessSelector(), AssembleConfig{
		Secret: "secret",
		RDT:    "rdt",
	})

	// first authentication with first certificate
	cert := []byte("certificate-one")
	fp := CalculateFP(cert)
	err := as.AuthenticateAndCommit(Auth{
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
	err = as.AuthenticateAndCommit(Auth{
		HMAC: CalculateHMAC("peer", fp, "secret"),
		RDT:  "peer",
	}, cert)
	c.Assert(err, check.ErrorMatches, `peer "peer" is using a new TLS certificate`)

	c.Assert(len(cm.commits), check.Equals, 1)
}

func (s *clusterSuite) TestAuthenticateWithKnownAddress(c *check.C) {
	var authoritative []struct {
		rdt DeviceToken
		via string
	}
	sel := statelessSelector()
	sel.AddAuthoritativeRouteFunc = func(rdt DeviceToken, via string) {
		authoritative = append(authoritative, struct {
			rdt DeviceToken
			via string
		}{rdt, via})
	}

	as, cm, _, _, _ := newAssembleStateWithTestKeys(c, sel, AssembleConfig{
		Secret: "secret",
		RDT:    "rdt",
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

	err = as.AuthenticateAndCommit(auth, peerCert)
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

func (s *clusterSuite) TestVerifyPeer(c *check.C) {
	as, _, _, _, _ := newAssembleStateWithTestKeys(c, statelessSelector(), AssembleConfig{
		Secret: "secret",
		RDT:    "rdt",
	})

	peerCert := []byte("peer-certificate")
	peerFP := CalculateFP(peerCert)
	peerRDT := DeviceToken("peer-rdt")

	err := as.AuthenticateAndCommit(Auth{
		HMAC: CalculateHMAC(peerRDT, peerFP, "secret"),
		RDT:  peerRDT,
	}, peerCert)
	c.Assert(err, check.IsNil)

	vp, err := as.VerifyPeer(peerCert)
	c.Assert(err, check.IsNil)

	h := vp.(*peerHandle)
	c.Assert(h.rdt, check.Equals, peerRDT)
}

func (s *clusterSuite) TestVerifyPeerUntrustedCert(c *check.C) {
	as, _, _, _, _ := newAssembleStateWithTestKeys(c, statelessSelector(), AssembleConfig{
		Secret: "secret",
		RDT:    "rdt",
	})

	// try to verify a certificate that was never authenticated
	handle, err := as.VerifyPeer([]byte("untrusted-certificate"))
	c.Assert(err, check.ErrorMatches, "given TLS certificate is not associated with a trusted RDT")
	c.Assert(handle, check.IsNil)
}

func trustedAndDiscoveredPeer(c *check.C, as *AssembleState, rdt DeviceToken) (vp VerifiedPeer, address string, cert []byte) {
	peerCert := []byte(fmt.Sprintf("%s-certificate", rdt))
	peerFP := CalculateFP(peerCert)

	err := as.AuthenticateAndCommit(Auth{
		HMAC: CalculateHMAC(rdt, peerFP, "secret"),
		RDT:  rdt,
	}, peerCert)
	c.Assert(err, check.IsNil)

	handle, err := as.VerifyPeer(peerCert)
	c.Assert(err, check.IsNil)

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

func trustedPeer(c *check.C, as *AssembleState, rdt DeviceToken) (vp VerifiedPeer, cert []byte) {
	peerCert := []byte(fmt.Sprintf("%s-certificate", rdt))
	peerFP := CalculateFP(peerCert)

	err := as.AuthenticateAndCommit(Auth{
		HMAC: CalculateHMAC(rdt, peerFP, "secret"),
		RDT:  rdt,
	}, peerCert)
	c.Assert(err, check.IsNil)

	handle, err := as.VerifyPeer(peerCert)
	c.Assert(err, check.IsNil)

	return handle, peerCert
}

func (s *clusterSuite) TestPublishDeviceQueries(c *check.C) {
	as, cm, _, _, signing := newAssembleStateWithTestKeys(c, statelessSelector(), AssembleConfig{
		Secret: "secret",
		RDT:    "rdt",
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
	oneID := createTestIdentity(c, signing, "one", CalculateFP([]byte("one-certificate")), "secret")
	err = peer.CommitDevices(Devices{
		Devices: []Identity{oneID},
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

func (s *clusterSuite) TestPublishDevicesAndCommit(c *check.C) {
	as, cm, _, key, signing := newAssembleStateWithTestKeys(c, statelessSelector(), AssembleConfig{
		Secret: "secret",
		RDT:    "self",
	})

	one, _, _ := trustedAndDiscoveredPeer(c, as, "one")

	// inform us of devices one and two
	oneID := createTestIdentity(c, signing, "one", CalculateFP([]byte("one-certificate")), "secret")
	twoID := createTestIdentity(c, signing, "two", CalculateFP([]byte("two-certificate")), "secret")
	err := one.CommitDevices(Devices{
		Devices: []Identity{oneID, twoID},
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
			c.Assert(devices.Devices, testutil.DeepUnsortedMatches, []Identity{twoID})
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

	// now test that we also send signed serial proofs for the local device
	three.CommitDeviceQueries(UnknownDevices{
		Devices: []DeviceToken{"self"}, // query for local device
	})

	called = 0
	client.TrustedFunc = func(ctx context.Context, addr string, cert []byte, kind string, message any) error {
		called++
		c.Assert(addr, check.Equals, threeAddr)
		c.Assert(cert, check.DeepEquals, threeCert)
		c.Assert(kind, check.Equals, "devices")

		devices := message.(Devices)
		c.Assert(len(devices.Devices), check.Equals, 1)

		d := devices.Devices[0]
		c.Assert(d.RDT, check.Equals, DeviceToken("self"))

		var sawSerial, sawAccountKey bool
		commitBundleAndObserve(c, signing.Trusted, d.SerialBundle, func(a asserts.Assertion) {
			switch asn := a.(type) {
			case *asserts.Serial:
				sawSerial = true
				c.Assert(asn.DeviceKey().ID(), check.Equals, key.PublicKey().ID())
			case *asserts.AccountKey:
				sawAccountKey = true
			}
		})
		c.Assert(sawSerial, check.Equals, true)
		c.Assert(sawAccountKey, check.Equals, true)

		// verify the serial proof contains a valid signature of the expected HMAC
		expectedHMAC := CalculateHMAC("self", d.FP, "secret")

		// verify the serial proof is a valid signature of the HMAC using the expected device key
		// this tests that the system used the correct key for signing
		err := asserts.RawVerifyWithKey(expectedHMAC, d.SerialProof, key.PublicKey())
		c.Assert(err, check.IsNil)

		return nil
	}

	baseline = len(cm.commits)
	as.publishDevicesAndCommit(context.Background(), &client)
	c.Assert(called, check.Equals, 1)
	c.Assert(cm.commits, check.HasLen, baseline+1)
}

func (s *clusterSuite) TestPublishDevicesIncludesAccountAndAccountKey(c *check.C) {
	assertDB, store := mockAssertDB(c)

	const brand = "external-brand"
	brandAccount := assertstest.NewAccount(store.RootSigning, brand, map[string]any{
		"account-id": brand,
	}, "")
	brandPK, _ := assertstest.GenerateKey(752)
	brandAccountKey := assertstest.NewAccountKey(store.RootSigning, brandAccount, map[string]any{
		"name": "default",
	}, brandPK.PublicKey(), "")

	assertstest.AddMany(assertDB, brandAccount, brandAccountKey)

	deviceKey, _ := assertstest.GenerateKey(752)
	devicePub, err := asserts.EncodePublicKey(deviceKey.PublicKey())
	c.Assert(err, check.IsNil)

	brandSigning := assertstest.NewSigningDB(brand, brandPK)
	serialHeaders := map[string]any{
		"authority-id":        brand,
		"brand-id":            brand,
		"model":               "test-model",
		"serial":              randutil.RandomString(10),
		"device-key":          string(devicePub),
		"device-key-sha3-384": deviceKey.PublicKey().ID(),
		"timestamp":           time.Now().Format(time.RFC3339),
	}

	serialAssertion, err := brandSigning.Sign(asserts.SerialType, serialHeaders, nil, "")
	c.Assert(err, check.IsNil)
	serial := serialAssertion.(*asserts.Serial)

	certPEM, keyPEM := createTestCertAndKey(c)
	cfg := AssembleConfig{
		Secret:  "secret",
		RDT:     DeviceToken("self"),
		TLSCert: certPEM,
		TLSKey:  keyPEM,
		Serial:  serial,
		Signer:  privateKeySigner(deviceKey),
		Clock:   time.Now,
	}

	cm := &committer{}
	as, err := NewAssembleState(cfg, AssembleSession{}, func(DeviceToken, Identifier) (RouteSelector, error) {
		return statelessSelector(), nil
	}, cm.commit, assertDB)
	c.Assert(err, check.IsNil)

	peer, peerAddr, peerCert := trustedAndDiscoveredPeer(c, as, DeviceToken("peer"))
	peer.CommitDeviceQueries(UnknownDevices{
		Devices: []DeviceToken{"self"},
	})

	var called int
	client := testClient{
		TrustedFunc: func(ctx context.Context, addr string, cert []byte, kind string, message any) error {
			called++
			c.Assert(addr, check.Equals, peerAddr)
			c.Assert(cert, check.DeepEquals, peerCert)
			c.Assert(kind, check.Equals, "devices")

			devices := message.(Devices)
			c.Assert(devices.Devices, check.HasLen, 1)
			d := devices.Devices[0]
			c.Assert(d.RDT, check.Equals, DeviceToken("self"))

			var sawAccount, sawAccountKey bool
			commitBundleAndObserve(c, store.Trusted, d.SerialBundle, func(a asserts.Assertion) {
				switch asn := a.(type) {
				case *asserts.Account:
					if asn.AccountID() == brand {
						sawAccount = true
					}
				case *asserts.AccountKey:
					if asn.AccountID() == brand && asn.PublicKeyID() == brandPK.PublicKey().ID() {
						sawAccountKey = true
					}
				}
			})

			c.Assert(sawAccount, check.Equals, true)
			c.Assert(sawAccountKey, check.Equals, true)
			return nil
		},
	}

	as.publishDevicesAndCommit(context.Background(), &client)
	c.Assert(called, check.Equals, 1)
}

func commitBundleAndObserve(c *check.C, trusted []asserts.Assertion, bundle string, observe func(asserts.Assertion)) {
	batch := asserts.NewBatch(nil)
	_, err := batch.AddStream(strings.NewReader(bundle))
	c.Assert(err, check.IsNil)

	db, err := asserts.OpenDatabase(&asserts.DatabaseConfig{
		Backstore: asserts.NewMemoryBackstore(),
		Trusted:   trusted,
	})
	c.Assert(err, check.IsNil)

	err = batch.CommitToAndObserve(db, observe, &asserts.CommitOptions{Precheck: true})
	c.Assert(err, check.IsNil)
}

func (s *clusterSuite) TestCommitDevicesFingerprintMismatch(c *check.C) {
	as, cm, _, _, _ := newAssembleStateWithTestKeys(c, statelessSelector(), AssembleConfig{
		Secret: "secret",
		RDT:    "self",
	})

	peerRDT := DeviceToken("peer-rdt")
	peer, _, _ := trustedAndDiscoveredPeer(c, as, peerRDT)

	baseline := len(cm.commits)

	// try to add a device identity with the peer's RDT but wrong fingerprint
	wrongFP := CalculateFP([]byte("wrong-certificate"))

	err := peer.CommitDevices(Devices{
		Devices: []Identity{{
			RDT:          peerRDT,
			FP:           wrongFP,
			SerialBundle: "peer-serial",
		}},
	})

	c.Assert(err, check.ErrorMatches, "fingerprint mismatch for device peer-rdt")
	c.Assert(len(cm.commits), check.Equals, baseline, check.Commentf("commit should not be called on fingerprint mismatch"))
}

func (s *clusterSuite) TestCommitDevicesInconsistentIdentity(c *check.C) {
	as, cm, _, _, signing := newAssembleStateWithTestKeys(c, statelessSelector(), AssembleConfig{
		Secret: "secret",
		RDT:    "self",
	})

	one, _, _ := trustedAndDiscoveredPeer(c, as, "peer-one")
	two, _, _ := trustedAndDiscoveredPeer(c, as, "peer-two")

	// first peer records an identity for a third device
	id := createTestIdentity(c, signing, "peer-three", CalculateFP([]byte("original-certificate")), "secret")

	err := one.CommitDevices(Devices{
		Devices: []Identity{id},
	})
	c.Assert(err, check.IsNil)

	baseline := len(cm.commits)

	// second peer tries to record a different identity for the same device
	conflicting := createTestIdentity(c, signing, "peer-three", CalculateFP([]byte("different-certificate")), "secret")

	err = two.CommitDevices(Devices{
		Devices: []Identity{conflicting},
	})

	c.Assert(err, check.ErrorMatches, "got inconsistent device identity")
	c.Assert(len(cm.commits), check.Equals, baseline, check.Commentf("commit should not be called on identity inconsistency"))
}

func (s *clusterSuite) TestCommitDevicesMissingBundlePrerequisites(c *check.C) {
	assertDB, store := mockAssertDB(c)
	cfg := createTestAssembleConfig(c, store, "secret", "self")

	cm := &committer{}
	as, err := NewAssembleState(cfg, AssembleSession{}, func(DeviceToken, Identifier) (RouteSelector, error) {
		return statelessSelector(), nil
	}, cm.commit, assertDB)
	c.Assert(err, check.IsNil)

	const brand = "external-brand"

	deviceKey, _ := assertstest.GenerateKey(752)
	devicePub, err := asserts.EncodePublicKey(deviceKey.PublicKey())
	c.Assert(err, check.IsNil)

	serialHeaders := map[string]any{
		"authority-id":        brand,
		"brand-id":            brand,
		"model":               "test-model",
		"serial":              randutil.RandomString(10),
		"device-key":          string(devicePub),
		"device-key-sha3-384": deviceKey.PublicKey().ID(),
		"timestamp":           time.Now().Format(time.RFC3339),
	}

	brandPK, _ := assertstest.GenerateKey(752)
	brandSigning := assertstest.NewSigningDB(brand, brandPK)

	serial, err := brandSigning.Sign(asserts.SerialType, serialHeaders, nil, "")
	c.Assert(err, check.IsNil)

	// note that we only encode the serial assertion. should trigger an error
	// about missing prereqs when attempting to commit the identity
	bundle := asserts.Encode(serial)

	fp := CalculateFP([]byte("cert"))
	hmac := CalculateHMAC("rdt", fp, "secret")

	proof, err := asserts.RawSignWithKey(hmac, deviceKey)
	c.Assert(err, check.IsNil)

	id := Identity{
		RDT:          "rdt",
		FP:           fp,
		SerialBundle: string(bundle),
		SerialProof:  proof,
	}

	peer, _, _ := trustedAndDiscoveredPeer(c, as, DeviceToken("peer"))
	err = peer.CommitDevices(Devices{
		Devices: []Identity{id},
	})
	c.Assert(err, check.ErrorMatches, "invalid serial assertion for device rdt: invalid identity for device rdt: cannot resolve prerequisite assertion: .*")
}

func (s *clusterSuite) TestRecordDevicesForgedIdentity(c *check.C) {
	as, _, _, _, signing := newAssembleStateWithTestKeys(c, statelessSelector(), AssembleConfig{
		Secret: "secret",
		RDT:    "self",
	})

	peer, _, _ := trustedAndDiscoveredPeer(c, as, "trusted-peer")

	cases := []struct {
		name     string
		identity func() Identity
		err      string
	}{
		{
			name: "wrong device key in serial proof",
			identity: func() Identity {
				// create a valid serial assertion
				_, bundle, _ := createTestSerialBundle(c, signing)

				// but sign the proof with a completely different key
				attackerKey, _ := assertstest.GenerateKey(752)

				rdt := DeviceToken("victim-rdt")
				fp := CalculateFP([]byte("victim-cert"))
				hmac := CalculateHMAC(rdt, fp, "secret")

				// sign with attacker's key instead of device key from serial
				proof, err := asserts.RawSignWithKey(hmac, attackerKey)
				c.Assert(err, check.IsNil)

				return Identity{
					RDT:          rdt,
					FP:           fp,
					SerialBundle: bundle,
					SerialProof:  proof,
				}
			},
			err: ".*serial proof verification failed.*",
		},
		{
			name: "modified serial assertion",
			identity: func() Identity {
				// create a valid serial assertion and device key
				serial, bundle, deviceKey := createTestSerialBundle(c, signing)

				rdt := DeviceToken("victim-rdt")
				fp := CalculateFP([]byte("victim-cert"))
				hmac := CalculateHMAC(rdt, fp, "secret")

				// create valid proof
				proof, err := asserts.RawSignWithKey(hmac, deviceKey)
				c.Assert(err, check.IsNil)

				// but modify the serial assertion after signing
				validSerial := string(asserts.Encode(serial))
				modifiedSerial := validSerial[:len(validSerial)-10] + "HACKED" + validSerial[len(validSerial)-6:]
				tamperedBundle := strings.Replace(bundle, validSerial, modifiedSerial, 1)

				return Identity{
					RDT:          rdt,
					FP:           fp,
					SerialBundle: tamperedBundle,
					SerialProof:  proof,
				}
			},
			err: "(?s).*cannot decode signature.*",
		},
		{
			name: "wrong secret in hmac",
			identity: func() Identity {
				_, bundle, deviceKey := createTestSerialBundle(c, signing)

				rdt := DeviceToken("victim-rdt")
				fp := CalculateFP([]byte("victim-cert"))

				// attacker doesn't know the real secret
				wrongHMAC := CalculateHMAC(rdt, fp, "wrong-secret")

				// sign the wrong HMAC with correct device key
				proof, err := asserts.RawSignWithKey(wrongHMAC, deviceKey)
				c.Assert(err, check.IsNil)

				return Identity{
					RDT:          rdt,
					FP:           fp,
					SerialBundle: bundle,
					SerialProof:  proof,
				}
			},
			err: ".*serial proof verification failed.*",
		},
		{
			name: "mismatched rdt in proof",
			identity: func() Identity {
				_, bundle, deviceKey := createTestSerialBundle(c, signing)

				rdt := DeviceToken("real-rdt")
				attackerRDT := DeviceToken("attacker-rdt")
				fp := CalculateFP([]byte("victim-cert"))

				// sign HMAC for different RDT than claimed
				hmac := CalculateHMAC(attackerRDT, fp, "secret")
				proof, err := asserts.RawSignWithKey(hmac, deviceKey)
				c.Assert(err, check.IsNil)

				return Identity{
					RDT:          rdt, // claim this RDT
					FP:           fp,
					SerialBundle: bundle,
					SerialProof:  proof, // but proof is for different RDT
				}
			},
			err: ".*serial proof verification failed.*",
		},
		{
			name: "invalid signature data",
			identity: func() Identity {
				_, bundle, _ := createTestSerialBundle(c, signing)

				rdt := DeviceToken("victim-rdt")
				fp := CalculateFP([]byte("victim-cert"))

				return Identity{
					RDT:          rdt,
					FP:           fp,
					SerialBundle: bundle,
					SerialProof:  []byte("garbage-signature-data"),
				}
			},
			err: ".*serial proof verification failed.*",
		},
	}

	for _, tc := range cases {
		id := tc.identity()

		// attempt to record the forged identity
		err := peer.CommitDevices(Devices{
			Devices: []Identity{id},
		})

		// verify the forged identity is rejected
		c.Assert(err, check.NotNil, check.Commentf("forged identity should be rejected: %s", tc.name))
		c.Assert(err, check.ErrorMatches, tc.err, check.Commentf("wrong error for case: %s", tc.name))

		// verify the forged identity was not added to the system
		_, exists := as.devices.Lookup(id.RDT)
		c.Assert(exists, check.Equals, false, check.Commentf("forged identity should not be stored: %s", tc.name))
	}
}

func (s *clusterSuite) TestPublishRoutes(c *check.C) {
	selector := statelessSelector()
	as, cm, _, _, _ := newAssembleStateWithTestKeys(c, selector, AssembleConfig{
		Secret: "secret",
		RDT:    "self",
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

func (s *clusterSuite) TestNewAssembleStateTimeout(c *check.C) {
	db, signing := mockAssertDB(c)
	cfg := createTestAssembleConfig(c, signing, "secret", "self")

	// use a fixed time for testing
	now := time.Now()
	cfg.Clock = func() time.Time {
		return now
	}

	commit := func(AssembleSession) {}

	// test with no initiated time (new session)
	_, err := NewAssembleState(cfg, AssembleSession{}, func(DeviceToken, Identifier) (RouteSelector, error) {
		return statelessSelector(), nil
	}, commit, db)
	c.Assert(err, check.IsNil)

	// test with recent session still within the timeout (30 minutes old)
	recent := AssembleSession{
		Initiated: now.Add(-30 * time.Minute),
	}
	_, err = NewAssembleState(cfg, recent, func(DeviceToken, Identifier) (RouteSelector, error) {
		return statelessSelector(), nil
	}, commit, db)
	c.Assert(err, check.IsNil)

	// test with expired session (2 hours old)
	expired := AssembleSession{
		Initiated: now.Add(-2 * time.Hour),
	}
	_, err = NewAssembleState(cfg, expired, func(DeviceToken, Identifier) (RouteSelector, error) {
		return statelessSelector(), nil
	}, commit, db)
	c.Assert(err, check.ErrorMatches, "invalid session data: cannot resume an assembly session that began more than an hour ago")
}

func (s *clusterSuite) TestNewAssembleStateWithSessionImport(c *check.C) {
	type peer struct {
		rdt  DeviceToken
		cert []byte
		fp   Fingerprint
	}

	certPEM, keyPEM := createTestCertAndKey(c)
	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	c.Assert(err, check.IsNil)

	local := peer{
		rdt:  "local-rdt",
		cert: certPEM,
		fp:   CalculateFP(cert.Certificate[0]),
	}

	var peers []peer
	for _, rdt := range []string{"peer-one-rdt", "peer-two-rdt", "peer-three-rdt"} {
		certPEM, keyPEM := createTestCertAndKey(c)
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

	assertDB, signing := mockAssertDB(c)

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
				createTestIdentity(c, signing, peers[0].rdt, peers[0].fp, "secret"),
				createTestIdentity(c, signing, peers[1].rdt, peers[1].fp, "secret"),
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
		CompleteFunc: func(size int) (bool, error) {
			return false, nil
		},
	}

	serial, bundle, deviceKey := createTestSerialBundle(c, signing)
	proof, err := asserts.RawSignWithKey(CalculateHMAC(local.rdt, local.fp, "secret"), deviceKey)
	c.Assert(err, check.IsNil)

	cfg := AssembleConfig{
		Secret:  "secret",
		RDT:     local.rdt,
		TLSCert: certPEM,
		TLSKey:  keyPEM,
		Clock:   clock,
		Serial:  serial,
		Signer:  privateKeySigner(deviceKey),
	}

	// create AssembleState with imported session
	as, err := NewAssembleState(cfg, session, func(self DeviceToken, identified func(DeviceToken) bool) (RouteSelector, error) {
		c.Assert(self, check.Equals, local.rdt)
		return selector, nil
	}, func(AssembleSession) {}, assertDB)
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
				RDT:          local.rdt,
				FP:           local.fp,
				SerialBundle: bundle,
				SerialProof:  proof,
			}})

			var sawSerial, sawAccountKey bool
			commitBundleAndObserve(c, signing.Trusted, devices.Devices[0].SerialBundle, func(a asserts.Assertion) {
				switch asn := a.(type) {
				case *asserts.Serial:
					sawSerial = true
					c.Assert(asn.DeviceKey().ID(), check.Equals, deviceKey.PublicKey().ID())
				case *asserts.AccountKey:
					sawAccountKey = true
				}
			})
			c.Assert(sawSerial, check.Equals, true)
			c.Assert(sawAccountKey, check.Equals, true)
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

func (s *clusterSuite) TestNewAssembleStateInvalidSessionData(c *check.C) {
	db, signing := mockAssertDB(c)
	cfg := createTestAssembleConfig(c, signing, "secret", "rdt")

	commit := func(AssembleSession) {}

	cert, err := tls.X509KeyPair([]byte(cfg.TLSCert), []byte(cfg.TLSKey))
	c.Assert(err, check.IsNil)
	localFP := CalculateFP(cert.Certificate[0])
	bundle, err := buildSerialBundle(cfg.Serial, db)
	c.Assert(err, check.IsNil)
	proof, err := cfg.Signer(CalculateHMAC(cfg.RDT, localFP, cfg.Secret))
	c.Assert(err, check.IsNil)

	expectedID := Identity{
		RDT:          cfg.RDT,
		FP:           localFP,
		SerialBundle: bundle,
		SerialProof:  proof,
	}

	cases := []struct {
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
				identity := expectedID
				identity.FP = CalculateFP([]byte("wrong-cert"))
				return AssembleSession{
					Devices: DeviceQueryTrackerData{
						IDs: []Identity{identity},
					},
				}
			}(),
			err: "fingerprint mismatch for local device.*",
		},
		{
			name: "local device serial bundle mismatch",
			session: func() AssembleSession {
				identity := expectedID
				identity.SerialBundle = expectedID.SerialBundle + "-different"
				return AssembleSession{
					Devices: DeviceQueryTrackerData{
						IDs: []Identity{identity},
					},
				}
			}(),
			err: "serial bundle mismatch for local device.*",
		},
		{
			name: "local device serial proof mismatch",
			session: func() AssembleSession {
				identity := expectedID
				identity.SerialProof = []byte("wrong-proof")
				return AssembleSession{
					Devices: DeviceQueryTrackerData{
						IDs: []Identity{identity},
					},
				}
			}(),
			err: "serial proof mismatch for local device.*",
		},
	}

	for _, tc := range cases {
		_, err := NewAssembleState(cfg, tc.session, func(DeviceToken, func(DeviceToken) bool) (RouteSelector, error) {
			return statelessSelector(), nil
		}, commit, db)

		c.Assert(err, check.NotNil, check.Commentf("test case %q", tc.name))
		c.Assert(err, check.ErrorMatches, tc.err, check.Commentf("test case %q", tc.name))
	}
}

func (s *clusterSuite) TestNewAssembleStateInvalidSigner(c *check.C) {
	db, signing := mockAssertDB(c)

	serial, _ := createTestSerial(c, signing)

	// create a different key that doesn't match the serial assertion
	wrongKey, _ := assertstest.GenerateKey(752)

	certPEM, keyPEM := createTestCertAndKey(c)
	cfg := AssembleConfig{
		Secret:  "secret",
		RDT:     DeviceToken("local-device"),
		TLSCert: certPEM,
		TLSKey:  keyPEM,
		Serial:  serial,
		Clock:   time.Now,

		// use a signer that signs with the wrong key
		Signer: privateKeySigner(wrongKey),
	}

	commit := func(AssembleSession) {}

	_, err := NewAssembleState(cfg, AssembleSession{}, func(DeviceToken, Identifier) (RouteSelector, error) {
		return statelessSelector(), nil
	}, commit, db)

	c.Assert(err, check.NotNil)
	c.Assert(err, check.ErrorMatches, ".*serial proof verification failed for device local-device.*invalid signature.*")
}

func (s *clusterSuite) TestRunTimeout(c *check.C) {
	db, signing := mockAssertDB(c)
	cfg := createTestAssembleConfig(c, signing, "secret", "rdt")

	started := time.Now()
	called := false
	cfg.Clock = func() time.Time {
		// first call during NewAssembleState: return current time
		if !called {
			called = true
			return started
		}

		// subsequent calls: return time that's past the 1-hour limit
		return started.Add(time.Hour + time.Second)
	}

	commit := func(AssembleSession) {}

	transport := &testTransport{
		ServeFunc: func(ctx context.Context, ln net.Listener, cert tls.Certificate, pv PeerAuthenticator) error {
			<-ctx.Done()
			return ctx.Err()
		},
		NewClientFunc: func(cert tls.Certificate) Client {
			return &testClient{}
		},
	}

	discover := make(chan []string)
	session := AssembleSession{
		Initiated: started,
	}
	as, err := NewAssembleState(cfg, session, func(DeviceToken, Identifier) (RouteSelector, error) {
		return statelessSelector(), nil
	}, commit, db)
	c.Assert(err, check.IsNil)

	// when Run is called, the clock will return a time past the 1-hour limit
	_, err = as.Run(context.Background(), testListener("addr"), transport, discover, RunOptions{})
	c.Assert(err, check.ErrorMatches, "cannot resume an assembly session that began more than an hour ago")
}

func (s *clusterSuite) TestRunServerError(c *check.C) {
	db, signing := mockAssertDB(c)
	cfg := createTestAssembleConfig(c, signing, "secret", "rdt")

	commit := func(AssembleSession) {}

	// create a transport that returns a non-context.Canceled error
	serverError := errors.New("server error")
	transport := &testTransport{
		ServeFunc: func(ctx context.Context, ln net.Listener, cert tls.Certificate, pv PeerAuthenticator) error {
			return serverError
		},
		NewClientFunc: func(cert tls.Certificate) Client {
			return &testClient{}
		},
	}

	discover := make(chan []string)
	as, err := NewAssembleState(cfg, AssembleSession{}, func(DeviceToken, Identifier) (RouteSelector, error) {
		return statelessSelector(), nil
	}, commit, db)
	c.Assert(err, check.IsNil)

	_, err = as.Run(context.Background(), testListener("addr"), transport, discover, RunOptions{})
	c.Assert(err, testutil.ErrorIs, serverError)
}

func (s *clusterSuite) TestMaxSizeCompletionOnStartup(c *check.C) {
	db, signing := mockAssertDB(c)
	cfg := createTestAssembleConfig(c, signing, "secret", "one")
	cfg.ExpectedSize = 2 // expect completion at 2 devices

	// create a mock selector that immediately reports that the graph is fully
	// connected
	selector := statelessSelector()
	selector.CompleteFunc = func(size int) (bool, error) {
		c.Assert(size, check.Equals, cfg.ExpectedSize)
		return true, nil
	}

	transport := &testTransport{
		ServeFunc: func(ctx context.Context, ln net.Listener, cert tls.Certificate, pv PeerAuthenticator) error {
			<-ctx.Done()
			return ctx.Err()
		},
		NewClientFunc: func(cert tls.Certificate) Client {
			return &testClient{}
		},
	}

	discover := make(chan []string)
	as, err := NewAssembleState(cfg, AssembleSession{}, func(DeviceToken, Identifier) (RouteSelector, error) {
		return selector, nil
	}, func(as AssembleSession) {}, db)
	c.Assert(err, check.IsNil)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_, err = as.Run(ctx, testListener("addr"), transport, discover, RunOptions{})
	c.Assert(err, check.IsNil)
}

func (s *clusterSuite) TestMaxSizeCompletionOnCommitDevices(c *check.C) {
	db, signing := mockAssertDB(c)
	cfg := createTestAssembleConfig(c, signing, "secret", "one")
	cfg.ExpectedSize = 2 // expect completion at 2 devices

	// create a mock selector that reports that the graph is fully
	// connected after the first call
	selector := statelessSelector()
	called := 0
	selector.CompleteFunc = func(size int) (bool, error) {
		c.Assert(size, check.Equals, cfg.ExpectedSize)
		called++
		return called > 1, nil
	}

	transport := &testTransport{
		ServeFunc: func(ctx context.Context, ln net.Listener, cert tls.Certificate, pv PeerAuthenticator) error {
			<-ctx.Done()
			return ctx.Err()
		},
		NewClientFunc: func(cert tls.Certificate) Client {
			return &testClient{}
		},
	}

	as, err := NewAssembleState(cfg, AssembleSession{}, func(DeviceToken, Identifier) (RouteSelector, error) {
		return selector, nil
	}, func(AssembleSession) {}, db)
	c.Assert(err, check.IsNil)

	h, _, _ := trustedAndDiscoveredPeer(c, as, "peer")
	h.CommitDevices(Devices{})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	discover := make(chan []string)
	_, err = as.Run(ctx, testListener("addr"), transport, discover, RunOptions{})
	c.Assert(err, check.IsNil)
}

func (s *clusterSuite) TestMaxSizeCompletionOnCommitRoutes(c *check.C) {
	db, signing := mockAssertDB(c)
	cfg := createTestAssembleConfig(c, signing, "secret", "one")
	cfg.ExpectedSize = 2 // expect completion at 2 devices

	// create a mock selector that reports that the graph is fully
	// connected after the first call
	selector := statelessSelector()
	called := 0
	selector.CompleteFunc = func(size int) (bool, error) {
		c.Assert(size, check.Equals, cfg.ExpectedSize)
		called++
		return called > 1, nil
	}

	transport := &testTransport{
		ServeFunc: func(ctx context.Context, ln net.Listener, cert tls.Certificate, pv PeerAuthenticator) error {
			<-ctx.Done()
			return ctx.Err()
		},
		NewClientFunc: func(cert tls.Certificate) Client {
			return &testClient{}
		},
	}

	as, err := NewAssembleState(cfg, AssembleSession{}, func(DeviceToken, Identifier) (RouteSelector, error) {
		return selector, nil
	}, func(AssembleSession) {}, db)
	c.Assert(err, check.IsNil)

	h, _, _ := trustedAndDiscoveredPeer(c, as, "peer")
	h.CommitRoutes(Routes{})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	discover := make(chan []string)
	_, err = as.Run(ctx, testListener("addr"), transport, discover, RunOptions{})
	c.Assert(err, check.IsNil)
}

func encodeCertAsFP(fingerprint []byte) string {
	fp := CalculateFP(fingerprint)
	return base64.StdEncoding.EncodeToString(fp[:])
}
