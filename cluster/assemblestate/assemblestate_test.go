package assemblestate_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"time"

	"gopkg.in/check.v1"

	as "github.com/snapcore/snapd/cluster/assemblestate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/testutil"
)

type ClusterSuite struct{}

var _ = check.Suite(&ClusterSuite{})

type selector struct {
	AddAuthoritativeRouteFunc func(r as.RDT, via string)
	AddRoutesFunc             func(r as.RDT, ro as.Routes, id func(as.RDT) bool) (int, int, error)
	VerifyRoutesFunc          func(func(as.RDT) bool)
	SelectFunc                func(count int) (to as.RDT, routes as.Routes, ack func(), ok bool)
	RoutesFunc                func() as.Routes
}

func (s *selector) AddAuthoritativeRoute(r as.RDT, via string) {
	if s.AddAuthoritativeRouteFunc == nil {
		panic("unexpected call")
	}
	s.AddAuthoritativeRouteFunc(r, via)
}

func (s *selector) AddRoutes(r as.RDT, ro as.Routes, id func(as.RDT) bool) (int, int, error) {
	if s.AddRoutesFunc == nil {
		panic("unexpected call")
	}
	return s.AddRoutesFunc(r, ro, id)
}

func (s *selector) VerifyRoutes(fn func(as.RDT) bool) {
	if s.VerifyRoutesFunc == nil {
		panic("unexpected call")
	}
	s.VerifyRoutesFunc(fn)
}

func (s *selector) Select(count int) (as.RDT, as.Routes, func(), bool) {
	if s.SelectFunc == nil {
		panic("unexpected call")
	}
	return s.SelectFunc(count)
}

func (s *selector) Routes() as.Routes {
	if s.RoutesFunc == nil {
		panic("unexpected call")
	}
	return s.RoutesFunc()
}

type messenger struct {
	TrustedFunc   func(ctx context.Context, rdt as.RDT, addr string, cert []byte, kind string, message any) error
	UntrustedFunc func(ctx context.Context, addr string, kind string, message any) (cert []byte, err error)
}

func (m *messenger) Trusted(ctx context.Context, rdt as.RDT, addr string, cert []byte, kind string, msg any) error {
	if m.TrustedFunc == nil {
		panic("unexpected call")
	}
	return m.TrustedFunc(ctx, rdt, addr, cert, kind, msg)
}

