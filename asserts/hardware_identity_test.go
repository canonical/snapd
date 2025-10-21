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
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/snapcore/snapd/asserts"
	"golang.org/x/crypto/sha3"
	. "gopkg.in/check.v1"
)

var _ = Suite(&hardwareIdentitySuite{})

type hardwareIdentitySuite struct {
	hardwareIDKey        crypto.PublicKey
	encodedHardwareIDKey string
	hashedHardwareIDKey  string
}

func (s *hardwareIdentitySuite) SetUpSuite(c *C) {
	curve := elliptic.P256()

	privateKey, err := ecdsa.GenerateKey(curve, rand.Reader)
	c.Assert(err, IsNil)

	publicKey := &privateKey.PublicKey

	s.hardwareIDKey = publicKey
	pubBytes, err := x509.MarshalPKIXPublicKey(&privateKey.PublicKey)
	c.Assert(err, IsNil)

	base64Body := base64.StdEncoding.EncodeToString(pubBytes)
	s.encodedHardwareIDKey = base64Body

	hash := sha3.New384()

	hash.Write([]byte(base64Body))
	hashed := hash.Sum(nil)

	s.hashedHardwareIDKey, err = asserts.EncodeDigest(crypto.SHA3_384, hashed)
	c.Assert(err, IsNil)
}

const (
	errPrefix               = "assertion hardware-identity: "
	hardwareIdentityExample = `type: hardware-identity
authority-id: account-id-1
issuer-id: account-id-1
manufacturer: some-manufacturer
hardware-name: raspberry-pi-4gb
sign-key-sha3-384: t9yuKGLyiezBq_PXMJZsGdkTukmL7MgrgqXAlxxiZF4TYryOjZcy48nnjDmEHQDp
hardware-id: random-id-1
hardware-id-key: HARDWAREIDKEY
hardware-id-key-sha3-384: HARDWAREIDKEYSHA3384

AXNpZw==`
)

func (s *hardwareIdentitySuite) TestDecodeOK(c *C) {
	encoded := strings.Replace(hardwareIdentityExample, "HARDWAREIDKEY", s.encodedHardwareIDKey, 1)
	encoded = strings.ReplaceAll(encoded, "HARDWAREIDKEYSHA3384", s.hashedHardwareIDKey)

	a, err := asserts.Decode([]byte(encoded))
	c.Assert(err, IsNil)
	c.Check(a, NotNil)
	c.Check(a.Type(), Equals, asserts.HardwareIdentityType)

	req := a.(*asserts.HardwareIdentity)
	c.Check(req.IssuerID(), Equals, "account-id-1")
	c.Check(req.Manufacturer(), Equals, "some-manufacturer")
	c.Check(req.HardwareName(), Equals, "raspberry-pi-4gb")
	c.Check(req.HardwareID(), Equals, "random-id-1")
	c.Check(req.HardwareIDKey(), DeepEquals, s.hardwareIDKey)
	c.Check(req.HardwareIDKeySha3384(), Equals, s.hashedHardwareIDKey)
	c.Check(string(req.Body()), Equals, "")
}

func (s *hardwareIdentitySuite) TestDecodeInvalid(c *C) {
	encoded := strings.Replace(hardwareIdentityExample, "HARDWAREIDKEY", s.encodedHardwareIDKey, 1)
	encoded = strings.ReplaceAll(encoded, "HARDWAREIDKEYSHA3384", s.hashedHardwareIDKey)

	hardwareIDKey := fmt.Sprintf("hardware-id-key: %s\n", s.encodedHardwareIDKey)
	// create hardware key with algorithm not supported by go crypto library
	elGamalhardwareKey := "hardware-id-key: TUZNd09BWUdLdzRIQWdFQk1DNENGUUR0Z0dwZGNhdXkraExpSFF2TzFVV240ck90Q3dJVkFPdmg2OEZYNjBHVQo1TllFOW05MzJESDhYOFpvQXhjQUFoUU5PdEFNYktUazdqQi9FSlgvaWJ3bGVpWFpDZz09\n"
	hardwareIDKeySha3384 := fmt.Sprintf("hardware-id-key-sha3-384: %s\n", s.hashedHardwareIDKey)

	invalidTests := []struct{ original, invalid, expectedErr string }{
		{"issuer-id: account-id-1\n", "", `"issuer-id" header is mandatory`},
		{"issuer-id: account-id-1\n", "issuer-id: \n", `"issuer-id" header should not be empty`},
		{"issuer-id: account-id-1\n", "issuer-id: @9\n", `"issuer-id" header contains invalid characters: "@9"`},
		{"issuer-id: account-id-1\n", "issuer-id: account-id-2\n", `issuer id must match authority id`},
		{"manufacturer: some-manufacturer\n", "", `"manufacturer" header is mandatory`},
		{"manufacturer: some-manufacturer\n", "manufacturer: \n", `"manufacturer" header should not be empty`},
		{"hardware-name: raspberry-pi-4gb\n", "", `"hardware-name" header is mandatory`},
		{"hardware-name: raspberry-pi-4gb\n", "hardware-name: \n", `"hardware-name" header should not be empty`},
		{"hardware-name: raspberry-pi-4gb\n", "hardware-name: raspberry&pi\n", `"hardware-name" header contains invalid characters: "raspberry&pi"`},
		{"hardware-id: random-id-1\n", "", `"hardware-id" header is mandatory`},
		{"hardware-id: random-id-1\n", "hardware-id: \n", `"hardware-id" header should not be empty`},
		{hardwareIDKey, "", `"hardware-id-key" header is mandatory`},
		{hardwareIDKey, "hardware-id-key: \n", `"hardware-id-key" header should not be empty`},
		{hardwareIDKey, "hardware-id-key: something\n", `no PEM block was found`},
		{hardwareIDKey, "hardware-id-key: TUlHZU1BMEdDU3FH\n", `cannot parse public key: .*`},
		{hardwareIDKey, elGamalhardwareKey, `cannot parse public key: .*`},
		{hardwareIDKeySha3384, "", `"hardware-id-key-sha3-384" header is mandatory`},
		{hardwareIDKeySha3384, "hardware-id-key-sha3-384: \n", `"hardware-id-key-sha3-384" header should not be empty`},
		{hardwareIDKeySha3384, "hardware-id-key-sha3-384: random\n", `hardware id key does not match provided hash`},
		{hardwareIDKeySha3384, "hardware-id-key-sha3-384: ~\n", `hardware id key does not match provided hash`},
	}

	for i, test := range invalidTests {
		invalid := strings.Replace(encoded, test.original, test.invalid, 1)
		_, err := asserts.Decode([]byte(invalid))
		c.Assert(err, ErrorMatches, errPrefix+test.expectedErr, Commentf("test %d/%d failed", i+1, len(invalidTests)))
	}
}
