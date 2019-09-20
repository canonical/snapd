// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
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
	"strings"
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
)

var (
	_ = Suite(&serialSuite{})
)

type serialSuite struct {
	ts            time.Time
	tsLine        string
	deviceKey     asserts.PrivateKey
	encodedDevKey string
}

func (ss *serialSuite) SetUpSuite(c *C) {
	ss.ts = time.Now().Truncate(time.Second).UTC()
	ss.tsLine = "timestamp: " + ss.ts.Format(time.RFC3339) + "\n"

	ss.deviceKey = testPrivKey2
	encodedPubKey, err := asserts.EncodePublicKey(ss.deviceKey.PublicKey())
	c.Assert(err, IsNil)
	ss.encodedDevKey = string(encodedPubKey)
}

const serialExample = "type: serial\n" +
	"authority-id: brand-id1\n" +
	"brand-id: brand-id1\n" +
	"model: baz-3000\n" +
	"serial: 2700\n" +
	"device-key:\n    DEVICEKEY\n" +
	"device-key-sha3-384: KEYID\n" +
	"TSLINE" +
	"body-length: 2\n" +
	"sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij\n\n" +
	"HW" +
	"\n\n" +
	"AXNpZw=="

func (ss *serialSuite) TestDecodeOK(c *C) {
	encoded := strings.Replace(serialExample, "TSLINE", ss.tsLine, 1)
	encoded = strings.Replace(encoded, "DEVICEKEY", strings.Replace(ss.encodedDevKey, "\n", "\n    ", -1), 1)
	encoded = strings.Replace(encoded, "KEYID", ss.deviceKey.PublicKey().ID(), 1)
	a, err := asserts.Decode([]byte(encoded))
	c.Assert(err, IsNil)
	c.Check(a.Type(), Equals, asserts.SerialType)
	serial := a.(*asserts.Serial)
	c.Check(serial.AuthorityID(), Equals, "brand-id1")
	c.Check(serial.Timestamp(), Equals, ss.ts)
	c.Check(serial.BrandID(), Equals, "brand-id1")
	c.Check(serial.Model(), Equals, "baz-3000")
	c.Check(serial.Serial(), Equals, "2700")
	c.Check(serial.DeviceKey().ID(), Equals, ss.deviceKey.PublicKey().ID())
}

const (
	deviceSessReqErrPrefix = "assertion device-session-request: "
	serialErrPrefix        = "assertion serial: "
	serialReqErrPrefix     = "assertion serial-request: "
)

func (ss *serialSuite) TestDecodeInvalid(c *C) {
	encoded := strings.Replace(serialExample, "TSLINE", ss.tsLine, 1)

	invalidTests := []struct{ original, invalid, expectedErr string }{
		{"brand-id: brand-id1\n", "", `"brand-id" header is mandatory`},
		{"brand-id: brand-id1\n", "brand-id: \n", `"brand-id" header should not be empty`},
		{"authority-id: brand-id1\n", "authority-id: random\n", `authority-id and brand-id must match, serial assertions are expected to be signed by the brand: "random" != "brand-id1"`},
		{"model: baz-3000\n", "", `"model" header is mandatory`},
		{"model: baz-3000\n", "model: \n", `"model" header should not be empty`},
		{"model: baz-3000\n", "model: _what\n", `"model" header contains invalid characters: "_what"`},
		{"serial: 2700\n", "", `"serial" header is mandatory`},
		{"serial: 2700\n", "serial: \n", `"serial" header should not be empty`},
		{ss.tsLine, "", `"timestamp" header is mandatory`},
		{ss.tsLine, "timestamp: \n", `"timestamp" header should not be empty`},
		{ss.tsLine, "timestamp: 12:30\n", `"timestamp" header is not a RFC3339 date: .*`},
		{"device-key:\n    DEVICEKEY\n", "", `"device-key" header is mandatory`},
		{"device-key:\n    DEVICEKEY\n", "device-key: \n", `"device-key" header should not be empty`},
		{"device-key:\n    DEVICEKEY\n", "device-key: $$$\n", `cannot decode public key: .*`},
		{"device-key-sha3-384: KEYID\n", "", `"device-key-sha3-384" header is mandatory`},
	}

	for _, test := range invalidTests {
		invalid := strings.Replace(encoded, test.original, test.invalid, 1)
		invalid = strings.Replace(invalid, "DEVICEKEY", strings.Replace(ss.encodedDevKey, "\n", "\n    ", -1), 1)
		invalid = strings.Replace(invalid, "KEYID", ss.deviceKey.PublicKey().ID(), 1)
		_, err := asserts.Decode([]byte(invalid))
		c.Check(err, ErrorMatches, serialErrPrefix+test.expectedErr)
	}
}

