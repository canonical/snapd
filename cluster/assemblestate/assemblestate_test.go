package assemblestate

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"time"

	"gopkg.in/check.v1"

	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/testutil"
)

type ClusterSuite struct{}

var _ = check.Suite(&ClusterSuite{})

type selector struct {
	AddAuthoritativeRouteFunc func(r DeviceToken, via string)
	AddRoutesFunc             func(r DeviceToken, ro Routes, id func(DeviceToken) bool) (int, int, error)
	VerifyRoutesFunc          func(func(DeviceToken) bool)
	SelectFunc                func(to DeviceToken, count int) (routes Routes, ack func(), ok bool)
	RoutesFunc                func() Routes
}

func (s *selector) AddAuthoritativeRoute(r DeviceToken, via string) {
	if s.AddAuthoritativeRouteFunc == nil {
		panic("unexpected call")
	}
	s.AddAuthoritativeRouteFunc(r, via)
}

func (s *selector) AddRoutes(r DeviceToken, ro Routes, id func(DeviceToken) bool) (int, int, error) {
	if s.AddRoutesFunc == nil {
		panic("unexpected call")
	}
	return s.AddRoutesFunc(r, ro, id)
}

func (s *selector) VerifyRoutes(fn func(DeviceToken) bool) {
	if s.VerifyRoutesFunc == nil {
		panic("unexpected call")
	}
	s.VerifyRoutesFunc(fn)
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

func createTestCertAndKey(ip net.IP) (certPEM []byte, keyPEM []byte, err error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, nil, err
	}

	serial, err := rand.Int(rand.Reader, big.NewInt(1<<62))
	if err != nil {
		return nil, nil, err
	}

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
	if err != nil {
		return nil, nil, err
	}

	der, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		return nil, nil, err
	}

	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert})
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})

	return certPEM, keyPEM, nil
}

func assembleStateWithTestKeys(c *check.C, st *state.State, sel *selector, cfg AssembleConfig) (*AssembleState, tls.Certificate) {
	certPEM, keyPEM, err := createTestCertAndKey(cfg.IP)
	c.Assert(err, check.IsNil)

	cfg.TLSCert = certPEM
	cfg.TLSKey = keyPEM

	st.Lock()
	st.Set("assemble-config", cfg)
	st.Unlock()

	commit := func(AssembleSession) {}
	as, err := NewAssembleState(cfg, AssembleSession{}, func(DeviceToken) (RouteSelector, error) {
		return sel, nil
	}, nil, commit)
	c.Assert(err, check.IsNil)

	cert, err := tls.X509KeyPair([]byte(cfg.TLSCert), []byte(cfg.TLSKey))
	c.Assert(err, check.IsNil)

	return as, cert
}

func statelessSelector() *selector {
	return &selector{
		AddAuthoritativeRouteFunc: func(r DeviceToken, via string) {},
		AddRoutesFunc: func(r DeviceToken, ro Routes, id func(DeviceToken) bool) (int, int, error) {
			return 0, 0, nil
		},
		VerifyRoutesFunc: func(f func(DeviceToken) bool) {},
		SelectFunc: func(to DeviceToken, count int) (Routes, func(), bool) {
			return Routes{}, nil, false
		},
		RoutesFunc: func() Routes { return Routes{} },
	}
}

