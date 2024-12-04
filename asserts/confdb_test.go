// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2024 Canonical Ltd
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
	"github.com/snapcore/snapd/confdb"
)

type confdbSuite struct {
	ts     time.Time
	tsLine string
}

var _ = Suite(&confdbSuite{})

func (s *confdbSuite) SetUpSuite(c *C) {
	s.ts = time.Now().Truncate(time.Second).UTC()
	s.tsLine = "timestamp: " + s.ts.Format(time.RFC3339) + "\n"
}

const (
	confdbExample = `type: confdb
authority-id: brand-id1
account-id: brand-id1
name: my-network
summary: confdb description
views:
  wifi-setup:
    rules:
      -
        request: ssids
        storage: wifi.ssids
      -
        request: ssid
        storage: wifi.ssid
        access: read-write
      -
        request: password
        storage: wifi.psk
        access: write
      -
        request: status
        storage: wifi.status
        access: read
      -
        request: private.{key}
        storage: wifi.{key}
` + "TSLINE" +
		"sign-key-sha3-384: jv8_jihiizjvco9m55ppdqsdwuvuhfdibjus-3vw7f_idjix7ffn5qmxb21zquij\n" +
		"body-length: 115" +
		"\n\n" +
		schema +
		"\n\n" +
		"AXNpZw=="
)

const schema = `{
  "storage": {
    "schema": {
      "wifi": {
        "type": "map",
        "values": "any"
      }
    }
  }
}`

func (s *confdbSuite) TestDecodeOK(c *C) {
	encoded := strings.Replace(confdbExample, "TSLINE", s.tsLine, 1)

	a, err := asserts.Decode([]byte(encoded))
	c.Assert(err, IsNil)
	c.Check(a, NotNil)
	c.Check(a.Type(), Equals, asserts.ConfdbType)
	ar := a.(*asserts.Confdb)
	c.Check(ar.AuthorityID(), Equals, "brand-id1")
	c.Check(ar.AccountID(), Equals, "brand-id1")
	c.Check(ar.Name(), Equals, "my-network")
	confdb := ar.Confdb()
	c.Assert(confdb, NotNil)
	c.Check(confdb.View("wifi-setup"), NotNil)
}

func (s *confdbSuite) TestDecodeInvalid(c *C) {
	const validationSetErrPrefix = "assertion confdb: "

	encoded := strings.Replace(confdbExample, "TSLINE", s.tsLine, 1)

	viewsStanza := encoded[strings.Index(encoded, "views:") : strings.Index(encoded, "timestamp:")+1]
	body := encoded[strings.Index(encoded, "body-length:"):strings.Index(encoded, "\n\nAXN")]

	invalidTests := []struct{ original, invalid, expectedErr string }{
		{"account-id: brand-id1\n", "", `"account-id" header is mandatory`},
		{"account-id: brand-id1\n", "account-id: \n", `"account-id" header should not be empty`},
		{"account-id: brand-id1\n", "account-id: random\n", `authority-id and account-id must match, confdb assertions are expected to be signed by the issuer account: "brand-id1" != "random"`},
		{"name: my-network\n", "", `"name" header is mandatory`},
		{"name: my-network\n", "name: \n", `"name" header should not be empty`},
		{"name: my-network\n", "name: my/network\n", `"name" primary key header cannot contain '/'`},
		{"name: my-network\n", "name: my+network\n", `"name" header contains invalid characters: "my\+network"`},
		{s.tsLine, "", `"timestamp" header is mandatory`},
		{viewsStanza, "views: foo\n", `"views" header must be a map`},
		{viewsStanza, "", `"views" stanza is mandatory`},
		{"read-write", "update", `cannot define view "wifi-setup": cannot create view rule:.*`},
		{body, "body-length: 0", `body must contain JSON`},
		{body, "body-length: 8\n\n  - foo\n", `invalid JSON in body: invalid character ' ' in numeric literal`},
		{body, "body-length: 2\n\n{}", `body must contain a "storage" stanza`},
		{body, "body-length: 19\n\n{\n  \"storage\": {}\n}", `invalid schema: cannot parse top level schema: must have a "schema" constraint`},
		{body, "body-length: 4\n\nnull", `body must contain a "storage" stanza`},
		{body, "body-length: 54\n\n{\n\t\"storage\": {\n\t\t\"schema\": {\n\t\t\t\"foo\": \"any\"\n\t\t}\n\t}\n}", `JSON in body must be indented with 2 spaces and sort object entries by key`},
		{body, `body-length: 79

{
  "storage": {
    "schema": {
      "c": "any",
      "a": "any"
    }
  }
}`, `JSON in body must be indented with 2 spaces and sort object entries by key`},
	}

	for i, test := range invalidTests {
		invalid := strings.Replace(encoded, test.original, test.invalid, 1)
		_, err := asserts.Decode([]byte(invalid))
		c.Check(err, ErrorMatches, validationSetErrPrefix+test.expectedErr, Commentf("test %d/%d failed", i+1, len(invalidTests)))
	}
}

