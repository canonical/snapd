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
	"crypto/dsa"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/asn1"
	"encoding/base64"
	"fmt"
	"math/big"
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
	elGamalHardwareKey := "hardware-id-key: TUZNd09BWUdLdzRIQWdFQk1DNENGUUR0Z0dwZGNhdXkraExpSFF2TzFVV240ck90Q3dJVkFPdmg2OEZYNjBHVQo1TllFOW05MzJESDhYOFpvQXhjQUFoUU5PdEFNYktUazdqQi9FSlgvaWJ3bGVpWFpDZz09\n"
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
		{hardwareIDKey, elGamalHardwareKey, `cannot parse public key: .*`},
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

func (s *hardwareIdentitySuite) TestVerifySignatureRSA(c *C) {
	privKey, err := rsa.GenerateKey(rand.Reader, 2048)
	c.Assert(err, IsNil)

	h, err := buildHardwareIdentityAssertion(&privKey.PublicKey)
	c.Assert(err, IsNil)

	nonce := []byte("test nonce")
	hash := sha256.New()
	hash.Write(nonce)
	hashed := hash.Sum(nil)

	signature, err := rsa.SignPKCS1v15(rand.Reader, privKey, crypto.SHA256, hashed)
	c.Assert(err, IsNil)

	err = h.VerifyNonceSignature(nonce, signature, crypto.SHA256)
	c.Assert(err, IsNil)

	err = h.VerifyNonceSignature(nonce, append(signature, 0), crypto.SHA256)
	c.Assert(err, ErrorMatches, "invalid signature: .*")
}

func (s *hardwareIdentitySuite) TestVerifySignatureECDSA(c *C) {
	privKey, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	c.Assert(err, IsNil)

	h, err := buildHardwareIdentityAssertion(&privKey.PublicKey)
	c.Assert(err, IsNil)

	nonce := []byte("test nonce")
	hash := sha3.New384()
	hash.Write(nonce)
	hashed := hash.Sum(nil)

	r, ss, err := ecdsa.Sign(rand.Reader, privKey, hashed)
	c.Assert(err, IsNil)

	signature, err := asn1.Marshal(struct{ R, S *big.Int }{r, ss})
	c.Assert(err, IsNil)

	// valid signature
	err = h.VerifyNonceSignature(nonce, signature, crypto.SHA3_384)
	c.Assert(err, IsNil)

	// invalid asn1 marshalling
	err = h.VerifyNonceSignature(nonce, nil, crypto.SHA3_384)
	c.Assert(err, ErrorMatches, "asn1: .*")

	// invalid remaining bytes
	err = h.VerifyNonceSignature(nonce, append(signature, 0), crypto.SHA3_384)
	c.Assert(err, ErrorMatches, "invalid signature: trailing bytes")

	// invalid signature
	err = h.VerifyNonceSignature(append(nonce, 1), signature, crypto.SHA3_384)
	c.Assert(err, ErrorMatches, "invalid signature")
}

func (s *hardwareIdentitySuite) TestVerifySignatureED25519(c *C) {
	pubKey, privKey, err := ed25519.GenerateKey(rand.Reader)
	c.Assert(err, IsNil)

	h, err := buildHardwareIdentityAssertion(pubKey)
	c.Assert(err, IsNil)

	nonce := []byte("test nonce")
	hash := sha3.New384()
	hash.Write(nonce)
	hashed := hash.Sum(nil)

	signature := ed25519.Sign(privKey, hashed)

	err = h.VerifyNonceSignature(nonce, signature, crypto.SHA3_384)
	c.Assert(err, IsNil)

	err = h.VerifyNonceSignature(nonce, append(signature, 1), crypto.SHA3_384)
	c.Assert(err, ErrorMatches, "invalid signature")
}

func (s *hardwareIdentitySuite) TestVerifySignatureDifferentHashAlgorithm(c *C) {
	privKey, err := rsa.GenerateKey(rand.Reader, 2048)
	c.Assert(err, IsNil)

	h, err := buildHardwareIdentityAssertion(&privKey.PublicKey)
	c.Assert(err, IsNil)

	nonce := []byte("test nonce")
	// Sign with SHA256
	hash := sha256.New()
	hash.Write(nonce)
	hashed := hash.Sum(nil)

	signature, err := rsa.SignPKCS1v15(rand.Reader, privKey, crypto.SHA256, hashed)
	c.Assert(err, IsNil)

	// Verify with correct hash algorithm - should pass
	err = h.VerifyNonceSignature(nonce, signature, crypto.SHA256)
	c.Assert(err, IsNil)

	// Verify with different hash algorithm (SHA2-512) - should fail
	err = h.VerifyNonceSignature(nonce, signature, crypto.SHA512)
	c.Assert(err, ErrorMatches, ".*verification error")

	// Sign with SHA2-512 and verify with SHA2-512 - should pass
	hash512 := crypto.SHA512.New()
	hash512.Write(nonce)
	hashed512 := hash512.Sum(nil)

	signature512, err := rsa.SignPKCS1v15(rand.Reader, privKey, crypto.SHA512, hashed512)
	c.Assert(err, IsNil)

	err = h.VerifyNonceSignature(nonce, signature512, crypto.SHA512)
	c.Assert(err, IsNil)
}