func (ss *serialSuite) TestDecodeKeyIDMismatch(c *C) {
	invalid := strings.Replace(serialExample, "TSLINE", ss.tsLine, 1)
	invalid = strings.Replace(invalid, "DEVICEKEY", strings.Replace(ss.encodedDevKey, "\n", "\n    ", -1), 1)
	invalid = strings.Replace(invalid, "KEYID", "Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij", 1)

	_, err := asserts.Decode([]byte(invalid))
	c.Check(err, ErrorMatches, serialErrPrefix+"device key does not match provided key id")
}

func (ss *serialSuite) TestSerialCheck(c *C) {
	encoded := strings.Replace(serialExample, "TSLINE", ss.tsLine, 1)
	encoded = strings.Replace(encoded, "DEVICEKEY", strings.Replace(ss.encodedDevKey, "\n", "\n    ", -1), 1)
	encoded = strings.Replace(encoded, "KEYID", ss.deviceKey.PublicKey().ID(), 1)
	ex, err := asserts.Decode([]byte(encoded))
	c.Assert(err, IsNil)

	storeDB, db := makeStoreAndCheckDB(c)
	brandDB := setup3rdPartySigning(c, "brand1", storeDB, db)

	tests := []struct {
		signDB  assertstest.SignerDB
		brandID string
		authID  string
		keyID   string
	}{
		{brandDB, brandDB.AuthorityID, "", brandDB.KeyID},
	}

	for _, test := range tests {
		headers := ex.Headers()
		headers["brand-id"] = test.brandID
		if test.authID != "" {
			headers["authority-id"] = test.authID
		} else {
			headers["authority-id"] = test.brandID
		}
		headers["timestamp"] = time.Now().Format(time.RFC3339)
		serial, err := test.signDB.Sign(asserts.SerialType, headers, nil, test.keyID)
		c.Assert(err, IsNil)

		err = db.Check(serial)
		c.Check(err, IsNil)
	}
}

func (ss *serialSuite) TestSerialRequestHappy(c *C) {
	sreq, err := asserts.SignWithoutAuthority(asserts.SerialRequestType,
		map[string]interface{}{
			"brand-id":   "brand-id1",
			"model":      "baz-3000",
			"device-key": ss.encodedDevKey,
			"request-id": "REQID",
		}, []byte("HW-DETAILS"), ss.deviceKey)
	c.Assert(err, IsNil)

	// roundtrip
	a, err := asserts.Decode(asserts.Encode(sreq))
	c.Assert(err, IsNil)

	sreq2, ok := a.(*asserts.SerialRequest)
	c.Assert(ok, Equals, true)

	// standalone signature check
	err = asserts.SignatureCheck(sreq2, sreq2.DeviceKey())
	c.Check(err, IsNil)

	c.Check(sreq2.BrandID(), Equals, "brand-id1")
	c.Check(sreq2.Model(), Equals, "baz-3000")
	c.Check(sreq2.RequestID(), Equals, "REQID")

	c.Check(sreq2.Serial(), Equals, "")
}

func (ss *serialSuite) TestSerialRequestHappyOptionalSerial(c *C) {
	sreq, err := asserts.SignWithoutAuthority(asserts.SerialRequestType,
		map[string]interface{}{
			"brand-id":   "brand-id1",
			"model":      "baz-3000",
			"serial":     "pserial",
			"device-key": ss.encodedDevKey,
			"request-id": "REQID",
		}, []byte("HW-DETAILS"), ss.deviceKey)
	c.Assert(err, IsNil)

	// roundtrip
	a, err := asserts.Decode(asserts.Encode(sreq))
	c.Assert(err, IsNil)

	sreq2, ok := a.(*asserts.SerialRequest)
	c.Assert(ok, Equals, true)

	c.Check(sreq2.Model(), Equals, "baz-3000")
	c.Check(sreq2.Serial(), Equals, "pserial")
}

