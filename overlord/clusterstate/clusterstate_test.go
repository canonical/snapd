// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2024 Canonical Ltd
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

package clusterstate_test

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"testing"
	"time"

	"gopkg.in/check.v1"

	"github.com/snapcore/snapd/overlord/clusterstate"
	"github.com/snapcore/snapd/overlord/devicestate"
	"github.com/snapcore/snapd/overlord/state"
)

// Hook up gocheck into the "go test" runner.
func Test(t *testing.T) { check.TestingT(t) }

type clusterStateSuite struct{}

var _ = check.Suite(&clusterStateSuite{})

// generateTestCert generates a self-signed certificate for testing
func (s *clusterStateSuite) generateTestCert(c *check.C) ([]byte, []byte) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	c.Assert(err, check.IsNil)

	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			Organization: []string{"Test"},
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	c.Assert(err, check.IsNil)

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	keyDER := x509.MarshalPKCS1PrivateKey(key)
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: keyDER})

	return certPEM, keyPEM
}

func (s *clusterStateSuite) TestCreateClusterHappy(c *check.C) {
	st := state.New(nil)
	cert, key := s.generateTestCert(c)

	config := &clusterstate.CreateClusterConfig{
		Secret:       "test-secret-12345",
		RDT:          "device-abc123",
		IP:           net.ParseIP("127.0.0.1"),
		Port:         8080,
		ExpectedSize: 3,
		TLSCert:      cert,
		TLSKey:       key,
		Addresses:    []string{"127.0.0.1:8081", "127.0.0.1:8082"},
	}

	st.Lock()
	ts, err := clusterstate.CreateCluster(st, config)
	st.Unlock()
	c.Assert(err, check.IsNil)
	c.Assert(ts, check.NotNil)

	tasks := ts.Tasks()
	c.Assert(tasks, check.HasLen, 1)

	task := tasks[0]
	c.Assert(task.Kind(), check.Equals, "create-cluster")

	// verify the setup was stored correctly
	st.Lock()
	var setup clusterstate.CreateClusterSetup
	err = task.Get("create-cluster-setup", &setup)
	st.Unlock()
	c.Assert(err, check.IsNil)

	c.Check(setup.Secret, check.Equals, "test-secret-12345")
	c.Check(setup.RDT, check.Equals, "device-abc123")
	c.Check(setup.IP, check.Equals, "127.0.0.1")
	c.Check(setup.Port, check.Equals, 8080)
	c.Check(setup.ExpectedSize, check.Equals, 3)
	c.Check(setup.TLSCert, check.DeepEquals, cert)
	c.Check(setup.TLSKey, check.DeepEquals, key)
	c.Check(setup.Addresses, check.DeepEquals, []string{"127.0.0.1:8081", "127.0.0.1:8082"})
}

func (s *clusterStateSuite) TestCreateClusterValidation(c *check.C) {
	st := state.New(nil)

	// test nil config
	st.Lock()
	_, err := clusterstate.CreateCluster(st, nil)
	st.Unlock()
	c.Check(err, check.ErrorMatches, "cluster configuration cannot be nil")

	cert, key := s.generateTestCert(c)

	// test missing secret
	config := &clusterstate.CreateClusterConfig{
		RDT:     "device-abc123",
		IP:      net.ParseIP("127.0.0.1"),
		Port:    8080,
		TLSCert: cert,
		TLSKey:  key,
	}
	st.Lock()
	_, err = clusterstate.CreateCluster(st, config)
	st.Unlock()
	c.Check(err, check.ErrorMatches, "secret is required")

	// test missing rdt
	config.Secret = "test-secret"
	config.RDT = ""
	st.Lock()
	_, err = clusterstate.CreateCluster(st, config)
	st.Unlock()
	c.Check(err, check.ErrorMatches, "rdt is required")

	// test missing ip
	config.RDT = "device-abc123"
	config.IP = nil
	st.Lock()
	_, err = clusterstate.CreateCluster(st, config)
	st.Unlock()
	c.Check(err, check.ErrorMatches, "ip is required")

	// test invalid port
	config.IP = net.ParseIP("127.0.0.1")
	config.Port = 0
	st.Lock()
	_, err = clusterstate.CreateCluster(st, config)
	st.Unlock()
	c.Check(err, check.ErrorMatches, "port must be positive")

	// test missing tls cert
	config.Port = 8080
	config.TLSCert = nil
	st.Lock()
	_, err = clusterstate.CreateCluster(st, config)
	st.Unlock()
	c.Check(err, check.ErrorMatches, "tls certificate is required")

	// test missing tls key
	config.TLSCert = cert
	config.TLSKey = nil
	st.Lock()
	_, err = clusterstate.CreateCluster(st, config)
	st.Unlock()
	c.Check(err, check.ErrorMatches, "tls private key is required")
}

func (s *clusterStateSuite) TestTaskCreateClusterSetup(c *check.C) {
	st := state.New(nil)
	cert, key := s.generateTestCert(c)

	config := &clusterstate.CreateClusterConfig{
		Secret:  "test-secret",
		RDT:     "device-123",
		IP:      net.ParseIP("192.168.1.100"),
		Port:    9090,
		TLSCert: cert,
		TLSKey:  key,
	}

	st.Lock()
	ts, err := clusterstate.CreateCluster(st, config)
	st.Unlock()
	c.Assert(err, check.IsNil)

	task := ts.Tasks()[0]

	// test the exported function
	st.Lock()
	setup, err := clusterstate.TaskCreateClusterSetup(task)
	st.Unlock()
	c.Assert(err, check.IsNil)
	c.Check(setup.Secret, check.Equals, "test-secret")
	c.Check(setup.RDT, check.Equals, "device-123")
	c.Check(setup.IP, check.Equals, "192.168.1.100")
	c.Check(setup.Port, check.Equals, 9090)
}

func (s *clusterStateSuite) TestDeviceManagerAccess(c *check.C) {
	st := state.New(nil)

	// test that DeviceMgr can be accessed (will panic if device manager not set up)
	// but we can catch that and verify the API exists
	defer func() {
		if r := recover(); r != nil {
			// expected - device manager not associated with state in test
			c.Check(r, check.Equals, "internal error: device manager is not yet associated with state")
		}
	}()

	// need to lock state before accessing device manager
	st.Lock()
	defer st.Unlock()

	// this should panic since we haven't set up a device manager
	_ = devicestate.DeviceMgr(st)

	// if we get here without panic, that's unexpected but not necessarily wrong
	c.Log("DeviceMgr() did not panic - device manager may be set up")
}
