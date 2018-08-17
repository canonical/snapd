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

type modelSuite struct {
	ts     time.Time
	tsLine string
}

var (
	_ = Suite(&modelSuite{})
	_ = Suite(&serialSuite{})
)

func (mods *modelSuite) SetUpSuite(c *C) {
	mods.ts = time.Now().Truncate(time.Second).UTC()
	mods.tsLine = "timestamp: " + mods.ts.Format(time.RFC3339) + "\n"
}

const (
	reqSnaps     = "required-snaps:\n  - foo\n  - bar\n"
	sysUserAuths = "system-user-authority: *\n"
)

const (
	modelExample = "type: model\n" +
		"authority-id: brand-id1\n" +
		"series: 16\n" +
		"brand-id: brand-id1\n" +
		"model: baz-3000\n" +
		"display-name: Baz 3000\n" +
		"architecture: amd64\n" +
		"gadget: brand-gadget\n" +
		"base: core18\n" +
		"kernel: baz-linux\n" +
		"store: brand-store\n" +
		sysUserAuths +
		reqSnaps +
		"TSLINE" +
		"body-length: 0\n" +
		"sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij" +
		"\n\n" +
		"AXNpZw=="

	classicModelExample = "type: model\n" +
		"authority-id: brand-id1\n" +
		"series: 16\n" +
		"brand-id: brand-id1\n" +
		"model: baz-3000\n" +
		"display-name: Baz 3000\n" +
		"classic: true\n" +
		"architecture: amd64\n" +
		"gadget: brand-gadget\n" +
		"store: brand-store\n" +
		reqSnaps +
		"TSLINE" +
		"body-length: 0\n" +
		"sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij" +
		"\n\n" +
		"AXNpZw=="
)

func (mods *modelSuite) TestDecodeOK(c *C) {
	encoded := strings.Replace(modelExample, "TSLINE", mods.tsLine, 1)
	a, err := asserts.Decode([]byte(encoded))
	c.Assert(err, IsNil)
	c.Check(a.Type(), Equals, asserts.ModelType)
	model := a.(*asserts.Model)
	c.Check(model.AuthorityID(), Equals, "brand-id1")
	c.Check(model.Timestamp(), Equals, mods.ts)
	c.Check(model.Series(), Equals, "16")
	c.Check(model.BrandID(), Equals, "brand-id1")
	c.Check(model.Model(), Equals, "baz-3000")
	c.Check(model.DisplayName(), Equals, "Baz 3000")
	c.Check(model.Architecture(), Equals, "amd64")
	c.Check(model.Gadget(), Equals, "brand-gadget")
	c.Check(model.GadgetTrack(), Equals, "")
	c.Check(model.Kernel(), Equals, "baz-linux")
	c.Check(model.KernelTrack(), Equals, "")
	c.Check(model.Base(), Equals, "core18")
	c.Check(model.Store(), Equals, "brand-store")
	c.Check(model.RequiredSnaps(), DeepEquals, []string{"foo", "bar"})
	c.Check(model.SystemUserAuthority(), HasLen, 0)
}

func (mods *modelSuite) TestDecodeStoreIsOptional(c *C) {
	withTimestamp := strings.Replace(modelExample, "TSLINE", mods.tsLine, 1)
	encoded := strings.Replace(withTimestamp, "store: brand-store\n", "store: \n", 1)
	a, err := asserts.Decode([]byte(encoded))
	c.Assert(err, IsNil)
	model := a.(*asserts.Model)
	c.Check(model.Store(), Equals, "")

	encoded = strings.Replace(withTimestamp, "store: brand-store\n", "", 1)
	a, err = asserts.Decode([]byte(encoded))
	c.Assert(err, IsNil)
	model = a.(*asserts.Model)
	c.Check(model.Store(), Equals, "")
}

