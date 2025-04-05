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
	"path/filepath"
	"strings"
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
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
	confdbExample = `type: confdb-schema
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
	c.Check(a.Type(), Equals, asserts.ConfdbSchemaType)
	ar := a.(*asserts.ConfdbSchema)
	c.Check(ar.AuthorityID(), Equals, "brand-id1")
	c.Check(ar.AccountID(), Equals, "brand-id1")
	c.Check(ar.Name(), Equals, "my-network")
	schema := ar.Schema()
	c.Assert(schema, NotNil)
	c.Check(schema.View("wifi-setup"), NotNil)
}

func (s *confdbSuite) TestDecodeInvalid(c *C) {
	const validationSetErrPrefix = "assertion confdb-schema: "

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
	acct1, err := asserts.AssembleAndSignInTest(asserts.ConfdbSchemaType, headers, []byte(schema), testPrivKey0)
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
	_, err := asserts.AssembleAndSignInTest(asserts.ConfdbSchemaType, headers, []byte(schema), testPrivKey0)
	c.Assert(err, ErrorMatches, `assertion confdb-schema: JSON in body must be indented with 2 spaces and sort object entries by key`)
}

type confdbCtrlSuite struct {
	db *asserts.Database
}

var _ = Suite(&confdbCtrlSuite{})

const (
	confdbControlExample = `type: confdb-control
brand-id: generic
model: generic-classic
serial: 03961d5d-26e5-443f-838d-6db046126bea
groups:
  -
    operators:
      - john
    authentications:
      - operator-key
    views:
      - canonical/network/control-device
      - canonical/network/observe-device
  -
    operators:
      - john
    authentications:
      - store
    views:
      - canonical/network/control-interfaces
  -
    operators:
      - jane
    authentications:
      - store
      - operator-key
    views:
      - canonical/network/observe-interfaces
  -
    operators:
      - alice
      - bob
    authentications:
      - store
      - operator-key
    views:
      - canonical/network/observe-device
      - canonical/network/control-interfaces
sign-key-sha3-384: t9yuKGLyiezBq_PXMJZsGdkTukmL7MgrgqXAlxxiZF4TYryOjZcy48nnjDmEHQDp

AXNpZw==`
)

func (s *confdbCtrlSuite) SetUpTest(c *C) {
	topDir := filepath.Join(c.MkDir(), "asserts-db")
	bs, err := asserts.OpenFSBackstore(topDir)
	c.Assert(err, IsNil)
	cfg := &asserts.DatabaseConfig{
		Backstore: bs,
		Trusted: []asserts.Assertion{
			asserts.BootstrapAccountForTest("canonical"),
			asserts.BootstrapAccountKeyForTest("canonical", testPrivKey0.PublicKey()),
		},
	}
	db, err := asserts.OpenDatabase(cfg)
	c.Assert(err, IsNil)
	s.db = db
}

func (s *confdbCtrlSuite) addSerial(c *C) {
	pubKey := testPrivKey0.PublicKey()
	encodedPubKey, err := asserts.EncodePublicKey(pubKey)
	c.Assert(err, IsNil)

	serial, err := asserts.AssembleAndSignInTest(asserts.SerialType, map[string]interface{}{
		"authority-id":        "canonical",
		"brand-id":            "canonical",
		"model":               "pc",
		"serial":              "42",
		"device-key":          string(encodedPubKey),
		"device-key-sha3-384": pubKey.ID(),
		"timestamp":           time.Now().Format(time.RFC3339),
	}, nil, testPrivKey0)
	c.Assert(err, IsNil)

	err = s.db.Add(serial)
	c.Assert(err, IsNil)
}

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

	ctrl := cc.Control()
	delegated, _ := ctrl.IsDelegated("john", "canonical/network/control-device", []string{"operator-key"})
	c.Check(delegated, Equals, true)
	delegated, _ = ctrl.IsDelegated("john", "canonical/network/observe-device", []string{"operator-key"})
	c.Check(delegated, Equals, true)
	delegated, _ = ctrl.IsDelegated("john", "canonical/network/control-interfaces", []string{"store"})
	c.Check(delegated, Equals, true)
	delegated, _ = ctrl.IsDelegated("jane", "canonical/network/observe-interfaces", []string{"store", "operator-key"})
	c.Check(delegated, Equals, true)
}

func (s *confdbCtrlSuite) TestDecodeEmptyAssertionOK(c *C) {
	emptyAssertion := `type: confdb-control
