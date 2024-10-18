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
	"fmt"
	"strings"
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
)

type registrySuite struct {
	ts     time.Time
	tsLine string
}

var (
	_ = Suite(&registrySuite{})
	_ = Suite(&registryControlSuite{})
)

func (s *registrySuite) SetUpSuite(c *C) {
	s.ts = time.Now().Truncate(time.Second).UTC()
	s.tsLine = "timestamp: " + s.ts.Format(time.RFC3339) + "\n"
}

const (
	registryExample = `type: registry
authority-id: brand-id1
account-id: brand-id1
name: my-network
summary: registry description
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

func (s *registrySuite) TestDecodeOK(c *C) {
	encoded := strings.Replace(registryExample, "TSLINE", s.tsLine, 1)

	a, err := asserts.Decode([]byte(encoded))
	c.Assert(err, IsNil)
	c.Check(a, NotNil)
	c.Check(a.Type(), Equals, asserts.RegistryType)
	ar := a.(*asserts.Registry)
	c.Check(ar.AuthorityID(), Equals, "brand-id1")
	c.Check(ar.AccountID(), Equals, "brand-id1")
	c.Check(ar.Name(), Equals, "my-network")
	registry := ar.Registry()
	c.Assert(registry, NotNil)
	c.Check(registry.View("wifi-setup"), NotNil)
}

func (s *registrySuite) TestDecodeInvalid(c *C) {
	const validationSetErrPrefix = "assertion registry: "

	encoded := strings.Replace(registryExample, "TSLINE", s.tsLine, 1)

	viewsStanza := encoded[strings.Index(encoded, "views:") : strings.Index(encoded, "timestamp:")+1]
	body := encoded[strings.Index(encoded, "body-length:"):strings.Index(encoded, "\n\nAXN")]

	invalidTests := []struct{ original, invalid, expectedErr string }{
		{"account-id: brand-id1\n", "", `"account-id" header is mandatory`},
		{"account-id: brand-id1\n", "account-id: \n", `"account-id" header should not be empty`},
		{"account-id: brand-id1\n", "account-id: random\n", `authority-id and account-id must match, registry assertions are expected to be signed by the issuer account: "brand-id1" != "random"`},
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

func (s *registrySuite) TestAssembleAndSignChecksSchemaFormatOK(c *C) {
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
	acct1, err := asserts.AssembleAndSignInTest(asserts.RegistryType, headers, []byte(schema), testPrivKey0)
	c.Assert(err, IsNil)
	c.Assert(string(acct1.Body()), Equals, schema)
}

func (s *registrySuite) TestAssembleAndSignChecksSchemaFormatFail(c *C) {
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
	_, err := asserts.AssembleAndSignInTest(asserts.RegistryType, headers, []byte(schema), testPrivKey0)
	c.Assert(err, ErrorMatches, `assertion registry: JSON in body must be indented with 2 spaces and sort object entries by key`)
}

type registryControlSuite struct{}

const (
	registryControlExample = `type: registry-control
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
      - operator-key
      - store
    views:
      - canonical/network/observe-interfaces
sign-key-sha3-384: t9yuKGLyiezBq_PXMJZsGdkTukmL7MgrgqXAlxxiZF4TYryOjZcy48nnjDmEHQDp

AXNpZw==`
)

func (s *registryControlSuite) TestDecodeOK(c *C) {
	encoded := registryControlExample

	a, err := asserts.Decode([]byte(encoded))
	c.Assert(err, IsNil)
	c.Check(a, NotNil)
	c.Check(a.Type(), Equals, asserts.RegistryControlType)

	rgCtrl := a.(*asserts.RegistryControl)
	c.Check(rgCtrl.BrandID(), Equals, "generic")
	c.Check(rgCtrl.Model(), Equals, "generic-classic")
	c.Check(rgCtrl.Serial(), Equals, "03961d5d-26e5-443f-838d-6db046126bea")
	c.Check(rgCtrl.AuthorityID(), Equals, "")

	c.Check(rgCtrl.IsDelegated("john", "canonical/network/control-device", "operator-key"), Equals, true)
	c.Check(rgCtrl.IsDelegated("john", "canonical/network/observe-device", "operator-key"), Equals, true)
	c.Check(rgCtrl.IsDelegated("john", "canonical/network/control-interfaces", "store"), Equals, true)
	c.Check(rgCtrl.IsDelegated("jane", "canonical/network/observe-interfaces", "store"), Equals, true)
	c.Check(rgCtrl.IsDelegated("jane", "canonical/network/observe-interfaces", "operator-key"), Equals, true)

	c.Check(rgCtrl.IsDelegated("john", "canonical/network/control-device", "store"), Equals, false)
	c.Check(rgCtrl.IsDelegated("john", "canonical/network/control-device", "management-system"), Equals, false)
	c.Check(rgCtrl.IsDelegated("jane", "canonical/network/control-device", "operator-key"), Equals, false)
	c.Check(rgCtrl.IsDelegated("unknown", "canonical/network/observe-interfaces", "operator-key"), Equals, false)
}

func (s *registryControlSuite) TestDecodeInvalid(c *C) {
	encoded := registryControlExample
	const validationSetErrPrefix = "assertion registry-control: "

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
		{"groups:", "groups:\n  - bar", `group at position 1: must be a map`},
		{"    operator-id: jane\n", "", `group at position 3: "operator-id" not provided`},
		{
			"operator-id: jane\n",
			"operator-id: \n",
			`group at position 3: "operator-id" must be a non-empty string`,
		},
		{
			"operator-id: jane\n",
			"operator-id: @op\n",
			`group at position 3: invalid "operator-id" @op`,
		},
		{
			"    authentication:\n      - store",
			"    authentication: abcd",
			`group at position 2: "authentication" field must be a list of strings`,
		},
		{
			"    authentication:\n      - store",
			"    foo: bar",
			`group at position 2: "authentication" must be provided`,
		},
		{
			"    views:\n      - canonical/network/control-interfaces",
			"    views: abcd",
			`group at position 2: "views" field must be a list of strings`,
		},
		{
			"    views:\n      - canonical/network/control-interfaces",
			"    foo: bar",
			`group at position 2: "views" must be provided`,
		},
		{
			"      - operator-key",
			"      - foo-bar",
			"group at position 1: unknown authentication method: foo-bar",
		},
		{
			"canonical/network/control-interfaces",
			"canonical",
			`group at position 2: "canonical" must be in the format account/registry/view`,
		},
	}

	for i, test := range invalidTests {
		invalid := strings.Replace(encoded, test.original, test.invalid, 1)
		_, err := asserts.Decode([]byte(invalid))
		c.Check(err, ErrorMatches, validationSetErrPrefix+test.expectedErr, Commentf("test %d/%d failed", i+1, len(invalidTests)))
	}
}

func (s *registryControlSuite) TestDelegate(c *C) {
	encoded := registryControlExample

	a, err := asserts.Decode([]byte(encoded))
	c.Assert(err, IsNil)

	rgCtrl := a.(*asserts.RegistryControl)
	c.Check(rgCtrl.IsDelegated("stephen", "canonical/network/control-device", "operator-key"), Equals, false)

	rgCtrl.Delegate(
		"stephen",
		[]string{"canonical/network/control-vpn", "canonical/network/control-device"},
		[]string{"operator-key"},
	)
	rgCtrl.Delegate(
		"stephen",
		[]string{"canonical/network/control-interfaces", "canonical/network/control-device"},
		[]string{"store", "operator-key"},
	)

	c.Check(rgCtrl.IsDelegated("stephen", "canonical/network/control-device", "operator-key"), Equals, true)
	c.Check(rgCtrl.IsDelegated("stephen", "canonical/network/control-device", "store"), Equals, true)
	c.Check(rgCtrl.IsDelegated("stephen", "canonical/network/control-vpn", "operator-key"), Equals, true)
	c.Check(rgCtrl.IsDelegated("stephen", "canonical/network/control-vpn", "store"), Equals, false)
}

func (s *registryControlSuite) TestHeuristics(c *C) {
	type testcase struct {
		// before
		before string
		// request
		action         string
		operatorID     string
		authentication []string
		views          []string
		// after
		after string
	}

	tcs := []testcase{
		{
			before: `
groups:
  -
    operator-id: john
    authentication:
      - operator-key
    views:
      - aa/b/c
      - dd/e/f`,
			action:         "delegate",
			operatorID:     "john",
			authentication: []string{"store", "operator-key"},
			views:          []string{"xx/y/z", "ii/j/k", "aa/b/c", "uu/v/w"},
			after: `
groups:
  -
    operator-id: john
    authentication:
      - operator-key
    views:
      - dd/e/f
  -
    operator-id: john
    authentication:
      - operator-key
      - store
    views:
      - aa/b/c
      - ii/j/k
      - uu/v/w
      - xx/y/z
`,
		},
		{
			before: `
groups:
  -
    operator-id: john
    authentication:
      - operator-key
    views:
      - aa/b/c
      - dd/e/f`,
			action:         "delegate",
			operatorID:     "jane",
			authentication: []string{"store"},
			views:          []string{"aa/b/c"},
			after: `
groups:
  -
    operator-id: jane
    authentication:
      - store
    views:
      - aa/b/c
  -
    operator-id: john
    authentication:
      - operator-key
    views:
      - aa/b/c
      - dd/e/f
`,
		},
		{
			before: `
groups:
  -
    operator-id: john
    authentication:
      - operator-key
    views:
      - dd/e/f
  -
    operator-id: john
    authentication:
      - operator-key
      - store
    views:
      - aa/b/c
      - xx/y/z`,
			action:     "revoke",
			operatorID: "john",
			views:      []string{"xx/y/z", "dd/e/f"},
			after: `
groups:
  -
    operator-id: john
    authentication:
      - operator-key
      - store
    views:
      - aa/b/c
`,
		},
		{
			before: `
groups:
  -
    operator-id: john
    authentication:
      - operator-key
    views:
      - dd/e/f
  -
    operator-id: john
    authentication:
      - operator-key
      - store
    views:
      - aa/b/c
      - xx/y/z
  -
    operator-id: jane
    authentication:
      - store
    views:
      - aa/b/c`,
			action:     "revoke",
			operatorID: "john",
			after: `
groups:
  -
    operator-id: jane
    authentication:
      - store
    views:
      - aa/b/c
`,
		},
		{
			before: `
groups:
  -
    operator-id: john
    authentication:
      - operator-key
    views:
      - dd/e/f
  -
    operator-id: john
    authentication:
      - operator-key
      - store
    views:
      - aa/b/c
      - xx/y/z`,
			action:         "revoke",
			operatorID:     "john",
			authentication: []string{"store"},
			views:          []string{"xx/y/z"},
			after: `
groups:
  -
    operator-id: john
    authentication:
      - operator-key
    views:
      - dd/e/f
      - xx/y/z
  -
    operator-id: john
    authentication:
      - operator-key
      - store
    views:
      - aa/b/c
`,
		},
		{
			before: `
groups:
  -
    operator-id: john
    authentication:
      - operator-key
    views:
      - dd/e/f
  -
    operator-id: john
    authentication:
      - operator-key
      - store
    views:
      - aa/b/c`,
			action:     "revoke",
			operatorID: "john",
			views:      []string{"aa/b/c", "dd/e/f"},
			after: `
groups:
`,
		},
	}

	prefix := `type: registry-control
brand-id: generic
model: generic-classic
serial: 03961d5d-26e5-443f-838d-6db046126bea`
	suffix := `
sign-key-sha3-384: t9yuKGLyiezBq_PXMJZsGdkTukmL7MgrgqXAlxxiZF4TYryOjZcy48nnjDmEHQDp

AXNpZw==`
	for i, tc := range tcs {
		assertion := fmt.Sprintf("%s%s%s", prefix, tc.before, suffix)
		a, err := asserts.Decode([]byte(assertion))
		c.Assert(err, IsNil)

		rgCtrl := a.(*asserts.RegistryControl)
		if tc.action == "delegate" {
			err = rgCtrl.Delegate(tc.operatorID, tc.views, tc.authentication)
		} else {
			err = rgCtrl.Revoke(tc.operatorID, tc.views, tc.authentication)
		}
		c.Assert(err, IsNil)

		cmt := Commentf("test number %d", i+1)
		c.Check("\n"+rgCtrl.PrintGroups(), Equals, tc.after, cmt)
	}
}

func (s *registryControlSuite) TestRevoke(c *C) {
	encoded := registryControlExample

	a, err := asserts.Decode([]byte(encoded))
	c.Assert(err, IsNil)

	rgCtrl := a.(*asserts.RegistryControl)

	c.Check(rgCtrl.IsDelegated("john", "canonical/network/control-device", "operator-key"), Equals, true)
	c.Check(rgCtrl.IsDelegated("jane", "canonical/network/observe-interfaces", "store"), Equals, true)
	c.Check(rgCtrl.IsDelegated("jane", "canonical/network/observe-interfaces", "operator-key"), Equals, true)

	rgCtrl.Revoke("john", []string{"canonical/network/control-device"}, []string{"operator-key"})
	c.Check(rgCtrl.IsDelegated("john", "canonical/network/control-device", "operator-key"), Equals, false)

	rgCtrl.Revoke(
		"jane",
		[]string{"canonical/network/observe-interfaces", "some/unknown/view"},
		[]string{"store", "operator-key"},
	)
	c.Check(rgCtrl.IsDelegated("jane", "canonical/network/observe-interfaces", "store"), Equals, false)
	c.Check(rgCtrl.IsDelegated("jane", "canonical/network/observe-interfaces", "operator-key"), Equals, false)

	// trying to revoke from a non-existent operator
	err = rgCtrl.Revoke("who?", []string{"canonical/network/control-device"}, []string{"operator-key"})
	c.Assert(err, IsNil)
	c.Check(rgCtrl.IsDelegated("who?", "canonical/network/control-device", "operator-key"), Equals, false)
}

func (s *registryControlSuite) TestUnknownAuthMethod(c *C) {
	encoded := registryControlExample

	a, err := asserts.Decode([]byte(encoded))
	c.Assert(err, IsNil)

	rgCtrl := a.(*asserts.RegistryControl)

	err = rgCtrl.Delegate(
		"jane",
		[]string{"canonical/network/control-interfaces"},
		[]string{"foo-bar"},
	)
	c.Check(err, ErrorMatches, "unknown authentication method: foo-bar")

	err = rgCtrl.Revoke(
		"jane",
		[]string{"canonical/network/control-interfaces"},
		[]string{"store", "baz", "operator-key"},
	)
	c.Check(err, ErrorMatches, "unknown authentication method: baz")

	c.Check(rgCtrl.IsDelegated("jane", "canonical/network/control-interfaces", "baz"), Equals, false)
}