func (s *hardwareIdentitySuite) TestVerifySignatureUnsupportedHashOrAlgorithm(c *C) {
	privKey, err := rsa.GenerateKey(rand.Reader, 2048)
	c.Assert(err, IsNil)

	h, err := buildHardwareIdentityAssertion(&privKey.PublicKey)
	c.Assert(err, IsNil)

	const UNSUPPORTED_HASH = crypto.Hash(0) // Invalid hash value
	err = h.VerifyNonceSignature(nil, nil, UNSUPPORTED_HASH)
	c.Assert(err, ErrorMatches, "unsupported hash type: .*")

	var params dsa.Parameters
	err = dsa.GenerateParameters(&params, rand.Reader, dsa.L1024N160)
	c.Assert(err, IsNil)

	privDSAKey := new(dsa.PrivateKey)
	privDSAKey.Parameters = params
	err = dsa.GenerateKey(privDSAKey, rand.Reader)
	c.Assert(err, IsNil)

	h, err = buildHardwareIdentityAssertion(&privDSAKey.PublicKey)
	c.Assert(err, IsNil)

	err = h.VerifyNonceSignature(nil, nil, crypto.SHA256)
	c.Assert(err, ErrorMatches, "unsupported algorithm type: .*")
}

func buildHardwareIdentityAssertion(hardwareKey crypto.PublicKey) (*asserts.HardwareIdentity, error) {
	var pubBytes []byte
	var err error

	// DSA keys need special handling since x509.MarshalPKIXPublicKey doesn't support them
	if dsaKey, ok := hardwareKey.(*dsa.PublicKey); ok {
		pubBytes, err = marshalDSAPublicKey(dsaKey)
	} else {
		pubBytes, err = x509.MarshalPKIXPublicKey(hardwareKey)
	}

	if err != nil {
		return nil, err
	}

	base64Body := base64.StdEncoding.EncodeToString(pubBytes)

	hash := sha3.New384()

	hash.Write([]byte(base64Body))
	hashed := hash.Sum(nil)

	hashedHardwareIDKey, err := asserts.EncodeDigest(crypto.SHA3_384, hashed)
	if err != nil {
		return nil, err
	}

	encoded := strings.Replace(hardwareIdentityExample, "HARDWAREIDKEY", base64Body, 1)
	encoded = strings.ReplaceAll(encoded, "HARDWAREIDKEYSHA3384", hashedHardwareIDKey)

	a, err := asserts.Decode([]byte(encoded))
	if err != nil {
		return nil, err
	}

	return a.(*asserts.HardwareIdentity), nil
}

// marshalDSAPublicKey marshals a DSA public key in PKIX format.
// DSA keys are not supported by x509.MarshalPKIXPublicKey, so we need custom marshaling.
// PKIX format: SEQUENCE { AlgorithmIdentifier, BIT STRING (public key) }
func marshalDSAPublicKey(pubKey *dsa.PublicKey) ([]byte, error) {
	// DSA algorithm OID: 1.2.840.10040.4.1
	dsaOID := asn1.ObjectIdentifier{1, 2, 840, 10040, 4, 1}

	// Marshal DSA parameters (p, q, g)
	params := struct {
		P, Q, G *big.Int
	}{
		P: pubKey.P,
		Q: pubKey.Q,
		G: pubKey.G,
	}
	paramBytes, err := asn1.Marshal(params)
	if err != nil {
		return nil, err
	}

	// Marshal the public key value Y as an INTEGER
	yBytes, err := asn1.Marshal(pubKey.Y)
	if err != nil {
		return nil, err
	}

	// Create the PKIX structure
	pkixKey := struct {
		Algorithm struct {
			OID    asn1.ObjectIdentifier
			Params asn1.RawValue
		}
		PublicKey asn1.BitString
	}{
		Algorithm: struct {
			OID    asn1.ObjectIdentifier
			Params asn1.RawValue
		}{
			OID:    dsaOID,
			Params: asn1.RawValue{FullBytes: paramBytes},
		},
		PublicKey: asn1.BitString{
			Bytes:     yBytes,
			BitLength: len(yBytes) * 8,
		},
	}

	return asn1.Marshal(pkixKey)
}
