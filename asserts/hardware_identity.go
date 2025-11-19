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

package asserts

import (
	"bytes"
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/rsa"
	"crypto/x509"
	"encoding/asn1"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"

	"golang.org/x/crypto/sha3"
)

// HardwareIdentity holds a hardware identity assertion, which is a statement
// that verifies the identity of a physical piece of hardware
type HardwareIdentity struct {
	assertionBase

	hardwareIDKeySha3384 string
	hardwareKey          crypto.PublicKey
}

// IssuerID returns the Snap Store account of the issuer and signer of the voucher.
// This can be used to ensure the voucher has originated from a suitable source
// (e.g. the manufacturer or brand).
func (h *HardwareIdentity) IssuerID() string {
	return h.HeaderString("issuer-id")
}

// Manufacturer returns the name of the device manufacturer.
func (h *HardwareIdentity) Manufacturer() string {
	return h.HeaderString("manufacturer")
}

// HardwareName returns the designation of the hardware device model.
func (h *HardwareIdentity) HardwareName() string {
	return h.HeaderString("hardware-name")
}

// HardwareID returns the identification of the individual hardware device.
// It is not called a serial number as there is no strict requirement that
// this value is the same as the serial number in a resulting serial assertion on the device.
func (h *HardwareIdentity) HardwareID() string {
	return h.HeaderString("hardware-id")
}

// HardwareIDKey returns hardware identity public key,
// which is the body of a parsable form (PEM) as defined by RFC7468§13.
func (h *HardwareIdentity) HardwareIDKey() crypto.PublicKey {
	return h.hardwareKey
}

// HardwareIDKeySha3384 returns the hash of the public key binary data encoded in the hardware-id-key header.
// This is included as it is used as part of the primary key for the assertion.
func (h *HardwareIdentity) HardwareIDKeySha3384() string {
	return h.hardwareIDKeySha3384
}

func assembleHardwareIdentity(assert assertionBase) (Assertion, error) {
	issuerID, err := checkStringMatches(assert.headers, "issuer-id", validAccountID)
	if err != nil {
		return nil, err
	}

	if issuerID != assert.HeaderString("authority-id") {
		return nil, errors.New("issuer id must match authority id")
	}

	_, err = checkNotEmptyString(assert.headers, "manufacturer")
	if err != nil {
		return nil, err
	}

	_, err = checkStringMatches(assert.headers, "hardware-name", validModel)
	if err != nil {
		return nil, err
	}

	_, err = checkNotEmptyString(assert.headers, "hardware-id")
	if err != nil {
		return nil, err
	}

	hardwareIDKey, err := checkNotEmptyString(assert.headers, "hardware-id-key")
	if err != nil {
		return nil, err
	}

	pubKey, err := checkStringIsPEM([]byte(hardwareIDKey))
	if err != nil {
		return nil, err
	}

	hardwareIDKeySha3384 := assert.HeaderString("hardware-id-key-sha3-384")

	hash := sha3.New384()
	hash.Write([]byte(hardwareIDKey))
	hashed := hash.Sum(nil)

	// no error can be returned because the hash is initialized beforehand
	hashedHardwareIDKey, _ := EncodeDigest(crypto.SHA3_384, hashed)

	if hardwareIDKeySha3384 != hashedHardwareIDKey {
		return nil, fmt.Errorf("hardware id key does not match provided hash")
	}

	return &HardwareIdentity{
		assertionBase:        assert,
		hardwareIDKeySha3384: hardwareIDKeySha3384,
		hardwareKey:          pubKey,
	}, nil
}

// checkStringIsPEM checks if string is the body of a parsable form (PEM).
// It assumes the BEGIN and END lines are omitted. The function returns a
// non-nil error if the string fails to be a PEM.
func checkStringIsPEM(data []byte) (crypto.PublicKey, error) {
	// add begin and end lines to PEM body
	var bb bytes.Buffer
	bb.WriteString("-----BEGIN PUBLIC KEY-----\n")
	bb.Write(data)
	bb.WriteString("\n-----END PUBLIC KEY-----\n")

	block, _ := pem.Decode(bb.Bytes())
	if block == nil {
		return nil, errors.New("no PEM block was found")
	}

	pubKey, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("cannot parse public key: %v", err)
	}

	return pubKey, nil
}

// VerifyNonceSignature checks the signature of a given nonce against the hardware id key.
// It is used by the model service to verify the request-id.
// It currently supports key with algorithms RSA, ECDSA, and ED25519.
// The hash algorithm used is also specified as a parameter.
func (h *HardwareIdentity) VerifyNonceSignature(nonce, signature []byte, hashAlg crypto.Hash) error {
	if !hashAlg.Available() {
		return fmt.Errorf("unsupported hash type: %s", hashAlg.String())
	}

	hash := hashAlg.New()

	hash.Write(nonce)
	hashed := hash.Sum(nil)

	switch keyType := h.hardwareKey.(type) {
	case *rsa.PublicKey:
		return verifySignatureWithRSAKey(hashed, signature, h.hardwareKey.(*rsa.PublicKey), hashAlg)
	case *ecdsa.PublicKey:
		return verifySignatureWithECDSAKey(hashed, signature, h.hardwareKey.(*ecdsa.PublicKey))
	case ed25519.PublicKey:
		return verifySignatureWithED25519Key(hashed, signature, h.hardwareKey.(ed25519.PublicKey))
	default:
		return fmt.Errorf("unsupported algorithm type: %T", keyType)
	}
}

func verifySignatureWithRSAKey(hashed, signature []byte, pubKey *rsa.PublicKey, hashAlg crypto.Hash) error {
	if err := rsa.VerifyPKCS1v15(pubKey, hashAlg, hashed, signature); err != nil {
		return fmt.Errorf("signature invalid: %v", err)
	}
	return nil
}

func verifySignatureWithECDSAKey(hashed, signature []byte, pubKey *ecdsa.PublicKey) error {
	// EcdsaSignature struct defines ASN.1 layout of ECDSA signature
	type EcdsaSignature struct {
		R, S *big.Int
	}

	var sig EcdsaSignature
	rest, err := asn1.Unmarshal(signature, &sig)
	if err != nil {
		return err
	}

	// Ensure all bytes were consumed
	if len(rest) > 0 {
		return errors.New("signature invalid: trailing bytes")
	}

	if !ecdsa.Verify(pubKey, hashed, sig.R, sig.S) {
		return errors.New("signature invalid")
	}

	return nil
}

func verifySignatureWithED25519Key(hashed, signature []byte, pubKey ed25519.PublicKey) error {
	if !ed25519.Verify(pubKey, hashed, signature) {
		return errors.New("signature invalid")
	}
	return nil
}
