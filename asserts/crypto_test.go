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

package asserts_test

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"fmt"
	"time"

	"golang.org/x/crypto/openpgp/packet"
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
)

type cryptoSuite struct{}

var _ = Suite(&cryptoSuite{})

func (s *cryptoSuite) TestSignWithKeyAndVerifyWithKey(c *C) {
	priv, _ := assertstest.GenerateKey(1024)

	data := []byte("some data to verify")
	signature, err := asserts.RawSignWithKey(data, priv)
	c.Assert(err, IsNil)

	pub := priv.PublicKey()
	err = asserts.RawVerifyWithKey(data, signature, pub)
	c.Check(err, IsNil)
}

func (s *cryptoSuite) TestVerifyWithKeyMismatch(c *C) {
	// generate two different key pairs
	privOne, _ := assertstest.GenerateKey(1024)
	privTwo, _ := assertstest.GenerateKey(1024)

	// sign with first key
	data := []byte("some data to verify")
	signature, err := asserts.RawSignWithKey(data, privOne)
	c.Assert(err, IsNil)

	// try to verify with wrong public key (from second key pair)
	pubTwo := privTwo.PublicKey()
	err = asserts.RawVerifyWithKey(data, signature, pubTwo)
	c.Check(err, NotNil)
	c.Check(err, ErrorMatches, ".*RSA verification failure")
}

func (s *cryptoSuite) TestVerifyWithKeyDataMismatch(c *C) {
	priv, _ := assertstest.GenerateKey(1024)
	pub := priv.PublicKey()

	data := []byte("original data")
	signature, err := asserts.RawSignWithKey(data, priv)
	c.Assert(err, IsNil)

	// try to verify with different data
	err = asserts.RawVerifyWithKey([]byte("different data"), signature, pub)
	c.Check(err, NotNil)
	c.Check(err, ErrorMatches, ".*hash tag doesn't match")
}

func (s *cryptoSuite) TestVerifyWithKeyWrongSignature(c *C) {
	priv, _ := assertstest.GenerateKey(1024)
	pub := priv.PublicKey()

	// sign two different pieces of data with the same key
	dataOne := []byte("data one")
	signatureOne, err := asserts.RawSignWithKey(dataOne, priv)
	c.Assert(err, IsNil)

	dataTwo := []byte("data two")
	signatureTwo, err := asserts.RawSignWithKey(dataTwo, priv)
	c.Assert(err, IsNil)

	err = asserts.RawVerifyWithKey(dataOne, signatureOne, pub)
	c.Check(err, IsNil)

	// try to verify dataOne with signatureTwo (wrong signature for this data)
	err = asserts.RawVerifyWithKey(dataOne, signatureTwo, pub)
	c.Check(err, NotNil)
	c.Check(err, ErrorMatches, ".*hash tag doesn't match")
}

func (s *cryptoSuite) TestReadOpenPGPRSAPublicKey(c *C) {
	privKey, err := rsa.GenerateKey(rand.Reader, 1024)
	c.Assert(err, IsNil)

	primary := packet.NewRSAPublicKey(time.Now(), &privKey.PublicKey)
	subkey := packet.NewRSAPublicKey(time.Now(), &privKey.PublicKey)
	subkey.IsSubkey = true

	buf := new(bytes.Buffer)
	err = primary.Serialize(buf)
	c.Assert(err, IsNil)
	err = subkey.Serialize(buf)
	c.Assert(err, IsNil)

	pubKey, fingerprint, err := asserts.ReadOpenPGPRSAPublicKeyInTest(bytes.NewReader(buf.Bytes()))
	c.Assert(err, IsNil)
	c.Check(pubKey.ID(), Equals, asserts.RSAPublicKey(&privKey.PublicKey).ID())
	c.Check(fingerprint, Equals, fmt.Sprintf("%X", primary.Fingerprint))
}