func (mods *modelSuite) TestDecodeBaseIsOptional(c *C) {
	withTimestamp := strings.Replace(modelExample, "TSLINE", mods.tsLine, 1)
	encoded := strings.Replace(withTimestamp, "base: core18\n", "base: \n", 1)
	a, err := asserts.Decode([]byte(encoded))
	c.Assert(err, IsNil)
	model := a.(*asserts.Model)
	c.Check(model.Base(), Equals, "")

	encoded = strings.Replace(withTimestamp, "base: core18\n", "", 1)
	a, err = asserts.Decode([]byte(encoded))
	c.Assert(err, IsNil)
	model = a.(*asserts.Model)
	c.Check(model.Base(), Equals, "")
}

func (mods *modelSuite) TestDecodeDisplayNameIsOptional(c *C) {
	withTimestamp := strings.Replace(modelExample, "TSLINE", mods.tsLine, 1)
	encoded := strings.Replace(withTimestamp, "display-name: Baz 3000\n", "display-name: \n", 1)
	a, err := asserts.Decode([]byte(encoded))
	c.Assert(err, IsNil)
	model := a.(*asserts.Model)
	// optional but we fallback to Model
	c.Check(model.DisplayName(), Equals, "baz-3000")

	encoded = strings.Replace(withTimestamp, "display-name: Baz 3000\n", "", 1)
	a, err = asserts.Decode([]byte(encoded))
	c.Assert(err, IsNil)
	model = a.(*asserts.Model)
	// optional but we fallback to Model
	c.Check(model.DisplayName(), Equals, "baz-3000")
}

func (mods *modelSuite) TestDecodeRequiredSnapsAreOptional(c *C) {
	withTimestamp := strings.Replace(modelExample, "TSLINE", mods.tsLine, 1)
	encoded := strings.Replace(withTimestamp, reqSnaps, "", 1)
	a, err := asserts.Decode([]byte(encoded))
	c.Assert(err, IsNil)
	model := a.(*asserts.Model)
	c.Check(model.RequiredSnaps(), HasLen, 0)
}

func (mods *modelSuite) TestDecodeSystemUserAuthorityIsOptional(c *C) {
	withTimestamp := strings.Replace(modelExample, "TSLINE", mods.tsLine, 1)
	encoded := strings.Replace(withTimestamp, sysUserAuths, "", 1)
	a, err := asserts.Decode([]byte(encoded))
	c.Assert(err, IsNil)
	model := a.(*asserts.Model)
	// the default is just to accept the brand itself
	c.Check(model.SystemUserAuthority(), DeepEquals, []string{"brand-id1"})

	encoded = strings.Replace(withTimestamp, sysUserAuths, "system-user-authority:\n  - foo\n  - bar\n", 1)
	a, err = asserts.Decode([]byte(encoded))
	c.Assert(err, IsNil)
	model = a.(*asserts.Model)
	c.Check(model.SystemUserAuthority(), DeepEquals, []string{"foo", "bar"})
}

func (mods *modelSuite) TestDecodeKernelTrack(c *C) {
	withTimestamp := strings.Replace(modelExample, "TSLINE", mods.tsLine, 1)
	encoded := strings.Replace(withTimestamp, "kernel: baz-linux\n", "kernel: baz-linux=18\n", 1)
	a, err := asserts.Decode([]byte(encoded))
	c.Assert(err, IsNil)
	model := a.(*asserts.Model)
	c.Check(model.Kernel(), Equals, "baz-linux")
	c.Check(model.KernelTrack(), Equals, "18")
}

func (mods *modelSuite) TestDecodeGadgetTrack(c *C) {
	withTimestamp := strings.Replace(modelExample, "TSLINE", mods.tsLine, 1)
	encoded := strings.Replace(withTimestamp, "gadget: brand-gadget\n", "gadget: brand-gadget=18\n", 1)
	a, err := asserts.Decode([]byte(encoded))
	c.Assert(err, IsNil)
	model := a.(*asserts.Model)
	c.Check(model.Gadget(), Equals, "brand-gadget")
	c.Check(model.GadgetTrack(), Equals, "18")
}

const (
	modelErrPrefix = "assertion model: "
)