brand-id: generic
model: generic-classic
serial: 03961d5d-26e5-443f-838d-6db046126bea
sign-key-sha3-384: t9yuKGLyiezBq_PXMJZsGdkTukmL7MgrgqXAlxxiZF4TYryOjZcy48nnjDmEHQDp

AXNpZw==`

	a, err := asserts.Decode([]byte(emptyAssertion))
	c.Assert(err, IsNil)
	c.Assert(a, NotNil)

	ctrl := a.(*asserts.ConfdbControl).Control()
	groups := ctrl.Groups()
	c.Assert(groups, HasLen, 0)
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
		{"groups:", "groups:\n  - bar", `cannot parse group at position 1: must be a map`},
		{"    operators:\n      - jane\n", "", `cannot parse group at position 3: "operators" must be provided`},
		{
			"    operators:\n      - jane\n",
			"    operators: abcd\n", `cannot parse group at position 3: "operators" field must be a list of strings`,
		},
		{
			"      - jane",
			"      - @op",
			`cannot parse group at position 3: invalid operator ID: @op`,
		},
		{
			"    authentications:\n      - store",
			"    authentications: abcd",
			`cannot parse group at position 2: "authentications" field must be a list of strings`,
		},
		{
			"    authentications:\n      - store",
			"    foo: bar",
			`cannot parse group at position 2: "authentications" must be provided`,
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
			"cannot parse group at position 1: cannot delegate: invalid authentication method: foo-bar",
		},
		{
			"canonical/network/control-interfaces",
			"canonical",
			`cannot parse group at position 2: cannot delegate: view "canonical" must be in the format account/confdb/view`,
		},
	}

	for i, test := range invalidTests {
		invalid := strings.Replace(encoded, test.original, test.invalid, 1)
		_, err := asserts.Decode([]byte(invalid))
		c.Assert(err, ErrorMatches, validationSetErrPrefix+test.expectedErr, Commentf("test %d/%d failed", i+1, len(invalidTests)))
	}
}

func (s *confdbCtrlSuite) TestPrerequisites(c *C) {
	a, err := asserts.Decode([]byte(confdbControlExample))
	c.Assert(err, IsNil)

	prereqs := a.Prerequisites()
	c.Assert(prereqs, HasLen, 1)
	c.Check(prereqs[0], DeepEquals, &asserts.Ref{
		Type:       asserts.SerialType,
		PrimaryKey: []string{"generic", "generic-classic", "03961d5d-26e5-443f-838d-6db046126bea"},
	})
}

func (s *confdbCtrlSuite) TestAckAssertionNoSerial(c *C) {
	headers := map[string]interface{}{
		"brand-id": "canonical", "model": "pc", "serial": "42", "groups": []interface{}{},
	}
	a, err := asserts.AssembleAndSignInTest(asserts.ConfdbControlType, headers, nil, testPrivKey0)
	c.Assert(err, IsNil)

	err = s.db.Add(a)
	c.Assert(
		err,
		ErrorMatches,
		`cannot check no-authority assertion type "confdb-control": cannot find matching device serial assertion: .* not found`,
	)
}

func (s *confdbCtrlSuite) TestAckAssertionKeysMismatch(c *C) {
	s.addSerial(c)

	headers := map[string]interface{}{
		"brand-id": "canonical", "model": "pc", "serial": "42", "groups": []interface{}{},
	}
	a, err := asserts.AssembleAndSignInTest(asserts.ConfdbControlType, headers, nil, testPrivKey2)
	c.Assert(err, IsNil)

	err = s.db.Add(a)
	c.Assert(
		err,
		ErrorMatches,
		`cannot check no-authority assertion type "confdb-control": confdb-control's signing key doesn't match the device key`,
	)
}

func (s *confdbCtrlSuite) TestAckAssertionOK(c *C) {
	s.addSerial(c)

	headers := map[string]interface{}{
		"brand-id": "canonical",
		"model":    "pc",
		"serial":   "42",
		"groups": []interface{}{
			map[string]interface{}{
				"operators":       []interface{}{"aa", "cc"},
				"authentications": []interface{}{"operator-key"},
				"views":           []interface{}{"pp/qq/rr"},
			},
		},
	}
	a, err := asserts.AssembleAndSignInTest(asserts.ConfdbControlType, headers, nil, testPrivKey0)
	c.Assert(err, IsNil)

	err = s.db.Add(a)
	c.Assert(err, IsNil)
}
