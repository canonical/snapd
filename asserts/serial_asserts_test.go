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

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
)

var _ = Suite(&serialSuite{})

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
	encodedPubKey := mylog.Check2(asserts.EncodePublicKey(ss.deviceKey.PublicKey()))

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
	a := mylog.Check2(asserts.Decode([]byte(encoded)))

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
		{"brand-id: brand-id1\n", "brand-id: ,1\n", `"brand-id" header contains invalid characters: ",1\"`},
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
		_ := mylog.Check2(asserts.Decode([]byte(invalid)))
		c.Check(err, ErrorMatches, serialErrPrefix+test.expectedErr)
	}
}

func (ss *serialSuite) TestDecodeKeyIDMismatch(c *C) {
	invalid := strings.Replace(serialExample, "TSLINE", ss.tsLine, 1)
	invalid = strings.Replace(invalid, "DEVICEKEY", strings.Replace(ss.encodedDevKey, "\n", "\n    ", -1), 1)
	invalid = strings.Replace(invalid, "KEYID", "Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij", 1)

	_ := mylog.Check2(asserts.Decode([]byte(invalid)))
	c.Check(err, ErrorMatches, serialErrPrefix+"device key does not match provided key id")
}

func (ss *serialSuite) TestSerialCheck(c *C) {
	encoded := strings.Replace(serialExample, "TSLINE", ss.tsLine, 1)
	encoded = strings.Replace(encoded, "DEVICEKEY", strings.Replace(ss.encodedDevKey, "\n", "\n    ", -1), 1)
	encoded = strings.Replace(encoded, "KEYID", ss.deviceKey.PublicKey().ID(), 1)
	ex := mylog.Check2(asserts.Decode([]byte(encoded)))


	storeDB, db := makeStoreAndCheckDB(c)
	brandDB := setup3rdPartySigning(c, "brand1", storeDB, db)

	const serialMismatchErr = `serial with authority "generic" different from brand "brand1" without model assertion with serial-authority set to to allow for them`
	brandID := brandDB.AuthorityID
	brandKeyID := brandDB.KeyID
	genericKeyID := storeDB.GenericKey.PublicKeyID()
	modelNA := []interface{}(nil)
	brandOnly := []interface{}{}
	tests := []struct {
		// serial-authority setting in model
		// nil == model not available at check (modelNA)
		// empty ==  just brand (brandOnly)
		serialAuth  []interface{}
		signDB      assertstest.SignerDB
		authID      string
		keyID       string
		expectedErr string
	}{
		{modelNA, brandDB, "", brandKeyID, ""},
		{brandOnly, brandDB, "", brandKeyID, ""},
		{[]interface{}{"generic"}, brandDB, "", brandKeyID, ""},
		{[]interface{}{"generic", brandID}, brandDB, "", brandKeyID, ""},
		{[]interface{}{"generic"}, storeDB, "generic", genericKeyID, ""},
		{brandOnly, storeDB, "generic", genericKeyID, serialMismatchErr},
		{modelNA, storeDB, "generic", genericKeyID, serialMismatchErr},
		{[]interface{}{"other"}, storeDB, "generic", genericKeyID, serialMismatchErr},
	}

	for _, test := range tests {
		checkDB := db.WithStackedBackstore(asserts.NewMemoryBackstore())

		if test.serialAuth != nil {
			modHeaders := map[string]interface{}{
				"series":       "16",
				"brand-id":     brandID,
				"architecture": "amd64",
				"model":        "baz-3000",
				"gadget":       "gadget",
				"kernel":       "kernel",
				"timestamp":    time.Now().Format(time.RFC3339),
			}
			if len(test.serialAuth) != 0 {
				modHeaders["serial-authority"] = test.serialAuth
			}
			model := mylog.Check2(brandDB.Sign(asserts.ModelType, modHeaders, nil, ""))

			mylog.Check(checkDB.Add(model))

		}

		headers := ex.Headers()
		headers["brand-id"] = brandID
		if test.authID != "" {
			headers["authority-id"] = test.authID
		} else {
			headers["authority-id"] = brandID
		}
		headers["timestamp"] = time.Now().Format(time.RFC3339)
		serial := mylog.Check2(test.signDB.Sign(asserts.SerialType, headers, nil, test.keyID))
		c.Check(err, IsNil)
		mylog.Check(checkDB.Check(serial))
		if test.expectedErr == "" {
			c.Check(err, IsNil)
		} else {
			c.Check(err, ErrorMatches, test.expectedErr)
		}
	}
}

func (ss *serialSuite) TestSerialRequestHappy(c *C) {
	sreq := mylog.Check2(asserts.SignWithoutAuthority(asserts.SerialRequestType,
		map[string]interface{}{
			"brand-id":   "brand-id1",
			"model":      "baz-3000",
			"device-key": ss.encodedDevKey,
			"request-id": "REQID",
		}, []byte("HW-DETAILS"), ss.deviceKey))


	// roundtrip
	a := mylog.Check2(asserts.Decode(asserts.Encode(sreq)))


	sreq2, ok := a.(*asserts.SerialRequest)
	c.Assert(ok, Equals, true)
	mylog.

		// standalone signature check
		Check(asserts.SignatureCheck(sreq2, sreq2.DeviceKey()))
	c.Check(err, IsNil)

	c.Check(sreq2.BrandID(), Equals, "brand-id1")
	c.Check(sreq2.Model(), Equals, "baz-3000")
	c.Check(sreq2.RequestID(), Equals, "REQID")

	c.Check(sreq2.Serial(), Equals, "")
}

func (ss *serialSuite) TestSerialRequestHappyOptionalSerial(c *C) {
	sreq := mylog.Check2(asserts.SignWithoutAuthority(asserts.SerialRequestType,
		map[string]interface{}{
			"brand-id":   "brand-id1",
			"model":      "baz-3000",
			"serial":     "pserial",
			"device-key": ss.encodedDevKey,
			"request-id": "REQID",
		}, []byte("HW-DETAILS"), ss.deviceKey))


	// roundtrip
	a := mylog.Check2(asserts.Decode(asserts.Encode(sreq)))


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

		_ := mylog.Check2(asserts.Decode([]byte(invalid)))
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

	_ := mylog.Check2(asserts.Decode([]byte(invalid)))
	c.Check(err, ErrorMatches, "assertion serial-request: device key does not match included signing key id")
}

func (ss *serialSuite) TestDeviceSessionRequest(c *C) {
	ts := time.Now().UTC().Round(time.Second)
	sessReq := mylog.Check2(asserts.SignWithoutAuthority(asserts.DeviceSessionRequestType,
		map[string]interface{}{
			"brand-id":  "brand-id1",
			"model":     "baz-3000",
			"serial":    "99990",
			"nonce":     "NONCE",
			"timestamp": ts.Format(time.RFC3339),
		}, nil, ss.deviceKey))


	// roundtrip
	a := mylog.Check2(asserts.Decode(asserts.Encode(sessReq)))


	sessReq2, ok := a.(*asserts.DeviceSessionRequest)
	c.Assert(ok, Equals, true)
	mylog.

		// standalone signature check
		Check(asserts.SignatureCheck(sessReq2, ss.deviceKey.PublicKey()))
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
		_ := mylog.Check2(asserts.Decode([]byte(invalid)))
		c.Check(err, ErrorMatches, deviceSessReqErrPrefix+test.expectedErr)
	}
}