func (mods *modelSuite) TestDecodeInvalid(c *C) {
	encoded := strings.Replace(modelExample, "TSLINE", mods.tsLine, 1)

	invalidTests := []struct{ original, invalid, expectedErr string }{
		{"series: 16\n", "", `"series" header is mandatory`},
		{"series: 16\n", "series: \n", `"series" header should not be empty`},
		{"brand-id: brand-id1\n", "", `"brand-id" header is mandatory`},
		{"brand-id: brand-id1\n", "brand-id: \n", `"brand-id" header should not be empty`},
		{"brand-id: brand-id1\n", "brand-id: random\n", `authority-id and brand-id must match, model assertions are expected to be signed by the brand: "brand-id1" != "random"`},
		{"model: baz-3000\n", "", `"model" header is mandatory`},
		{"model: baz-3000\n", "model: \n", `"model" header should not be empty`},
		{"model: baz-3000\n", "model: baz/3000\n", `"model" primary key header cannot contain '/'`},
		// lift this restriction at a later point
		{"model: baz-3000\n", "model: BAZ-3000\n", `"model" header cannot contain uppercase letters`},
		{"display-name: Baz 3000\n", "display-name:\n  - xyz\n", `"display-name" header must be a string`},
		{"architecture: amd64\n", "", `"architecture" header is mandatory`},
		{"architecture: amd64\n", "architecture: \n", `"architecture" header should not be empty`},
		{"gadget: brand-gadget\n", "", `"gadget" header is mandatory`},
		{"gadget: brand-gadget\n", "gadget: \n", `"gadget" header should not be empty`},
		{"gadget: brand-gadget\n", "gadget: brand-gadget=x/x/x\n", `"gadget" channel selector must be a track name only`},
		{"gadget: brand-gadget\n", "gadget: brand-gadget=stable\n", `"gadget" channel selector must be a track name`},
		{"gadget: brand-gadget\n", "gadget: brand-gadget=18/beta\n", `"gadget" channel selector must be a track name only`},
		{"gadget: brand-gadget\n", "gadget:\n  - xyz \n", `"gadget" header must be a string`},
		{"kernel: baz-linux\n", "", `"kernel" header is mandatory`},
		{"kernel: baz-linux\n", "kernel: \n", `"kernel" header should not be empty`},
		{"kernel: baz-linux\n", "kernel: baz-linux=x/x/x\n", `"kernel" channel selector must be a track name only`},
		{"kernel: baz-linux\n", "kernel: baz-linux=stable\n", `"kernel" channel selector must be a track name`},
		{"kernel: baz-linux\n", "kernel: baz-linux=18/beta\n", `"kernel" channel selector must be a track name only`},
		{"kernel: baz-linux\n", "kernel:\n  - xyz \n", `"kernel" header must be a string`},
		{"store: brand-store\n", "store:\n  - xyz\n", `"store" header must be a string`},
		{mods.tsLine, "", `"timestamp" header is mandatory`},
		{mods.tsLine, "timestamp: \n", `"timestamp" header should not be empty`},
		{mods.tsLine, "timestamp: 12:30\n", `"timestamp" header is not a RFC3339 date: .*`},
		{reqSnaps, "required-snaps: foo\n", `"required-snaps" header must be a list of strings`},
		{reqSnaps, "required-snaps:\n  -\n    - nested\n", `"required-snaps" header must be a list of strings`},
		{sysUserAuths, "system-user-authority:\n  a: 1\n", `"system-user-authority" header must be '\*' or a list of account ids`},
		{sysUserAuths, "system-user-authority:\n  - 5_6\n", `"system-user-authority" header must be '\*' or a list of account ids`},
	}

	for _, test := range invalidTests {
		invalid := strings.Replace(encoded, test.original, test.invalid, 1)
		_, err := asserts.Decode([]byte(invalid))
		c.Check(err, ErrorMatches, modelErrPrefix+test.expectedErr)
	}
}

func (mods *modelSuite) TestModelCheck(c *C) {
	ex, err := asserts.Decode([]byte(strings.Replace(modelExample, "TSLINE", mods.tsLine, 1)))
	c.Assert(err, IsNil)

	storeDB, db := makeStoreAndCheckDB(c)
	brandDB := setup3rdPartySigning(c, "brand-id1", storeDB, db)

	headers := ex.Headers()
	headers["brand-id"] = brandDB.AuthorityID
	headers["timestamp"] = time.Now().Format(time.RFC3339)
	model, err := brandDB.Sign(asserts.ModelType, headers, nil, "")
	c.Assert(err, IsNil)

	err = db.Check(model)
	c.Assert(err, IsNil)
}