func (s *confdbSuite) TestAssembleAndSignChecksSchemaFormatOK(c *C) {
	headers := map[string]interface{}{
		"authority-id": "brand-id1",
		"account-id":   "brand-id1",
		"name":         "my-network",
		"views": map[string]interface{}{
			"foo": map[string]interface{}{
				"rules": []interface{}{
					map[string]interface{}{"request": "wifi", "storage": "wifi"},
				},
			},
		},
		"body-length": "60",
		"timestamp":   s.ts.Format(time.RFC3339),
	}

	schema := `{
  "storage": {
    "schema": {
      "wifi": {
        "type": "map",
        "values": "any"
      }
    }
  }
}`
	acct1, err := asserts.AssembleAndSignInTest(asserts.ConfdbType, headers, []byte(schema), testPrivKey0)
	c.Assert(err, IsNil)
	c.Assert(string(acct1.Body()), Equals, schema)
}

func (s *confdbSuite) TestAssembleAndSignChecksSchemaFormatFail(c *C) {
	headers := map[string]interface{}{
		"authority-id": "brand-id1",
		"account-id":   "brand-id1",
		"name":         "my-network",
		"views": map[string]interface{}{
			"foo": map[string]interface{}{
				"rules": []interface{}{
					map[string]interface{}{"request": "wifi", "storage": "wifi"},
				},
			},
		},
		"body-length": "60",
		"timestamp":   s.ts.Format(time.RFC3339),
	}

	schema := `{ "storage": { "schema": { "foo": "any" } } }`
	_, err := asserts.AssembleAndSignInTest(asserts.ConfdbType, headers, []byte(schema), testPrivKey0)
	c.Assert(err, ErrorMatches, `assertion confdb: JSON in body must be indented with 2 spaces and sort object entries by key`)
}

type confdbCtrlSuite struct{}

var _ = Suite(&confdbCtrlSuite{})

const (
	confdbControlExample = `type: confdb-control
brand-id: generic
model: generic-classic
serial: 03961d5d-26e5-443f-838d-6db046126bea
groups:
  -
    operator-id: john
    authentication:
      - operator-key
    views:
      - canonical/network/control-device
      - canonical/network/observe-device
  -
    operator-id: john
    authentication:
      - store
    views:
      - canonical/network/control-interfaces
  -
    operator-id: jane
    authentication:
      - store
      - operator-key
    views:
      - canonical/network/observe-interfaces
sign-key-sha3-384: t9yuKGLyiezBq_PXMJZsGdkTukmL7MgrgqXAlxxiZF4TYryOjZcy48nnjDmEHQDp

AXNpZw==`
)