func (s *ClusterSuite) TestPublishAuth(c *check.C) {
	as, tlsCert := assembleStateWithTestKeys(c, state.New(nil), statelessSelector(), AssembleConfig{
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

	err := as.publishAuth(context.Background(), []string{"127.0.0.1:8002"}, &client)
	c.Assert(err, check.IsNil)

	c.Assert(called, check.Equals, 1)

	// the second time around we shouldn't publish anything, since we already
	// have delivered an auth message to this peer
	called = 0
	err = as.publishAuth(context.Background(), []string{"127.0.0.1:8002"}, &client)
	c.Assert(err, check.IsNil)
	c.Assert(called, check.Equals, 0)
}

func (s *ClusterSuite) TestAuthenticate(c *check.C) {
	as, _ := assembleStateWithTestKeys(c, state.New(nil), statelessSelector(), AssembleConfig{
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
	err := as.Authenticate(auth, peerCert)
	c.Assert(err, check.ErrorMatches, "received invalid HMAC from peer")

	// wrong RDT in message
	auth = Auth{
		HMAC: CalculateHMAC(peerRDT, peerFP, "secret"),
		RDT:  "wrong-rdt",
	}
	err = as.Authenticate(auth, peerCert)
	c.Assert(err, check.ErrorMatches, "received invalid HMAC from peer")

	// wrong FP in HMAC
	auth = Auth{
		HMAC: CalculateHMAC(peerRDT, CalculateFP([]byte("wrong-cert")), "secret"),
		RDT:  peerRDT,
	}
	err = as.Authenticate(auth, peerCert)
	c.Assert(err, check.ErrorMatches, "received invalid HMAC from peer")

	// wrong cert from transport layer
	auth = Auth{
		HMAC: CalculateHMAC(peerRDT, peerFP, "secret"),
		RDT:  peerRDT,
	}
	err = as.Authenticate(auth, []byte("wrong-cert"))
	c.Assert(err, check.ErrorMatches, "received invalid HMAC from peer")

	// wrong secret
	auth = Auth{
		HMAC: CalculateHMAC(peerRDT, peerFP, "wrong-secret"),
		RDT:  peerRDT,
	}
	err = as.Authenticate(auth, peerCert)
	c.Assert(err, check.ErrorMatches, "received invalid HMAC from peer")

	// valid case
	auth = Auth{
		HMAC: CalculateHMAC(peerRDT, peerFP, "secret"),
		RDT:  peerRDT,
	}
	err = as.Authenticate(auth, peerCert)
	c.Assert(err, check.IsNil)

}

func (s *ClusterSuite) TestTrusted(c *check.C) {
	as, _ := assembleStateWithTestKeys(c, state.New(nil), statelessSelector(), AssembleConfig{
		Secret: "secret",
		RDT:    "rdt",
		IP:     net.IPv4(127, 0, 0, 1),
		Port:   8001,
	})

	peerCert := []byte("peer-certificate")
	peerFP := CalculateFP(peerCert)
	peerRDT := DeviceToken("peer-rdt")

	err := as.Authenticate(Auth{
		HMAC: CalculateHMAC(peerRDT, peerFP, "secret"),
		RDT:  peerRDT,
	}, peerCert)
	c.Assert(err, check.IsNil)

	handle, err := as.VerifyPeer(peerCert)
	c.Assert(err, check.IsNil)

	c.Assert(handle.RDT(), check.Equals, peerRDT)
}

func trustedAndDiscoveredPeer(c *check.C, as *AssembleState, rdt DeviceToken) (h *PeerHandle, address string, cert []byte) {
	peerCert := []byte(fmt.Sprintf("%s-certificate", rdt))
	peerFP := CalculateFP(peerCert)

	err := as.Authenticate(Auth{
		HMAC: CalculateHMAC(rdt, peerFP, "secret"),
		RDT:  rdt,
	}, peerCert)
	c.Assert(err, check.IsNil)

	handle, err := as.VerifyPeer(peerCert)
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

	err = as.publishAuth(context.Background(), []string{peerAddr}, &client)
	c.Assert(err, check.IsNil)

	return handle, peerAddr, peerCert
}

func trustedPeer(c *check.C, as *AssembleState, rdt DeviceToken) (h *PeerHandle, cert []byte) {
	peerCert := []byte(fmt.Sprintf("%s-certificate", rdt))
	peerFP := CalculateFP(peerCert)

	err := as.Authenticate(Auth{
		HMAC: CalculateHMAC(rdt, peerFP, "secret"),
		RDT:  rdt,
	}, peerCert)
	c.Assert(err, check.IsNil)

	handle, err := as.VerifyPeer(peerCert)
	c.Assert(err, check.IsNil)

	c.Assert(handle.RDT(), check.Equals, rdt)

	return handle, peerCert
}

func (s *ClusterSuite) TestPublishDeviceQueries(c *check.C) {
	as, _ := assembleStateWithTestKeys(c, state.New(nil), statelessSelector(), AssembleConfig{
		Secret: "secret",
		RDT:    "rdt",
		IP:     net.IPv4(127, 0, 0, 1),
		Port:   8001,
	})

	peerRDT := DeviceToken("peer")
	peer, peerAddr, peerCert := trustedAndDiscoveredPeer(c, as, peerRDT)

	// this tells us that this peer has knowledge of one and two.
	err := peer.AddRoutes(Routes{
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
	as.publishDeviceQueries(context.Background(), &client)

	// act as if the peer responded for only one of the devices
	err = peer.AddDevices(Devices{
		Devices: []Identity{{
			RDT: "one",
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
	as.publishDeviceQueries(context.Background(), &client)
}

func (s *ClusterSuite) TestPublishDevices(c *check.C) {
	as, _ := assembleStateWithTestKeys(c, state.New(nil), statelessSelector(), AssembleConfig{
		Secret: "secret",
		RDT:    "rdt",
		IP:     net.IPv4(127, 0, 0, 1),
		Port:   8001,
	})

	oneRDT := DeviceToken("one")
	one, _, _ := trustedAndDiscoveredPeer(c, as, oneRDT)

	// inform us of devices one and two
	err := one.AddDevices(Devices{
		Devices: []Identity{
			{
				RDT: "one",
			},
			{
				RDT: "two",
			},
		},
	})
	c.Assert(err, check.IsNil)

	threeRDT := DeviceToken("three")
	three, threeAddr, threeCert := trustedAndDiscoveredPeer(c, as, threeRDT)

	// nothing should be published, since we don't have anything that someone
	// has asked for
	as.publishDevices(context.Background(), &testClient{})

	// three asks us for information on two
	three.AddQueries(UnknownDevices{
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
			}})
			return nil
		},
	}
	as.publishDevices(context.Background(), &client)
	c.Assert(called, check.Equals, 1)

	// since we successfully published the response to the query, we don't send
	// anything
	as.publishDevices(context.Background(), &testClient{})
}

func (s *ClusterSuite) TestPublishRoutes(c *check.C) {
	selector := statelessSelector()
	as, _ := assembleStateWithTestKeys(c, state.New(nil), selector, AssembleConfig{
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
}