func (m *messenger) Untrusted(ctx context.Context, addr, kind string, msg any) ([]byte, error) {
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

func clusterStateWithTestKeys(c *check.C, st *state.State, sel *selector, cfg as.ClusterConfig) *as.ClusterState {
	certPEM, keyPEM, err := createTestCertAndKey(cfg.IP)
	c.Assert(err, check.IsNil)

	cfg.TLSCert = certPEM
	cfg.TLSKey = keyPEM

	st.Lock()
	st.Set("cluster-config", cfg)
	st.Unlock()

	cs, err := as.NewClusterState(st, func(as.RDT) (as.RouteSelector, error) {
		return sel, nil
	})
	c.Assert(err, check.IsNil)

	return cs
}

func statelessSelector() *selector {
	return &selector{
		AddAuthoritativeRouteFunc: func(r as.RDT, via string) {},
		AddRoutesFunc: func(r as.RDT, ro as.Routes, id func(as.RDT) bool) (int, int, error) {
			return 0, 0, nil
		},
		VerifyRoutesFunc: func(f func(as.RDT) bool) {},
		SelectFunc:       func(_ int) (as.RDT, as.Routes, func(), bool) { return "", as.Routes{}, nil, false },
		RoutesFunc:       func() as.Routes { return as.Routes{} },
	}
}

func (s *ClusterSuite) TestAddress(c *check.C) {
	cs := clusterStateWithTestKeys(c, state.New(nil), statelessSelector(), as.ClusterConfig{
		Secret: "secret",
		RDT:    "rdt",
		IP:     net.IPv4(127, 0, 0, 1),
		Port:   8001,
	})

	c.Assert(cs.Address(), check.Equals, "127.0.0.1:8001")
}

func (s *ClusterSuite) TestCert(c *check.C) {
	cs := clusterStateWithTestKeys(c, state.New(nil), statelessSelector(), as.ClusterConfig{
		Secret: "secret",
		RDT:    "rdt",
		IP:     net.IPv4(127, 0, 0, 1),
		Port:   8001,
	})

	_, err := x509.ParseCertificate(cs.Cert().Certificate[0])
	c.Assert(err, check.IsNil)
}

func (s *ClusterSuite) TestPublishAuth(c *check.C) {
	cs := clusterStateWithTestKeys(c, state.New(nil), statelessSelector(), as.ClusterConfig{
		Secret: "secret",
		RDT:    "rdt",
		IP:     net.IPv4(127, 0, 0, 1),
		Port:   8001,
	})

	var called int
	msg := messenger{
		UntrustedFunc: func(ctx context.Context, addr, kind string, message any) (cert []byte, err error) {
			called++

			c.Assert(addr, check.Equals, "127.0.0.1:8002")
			c.Assert(kind, check.Equals, "auth")

			auth := message.(as.Auth)

			expectedHMAC := as.CalculateHMAC("rdt", as.CalculateFP(cs.Cert().Certificate[0]), "secret")
			c.Assert(auth.HMAC, check.DeepEquals, expectedHMAC)
			c.Assert(auth.RDT, check.Equals, as.RDT("rdt"))

			return []byte("peer-certificate"), nil
		},
	}

	err := cs.PublishAuth(context.Background(), []string{"127.0.0.1:8002"}, &msg)
	c.Assert(err, check.IsNil)

	c.Assert(called, check.Equals, 1)

	// the second time around we shouldn't publish anything, since we already
	// have delivered an auth message to this peer
	called = 0
	err = cs.PublishAuth(context.Background(), []string{"127.0.0.1:8002"}, &msg)
	c.Assert(err, check.IsNil)
	c.Assert(called, check.Equals, 0)
}

func (s *ClusterSuite) TestAuthenticate(c *check.C) {
	cs := clusterStateWithTestKeys(c, state.New(nil), statelessSelector(), as.ClusterConfig{
		Secret: "secret",
		RDT:    "rdt",
		IP:     net.IPv4(127, 0, 0, 1),
		Port:   8001,
	})

	peerCert := []byte("peer-certificate")
	peerFP := as.CalculateFP(peerCert)
	peerRDT := as.RDT("peer-rdt")

	// wrong RDT in HMAC
	auth := as.Auth{
		HMAC: as.CalculateHMAC("wrong-rdt", peerFP, "secret"),
		RDT:  peerRDT,
	}
	err := cs.Authenticate(auth, peerCert)
	c.Assert(err, check.ErrorMatches, "received invalid HMAC from peer")

	// wrong RDT in message
	auth = as.Auth{
		HMAC: as.CalculateHMAC(peerRDT, peerFP, "secret"),
		RDT:  "wrong-rdt",
	}
	err = cs.Authenticate(auth, peerCert)
	c.Assert(err, check.ErrorMatches, "received invalid HMAC from peer")

	// wrong FP in HMAC
	auth = as.Auth{
		HMAC: as.CalculateHMAC(peerRDT, as.CalculateFP([]byte("wrong-cert")), "secret"),
		RDT:  peerRDT,
	}
	err = cs.Authenticate(auth, peerCert)
	c.Assert(err, check.ErrorMatches, "received invalid HMAC from peer")

	// wrong cert from transport layer
	auth = as.Auth{
		HMAC: as.CalculateHMAC(peerRDT, peerFP, "secret"),
		RDT:  peerRDT,
	}
	err = cs.Authenticate(auth, []byte("wrong-cert"))
	c.Assert(err, check.ErrorMatches, "received invalid HMAC from peer")

	// wrong secret
	auth = as.Auth{
		HMAC: as.CalculateHMAC(peerRDT, peerFP, "wrong-secret"),
		RDT:  peerRDT,
	}
	err = cs.Authenticate(auth, peerCert)
	c.Assert(err, check.ErrorMatches, "received invalid HMAC from peer")

	// valid case
	auth = as.Auth{
		HMAC: as.CalculateHMAC(peerRDT, peerFP, "secret"),
		RDT:  peerRDT,
	}
	err = cs.Authenticate(auth, peerCert)
	c.Assert(err, check.IsNil)

}

func (s *ClusterSuite) TestTrusted(c *check.C) {
	cs := clusterStateWithTestKeys(c, state.New(nil), statelessSelector(), as.ClusterConfig{
		Secret: "secret",
		RDT:    "rdt",
		IP:     net.IPv4(127, 0, 0, 1),
		Port:   8001,
	})

	peerCert := []byte("peer-certificate")
	peerFP := as.CalculateFP(peerCert)
	peerRDT := as.RDT("peer-rdt")

	err := cs.Authenticate(as.Auth{
		HMAC: as.CalculateHMAC(peerRDT, peerFP, "secret"),
		RDT:  peerRDT,
	}, peerCert)
	c.Assert(err, check.IsNil)

	handle, err := cs.Trusted(peerCert)
	c.Assert(err, check.IsNil)

	c.Assert(handle.RDT(), check.Equals, peerRDT)
}

func trustedAndDiscoveredPeer(c *check.C, cs *as.ClusterState, rdt as.RDT) (h *as.PeerHandle, address string, cert []byte) {
	peerCert := []byte(fmt.Sprintf("%s-certificate", rdt))
	peerFP := as.CalculateFP(peerCert)

	err := cs.Authenticate(as.Auth{
		HMAC: as.CalculateHMAC(rdt, peerFP, "secret"),
		RDT:  rdt,
	}, peerCert)
	c.Assert(err, check.IsNil)

	handle, err := cs.Trusted(peerCert)
	c.Assert(err, check.IsNil)

	c.Assert(handle.RDT(), check.Equals, rdt)

	peerAddr := fmt.Sprintf("%s-addr", rdt)
	msg := messenger{
		UntrustedFunc: func(ctx context.Context, addr, kind string, message any) (cert []byte, err error) {
			c.Assert(addr, check.Equals, peerAddr)
			c.Assert(kind, check.Equals, "auth")
			return peerCert, nil
		},
	}

	err = cs.PublishAuth(context.Background(), []string{peerAddr}, &msg)
	c.Assert(err, check.IsNil)

	return handle, peerAddr, peerCert
}

func trustedPeer(c *check.C, cs *as.ClusterState, rdt as.RDT) (h *as.PeerHandle, cert []byte) {
	peerCert := []byte(fmt.Sprintf("%s-certificate", rdt))
	peerFP := as.CalculateFP(peerCert)

	err := cs.Authenticate(as.Auth{
		HMAC: as.CalculateHMAC(rdt, peerFP, "secret"),
		RDT:  rdt,
	}, peerCert)
	c.Assert(err, check.IsNil)

	handle, err := cs.Trusted(peerCert)
	c.Assert(err, check.IsNil)

	c.Assert(handle.RDT(), check.Equals, rdt)

	return handle, peerCert
}

func (s *ClusterSuite) TestPublishDeviceQueries(c *check.C) {
	cs := clusterStateWithTestKeys(c, state.New(nil), statelessSelector(), as.ClusterConfig{
		Secret: "secret",
		RDT:    "rdt",
		IP:     net.IPv4(127, 0, 0, 1),
		Port:   8001,
	})

	peerRDT := as.RDT("peer")
	peer, peerAddr, peerCert := trustedAndDiscoveredPeer(c, cs, peerRDT)

	// this tells us that this peer has knowledge of one and two.
	_, _, err := peer.AddRoutes(as.Routes{
		Devices: []as.RDT{"one", "two"},
	})
	c.Assert(err, check.IsNil)

	msg := messenger{
		TrustedFunc: func(ctx context.Context, rdt as.RDT, addr string, cert []byte, kind string, message any) error {
			c.Assert(rdt, check.Equals, peerRDT)
			c.Assert(addr, check.Equals, peerAddr)
			c.Assert(cert, check.DeepEquals, peerCert)
			c.Assert(kind, check.Equals, "unknown")

			unknown := message.(as.UnknownDevices)
			c.Assert(unknown.Devices, testutil.DeepUnsortedMatches, []as.RDT{"one", "two"})
			return nil
		},
	}
	cs.PublishDeviceQueries(context.Background(), &msg)

	// act as if the peer responded for only one of the devices
	err = peer.AddDevices(as.Devices{
		Devices: []as.Identity{{
			RDT: "one",
		}},
	})
	c.Assert(err, check.IsNil)

	// now, we should expect to see a query for just "two"
	msg.TrustedFunc = func(ctx context.Context, rdt as.RDT, addr string, cert []byte, kind string, message any) error {
		c.Assert(rdt, check.Equals, peerRDT)
		c.Assert(addr, check.Equals, peerAddr)
		c.Assert(cert, check.DeepEquals, peerCert)
		c.Assert(kind, check.Equals, "unknown")

		unknown := message.(as.UnknownDevices)
		c.Assert(unknown.Devices, testutil.DeepUnsortedMatches, []as.RDT{"two"})
		return nil
	}
	cs.PublishDeviceQueries(context.Background(), &msg)
}

func (s *ClusterSuite) TestPublishDevices(c *check.C) {
	cs := clusterStateWithTestKeys(c, state.New(nil), statelessSelector(), as.ClusterConfig{
		Secret: "secret",
		RDT:    "rdt",
		IP:     net.IPv4(127, 0, 0, 1),
		Port:   8001,
	})

	oneRDT := as.RDT("one")
	one, _, _ := trustedAndDiscoveredPeer(c, cs, oneRDT)

	// inform us of devices one and two
	err := one.AddDevices(as.Devices{
		Devices: []as.Identity{
			{
				RDT: "one",
			},
			{
				RDT: "two",
			},
		},
	})
	c.Assert(err, check.IsNil)

	threeRDT := as.RDT("three")
	three, threeAddr, threeCert := trustedAndDiscoveredPeer(c, cs, threeRDT)

	// nothing should be published, since we don't have anything that someone
	// has asked for
	cs.PublishDevices(context.Background(), &messenger{})

	// three asks us for information on two
	three.AddQueries(as.UnknownDevices{
		Devices: []as.RDT{"two"},
	})

	var called int
	msg := messenger{
		TrustedFunc: func(ctx context.Context, rdt as.RDT, addr string, cert []byte, kind string, message any) error {
			called++
			c.Assert(rdt, check.Equals, threeRDT)
			c.Assert(addr, check.Equals, threeAddr)
			c.Assert(cert, check.DeepEquals, threeCert)
			c.Assert(kind, check.Equals, "devices")

			devices := message.(as.Devices)
			c.Assert(devices.Devices, testutil.DeepUnsortedMatches, []as.Identity{{
				RDT: "two",
			}})
			return nil
		},
	}
	cs.PublishDevices(context.Background(), &msg)
	c.Assert(called, check.Equals, 1)

	// since we successfully published the response to the query, we don't send
	// anything
	cs.PublishDevices(context.Background(), &messenger{})
}

func (s *ClusterSuite) TestPublishRoutes(c *check.C) {
	selector := statelessSelector()
	cs := clusterStateWithTestKeys(c, state.New(nil), selector, as.ClusterConfig{
		Secret: "secret",
		RDT:    "rdt",
		IP:     net.IPv4(127, 0, 0, 1),
		Port:   8001,
	})

	oneRDT := as.RDT("one")
	_, oneAddr, oneCert := trustedAndDiscoveredPeer(c, cs, oneRDT)

	twoRDT := as.RDT("two")
	_, twoAddr, twoCert := trustedAndDiscoveredPeer(c, cs, twoRDT)

	threeRDT := as.RDT("three")
	trustedPeer(c, cs, threeRDT)

	var msg messenger
	var called int
	peers := []as.RDT{oneRDT, twoRDT, threeRDT, "four", oneRDT} // 5 calls to publish as expected
	acked := make(map[as.RDT]int)

	selector.SelectFunc = func(count int) (as.RDT, as.Routes, func(), bool) {
		peer := peers[called]
		called++
		return peer, as.Routes{}, func() {
			acked[peer]++
		}, true
	}

	msg.TrustedFunc = func(ctx context.Context, rdt as.RDT, addr string, cert []byte, kind string, message any) error {
		switch rdt {
		case oneRDT:
			c.Assert(addr, check.Equals, oneAddr)
			c.Assert(cert, check.DeepEquals, oneCert)
		case twoRDT:
			c.Assert(addr, check.Equals, twoAddr)
			c.Assert(cert, check.DeepEquals, twoCert)
		}
		c.Assert(kind, check.Equals, "routes")
		_ = message.(as.Routes)
		return nil
	}

	cs.PublishRoutes(context.Background(), &msg)
	c.Assert(called, check.Equals, 5)

	// since peer four isn't known and peer three isn't discovered, we should
	// have only acked our publications to peer one and two
	c.Assert(acked, check.DeepEquals, map[as.RDT]int{
		oneRDT: 2,
		twoRDT: 1,
	})
}