func (s *confdbCtrlSuite) TestDecodeOK(c *C) {
	encoded := confdbControlExample

	a, err := asserts.Decode([]byte(encoded))
	c.Assert(err, IsNil)
	c.Assert(a, NotNil)
	c.Assert(a.Type(), Equals, asserts.ConfdbControlType)

	cc := a.(*asserts.ConfdbControl)
	c.Assert(cc.BrandID(), Equals, "generic")
	c.Assert(cc.Model(), Equals, "generic-classic")
	c.Assert(cc.Serial(), Equals, "03961d5d-26e5-443f-838d-6db046126bea")
	c.Assert(cc.AuthorityID(), Equals, "")

	operators := cc.Operators()

	john, ok := operators["john"]
	c.Assert(ok, Equals, true)
	c.Assert(john.ID, Equals, "john")
	c.Assert(len(john.Groups), Equals, 2)

	g := john.Groups[0]
	c.Assert(g.Authentication, DeepEquals, []confdb.AuthenticationMethod{"operator-key"})
	expectedViews := []*confdb.ViewRef{
		{Account: "canonical", Confdb: "network", View: "control-device"},
		{Account: "canonical", Confdb: "network", View: "observe-device"},
	}
	c.Assert(g.Views, DeepEquals, expectedViews)

	g = john.Groups[1]
	c.Assert(g.Authentication, DeepEquals, []confdb.AuthenticationMethod{"store"})
	expectedViews = []*confdb.ViewRef{
		{Account: "canonical", Confdb: "network", View: "control-interfaces"},
	}
	c.Assert(g.Views, DeepEquals, expectedViews)

	jane, ok := operators["jane"]
	c.Assert(ok, Equals, true)
	c.Assert(jane.ID, Equals, "jane")
	c.Assert(len(jane.Groups), Equals, 1)

	g = jane.Groups[0]
	c.Assert(g.Authentication, DeepEquals, []confdb.AuthenticationMethod{"operator-key", "store"})
	expectedViews = []*confdb.ViewRef{
		{Account: "canonical", Confdb: "network", View: "observe-interfaces"},
	}
	c.Assert(g.Views, DeepEquals, expectedViews)
}

func (s *confdbCtrlSuite) TestDecodeInvalid(c *C) {
	encoded := confdbControlExample
	const validationSetErrPrefix = "assertion confdb-control: "

	invalidTests := []struct{ original, invalid, expectedErr string }{
		{"brand-id: generic\n", "", `"brand-id" header is mandatory`},
		{"brand-id: generic\n", "brand-id: \n", `"brand-id" header should not be empty`},
		{"brand-id: generic\n", "brand-id: 456#\n", `"brand-id" header contains invalid characters: "456#"`},
		{"model: generic-classic\n", "", `"model" header is mandatory`},
		{"model: generic-classic\n", "model: \n", `"model" header should not be empty`},
		{"model: generic-classic\n", "model: #\n", `"model" header contains invalid characters: "#"`},
		{"serial: 03961d5d-26e5-443f-838d-6db046126bea\n", "", `"serial" header is mandatory`},
		{"serial: 03961d5d-26e5-443f-838d-6db046126bea\n", "serial: \n", `"serial" header should not be empty`},
		{"groups:", "groups: foo\nviews:", `"groups" header must be a list`},
		{"groups:", "views:", `"groups" stanza is mandatory`},
		{"groups:", "groups:\n  - bar", `cannot parse group at position 1: must be a map`},
		{"    operator-id: jane\n", "", `cannot parse group at position 3: "operator-id" field is mandatory`},
		{
			"operator-id: jane\n",
			"operator-id: \n",
			`cannot parse group at position 3: "operator-id" field should not be empty`,
		},
		{
			"operator-id: jane\n",
			"operator-id: @op\n",
			`cannot parse group at position 3: invalid "operator-id" @op`,
		},
		{
			"    authentication:\n      - store",
			"    authentication: abcd",
			`cannot parse group at position 2: "authentication" field must be a list of strings`,
		},
		{
			"    authentication:\n      - store",
			"    foo: bar",
			`cannot parse group at position 2: "authentication" must be provided`,
		},
		{
			"    views:\n      - canonical/network/control-interfaces",
			"    views: abcd",
			`cannot parse group at position 2: "views" field must be a list of strings`,
		},
		{
			"    views:\n      - canonical/network/control-interfaces",
			"    foo: bar",
			`cannot parse group at position 2: "views" must be provided`,
		},
		{
			"      - operator-key",
			"      - foo-bar",
			"cannot parse group at position 1: cannot add group: invalid authentication method: foo-bar",
		},
		{
			"canonical/network/control-interfaces",
			"canonical",
			`cannot parse group at position 2: view "canonical" must be in the format account/confdb/view`,
		},
	}

	for i, test := range invalidTests {
		invalid := strings.Replace(encoded, test.original, test.invalid, 1)
		_, err := asserts.Decode([]byte(invalid))
		c.Assert(err, ErrorMatches, validationSetErrPrefix+test.expectedErr, Commentf("test %d/%d failed", i+1, len(invalidTests)))
	}
}
