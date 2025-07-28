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
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"strconv"
	"sync"
	"time"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/cluster/assemblestate"
	"github.com/snapcore/snapd/testutil"
	"gopkg.in/check.v1"
)

type assembleSuite struct{}

var _ = check.Suite(&assembleSuite{})

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

func (s *assembleSuite) TestRun(c *check.C) {
	db, signing := mockAssertDB(c)

	const count = 16
	rdts := make([]assemblestate.DeviceToken, 0, count)
	addrs := make([]string, 0, count)
	listeners := make(map[assemblestate.DeviceToken]net.Listener, count)
	for i := 1; i <= count; i++ {
		rdt := assemblestate.DeviceToken(strconv.Itoa(i))
		rdts = append(rdts, rdt)

		ln, err := net.Listen("tcp", "127.0.0.1:0")
		c.Assert(err, check.IsNil)
		defer ln.Close()

		addrs = append(addrs, ln.Addr().String())
		listeners[rdt] = ln
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var wg sync.WaitGroup
	for _, rdt := range rdts[1:] {
		rdt := rdt

		cert, key := createTestCertAndKey(c)
		serial, pk := createTestSerial(c, signing)
		as, err := assemblestate.NewAssembleState(assemblestate.AssembleConfig{
			Secret:  "secret",
			RDT:     assemblestate.DeviceToken(rdt),
			TLSCert: cert,
			TLSKey:  key,
			Serial:  serial,
			Signer:  privateKeySigner(pk),
		}, assemblestate.AssembleSession{},
			func(self assemblestate.DeviceToken, identified func(assemblestate.DeviceToken) bool) (assemblestate.RouteSelector, error) {
				return assemblestate.NewPrioritySelector(self, nil, identified), nil
			},
			func(as assemblestate.AssembleSession) {},
			db,
		)
		c.Assert(err, check.IsNil)

		disco := make(chan string, len(addrs))
		for _, addr := range addrs {
			disco <- addr
		}

		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := as.Run(
				ctx,
				listeners[rdt],
				assemblestate.NewHTTPSTransport(),
				disco,
				assemblestate.RunOptions{Period: time.Millisecond * 100},
			)
			c.Assert(err, check.IsNil)
		}()
	}

	rdt := rdts[0]
	cert, key := createTestCertAndKey(c)
	serial, pk := createTestSerial(c, signing)
	as, err := assemblestate.NewAssembleState(assemblestate.AssembleConfig{
		Secret:  "secret",
		RDT:     assemblestate.DeviceToken(rdt),
		TLSCert: cert,
		TLSKey:  key,
		Serial:  serial,
		Signer:  privateKeySigner(pk),

		// this session has an expected size, so it will terminate on its own
		ExpectedSize: count,
	}, assemblestate.AssembleSession{},
		func(self assemblestate.DeviceToken, identified func(assemblestate.DeviceToken) bool) (assemblestate.RouteSelector, error) {
			return assemblestate.NewPrioritySelector(self, nil, identified), nil
		},
		func(as assemblestate.AssembleSession) {},
		db,
	)
	c.Assert(err, check.IsNil)

	disco := make(chan string, len(addrs))
	for _, addr := range addrs {
		disco <- addr
	}

	routes, err := as.Run(
		ctx,
		listeners[rdt],
		assemblestate.NewHTTPSTransport(),
		disco,
		assemblestate.RunOptions{Period: time.Millisecond * 100},
	)
	c.Assert(err, check.IsNil)

	// shut everyone else down
	cancel()
	wg.Wait()

	c.Assert(routes.Addresses, testutil.DeepUnsortedMatches, addrs)
	c.Assert(routes.Devices, testutil.DeepUnsortedMatches, rdts)
	c.Assert(len(routes.Routes)/3, check.Equals, count*(count-1))
}

func privateKeySigner(pk asserts.PrivateKey) func([]byte) ([]byte, error) {
	return func(data []byte) ([]byte, error) {
		return asserts.RawSignWithKey(data, pk)
	}
}
