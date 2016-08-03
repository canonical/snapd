// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2015-2016 Canonical Ltd
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
	"io"
	"time"

	"golang.org/x/crypto/openpgp/packet"
)

// expose test-only things here

// access internal openpgp lib packet
func PrivateKeyPacket(pk PrivateKey) *packet.PrivateKey {
	return pk.(openpgpPrivateKey).privk
}

// assembleAndSign exposed for tests
var AssembleAndSignInTest = assembleAndSign

// decodePrivateKey exposed for tests
var DecodePrivateKeyInTest = decodePrivateKey

// NewDecoderStressed makes a Decoder with a stressed setup with the given buffer and maximum sizes.
func NewDecoderStressed(r io.Reader, bufSize, maxHeadersSize, maxBodySize, maxSigSize int) *Decoder {
	return (&Decoder{
		rd:             r,
		initialBufSize: bufSize,
		maxHeadersSize: maxHeadersSize,
		maxBodySize:    maxBodySize,
		maxSigSize:     maxSigSize,
	}).initBuffer()
}

// Encoder.append exposed for tests
func EncoderAppend(enc *Encoder, encoded []byte) error {
	return enc.append(encoded)
}

func BootstrapAccountForTest(authorityID string) *Account {
	return &Account{
		assertionBase: assertionBase{
			headers: map[string]interface{}{
				"type":         "account",
				"authority-id": authorityID,
				"account-id":   authorityID,
				"validation":   "certified",
			},
		},
		timestamp: time.Now().UTC(),
	}
}

func makeAccountKeyForTest(authorityID string, openPGPPubKey PublicKey, validYears int) *AccountKey {
	return &AccountKey{
		assertionBase: assertionBase{
			headers: map[string]interface{}{
				"type":          "account-key",
				"authority-id":  authorityID,
				"account-id":    authorityID,
				"public-key-id": openPGPPubKey.ID(),
			},
		},
		since:  time.Time{},
		until:  time.Time{}.UTC().AddDate(validYears, 0, 0),
		pubKey: openPGPPubKey,
	}
}

func BootstrapAccountKeyForTest(authorityID string, pubKey PublicKey) *AccountKey {
	return makeAccountKeyForTest(authorityID, pubKey, 9999)
}

func ExpiredAccountKeyForTest(authorityID string, pubKey PublicKey) *AccountKey {
	return makeAccountKeyForTest(authorityID, pubKey, 1)
}

// define dummy assertion types to use in the tests

type TestOnly struct {
	assertionBase
}

func assembleTestOnly(assert assertionBase) (Assertion, error) {
	// for testing error cases
	if _, err := checkIntWithDefault(assert.headers, "count", 0); err != nil {
		return nil, err
	}
	return &TestOnly{assert}, nil
}

var TestOnlyType = &AssertionType{"test-only", []string{"primary-key"}, assembleTestOnly}

type TestOnly2 struct {
	assertionBase
}

func assembleTestOnly2(assert assertionBase) (Assertion, error) {
	return &TestOnly2{assert}, nil
}

var TestOnly2Type = &AssertionType{"test-only-2", []string{"pk1", "pk2"}, assembleTestOnly2}

func init() {
	typeRegistry[TestOnlyType.Name] = TestOnlyType
	typeRegistry[TestOnly2Type.Name] = TestOnly2Type
}

// AccountKeyIsKeyValidAt exposes isKeyValidAt on AccountKey for tests
func AccountKeyIsKeyValidAt(ak *AccountKey, when time.Time) bool {
	return ak.isKeyValidAt(when)
}

type GPGRunner func(homedir string, input []byte, args ...string) ([]byte, error)

func MockRunGPG(mock func(prev GPGRunner, homedir string, input []byte, args ...string) ([]byte, error)) (restore func()) {
	prevRunGPG := runGPG
	runGPG = func(homedir string, input []byte, args ...string) ([]byte, error) {
		return mock(prevRunGPG, homedir, input, args...)
	}
	return func() {
		runGPG = prevRunGPG
	}
}

// Headers helpers to test
var (
	ParseHeaders = parseHeaders
	AppendEntry  = appendEntry
)
