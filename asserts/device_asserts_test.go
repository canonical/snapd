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

const modelExample = "type: model\n" +
	"authority-id: brand-id1\n" +
	"series: 16\n" +
	"brand-id: brand-id1\n" +
	"model: baz-3000\n" +
	"os: core\n" +
	"architecture: amd64\n" +
	"gadget: brand-gadget\n" +
	"kernel: baz-linux\n" +
	"store: brand-store\n" +
	"allowed-modes: \n" +
	"required-snaps: foo, bar\n" +
	"class: fixed\n" +
	"TSLINE" +
	"body-length: 0" +
	"\n\n" +
	"openpgp c2ln"

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
	c.Check(model.Class(), Equals, "fixed")
	c.Check(model.OS(), Equals, "core")
	c.Check(model.Architecture(), Equals, "amd64")
	c.Check(model.Gadget(), Equals, "brand-gadget")
	c.Check(model.Kernel(), Equals, "baz-linux")
	c.Check(model.Store(), Equals, "brand-store")
	c.Check(model.AllowedModes(), HasLen, 0)
	c.Check(model.RequiredSnaps(), DeepEquals, []string{"foo", "bar"})
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
		{"os: core\n", "", `"os" header is mandatory`},
		{"os: core\n", "os: \n", `"os" header should not be empty`},
		{"architecture: amd64\n", "", `"architecture" header is mandatory`},
		{"architecture: amd64\n", "architecture: \n", `"architecture" header should not be empty`},
		{"gadget: brand-gadget\n", "", `"gadget" header is mandatory`},
		{"gadget: brand-gadget\n", "gadget: \n", `"gadget" header should not be empty`},
		{"kernel: baz-linux\n", "", `"kernel" header is mandatory`},
		{"kernel: baz-linux\n", "kernel: \n", `"kernel" header should not be empty`},
		{"store: brand-store\n", "", `"store" header is mandatory`},
		{"store: brand-store\n", "store: \n", `"store" header should not be empty`},
		{"allowed-modes: \n", "", `"allowed-modes" header is mandatory`},
		{"allowed-modes: \n", "allowed-modes: ,\n", `empty entry in comma separated "allowed-modes" header: ","`},
		{"required-snaps: foo, bar\n", "", `"required-snaps" header is mandatory`},
		{"required-snaps: foo, bar\n", "required-snaps: foo,\n", `empty entry in comma separated "required-snaps" header: "foo,"`},
		{"class: fixed\n", "", `"class" header is mandatory`},
		{"class: fixed\n", "class: \n", `"class" header should not be empty`},
		{mods.tsLine, "", `"timestamp" header is mandatory`},
		{mods.tsLine, "timestamp: \n", `"timestamp" header should not be empty`},
		{mods.tsLine, "timestamp: 12:30\n", `"timestamp" header is not a RFC3339 date: .*`},
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

	signingKeyID, accSignDB, db := makeSignAndCheckDbWithAccountKey(c, "brand-id1")

	headers := ex.Headers()
	headers["timestamp"] = "2015-11-25T20:00:00Z"
	model, err := accSignDB.Sign(asserts.ModelType, headers, nil, signingKeyID)
	c.Assert(err, IsNil)

	err = db.Check(model)
	c.Assert(err, IsNil)
}

func (mods *modelSuite) TestModelCheckInconsistentTimestamp(c *C) {
	ex, err := asserts.Decode([]byte(strings.Replace(modelExample, "TSLINE", mods.tsLine, 1)))
	c.Assert(err, IsNil)

	signingKeyID, accSignDB, db := makeSignAndCheckDbWithAccountKey(c, "brand-id1")

	headers := ex.Headers()
	headers["timestamp"] = "2011-01-01T14:00:00Z"
	model, err := accSignDB.Sign(asserts.ModelType, headers, nil, signingKeyID)
	c.Assert(err, IsNil)

	err = db.Check(model)
	c.Assert(err, ErrorMatches, "model assertion timestamp outside of signing key validity")
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
	"authority-id: canonical\n" +
	"brand-id: brand-id1\n" +
	"model: baz-3000\n" +
	"serial: 2700\n" +
	"device-key:\n DEVICEKEY\n" +
	"TSLINE" +
	"body-length: 2\n\n" +
	"HW" +
	"\n\n" +
	"openpgp c2ln"

func (ss *serialSuite) TestDecodeOK(c *C) {
	encoded := strings.Replace(serialExample, "TSLINE", ss.tsLine, 1)
	encoded = strings.Replace(encoded, "DEVICEKEY", strings.Replace(ss.encodedDevKey, "\n", "\n ", -1), 1)
	a, err := asserts.Decode([]byte(encoded))
	c.Assert(err, IsNil)
	c.Check(a.Type(), Equals, asserts.SerialType)
	serial := a.(*asserts.Serial)
	c.Check(serial.AuthorityID(), Equals, "canonical")
	c.Check(serial.Timestamp(), Equals, ss.ts)
	c.Check(serial.BrandID(), Equals, "brand-id1")
	c.Check(serial.Model(), Equals, "baz-3000")
	c.Check(serial.Serial(), Equals, "2700")
	c.Check(serial.DeviceKey().Fingerprint(), Equals, ss.deviceKey.PublicKey().Fingerprint())
}

const (
	serialErrPrefix = "assertion serial: "
)

func (ss *serialSuite) TestDecodeInvalid(c *C) {
	encoded := strings.Replace(serialExample, "TSLINE", ss.tsLine, 1)

	invalidTests := []struct{ original, invalid, expectedErr string }{
		{"brand-id: brand-id1\n", "", `"brand-id" header is mandatory`},
		{"brand-id: brand-id1\n", "brand-id: \n", `"brand-id" header should not be empty`},
		{"model: baz-3000\n", "", `"model" header is mandatory`},
		{"model: baz-3000\n", "model: \n", `"model" header should not be empty`},
		{"serial: 2700\n", "", `"serial" header is mandatory`},
		{"serial: 2700\n", "serial: \n", `"serial" header should not be empty`},
		{ss.tsLine, "", `"timestamp" header is mandatory`},
		{ss.tsLine, "timestamp: \n", `"timestamp" header should not be empty`},
		{ss.tsLine, "timestamp: 12:30\n", `"timestamp" header is not a RFC3339 date: .*`},
		{"device-key:\n DEVICEKEY\n", "", `"device-key" header is mandatory`},
		{"device-key:\n DEVICEKEY\n", "device-key: \n", `"device-key" header should not be empty`},
		{"device-key:\n DEVICEKEY\n", "device-key: openpgp ZZZ\n", `public key: could not decode base64 data:.*`},
	}

	for _, test := range invalidTests {
		invalid := strings.Replace(encoded, test.original, test.invalid, 1)
		invalid = strings.Replace(invalid, "DEVICEKEY", strings.Replace(ss.encodedDevKey, "\n", "\n ", -1), 1)

		_, err := asserts.Decode([]byte(invalid))
		c.Check(err, ErrorMatches, serialErrPrefix+test.expectedErr)
	}
}
