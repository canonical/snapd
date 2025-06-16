// Copyright (c) Abstract Machines
// SPDX-License-Identifier: Apache-2.0
package tls_test

import (
	ctls "crypto/tls"
	"testing"

	"github.com/snapcore/snapd/telemagent/pkg/tls"
	. "gopkg.in/check.v1"
)

func Test(t *testing.T) { TestingT(t) }

type tlsSuite struct {
	WeirdFile    string
	CertFile     string
	KeyFile      string
	ServerCAFile string
	ClientCAFile string
}

var _ = Suite(&tlsSuite{})

func (cs *tlsSuite) SetUpSuite(c *C) {
	cs.WeirdFile = "weird.txt"
	cs.CertFile = "../../ssl/certs/server.crt"
	cs.KeyFile = "../../ssl/certs/server.key"
	cs.ServerCAFile = "../../ssl/certs/ca.crt"
	cs.ClientCAFile = "../../ssl/certs/ca.crt"
}

func (cs *tlsSuite) TestLoadEmpty(c *C) {
	t, err := tls.Load(&tls.Config{})
	c.Check(err, IsNil)
	c.Check(t, IsNil)
}

func (cs *tlsSuite) TestLoadBadPair(c *C) {
	cfg := tls.Config{CertFile: cs.WeirdFile, KeyFile: cs.WeirdFile}

	_, err := tls.Load(&cfg)
	c.Check(err, NotNil)
}

func (cs *tlsSuite) TestLoadGoodPairBadServerCa(c *C) {
	cfg := tls.Config{CertFile: cs.CertFile, KeyFile: cs.KeyFile, ServerCAFile: cs.WeirdFile}

	_, err := tls.Load(&cfg)

	c.Check(err, NotNil)
}

func (cs *tlsSuite) TestLoadGoodPairGoodServerGoodClient(c *C) {
	cfg := tls.Config{
		CertFile:     cs.CertFile,
		KeyFile:      cs.KeyFile,
		ServerCAFile: cs.ServerCAFile,
		ClientCAFile: cs.ClientCAFile,
	}

	_, err := tls.Load(&cfg)

	c.Check(err, IsNil)
}

func (cs *tlsSuite) TestSecurityStatusNilConfig(c *C) {
	status := tls.SecurityStatus(nil)

	c.Check(status, Equals, "no TLS")
}

func (cs *tlsSuite) TestSecurityStatusEmptyConfig(c *C) {
	status := tls.SecurityStatus(&ctls.Config{})

	c.Check(status, Equals, "no server certificates")
}

func (cs *tlsSuite) TestSecurityStatusWithCertificates(c *C) {
	cfg := ctls.Config{Certificates: []ctls.Certificate{{Certificate: [][]byte{}}}, ClientAuth: 1}

	status := tls.SecurityStatus(&cfg)

	c.Check(status, Equals, "TLS")
}
