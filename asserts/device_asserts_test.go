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
	"fmt"
	"strings"
	"time"

	. "gopkg.in/check.v1"

	"github.com/ubuntu-core/snappy/asserts"
)

type modelSuite struct {
	ts     time.Time
	tsLine string
}

var (
	_ = Suite(&modelSuite{})
)

func (mods *modelSuite) SetUpSuite(c *C) {
	mods.ts = time.Now().Truncate(time.Second).UTC()
	mods.tsLine = "timestamp: " + mods.ts.Format(time.RFC3339) + "\n"
}

const modelExample = "type: model\n" +
	"authority-id: brand-id1\n" +
	"brand-id: brand-id1\n" +
	"model: baz-3000\n" +
	"series: 16\n" +
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
	c.Check(model.BrandID(), Equals, "brand-id1")
	c.Check(model.Model(), Equals, "baz-3000")
	c.Check(model.Series(), Equals, "16")
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

func (mods *modelSuite) TestDecodeInvalidMandatory(c *C) {
	encoded := strings.Replace(modelExample, "TSLINE", mods.tsLine, 1)

	mandatoryHeaders := []string{"brand-id", "model", "series", "os", "architecture", "gadget", "kernel", "store", "allowed-modes", "required-snaps", "class", "timestamp"}

	for _, mandatory := range mandatoryHeaders {
		invalid := strings.Replace(encoded, mandatory+":", "xyz:", 1)
		_, err := asserts.Decode([]byte(invalid))
		c.Check(err, ErrorMatches, fmt.Sprintf("%s%q header is mandatory", modelErrPrefix, mandatory))
	}
}

func (mods *modelSuite) TestDecodeInvalid(c *C) {
	encoded := strings.Replace(modelExample, "TSLINE", mods.tsLine, 1)

	invalidTests := []struct{ original, invalid, expectedErr string }{
		{"brand-id: brand-id1\n", "brand-id: random\n", `authority-id and brand-id must match, model assertions are expected to be signed by the brand: "brand-id1" != "random"`},
		{"required-snaps: foo, bar\n", "required-snaps: foo,\n", `empty entry in comma separated "required-snaps" header: "foo,"`},
		{"allowed-modes: \n", "allowed-modes: ,\n", `empty entry in comma separated "allowed-modes" header: ","`},
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
	c.Assert(err, ErrorMatches, "signature verifies but assertion violates other knowledge: model assertion timestamp outside of signing key validity")
}
