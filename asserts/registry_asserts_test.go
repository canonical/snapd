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
operator-id: f22PSauKuNkwQTM9Wz67ZCjNACuSjjhN
views:
  -
    name: canonical/network/control-device
  -
    name: canonical/network/observe-device
  -
    name: canonical/network/control-interfaces
  -
    name: canonical/network/observe-interfaces
sign-key-sha3-384: t9yuKGLyiezBq_PXMJZsGdkTukmL7MgrgqXAlxxiZF4TYryOjZcy48nnjDmEHQDp

AXNpZw==`
)

func (s *registryControlSuite) TestDecodeOK(c *C) {
	encoded := registryControlExample

	a, err := asserts.Decode([]byte(encoded))
	c.Assert(err, IsNil)
	c.Check(a, NotNil)
	c.Check(a.Type(), Equals, asserts.RegistryControlType)

	rgCtrlA := a.(*asserts.RegistryControl)
	c.Check(rgCtrlA.AuthorityID(), Equals, "")
	c.Check(rgCtrlA.BrandID(), Equals, "generic")
	c.Check(rgCtrlA.Model(), Equals, "generic-classic")
	c.Check(rgCtrlA.Serial(), Equals, "03961d5d-26e5-443f-838d-6db046126bea")
	c.Check(rgCtrlA.OperatorID(), Equals, "f22PSauKuNkwQTM9Wz67ZCjNACuSjjhN")

	rgCtrl := rgCtrlA.RegistryControl()
	c.Assert(rgCtrl, NotNil)
	networkRegistry := rgCtrl.Registries["canonical/network"]
	c.Assert(networkRegistry, NotNil)

	c.Check(len(networkRegistry.Views), Equals, 4)
	c.Check(rgCtrl.IsDelegated("canonical", "network", "control-device"), Equals, true)
	c.Check(rgCtrl.IsDelegated("canonical", "network", "control-interfaces"), Equals, true)
	c.Check(rgCtrl.IsDelegated("canonical", "network", "observe-device"), Equals, true)
	c.Check(rgCtrl.IsDelegated("canonical", "network", "observe-interfaces"), Equals, true)

	c.Check(rgCtrl.IsDelegated("canonical", "network", "control-vpn"), Equals, false)

	rgCtrl.Delegate("canonical", "network", "control-vpn")
	c.Check(rgCtrl.IsDelegated("canonical", "network", "control-vpn"), Equals, true)

	rgCtrl.Revoke("canonical", "network", "control-vpn")
	c.Check(rgCtrl.IsDelegated("canonical", "network", "control-vpn"), Equals, false)
}

func (s *registryControlSuite) TestDecodeInvalid(c *C) {
	encoded := registryControlExample
	const validationSetErrPrefix = "assertion registry-control: "

	viewsStanza := encoded[strings.Index(encoded, "views:") : strings.Index(encoded, "sign-key-sha3-384:")-1]

	invalidTests := []struct{ original, invalid, expectedErr string }{
		{"brand-id: generic\n", "", `"brand-id" header is mandatory`},
		{"brand-id: generic\n", "brand-id: \n", `"brand-id" header should not be empty`},
		{"brand-id: generic\n", "brand-id: 456#\n", `"brand-id" header contains invalid characters: "456#"`},
		{"model: generic-classic\n", "", `"model" header is mandatory`},
		{"model: generic-classic\n", "model: \n", `"model" header should not be empty`},
		{"model: generic-classic\n", "model: #\n", `"model" header contains invalid characters: "#"`},
		{"serial: 03961d5d-26e5-443f-838d-6db046126bea\n", "", `"serial" header is mandatory`},
		{"serial: 03961d5d-26e5-443f-838d-6db046126bea\n", "serial: \n", `"serial" header should not be empty`},
		{"operator-id: f22PSauKuNkwQTM9Wz67ZCjNACuSjjhN\n", "", `"operator-id" header is mandatory`},
		{"operator-id: f22PSauKuNkwQTM9Wz67ZCjNACuSjjhN\n", "operator-id: \n", `"operator-id" header should not be empty`},
		{
			"operator-id: f22PSauKuNkwQTM9Wz67ZCjNACuSjjhN\n",
			"operator-id: @op\n",
			"invalid Operator ID @op",
		},
		{viewsStanza, "views: abcd", `"views" header must be a list`},
		{viewsStanza, "foo: bar", `"views" stanza is mandatory`},
		{
			"canonical/network/control-interfaces",
			"canonical",
			`view at position 3: "name" must be in the format account/registry/view: canonical`,
		},
	}

	for i, test := range invalidTests {
		invalid := strings.Replace(encoded, test.original, test.invalid, 1)
		_, err := asserts.Decode([]byte(invalid))
		c.Check(err, ErrorMatches, validationSetErrPrefix+test.expectedErr, Commentf("test %d/%d failed", i+1, len(invalidTests)))
	}
}