func (mods *modelSuite) TestModelCheckInconsistentTimestamp(c *C) {
	ex, err := asserts.Decode([]byte(strings.Replace(modelExample, "TSLINE", mods.tsLine, 1)))
	c.Assert(err, IsNil)

	storeDB, db := makeStoreAndCheckDB(c)
	brandDB := setup3rdPartySigning(c, "brand-id1", storeDB, db)

	headers := ex.Headers()
	headers["brand-id"] = brandDB.AuthorityID
	headers["timestamp"] = "2011-01-01T14:00:00Z"
	model, err := brandDB.Sign(asserts.ModelType, headers, nil, "")
	c.Assert(err, IsNil)

	err = db.Check(model)
	c.Assert(err, ErrorMatches, `model assertion timestamp outside of signing key validity \(key valid since.*\)`)
}

func (mods *modelSuite) TestClassicDecodeOK(c *C) {
	encoded := strings.Replace(classicModelExample, "TSLINE", mods.tsLine, 1)
	a, err := asserts.Decode([]byte(encoded))
	c.Assert(err, IsNil)
	c.Check(a.Type(), Equals, asserts.ModelType)
	model := a.(*asserts.Model)
	c.Check(model.AuthorityID(), Equals, "brand-id1")
	c.Check(model.Timestamp(), Equals, mods.ts)
	c.Check(model.Series(), Equals, "16")
	c.Check(model.BrandID(), Equals, "brand-id1")
	c.Check(model.Model(), Equals, "baz-3000")
	c.Check(model.DisplayName(), Equals, "Baz 3000")
	c.Check(model.Classic(), Equals, true)
	c.Check(model.Architecture(), Equals, "amd64")
	c.Check(model.Gadget(), Equals, "brand-gadget")
	c.Check(model.Kernel(), Equals, "")
	c.Check(model.KernelTrack(), Equals, "")
	c.Check(model.Store(), Equals, "brand-store")
	c.Check(model.RequiredSnaps(), DeepEquals, []string{"foo", "bar"})
}

func (mods *modelSuite) TestClassicDecodeInvalid(c *C) {
	encoded := strings.Replace(classicModelExample, "TSLINE", mods.tsLine, 1)

	invalidTests := []struct{ original, invalid, expectedErr string }{
		{"classic: true\n", "classic: foo\n", `"classic" header must be 'true' or 'false'`},
		{"architecture: amd64\n", "architecture:\n  - foo\n", `"architecture" header must be a string`},
		{"gadget: brand-gadget\n", "gadget:\n  - foo\n", `"gadget" header must be a string`},
		{"gadget: brand-gadget\n", "kernel: brand-kernel\n", `cannot specify a kernel with a classic model`},
		{"gadget: brand-gadget\n", "base: some-base\n", `cannot specify a base with a classic model`},
	}

	for _, test := range invalidTests {
		invalid := strings.Replace(encoded, test.original, test.invalid, 1)
		_, err := asserts.Decode([]byte(invalid))
		c.Check(err, ErrorMatches, modelErrPrefix+test.expectedErr)
	}
}

func (mods *modelSuite) TestClassicDecodeGadgetAndArchOptional(c *C) {
	encoded := strings.Replace(classicModelExample, "TSLINE", mods.tsLine, 1)
	encoded = strings.Replace(encoded, "gadget: brand-gadget\n", "", 1)
	encoded = strings.Replace(encoded, "architecture: amd64\n", "", 1)
	a, err := asserts.Decode([]byte(encoded))
	c.Assert(err, IsNil)
	c.Check(a.Type(), Equals, asserts.ModelType)
	model := a.(*asserts.Model)
	c.Check(model.Classic(), Equals, true)
	c.Check(model.Architecture(), Equals, "")
	c.Check(model.Gadget(), Equals, "")
}

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