func (ss *serialSuite) TestSerialRequestDecodeInvalid(c *C) {
	encoded := "type: serial-request\n" +
		"brand-id: brand-id1\n" +
		"model: baz-3000\n" +
		"device-key:\n    DEVICEKEY\n" +
		"request-id: REQID\n" +
		"serial: S\n" +
		"body-length: 2\n" +
		"sign-key-sha3-384: " + ss.deviceKey.PublicKey().ID() + "\n\n" +
		"HW" +
		"\n\n" +
		"AXNpZw=="

	invalidTests := []struct{ original, invalid, expectedErr string }{
		{"brand-id: brand-id1\n", "", `"brand-id" header is mandatory`},
		{"brand-id: brand-id1\n", "brand-id: \n", `"brand-id" header should not be empty`},
		{"model: baz-3000\n", "", `"model" header is mandatory`},
		{"model: baz-3000\n", "model: \n", `"model" header should not be empty`},
		{"request-id: REQID\n", "", `"request-id" header is mandatory`},
		{"request-id: REQID\n", "request-id: \n", `"request-id" header should not be empty`},
		{"device-key:\n    DEVICEKEY\n", "", `"device-key" header is mandatory`},
		{"device-key:\n    DEVICEKEY\n", "device-key: \n", `"device-key" header should not be empty`},
		{"device-key:\n    DEVICEKEY\n", "device-key: $$$\n", `cannot decode public key: .*`},
		{"serial: S\n", "serial:\n  - xyz\n", `"serial" header must be a string`},
	}

	for _, test := range invalidTests {
		invalid := strings.Replace(encoded, test.original, test.invalid, 1)
		invalid = strings.Replace(invalid, "DEVICEKEY", strings.Replace(ss.encodedDevKey, "\n", "\n    ", -1), 1)

		_, err := asserts.Decode([]byte(invalid))
		c.Check(err, ErrorMatches, serialReqErrPrefix+test.expectedErr)
	}
}

func (ss *serialSuite) TestSerialRequestDecodeKeyIDMismatch(c *C) {
	invalid := "type: serial-request\n" +
		"brand-id: brand-id1\n" +
		"model: baz-3000\n" +
		"device-key:\n    " + strings.Replace(ss.encodedDevKey, "\n", "\n    ", -1) + "\n" +
		"request-id: REQID\n" +
		"body-length: 2\n" +
		"sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij\n\n" +
		"HW" +
		"\n\n" +
		"AXNpZw=="

	_, err := asserts.Decode([]byte(invalid))
	c.Check(err, ErrorMatches, "assertion serial-request: device key does not match included signing key id")
}

func (ss *serialSuite) TestDeviceSessionRequest(c *C) {
	ts := time.Now().UTC().Round(time.Second)
	sessReq, err := asserts.SignWithoutAuthority(asserts.DeviceSessionRequestType,
		map[string]interface{}{
			"brand-id":  "brand-id1",
			"model":     "baz-3000",
			"serial":    "99990",
			"nonce":     "NONCE",
			"timestamp": ts.Format(time.RFC3339),
		}, nil, ss.deviceKey)
	c.Assert(err, IsNil)

	// roundtrip
	a, err := asserts.Decode(asserts.Encode(sessReq))
	c.Assert(err, IsNil)

	sessReq2, ok := a.(*asserts.DeviceSessionRequest)
	c.Assert(ok, Equals, true)

	// standalone signature check
	err = asserts.SignatureCheck(sessReq2, ss.deviceKey.PublicKey())
	c.Check(err, IsNil)

	c.Check(sessReq2.BrandID(), Equals, "brand-id1")
	c.Check(sessReq2.Model(), Equals, "baz-3000")
	c.Check(sessReq2.Serial(), Equals, "99990")
	c.Check(sessReq2.Nonce(), Equals, "NONCE")
	c.Check(sessReq2.Timestamp().Equal(ts), Equals, true)
}

func (ss *serialSuite) TestDeviceSessionRequestDecodeInvalid(c *C) {
	tsLine := "timestamp: " + time.Now().Format(time.RFC3339) + "\n"
	encoded := "type: device-session-request\n" +
		"brand-id: brand-id1\n" +
		"model: baz-3000\n" +
		"serial: 99990\n" +
		"nonce: NONCE\n" +
		tsLine +
		"body-length: 0\n" +
		"sign-key-sha3-384: " + ss.deviceKey.PublicKey().ID() + "\n\n" +
		"AXNpZw=="

	invalidTests := []struct{ original, invalid, expectedErr string }{
		{"brand-id: brand-id1\n", "brand-id: \n", `"brand-id" header should not be empty`},
		{"model: baz-3000\n", "model: \n", `"model" header should not be empty`},
		{"serial: 99990\n", "", `"serial" header is mandatory`},
		{"nonce: NONCE\n", "nonce: \n", `"nonce" header should not be empty`},
		{tsLine, "timestamp: 12:30\n", `"timestamp" header is not a RFC3339 date: .*`},
	}

	for _, test := range invalidTests {
		invalid := strings.Replace(encoded, test.original, test.invalid, 1)
		_, err := asserts.Decode([]byte(invalid))
		c.Check(err, ErrorMatches, deviceSessReqErrPrefix+test.expectedErr)
	}
}
