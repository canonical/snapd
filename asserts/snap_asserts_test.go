// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2015-2022 Canonical Ltd
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
	"encoding/base64"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"golang.org/x/crypto/sha3"
	. "gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
)

var (
	_ = Suite(&snapDeclSuite{})
	_ = Suite(&snapFileDigestSuite{})
	_ = Suite(&snapBuildSuite{})
	_ = Suite(&snapRevSuite{})
	_ = Suite(&validationSuite{})
	_ = Suite(&baseDeclSuite{})
	_ = Suite(&snapDevSuite{})
)

type snapDeclSuite struct {
	ts     time.Time
	tsLine string
}

type emptyAttrerObject struct{}

func (o emptyAttrerObject) Lookup(path string) (interface{}, bool) {
	return nil, false
}

func (sds *snapDeclSuite) SetUpSuite(c *C) {
	sds.ts = time.Now().Truncate(time.Second).UTC()
	sds.tsLine = "timestamp: " + sds.ts.Format(time.RFC3339) + "\n"
}

func (sds *snapDeclSuite) TestDecodeOK(c *C) {
	encoded := "type: snap-declaration\n" +
		"authority-id: canonical\n" +
		"series: 16\n" +
		"snap-id: snap-id-1\n" +
		"snap-name: first\n" +
		"publisher-id: dev-id1\n" +
		"refresh-control:\n  - foo\n  - bar\n" +
		"auto-aliases:\n  - cmd1\n  - cmd_2\n  - Cmd-3\n  - CMD.4\n" +
		sds.tsLine +
		`aliases:
  -
    name: cmd1
    target: cmd-1
  -
    name: cmd_2
    target: cmd-2
  -
    name: Cmd-3
    target: cmd-3
  -
    name: CMD.4
    target: cmd-4
` +
		"body-length: 0\n" +
		"sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij" +
		"\n\n" +
		"AXNpZw=="
	a := mylog.Check2(asserts.Decode([]byte(encoded)))

	c.Check(a.Type(), Equals, asserts.SnapDeclarationType)
	snapDecl := a.(*asserts.SnapDeclaration)
	c.Check(snapDecl.AuthorityID(), Equals, "canonical")
	c.Check(snapDecl.Timestamp(), Equals, sds.ts)
	c.Check(snapDecl.Series(), Equals, "16")
	c.Check(snapDecl.SnapID(), Equals, "snap-id-1")
	c.Check(snapDecl.SnapName(), Equals, "first")
	c.Check(snapDecl.PublisherID(), Equals, "dev-id1")
	c.Check(snapDecl.RefreshControl(), DeepEquals, []string{"foo", "bar"})
	c.Check(snapDecl.AutoAliases(), DeepEquals, []string{"cmd1", "cmd_2", "Cmd-3", "CMD.4"})
	c.Check(snapDecl.Aliases(), DeepEquals, map[string]string{
		"cmd1":  "cmd-1",
		"cmd_2": "cmd-2",
		"Cmd-3": "cmd-3",
		"CMD.4": "cmd-4",
	})
	c.Check(snapDecl.RevisionAuthority(""), IsNil)
}

func (sds *snapDeclSuite) TestDecodeOKWithRevisionAuthority(c *C) {
	encoded := "type: snap-declaration\n" +
		"authority-id: canonical\n" +
		"series: 16\n" +
		"snap-id: snap-id-1\n" +
		"snap-name: first\n" +
		"publisher-id: dev-id1\n" +
		"refresh-control:\n  - foo\n  - bar\n" +
		sds.tsLine +
		`revision-authority:
  -
    account-id: delegated-acc-id
    provenance:
      - prov1
      - prov2
    min-revision: 100
    max-revision: 1000000
    on-store:
      - store1
` +
		"body-length: 0\n" +
		"sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij" +
		"\n\n" +
		"AXNpZw=="
	a := mylog.Check2(asserts.Decode([]byte(encoded)))

	c.Check(a.Type(), Equals, asserts.SnapDeclarationType)
	snapDecl := a.(*asserts.SnapDeclaration)
	c.Check(snapDecl.AuthorityID(), Equals, "canonical")
	c.Check(snapDecl.Timestamp(), Equals, sds.ts)
	c.Check(snapDecl.Series(), Equals, "16")
	c.Check(snapDecl.SnapID(), Equals, "snap-id-1")
	c.Check(snapDecl.SnapName(), Equals, "first")
	c.Check(snapDecl.PublisherID(), Equals, "dev-id1")
	c.Check(snapDecl.RefreshControl(), DeepEquals, []string{"foo", "bar"})
	ras := snapDecl.RevisionAuthority("prov1")
	c.Check(ras, DeepEquals, []*asserts.RevisionAuthority{
		{
			AccountID:   "delegated-acc-id",
			Provenance:  []string{"prov1", "prov2"},
			MinRevision: 100,
			MaxRevision: 1000000,
			DeviceScope: &asserts.DeviceScopeConstraint{
				Store: []string{"store1"},
			},
		},
	})
}

func (sds *snapDeclSuite) TestDecodeOKWithRevisionAuthorityDefaults(c *C) {
	initial := "type: snap-declaration\n" +
		"authority-id: canonical\n" +
		"series: 16\n" +
		"snap-id: snap-id-1\n" +
		"snap-name: first\n" +
		"publisher-id: dev-id1\n" +
		"refresh-control:\n  - foo\n  - bar\n" +
		sds.tsLine +
		`revision-authority:
  -
    account-id: delegated-acc-id
    provenance:
      - prov1
      - prov2
    min-revision: 100
` +
		"body-length: 0\n" +
		"sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij" +
		"\n\n" +
		"AXNpZw=="
	tests := []struct {
		original, replaced string
		revAuth            asserts.RevisionAuthority
	}{
		{"min", "min", asserts.RevisionAuthority{
			AccountID:   "delegated-acc-id",
			Provenance:  []string{"prov1", "prov2"},
			MinRevision: 100,
		}},
		{"min", "max", asserts.RevisionAuthority{
			AccountID:   "delegated-acc-id",
			Provenance:  []string{"prov1", "prov2"},
			MinRevision: 1,
			MaxRevision: 100,
		}},
		{"    min-revision: 100\n", "", asserts.RevisionAuthority{
			AccountID:   "delegated-acc-id",
			Provenance:  []string{"prov1", "prov2"},
			MinRevision: 1,
		}},
	}

	for _, t := range tests {
		encoded := strings.Replace(initial, t.original, t.replaced, 1)
		a := mylog.Check2(asserts.Decode([]byte(encoded)))

		snapDecl := a.(*asserts.SnapDeclaration)
		ras := snapDecl.RevisionAuthority("prov2")
		c.Check(ras, HasLen, 1)
		c.Check(*ras[0], DeepEquals, t.revAuth)
	}
}

func (sds *snapDeclSuite) TestEmptySnapName(c *C) {
	encoded := "type: snap-declaration\n" +
		"authority-id: canonical\n" +
		"series: 16\n" +
		"snap-id: snap-id-1\n" +
		"snap-name: \n" +
		"publisher-id: dev-id1\n" +
		sds.tsLine +
		"body-length: 0\n" +
		"sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij" +
		"\n\n" +
		"AXNpZw=="
	a := mylog.Check2(asserts.Decode([]byte(encoded)))

	snapDecl := a.(*asserts.SnapDeclaration)
	c.Check(snapDecl.SnapName(), Equals, "")
}

func (sds *snapDeclSuite) TestMissingRefreshControlAutoAliases(c *C) {
	encoded := "type: snap-declaration\n" +
		"authority-id: canonical\n" +
		"series: 16\n" +
		"snap-id: snap-id-1\n" +
		"snap-name: \n" +
		"publisher-id: dev-id1\n" +
		sds.tsLine +
		"body-length: 0\n" +
		"sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij" +
		"\n\n" +
		"AXNpZw=="
	a := mylog.Check2(asserts.Decode([]byte(encoded)))

	snapDecl := a.(*asserts.SnapDeclaration)
	c.Check(snapDecl.RefreshControl(), HasLen, 0)
	c.Check(snapDecl.AutoAliases(), HasLen, 0)
}

const (
	snapDeclErrPrefix = "assertion snap-declaration: "
)

func (sds *snapDeclSuite) TestDecodeInvalid(c *C) {
	aliases := `aliases:
  -
    name: cmd_1
    target: cmd-1
`
	encoded := "type: snap-declaration\n" +
		"authority-id: canonical\n" +
		"series: 16\n" +
		"snap-id: snap-id-1\n" +
		"snap-name: first\n" +
		"publisher-id: dev-id1\n" +
		"refresh-control:\n  - foo\n  - bar\n" +
		"auto-aliases:\n  - cmd1\n  - cmd2\n" +
		aliases +
		"plugs:\n  interface1: true\n" +
		"slots:\n  interface2: true\n" +
		sds.tsLine +
		"body-length: 0\n" +
		"sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij" +
		"\n\n" +
		"AXNpZw=="

	invalidTests := []struct{ original, invalid, expectedErr string }{
		{"series: 16\n", "", `"series" header is mandatory`},
		{"series: 16\n", "series: \n", `"series" header should not be empty`},
		{"snap-id: snap-id-1\n", "", `"snap-id" header is mandatory`},
		{"snap-id: snap-id-1\n", "snap-id: \n", `"snap-id" header should not be empty`},
		{"snap-name: first\n", "", `"snap-name" header is mandatory`},
		{"publisher-id: dev-id1\n", "", `"publisher-id" header is mandatory`},
		{"publisher-id: dev-id1\n", "publisher-id: \n", `"publisher-id" header should not be empty`},
		{"refresh-control:\n  - foo\n  - bar\n", "refresh-control: foo\n", `"refresh-control" header must be a list of strings`},
		{"refresh-control:\n  - foo\n  - bar\n", "refresh-control:\n  -\n    - nested\n", `"refresh-control" header must be a list of strings`},
		{"plugs:\n  interface1: true\n", "plugs: \n", `"plugs" header must be a map`},
		{"plugs:\n  interface1: true\n", "plugs:\n  intf1:\n    foo: bar\n", `plug rule for interface "intf1" must specify at least one of.*`},
		{"slots:\n  interface2: true\n", "slots: \n", `"slots" header must be a map`},
		{"slots:\n  interface2: true\n", "slots:\n  intf1:\n    foo: bar\n", `slot rule for interface "intf1" must specify at least one of.*`},
		{"auto-aliases:\n  - cmd1\n  - cmd2\n", "auto-aliases: cmd0\n", `"auto-aliases" header must be a list of strings`},
		{"auto-aliases:\n  - cmd1\n  - cmd2\n", "auto-aliases:\n  -\n    - nested\n", `"auto-aliases" header must be a list of strings`},
		{"auto-aliases:\n  - cmd1\n  - cmd2\n", "auto-aliases:\n  - _cmd-1\n  - cmd2\n", `"auto-aliases" header contains an invalid element: "_cmd-1"`},
		{aliases, "aliases: cmd0\n", `"aliases" header must be a list of alias maps`},
		{aliases, "aliases:\n  - cmd1\n", `"aliases" header must be a list of alias maps`},
		{"name: cmd_1\n", "name: .cmd1\n", `"name" in "aliases" item 1 contains invalid characters: ".cmd1"`},
		{"target: cmd-1\n", "target: -cmd-1\n", `"target" for alias "cmd_1" contains invalid characters: "-cmd-1"`},
		{aliases, aliases + "  -\n    name: cmd_1\n    target: foo\n", `duplicated definition in "aliases" for alias "cmd_1"`},
		{sds.tsLine, "", `"timestamp" header is mandatory`},
		{sds.tsLine, "timestamp: \n", `"timestamp" header should not be empty`},
		{sds.tsLine, "timestamp: 12:30\n", `"timestamp" header is not a RFC3339 date: .*`},
	}

	for _, test := range invalidTests {
		invalid := strings.Replace(encoded, test.original, test.invalid, 1)
		_ := mylog.Check2(asserts.Decode([]byte(invalid)))
		c.Check(err, ErrorMatches, snapDeclErrPrefix+test.expectedErr)
	}
}

func (sds *snapDeclSuite) TestDecodeInvalidWithRevisionAuthority(c *C) {
	const revAuth = `revision-authority:
  -
    account-id: delegated-acc-id
    provenance:
      - prov1
      - prov2
    min-revision: 100
    max-revision: 1000000
    on-store:
      - store1
`
	encoded := "type: snap-declaration\n" +
		"authority-id: canonical\n" +
		"series: 16\n" +
		"snap-id: snap-id-1\n" +
		"snap-name: first\n" +
		"publisher-id: dev-id1\n" +
		"refresh-control:\n  - foo\n  - bar\n" +
		sds.tsLine +
		revAuth +
		"body-length: 0\n" +
		"sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij" +
		"\n\n" +
		"AXNpZw=="

	invalidTests := []struct{ original, invalid, expectedErr string }{
		{revAuth, "revision-authority: x\n", `revision-authority stanza must be a list of maps`},
		{revAuth, "revision-authority:\n  - x\n", `revision-authority stanza must be a list of maps`},
		{"    account-id: delegated-acc-id\n", "", `"account-id" in revision authority is mandatory`},
		{"account-id: delegated-acc-id\n", "account-id: *\n", `"account-id" in revision authority contains invalid characters: "\*"`},
		{"    provenance:\n      - prov1\n      - prov2\n", "    provenance: \n", `provenance in revision authority must be a list of strings`},
		{"prov2\n", "*\n", `provenance in revision authority contains an invalid element: "\*"`},
		{"    min-revision: 100\n", "    min-revision: 0\n", `"min-revision" in revision authority must be >=1: 0`},
		{"    max-revision: 1000000\n", "    max-revision: 0\n", `"max-revision" in revision authority must be >=1: 0`},
		{"    max-revision: 1000000\n", "    max-revision: 10\n", `optional max-revision cannot be less than min-revision in revision-authority`},
		{"    on-store:\n      - store1\n", "    on-store: foo", `on-store in revision-authority must be a list of strings`},
	}

	for _, test := range invalidTests {
		invalid := strings.Replace(encoded, test.original, test.invalid, 1)
		_ := mylog.Check2(asserts.Decode([]byte(invalid)))
		c.Check(err, ErrorMatches, snapDeclErrPrefix+test.expectedErr)
	}
}

func (sds *snapDeclSuite) TestDecodePlugsAndSlots(c *C) {
	encoded := `type: snap-declaration
format: 1
authority-id: canonical
series: 16
snap-id: snap-id-1
snap-name: first
publisher-id: dev-id1
plugs:
  interface1:
    deny-installation: false
    allow-auto-connection:
      slot-snap-type:
        - app
      slot-publisher-id:
        - acme
      slot-attributes:
        a1: /foo/.*
      plug-attributes:
        b1: B1
    deny-auto-connection:
      slot-attributes:
        a1: !A1
      plug-attributes:
        b1: !B1
  interface2:
    allow-installation: true
    allow-connection:
      plug-attributes:
        a2: A2
      slot-attributes:
        b2: B2
    deny-connection:
      slot-snap-id:
        - snapidsnapidsnapidsnapidsnapid01
        - snapidsnapidsnapidsnapidsnapid02
      plug-attributes:
        a2: !A2
      slot-attributes:
        b2: !B2
slots:
  interface3:
    deny-installation: false
    allow-auto-connection:
      plug-snap-type:
        - app
      plug-publisher-id:
        - acme
      slot-attributes:
        c1: /foo/.*
      plug-attributes:
        d1: C1
    deny-auto-connection:
      slot-attributes:
        c1: !C1
      plug-attributes:
        d1: !D1
  interface4:
    allow-connection:
      plug-attributes:
        c2: C2
      slot-attributes:
        d2: D2
    deny-connection:
      plug-snap-id:
        - snapidsnapidsnapidsnapidsnapid01
        - snapidsnapidsnapidsnapidsnapid02
      plug-attributes:
        c2: !D2
      slot-attributes:
        d2: !D2
    allow-installation:
      slot-snap-type:
        - app
      slot-attributes:
        e1: E1
TSLINE
body-length: 0
sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij

AXNpZw==`
	encoded = strings.Replace(encoded, "TSLINE\n", sds.tsLine, 1)
	a := mylog.Check2(asserts.Decode([]byte(encoded)))

	c.Check(a.SupportedFormat(), Equals, true)
	snapDecl := a.(*asserts.SnapDeclaration)
	c.Check(snapDecl.Series(), Equals, "16")
	c.Check(snapDecl.SnapID(), Equals, "snap-id-1")

	c.Check(snapDecl.PlugRule("interfaceX"), IsNil)
	c.Check(snapDecl.SlotRule("interfaceX"), IsNil)

	plugRule1 := snapDecl.PlugRule("interface1")
	c.Assert(plugRule1, NotNil)
	c.Assert(plugRule1.DenyInstallation, HasLen, 1)
	c.Check(plugRule1.DenyInstallation[0].PlugAttributes, Equals, asserts.NeverMatchAttributes)
	c.Assert(plugRule1.AllowAutoConnection, HasLen, 1)

	plug := emptyAttrerObject{}
	slot := emptyAttrerObject{}

	c.Check(plugRule1.AllowAutoConnection[0].SlotAttributes.Check(slot, nil), ErrorMatches, `attribute "a1".*`)
	c.Check(plugRule1.AllowAutoConnection[0].PlugAttributes.Check(plug, nil), ErrorMatches, `attribute "b1".*`)
	c.Check(plugRule1.AllowAutoConnection[0].SlotSnapTypes, DeepEquals, []string{"app"})
	c.Check(plugRule1.AllowAutoConnection[0].SlotPublisherIDs, DeepEquals, []string{"acme"})
	c.Assert(plugRule1.DenyAutoConnection, HasLen, 1)
	c.Check(plugRule1.DenyAutoConnection[0].SlotAttributes.Check(slot, nil), ErrorMatches, `attribute "a1".*`)
	c.Check(plugRule1.DenyAutoConnection[0].PlugAttributes.Check(plug, nil), ErrorMatches, `attribute "b1".*`)
	plugRule2 := snapDecl.PlugRule("interface2")
	c.Assert(plugRule2, NotNil)
	c.Assert(plugRule2.AllowInstallation, HasLen, 1)
	c.Check(plugRule2.AllowInstallation[0].PlugAttributes, Equals, asserts.AlwaysMatchAttributes)
	c.Assert(plugRule2.AllowConnection, HasLen, 1)
	c.Check(plugRule2.AllowConnection[0].PlugAttributes.Check(plug, nil), ErrorMatches, `attribute "a2".*`)
	c.Check(plugRule2.AllowConnection[0].SlotAttributes.Check(slot, nil), ErrorMatches, `attribute "b2".*`)
	c.Assert(plugRule2.DenyConnection, HasLen, 1)
	c.Check(plugRule2.DenyConnection[0].PlugAttributes.Check(plug, nil), ErrorMatches, `attribute "a2".*`)
	c.Check(plugRule2.DenyConnection[0].SlotAttributes.Check(slot, nil), ErrorMatches, `attribute "b2".*`)
	c.Check(plugRule2.DenyConnection[0].SlotSnapIDs, DeepEquals, []string{"snapidsnapidsnapidsnapidsnapid01", "snapidsnapidsnapidsnapidsnapid02"})

	slotRule3 := snapDecl.SlotRule("interface3")
	c.Assert(slotRule3, NotNil)
	c.Assert(slotRule3.DenyInstallation, HasLen, 1)
	c.Check(slotRule3.DenyInstallation[0].SlotAttributes, Equals, asserts.NeverMatchAttributes)
	c.Assert(slotRule3.AllowAutoConnection, HasLen, 1)
	c.Check(slotRule3.AllowAutoConnection[0].SlotAttributes.Check(slot, nil), ErrorMatches, `attribute "c1".*`)
	c.Check(slotRule3.AllowAutoConnection[0].PlugAttributes.Check(plug, nil), ErrorMatches, `attribute "d1".*`)
	c.Check(slotRule3.AllowAutoConnection[0].PlugSnapTypes, DeepEquals, []string{"app"})
	c.Check(slotRule3.AllowAutoConnection[0].PlugPublisherIDs, DeepEquals, []string{"acme"})
	c.Assert(slotRule3.DenyAutoConnection, HasLen, 1)
	c.Check(slotRule3.DenyAutoConnection[0].SlotAttributes.Check(slot, nil), ErrorMatches, `attribute "c1".*`)
	c.Check(slotRule3.DenyAutoConnection[0].PlugAttributes.Check(plug, nil), ErrorMatches, `attribute "d1".*`)
	slotRule4 := snapDecl.SlotRule("interface4")
	c.Assert(slotRule4, NotNil)
	c.Assert(slotRule4.AllowAutoConnection, HasLen, 1)
	c.Check(slotRule4.AllowConnection[0].PlugAttributes.Check(plug, nil), ErrorMatches, `attribute "c2".*`)
	c.Check(slotRule4.AllowConnection[0].SlotAttributes.Check(slot, nil), ErrorMatches, `attribute "d2".*`)
	c.Assert(slotRule4.DenyAutoConnection, HasLen, 1)
	c.Check(slotRule4.DenyConnection[0].PlugAttributes.Check(plug, nil), ErrorMatches, `attribute "c2".*`)
	c.Check(slotRule4.DenyConnection[0].SlotAttributes.Check(slot, nil), ErrorMatches, `attribute "d2".*`)
	c.Check(slotRule4.DenyConnection[0].PlugSnapIDs, DeepEquals, []string{"snapidsnapidsnapidsnapidsnapid01", "snapidsnapidsnapidsnapidsnapid02"})
	c.Assert(slotRule4.AllowInstallation, HasLen, 1)
	c.Check(slotRule4.AllowInstallation[0].SlotAttributes.Check(slot, nil), ErrorMatches, `attribute "e1".*`)
	c.Check(slotRule4.AllowInstallation[0].SlotSnapTypes, DeepEquals, []string{"app"})
}

func (sds *snapDeclSuite) TestSuggestedFormat(c *C) {
	fmtnum := mylog.Check2(asserts.SuggestFormat(asserts.SnapDeclarationType, nil, nil))

	c.Check(fmtnum, Equals, 0)

	headers := map[string]interface{}{
		"plugs": map[string]interface{}{
			"interface1": "true",
		},
	}
	fmtnum = mylog.Check2(asserts.SuggestFormat(asserts.SnapDeclarationType, headers, nil))

	c.Check(fmtnum, Equals, 1)

	headers = map[string]interface{}{
		"slots": map[string]interface{}{
			"interface2": "true",
		},
	}
	fmtnum = mylog.Check2(asserts.SuggestFormat(asserts.SnapDeclarationType, headers, nil))

	c.Check(fmtnum, Equals, 1)

	headers = map[string]interface{}{
		"plugs": map[string]interface{}{
			"interface3": map[string]interface{}{
				"allow-auto-connection": map[string]interface{}{
					"plug-attributes": map[string]interface{}{
						"x": "$SLOT(x)",
					},
				},
			},
		},
	}
	fmtnum = mylog.Check2(asserts.SuggestFormat(asserts.SnapDeclarationType, headers, nil))

	c.Check(fmtnum, Equals, 2)

	headers = map[string]interface{}{
		"slots": map[string]interface{}{
			"interface3": map[string]interface{}{
				"allow-auto-connection": map[string]interface{}{
					"plug-attributes": map[string]interface{}{
						"x": "$SLOT(x)",
					},
				},
			},
		},
	}
	fmtnum = mylog.Check2(asserts.SuggestFormat(asserts.SnapDeclarationType, headers, nil))

	c.Check(fmtnum, Equals, 2)

	// combinations with on-store/on-brand/on-model => format 3
	for _, side := range []string{"plugs", "slots"} {
		for k, vals := range deviceScopeConstrs {

			headers := map[string]interface{}{
				side: map[string]interface{}{
					"interface3": map[string]interface{}{
						"allow-installation": map[string]interface{}{
							k: vals,
						},
					},
				},
			}
			fmtnum = mylog.Check2(asserts.SuggestFormat(asserts.SnapDeclarationType, headers, nil))

			c.Check(fmtnum, Equals, 3)

			for _, conn := range []string{"connection", "auto-connection"} {

				headers = map[string]interface{}{
					side: map[string]interface{}{
						"interface3": map[string]interface{}{
							"allow-" + conn: map[string]interface{}{
								k: vals,
							},
						},
					},
				}
				fmtnum = mylog.Check2(asserts.SuggestFormat(asserts.SnapDeclarationType, headers, nil))

				c.Check(fmtnum, Equals, 3)
			}
		}
	}

	// higher format features win

	headers = map[string]interface{}{
		"plugs": map[string]interface{}{
			"interface3": map[string]interface{}{
				"allow-auto-connection": map[string]interface{}{
					"on-store": []interface{}{"store"},
				},
			},
		},
		"slots": map[string]interface{}{
			"interface4": map[string]interface{}{
				"allow-auto-connection": map[string]interface{}{
					"plug-attributes": map[string]interface{}{
						"x": "$SLOT(x)",
					},
				},
			},
		},
	}
	fmtnum = mylog.Check2(asserts.SuggestFormat(asserts.SnapDeclarationType, headers, nil))

	c.Check(fmtnum, Equals, 3)

	headers = map[string]interface{}{
		"plugs": map[string]interface{}{
			"interface4": map[string]interface{}{
				"allow-auto-connection": map[string]interface{}{
					"slot-attributes": map[string]interface{}{
						"x": "$SLOT(x)",
					},
				},
			},
		},
		"slots": map[string]interface{}{
			"interface3": map[string]interface{}{
				"allow-auto-connection": map[string]interface{}{
					"on-store": []interface{}{"store"},
				},
			},
		},
	}
	fmtnum = mylog.Check2(asserts.SuggestFormat(asserts.SnapDeclarationType, headers, nil))

	c.Check(fmtnum, Equals, 3)

	// errors
	headers = map[string]interface{}{
		"plugs": "what",
	}
	_ = mylog.Check2(asserts.SuggestFormat(asserts.SnapDeclarationType, headers, nil))
	c.Assert(err, ErrorMatches, `assertion snap-declaration: "plugs" header must be a map`)

	headers = map[string]interface{}{
		"slots": "what",
	}
	_ = mylog.Check2(asserts.SuggestFormat(asserts.SnapDeclarationType, headers, nil))
	c.Assert(err, ErrorMatches, `assertion snap-declaration: "slots" header must be a map`)

	// plug-names/slot-names => format 4
	for _, sidePrefix := range []string{"plug", "slot"} {
		side := sidePrefix + "s"
		headers := map[string]interface{}{
			side: map[string]interface{}{
				"interface3": map[string]interface{}{
					"allow-installation": map[string]interface{}{
						sidePrefix + "-names": []interface{}{"foo"},
					},
				},
			},
		}
		fmtnum = mylog.Check2(asserts.SuggestFormat(asserts.SnapDeclarationType, headers, nil))

		c.Check(fmtnum, Equals, 4)

		for _, conn := range []string{"connection", "auto-connection"} {

			headers = map[string]interface{}{
				side: map[string]interface{}{
					"interface3": map[string]interface{}{
						"allow-" + conn: map[string]interface{}{
							sidePrefix + "-names": []interface{}{"foo"},
						},
					},
				},
			}
			fmtnum = mylog.Check2(asserts.SuggestFormat(asserts.SnapDeclarationType, headers, nil))

			c.Check(fmtnum, Equals, 4)

			headers = map[string]interface{}{
				side: map[string]interface{}{
					"interface3": map[string]interface{}{
						"allow-" + conn: map[string]interface{}{
							"plug-names": []interface{}{"Pfoo"},
							"slot-names": []interface{}{"Sfoo"},
						},
					},
				},
			}
			fmtnum = mylog.Check2(asserts.SuggestFormat(asserts.SnapDeclarationType, headers, nil))

			c.Check(fmtnum, Equals, 4)
		}
	}

	// alt matcher (so far unused) => format 5
	for _, sidePrefix := range []string{"plug", "slot"} {
		headers = map[string]interface{}{
			sidePrefix + "s": map[string]interface{}{
				"interface5": map[string]interface{}{
					"allow-auto-connection": map[string]interface{}{
						sidePrefix + "-attributes": map[string]interface{}{
							"x": []interface{}{"alt1", "alt2"}, // alt matcher
						},
					},
				},
			},
		}
		fmtnum = mylog.Check2(asserts.SuggestFormat(asserts.SnapDeclarationType, headers, nil))

		c.Check(fmtnum, Equals, 5)
	}
}

func prereqDevAccount(c *C, storeDB assertstest.SignerDB, db *asserts.Database) {
	dev1Acct := assertstest.NewAccount(storeDB, "developer1", map[string]interface{}{
		"account-id": "dev-id1",
	}, "")
	mylog.Check(db.Add(dev1Acct))

}

func (sds *snapDeclSuite) TestSnapDeclarationCheck(c *C) {
	storeDB, db := makeStoreAndCheckDB(c)

	prereqDevAccount(c, storeDB, db)

	headers := map[string]interface{}{
		"series":       "16",
		"snap-id":      "snap-id-1",
		"snap-name":    "foo",
		"publisher-id": "dev-id1",
		"timestamp":    time.Now().Format(time.RFC3339),
	}
	snapDecl := mylog.Check2(storeDB.Sign(asserts.SnapDeclarationType, headers, nil, ""))

	mylog.Check(db.Check(snapDecl))

}

func (sds *snapDeclSuite) TestSnapDeclarationCheckUntrustedAuthority(c *C) {
	storeDB, db := makeStoreAndCheckDB(c)

	otherDB := setup3rdPartySigning(c, "other", storeDB, db)

	headers := map[string]interface{}{
		"series":       "16",
		"snap-id":      "snap-id-1",
		"snap-name":    "foo",
		"publisher-id": "dev-id1",
		"timestamp":    time.Now().Format(time.RFC3339),
	}
	snapDecl := mylog.Check2(otherDB.Sign(asserts.SnapDeclarationType, headers, nil, ""))

	mylog.Check(db.Check(snapDecl))
	c.Assert(err, ErrorMatches, `snap-declaration assertion for "foo" \(id "snap-id-1"\) is not signed by a directly trusted authority:.*`)
}

func (sds *snapDeclSuite) TestSnapDeclarationCheckMissingPublisherAccount(c *C) {
	storeDB, db := makeStoreAndCheckDB(c)

	headers := map[string]interface{}{
		"series":       "16",
		"snap-id":      "snap-id-1",
		"snap-name":    "foo",
		"publisher-id": "dev-id1",
		"timestamp":    time.Now().Format(time.RFC3339),
	}
	snapDecl := mylog.Check2(storeDB.Sign(asserts.SnapDeclarationType, headers, nil, ""))

	mylog.Check(db.Check(snapDecl))
	c.Assert(err, ErrorMatches, `snap-declaration assertion for "foo" \(id "snap-id-1"\) does not have a matching account assertion for the publisher "dev-id1"`)
}

type snapFileDigestSuite struct{}

func (s *snapFileDigestSuite) TestSnapFileSHA3_384(c *C) {
	exData := []byte("hashmeplease")

	tempdir := c.MkDir()
	snapFn := filepath.Join(tempdir, "ex.snap")
	mylog.Check(os.WriteFile(snapFn, exData, 0644))


	encDgst, size := mylog.Check3(asserts.SnapFileSHA3_384(snapFn))

	c.Check(size, Equals, uint64(len(exData)))

	h3_384 := sha3.Sum384(exData)
	expected := base64.RawURLEncoding.EncodeToString(h3_384[:])
	c.Check(encDgst, DeepEquals, expected)
}

type snapBuildSuite struct {
	ts     time.Time
	tsLine string
}

func (sds *snapDeclSuite) TestPrerequisites(c *C) {
	encoded := "type: snap-declaration\n" +
		"authority-id: canonical\n" +
		"series: 16\n" +
		"snap-id: snap-id-1\n" +
		"snap-name: first\n" +
		"publisher-id: dev-id1\n" +
		sds.tsLine +
		"body-length: 0\n" +
		"sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij" +
		"\n\n" +
		"AXNpZw=="
	a := mylog.Check2(asserts.Decode([]byte(encoded)))


	prereqs := a.Prerequisites()
	c.Assert(prereqs, HasLen, 1)
	c.Check(prereqs[0], DeepEquals, &asserts.Ref{
		Type:       asserts.AccountType,
		PrimaryKey: []string{"dev-id1"},
	})
}

func (sbs *snapBuildSuite) SetUpSuite(c *C) {
	sbs.ts = time.Now().Truncate(time.Second).UTC()
	sbs.tsLine = "timestamp: " + sbs.ts.Format(time.RFC3339) + "\n"
}

const (
	blobSHA3_384 = "QlqR0uAWEAWF5Nwnzj5kqmmwFslYPu1IL16MKtLKhwhv0kpBv5wKZ_axf_nf_2cL"
)

func (sbs *snapBuildSuite) TestDecodeOK(c *C) {
	encoded := "type: snap-build\n" +
		"authority-id: dev-id1\n" +
		"snap-sha3-384: " + blobSHA3_384 + "\n" +
		"grade: stable\n" +
		"snap-id: snap-id-1\n" +
		"snap-size: 10000\n" +
		sbs.tsLine +
		"body-length: 0\n" +
		"sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij" +
		"\n\n" +
		"AXNpZw=="
	a := mylog.Check2(asserts.Decode([]byte(encoded)))

	c.Check(a.Type(), Equals, asserts.SnapBuildType)
	snapBuild := a.(*asserts.SnapBuild)
	c.Check(snapBuild.AuthorityID(), Equals, "dev-id1")
	c.Check(snapBuild.Timestamp(), Equals, sbs.ts)
	c.Check(snapBuild.SnapID(), Equals, "snap-id-1")
	c.Check(snapBuild.SnapSHA3_384(), Equals, blobSHA3_384)
	c.Check(snapBuild.SnapSize(), Equals, uint64(10000))
	c.Check(snapBuild.Grade(), Equals, "stable")
}

const (
	snapBuildErrPrefix = "assertion snap-build: "
)

func (sbs *snapBuildSuite) TestDecodeInvalid(c *C) {
	digestHdr := "snap-sha3-384: " + blobSHA3_384 + "\n"

	encoded := "type: snap-build\n" +
		"authority-id: dev-id1\n" +
		digestHdr +
		"grade: stable\n" +
		"snap-id: snap-id-1\n" +
		"snap-size: 10000\n" +
		sbs.tsLine +
		"body-length: 0\n" +
		"sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij" +
		"\n\n" +
		"AXNpZw=="

	invalidTests := []struct{ original, invalid, expectedErr string }{
		{"snap-id: snap-id-1\n", "", `"snap-id" header is mandatory`},
		{"snap-id: snap-id-1\n", "snap-id: \n", `"snap-id" header should not be empty`},
		{digestHdr, "", `"snap-sha3-384" header is mandatory`},
		{digestHdr, "snap-sha3-384: \n", `"snap-sha3-384" header should not be empty`},
		{digestHdr, "snap-sha3-384: #\n", `"snap-sha3-384" header cannot be decoded:.*`},
		{"snap-size: 10000\n", "", `"snap-size" header is mandatory`},
		{"snap-size: 10000\n", "snap-size: -1\n", `"snap-size" header is not an unsigned integer: -1`},
		{"snap-size: 10000\n", "snap-size: zzz\n", `"snap-size" header is not an unsigned integer: zzz`},
		{"snap-size: 10000\n", "snap-size: 010\n", `"snap-size" header has invalid prefix zeros: 010`},
		{"snap-size: 10000\n", "snap-size: 99999999999999999999\n", `"snap-size" header is out of range: 99999999999999999999`},
		{"grade: stable\n", "", `"grade" header is mandatory`},
		{"grade: stable\n", "grade: \n", `"grade" header should not be empty`},
		{sbs.tsLine, "", `"timestamp" header is mandatory`},
		{sbs.tsLine, "timestamp: \n", `"timestamp" header should not be empty`},
		{sbs.tsLine, "timestamp: 12:30\n", `"timestamp" header is not a RFC3339 date: .*`},
	}

	for _, test := range invalidTests {
		invalid := strings.Replace(encoded, test.original, test.invalid, 1)
		_ := mylog.Check2(asserts.Decode([]byte(invalid)))
		c.Check(err, ErrorMatches, snapBuildErrPrefix+test.expectedErr)
	}
}

func makeStoreAndCheckDB(c *C) (store *assertstest.StoreStack, checkDB *asserts.Database) {
	store = assertstest.NewStoreStack("canonical", nil)
	cfg := &asserts.DatabaseConfig{
		Backstore:       asserts.NewMemoryBackstore(),
		Trusted:         store.Trusted,
		OtherPredefined: store.Generic,
	}
	checkDB := mylog.Check2(asserts.OpenDatabase(cfg))

	mylog.

		// add store key
		Check(checkDB.Add(store.StoreAccountKey("")))

	mylog.
		// add generic key
		Check(checkDB.Add(store.GenericKey))


	return store, checkDB
}

func setup3rdPartySigning(c *C, username string, storeDB assertstest.SignerDB, checkDB *asserts.Database) (signingDB *assertstest.SigningDB) {
	privKey := testPrivKey2

	acct := assertstest.NewAccount(storeDB, username, map[string]interface{}{
		"account-id": username,
	}, "")
	accKey := assertstest.NewAccountKey(storeDB, acct, nil, privKey.PublicKey(), "")
	mylog.Check(checkDB.Add(acct))

	mylog.Check(checkDB.Add(accKey))


	return assertstest.NewSigningDB(acct.AccountID(), privKey)
}

func (sbs *snapBuildSuite) TestSnapBuildCheck(c *C) {
	storeDB, db := makeStoreAndCheckDB(c)
	devDB := setup3rdPartySigning(c, "devel1", storeDB, db)

	headers := map[string]interface{}{
		"authority-id":  "devel1",
		"snap-sha3-384": blobSHA3_384,
		"snap-id":       "snap-id-1",
		"grade":         "devel",
		"snap-size":     "1025",
		"timestamp":     time.Now().Format(time.RFC3339),
	}
	snapBuild := mylog.Check2(devDB.Sign(asserts.SnapBuildType, headers, nil, ""))

	mylog.Check(db.Check(snapBuild))

}

func (sbs *snapBuildSuite) TestSnapBuildCheckInconsistentTimestamp(c *C) {
	storeDB, db := makeStoreAndCheckDB(c)
	devDB := setup3rdPartySigning(c, "devel1", storeDB, db)

	headers := map[string]interface{}{
		"snap-sha3-384": blobSHA3_384,
		"snap-id":       "snap-id-1",
		"grade":         "devel",
		"snap-size":     "1025",
		"timestamp":     "2013-01-01T14:00:00Z",
	}
	snapBuild := mylog.Check2(devDB.Sign(asserts.SnapBuildType, headers, nil, ""))

	mylog.Check(db.Check(snapBuild))
	c.Assert(err, ErrorMatches, `snap-build assertion timestamp "2013-01-01 14:00:00 \+0000 UTC" outside of signing key validity \(key valid since.*\)`)
}

type snapRevSuite struct {
	ts     time.Time
	tsLine string
}

func (srs *snapRevSuite) SetUpSuite(c *C) {
	srs.ts = time.Now().Truncate(time.Second).UTC()
	srs.tsLine = "timestamp: " + srs.ts.Format(time.RFC3339) + "\n"
}

func (srs *snapRevSuite) makeValidEncoded() string {
	return "type: snap-revision\n" +
		"authority-id: store-id1\n" +
		"snap-sha3-384: " + blobSHA3_384 + "\n" +
		"snap-id: snap-id-1\n" +
		"snap-size: 123\n" +
		"snap-revision: 1\n" +
		"developer-id: dev-id1\n" +
		"revision: 1\n" +
		srs.tsLine +
		"body-length: 0\n" +
		"sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij" +
		"\n\n" +
		"AXNpZw=="
}

func (srs *snapRevSuite) makeValidEncodedWithIntegrity() string {
	return "type: snap-revision\n" +
		"authority-id: store-id1\n" +
		"snap-sha3-384: " + blobSHA3_384 + "\n" +
		"snap-id: snap-id-1\n" +
		"snap-size: 123\n" +
		"snap-revision: 1\n" +
		"integrity:\n" +
		"  sha3-384: " + blobSHA3_384 + "\n" +
		"  size: 128\n" +
		"developer-id: dev-id1\n" +
		"revision: 1\n" +
		srs.tsLine +
		"body-length: 0\n" +
		"sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij" +
		"\n\n" +
		"AXNpZw=="
}

func makeSnapRevisionHeaders(overrides map[string]interface{}) map[string]interface{} {
	headers := map[string]interface{}{
		"authority-id":  "canonical",
		"snap-sha3-384": blobSHA3_384,
		"snap-id":       "snap-id-1",
		"snap-size":     "123",
		"snap-revision": "1",
		"developer-id":  "dev-id1",
		"revision":      "1",
		"timestamp":     time.Now().Format(time.RFC3339),
	}
	for k, v := range overrides {
		headers[k] = v
	}
	return headers
}

func (srs *snapRevSuite) makeHeaders(overrides map[string]interface{}) map[string]interface{} {
	return makeSnapRevisionHeaders(overrides)
}

func (srs *snapRevSuite) TestDecodeOK(c *C) {
	encoded := srs.makeValidEncoded()
	a := mylog.Check2(asserts.Decode([]byte(encoded)))

	c.Check(a.Type(), Equals, asserts.SnapRevisionType)
	snapRev := a.(*asserts.SnapRevision)
	c.Check(snapRev.AuthorityID(), Equals, "store-id1")
	c.Check(snapRev.Timestamp(), Equals, srs.ts)
	c.Check(snapRev.SnapID(), Equals, "snap-id-1")
	c.Check(snapRev.SnapSHA3_384(), Equals, blobSHA3_384)
	c.Check(snapRev.SnapSize(), Equals, uint64(123))
	c.Check(snapRev.SnapRevision(), Equals, 1)
	c.Check(snapRev.DeveloperID(), Equals, "dev-id1")
	c.Check(snapRev.Revision(), Equals, 1)
	c.Check(snapRev.Provenance(), Equals, "global-upload")
}

func (srs *snapRevSuite) TestDecodeOKWithProvenance(c *C) {
	encoded := srs.makeValidEncoded()
	encoded = strings.Replace(encoded, "snap-id: snap-id-1", "provenance: foo\nsnap-id: snap-id-1", 1)
	a := mylog.Check2(asserts.Decode([]byte(encoded)))

	c.Check(a.Type(), Equals, asserts.SnapRevisionType)
	snapRev := a.(*asserts.SnapRevision)
	c.Check(snapRev.AuthorityID(), Equals, "store-id1")
	c.Check(snapRev.Timestamp(), Equals, srs.ts)
	c.Check(snapRev.SnapID(), Equals, "snap-id-1")
	c.Check(snapRev.SnapSHA3_384(), Equals, blobSHA3_384)
	c.Check(snapRev.SnapSize(), Equals, uint64(123))
	c.Check(snapRev.SnapRevision(), Equals, 1)
	c.Check(snapRev.DeveloperID(), Equals, "dev-id1")
	c.Check(snapRev.Revision(), Equals, 1)
	c.Check(snapRev.Provenance(), Equals, "foo")
}

func (srs *snapRevSuite) TestDecodeOKWithIntegrity(c *C) {
	encoded := srs.makeValidEncodedWithIntegrity()
	a := mylog.Check2(asserts.Decode([]byte(encoded)))

	c.Check(a.Type(), Equals, asserts.SnapRevisionType)
	snapRev := a.(*asserts.SnapRevision)
	c.Check(snapRev.AuthorityID(), Equals, "store-id1")
	c.Check(snapRev.Timestamp(), Equals, srs.ts)
	c.Check(snapRev.SnapID(), Equals, "snap-id-1")
	c.Check(snapRev.SnapSHA3_384(), Equals, blobSHA3_384)
	c.Check(snapRev.SnapSize(), Equals, uint64(123))
	c.Check(snapRev.SnapRevision(), Equals, 1)
	c.Check(snapRev.DeveloperID(), Equals, "dev-id1")
	c.Check(snapRev.Revision(), Equals, 1)
	c.Check(snapRev.Provenance(), Equals, "global-upload")
	c.Check(snapRev.SnapIntegrity().SHA3_384, Equals, blobSHA3_384)
	c.Check(snapRev.SnapIntegrity().Size, Equals, uint64(128))
}

const (
	snapRevErrPrefix = "assertion snap-revision: "
)

func (srs *snapRevSuite) TestDecodeInvalid(c *C) {
	encoded := srs.makeValidEncoded()

	digestHdr := "snap-sha3-384: " + blobSHA3_384 + "\n"
	invalidTests := []struct{ original, invalid, expectedErr string }{
		{"snap-id: snap-id-1\n", "", `"snap-id" header is mandatory`},
		{"snap-id: snap-id-1\n", "snap-id: \n", `"snap-id" header should not be empty`},
		{digestHdr, "", `"snap-sha3-384" header is mandatory`},
		{digestHdr, "snap-sha3-384: \n", `"snap-sha3-384" header should not be empty`},
		{digestHdr, "snap-sha3-384: #\n", `"snap-sha3-384" header cannot be decoded:.*`},
		{digestHdr, "snap-sha3-384: eHl6\n", `"snap-sha3-384" header does not have the expected bit length: 24`},
		{"snap-id: snap-id-1\n", "provenance: \nsnap-id: snap-id-1\n", `"provenance" header should not be empty`},
		{"snap-id: snap-id-1\n", "provenance: *\nsnap-id: snap-id-1\n", `"provenance" header contains invalid characters: "\*"`},
		{"snap-size: 123\n", "", `"snap-size" header is mandatory`},
		{"snap-size: 123\n", "snap-size: \n", `"snap-size" header should not be empty`},
		{"snap-size: 123\n", "snap-size: -1\n", `"snap-size" header is not an unsigned integer: -1`},
		{"snap-size: 123\n", "snap-size: zzz\n", `"snap-size" header is not an unsigned integer: zzz`},
		{"snap-revision: 1\n", "", `"snap-revision" header is mandatory`},
		{"snap-revision: 1\n", "snap-revision: \n", `"snap-revision" header should not be empty`},
		{"snap-revision: 1\n", "snap-revision: -1\n", `"snap-revision" header must be >=1: -1`},
		{"snap-revision: 1\n", "snap-revision: 0\n", `"snap-revision" header must be >=1: 0`},
		{"snap-revision: 1\n", "snap-revision: zzz\n", `"snap-revision" header is not an integer: zzz`},
		{"developer-id: dev-id1\n", "", `"developer-id" header is mandatory`},
		{"developer-id: dev-id1\n", "developer-id: \n", `"developer-id" header should not be empty`},
		{srs.tsLine, "", `"timestamp" header is mandatory`},
		{srs.tsLine, "timestamp: \n", `"timestamp" header should not be empty`},
		{srs.tsLine, "timestamp: 12:30\n", `"timestamp" header is not a RFC3339 date: .*`},
	}

	for _, test := range invalidTests {
		invalid := strings.Replace(encoded, test.original, test.invalid, 1)
		_ := mylog.Check2(asserts.Decode([]byte(invalid)))
		c.Check(err, ErrorMatches, snapRevErrPrefix+test.expectedErr)
	}
}

func (srs *snapRevSuite) TestDecodeInvalidWithIntegrity(c *C) {
	encoded := srs.makeValidEncodedWithIntegrity()

	integrityHdr := "integrity:\n" +
		"  sha3-384: " + blobSHA3_384 + "\n" +
		"  size: 128\n"

	integrityShaHdr := "  sha3-384: " + blobSHA3_384 + "\n"

	integritySizeHdr := "  size: 128\n"

	invalidTests := []struct{ original, invalid, expectedErr string }{
		{integrityHdr, "integrity: \n", `"integrity" header must be a map`},
		{integrityShaHdr, "  sha3-384: \n", `"sha3-384" of integrity header should not be empty`},
		{integrityShaHdr, "  sha3-384: #\n", `"sha3-384" of integrity header cannot be decoded:.*`},
		{integrityShaHdr, "  sha3-384: eHl6\n", `"sha3-384" of integrity header does not have the expected bit length: 24`},
		{integritySizeHdr, "", `"size" of integrity header is mandatory`},
		{integritySizeHdr, "  size: \n", `"size" of integrity header should not be empty`},
		{integritySizeHdr, "  size: -1\n", `"size" of integrity header is not an unsigned integer: -1`},
		{integritySizeHdr, "  size: zzz\n", `"size" of integrity header is not an unsigned integer: zzz`},
	}

	for _, test := range invalidTests {
		invalid := strings.Replace(encoded, test.original, test.invalid, 1)
		_ := mylog.Check2(asserts.Decode([]byte(invalid)))
		c.Check(err, ErrorMatches, snapRevErrPrefix+test.expectedErr)
	}
}

func prereqSnapDecl(c *C, storeDB assertstest.SignerDB, db *asserts.Database) {
	snapDecl := mylog.Check2(storeDB.Sign(asserts.SnapDeclarationType, map[string]interface{}{
		"series":       "16",
		"snap-id":      "snap-id-1",
		"snap-name":    "foo",
		"publisher-id": "dev-id1",
		"timestamp":    time.Now().Format(time.RFC3339),
	}, nil, ""))

	mylog.Check(db.Add(snapDecl))

}

func (srs *snapRevSuite) TestSnapRevisionCheck(c *C) {
	storeDB, db := makeStoreAndCheckDB(c)

	prereqDevAccount(c, storeDB, db)
	prereqSnapDecl(c, storeDB, db)

	headers := srs.makeHeaders(nil)
	snapRev := mylog.Check2(storeDB.Sign(asserts.SnapRevisionType, headers, nil, ""))

	mylog.Check(db.Check(snapRev))

}

func (srs *snapRevSuite) TestSnapRevisionCheckInconsistentTimestamp(c *C) {
	storeDB, db := makeStoreAndCheckDB(c)

	headers := srs.makeHeaders(map[string]interface{}{
		"timestamp": "2013-01-01T14:00:00Z",
	})
	snapRev := mylog.Check2(storeDB.Sign(asserts.SnapRevisionType, headers, nil, ""))

	mylog.Check(db.Check(snapRev))
	c.Assert(err, ErrorMatches, `snap-revision assertion timestamp "2013-01-01 14:00:00 \+0000 UTC" outside of signing key validity \(key valid since.*\)`)
}

func (srs *snapRevSuite) TestSnapRevisionCheckUntrustedAuthority(c *C) {
	storeDB, db := makeStoreAndCheckDB(c)

	otherDB := setup3rdPartySigning(c, "other", storeDB, db)

	headers := srs.makeHeaders(map[string]interface{}{
		"authority-id": "other",
	})
	snapRev := mylog.Check2(otherDB.Sign(asserts.SnapRevisionType, headers, nil, ""))

	mylog.Check(db.Check(snapRev))
	c.Assert(err, ErrorMatches, `snap-revision assertion for snap id "snap-id-1" is not signed by a store:.*`)
}

func (srs *snapRevSuite) TestSnapRevisionCheckMissingDeveloperAccount(c *C) {
	storeDB, db := makeStoreAndCheckDB(c)

	headers := srs.makeHeaders(nil)
	snapRev := mylog.Check2(storeDB.Sign(asserts.SnapRevisionType, headers, nil, ""))

	mylog.Check(db.Check(snapRev))
	c.Assert(err, ErrorMatches, `snap-revision assertion for snap id "snap-id-1" does not have a matching account assertion for the developer "dev-id1"`)
}

func (srs *snapRevSuite) TestSnapRevisionCheckMissingDeclaration(c *C) {
	storeDB, db := makeStoreAndCheckDB(c)

	prereqDevAccount(c, storeDB, db)

	headers := srs.makeHeaders(nil)
	snapRev := mylog.Check2(storeDB.Sign(asserts.SnapRevisionType, headers, nil, ""))

	mylog.Check(db.Check(snapRev))
	c.Assert(err, ErrorMatches, `snap-revision assertion for snap id "snap-id-1" does not have a matching snap-declaration assertion`)
}

func (srs *snapRevSuite) TestRevisionAuthorityCheck(c *C) {
	storeDB, db := makeStoreAndCheckDB(c)

	delegatedDB := setup3rdPartySigning(c, "delegated-id", storeDB, db)
	headers := srs.makeHeaders(map[string]interface{}{
		"authority-id":  "delegated-id",
		"developer-id":  "delegated-id",
		"snap-revision": "200",
		"provenance":    "prov1",
	})
	a := mylog.Check2(delegatedDB.Sign(asserts.SnapRevisionType, headers, nil, ""))

	snapRev := a.(*asserts.SnapRevision)

	tests := []struct {
		revAuth asserts.RevisionAuthority
		err     string
	}{
		{asserts.RevisionAuthority{
			AccountID:   "delegated-id",
			Provenance:  []string{"prov1", "prov2"},
			MinRevision: 1,
		}, ""},
		{asserts.RevisionAuthority{
			AccountID:   "delegated-id",
			Provenance:  []string{"prov1", "prov2"},
			MinRevision: 1,
			MaxRevision: 1000,
		}, ""},
		{asserts.RevisionAuthority{
			AccountID:   "delegated-id",
			Provenance:  []string{"prov2"},
			MinRevision: 1,
			MaxRevision: 1000,
		}, "provenance mismatch"},
		{asserts.RevisionAuthority{
			AccountID:   "delegated-id-2",
			Provenance:  []string{"prov1", "prov2"},
			MinRevision: 1,
			MaxRevision: 1000,
		}, "authority-id mismatch"},
		{asserts.RevisionAuthority{
			AccountID:   "delegated-id",
			Provenance:  []string{"prov1", "prov2"},
			MinRevision: 1000,
		}, "snap revision 200 is less than min-revision 1000"},
		{asserts.RevisionAuthority{
			AccountID:   "delegated-id",
			Provenance:  []string{"prov1", "prov2"},
			MinRevision: 10,
			MaxRevision: 110,
		}, "snap revision 200 is greater than max-revision 110"},
	}

	for _, t := range tests {
		mylog.Check(t.revAuth.Check(snapRev, nil, nil))
		if t.err == "" {
			c.Check(err, IsNil)
		} else {
			c.Check(err, ErrorMatches, t.err)
		}
	}
}

func (srs *snapRevSuite) TestRevisionAuthorityCheckDeviceScope(c *C) {
	a := mylog.Check2(asserts.Decode([]byte(`type: model
authority-id: my-brand
series: 16
brand-id: my-brand
model: my-model
store: substore
architecture: armhf
kernel: krnl
gadget: gadget
timestamp: 2018-09-12T12:00:00Z
sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij

AXNpZw==`)))

	myModel := a.(*asserts.Model)

	a = mylog.Check2(asserts.Decode([]byte(`type: store
store: substore
authority-id: canonical
operator-id: canonical
friendly-stores:
  - a-store
  - store1
  - store2
timestamp: 2018-09-12T12:00:00Z
sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij

AXNpZw==`)))

	substore := a.(*asserts.Store)

	storeDB, db := makeStoreAndCheckDB(c)

	delegatedDB := setup3rdPartySigning(c, "my-brand", storeDB, db)
	headers := srs.makeHeaders(map[string]interface{}{
		"authority-id":  "my-brand",
		"developer-id":  "my-brand",
		"snap-revision": "200",
		"provenance":    "prov1",
	})
	a = mylog.Check2(delegatedDB.Sign(asserts.SnapRevisionType, headers, nil, ""))

	snapRev := a.(*asserts.SnapRevision)

	tests := []struct {
		revAuth  asserts.RevisionAuthority
		substore *asserts.Store
		err      string
	}{
		{asserts.RevisionAuthority{
			AccountID:   "my-brand",
			Provenance:  []string{"prov1", "prov2"},
			MinRevision: 1,
		}, nil, ""},
		{asserts.RevisionAuthority{
			AccountID:   "my-brand",
			Provenance:  []string{"prov1", "prov2"},
			MinRevision: 1,
			DeviceScope: &asserts.DeviceScopeConstraint{
				Store: []string{"other-store"},
			},
		}, nil, "on-store mismatch"},
		{asserts.RevisionAuthority{
			AccountID:   "my-brand",
			Provenance:  []string{"prov1", "prov2"},
			MinRevision: 1,
			DeviceScope: &asserts.DeviceScopeConstraint{
				Store: []string{"substore"},
			},
		}, nil, ""},
		{asserts.RevisionAuthority{
			AccountID:   "my-brand",
			Provenance:  []string{"prov1", "prov2"},
			MinRevision: 1,
			DeviceScope: &asserts.DeviceScopeConstraint{
				Store: []string{"substore"},
			},
		}, substore, ""},
		{asserts.RevisionAuthority{
			AccountID:   "my-brand",
			Provenance:  []string{"prov1", "prov2"},
			MinRevision: 1,
			DeviceScope: &asserts.DeviceScopeConstraint{
				Store: []string{"a-store"},
			},
		}, substore, ""},
		{asserts.RevisionAuthority{
			AccountID:   "my-brand",
			Provenance:  []string{"prov1", "prov2"},
			MinRevision: 1,
			DeviceScope: &asserts.DeviceScopeConstraint{
				Store: []string{"store1"},
			},
		}, nil, "on-store mismatch"},
		{asserts.RevisionAuthority{
			AccountID:   "my-brand",
			Provenance:  []string{"prov1", "prov2"},
			MinRevision: 1,
			DeviceScope: &asserts.DeviceScopeConstraint{
				Store: []string{"store1", "other-store"},
			},
		}, substore, ""},
	}

	for _, t := range tests {
		mylog.Check(t.revAuth.Check(snapRev, myModel, t.substore))
		if t.err == "" {
			c.Check(err, IsNil)
		} else {
			c.Check(err, ErrorMatches, t.err)
		}
	}
}

func (srs *snapRevSuite) TestSnapRevisionDelegation(c *C) {
	storeDB, db := makeStoreAndCheckDB(c)

	delegatedDB := setup3rdPartySigning(c, "delegated-id", storeDB, db)

	snapDecl := mylog.Check2(storeDB.Sign(asserts.SnapDeclarationType, map[string]interface{}{
		"series":       "16",
		"snap-id":      "snap-id-1",
		"snap-name":    "foo",
		"publisher-id": "delegated-id",
		"timestamp":    time.Now().Format(time.RFC3339),
	}, nil, ""))

	mylog.Check(db.Add(snapDecl))


	headers := srs.makeHeaders(map[string]interface{}{
		"authority-id": "delegated-id",
		"developer-id": "delegated-id",
		"provenance":   "prov1",
	})
	snapRev := mylog.Check2(delegatedDB.Sign(asserts.SnapRevisionType, headers, nil, ""))

	mylog.Check(db.Check(snapRev))
	c.Check(err, ErrorMatches, `snap-revision assertion with provenance "prov1" for snap id "snap-id-1" is not signed by an authorized authority: delegated-id`)

	// establish delegation
	snapDecl = mylog.Check2(storeDB.Sign(asserts.SnapDeclarationType, map[string]interface{}{
		"series":       "16",
		"snap-id":      "snap-id-1",
		"snap-name":    "foo",
		"publisher-id": "delegated-id",
		"revision":     "1",
		"revision-authority": []interface{}{
			map[string]interface{}{
				"account-id": "delegated-id",
				"provenance": []interface{}{
					"prov1",
				},
				// present but not checked at this level
				"on-store": []interface{}{
					"store1",
				},
			},
		},
		"timestamp": time.Now().Format(time.RFC3339),
	}, nil, ""))

	mylog.Check(db.Add(snapDecl))

	mylog.

		// now revision should be accepted
		Check(db.Check(snapRev))
	c.Check(err, IsNil)
}

func (srs *snapRevSuite) TestSnapRevisionDelegationRevisionOutOfRange(c *C) {
	storeDB, db := makeStoreAndCheckDB(c)

	delegatedDB := setup3rdPartySigning(c, "delegated-id", storeDB, db)

	// establish delegation
	snapDecl := mylog.Check2(storeDB.Sign(asserts.SnapDeclarationType, map[string]interface{}{
		"series":       "16",
		"snap-id":      "snap-id-1",
		"snap-name":    "foo",
		"publisher-id": "delegated-id",
		"revision-authority": []interface{}{
			map[string]interface{}{
				"account-id": "delegated-id",
				"provenance": []interface{}{
					"prov1",
				},
				// present but not checked at this level
				"on-store": []interface{}{
					"store1",
				},
				"max-revision": "200",
			},
		},
		"timestamp": time.Now().Format(time.RFC3339),
	}, nil, ""))

	mylog.Check(db.Add(snapDecl))


	headers := srs.makeHeaders(map[string]interface{}{
		"authority-id":  "delegated-id",
		"developer-id":  "delegated-id",
		"provenance":    "prov1",
		"snap-revision": "1000",
	})
	snapRev := mylog.Check2(delegatedDB.Sign(asserts.SnapRevisionType, headers, nil, ""))

	mylog.Check(db.Check(snapRev))
	c.Check(err, ErrorMatches, `snap-revision assertion with provenance "prov1" for snap id "snap-id-1" is not signed by an authorized authority: delegated-id`)
}

func (srs *snapRevSuite) TestPrimaryKey(c *C) {
	storeDB, db := makeStoreAndCheckDB(c)

	prereqDevAccount(c, storeDB, db)
	prereqSnapDecl(c, storeDB, db)

	headers := srs.makeHeaders(nil)
	snapRev := mylog.Check2(storeDB.Sign(asserts.SnapRevisionType, headers, nil, ""))

	mylog.Check(db.Add(snapRev))


	_ = mylog.Check2(db.Find(asserts.SnapRevisionType, map[string]string{
		"snap-sha3-384": headers["snap-sha3-384"].(string),
	}))

}

func (srs *snapRevSuite) TestPrerequisites(c *C) {
	encoded := srs.makeValidEncoded()
	a := mylog.Check2(asserts.Decode([]byte(encoded)))


	prereqs := a.Prerequisites()
	c.Assert(prereqs, HasLen, 2)
	c.Check(prereqs[0], DeepEquals, &asserts.Ref{
		Type:       asserts.SnapDeclarationType,
		PrimaryKey: []string{"16", "snap-id-1"},
	})
	c.Check(prereqs[1], DeepEquals, &asserts.Ref{
		Type:       asserts.AccountType,
		PrimaryKey: []string{"dev-id1"},
	})
}

type validationSuite struct {
	ts     time.Time
	tsLine string
}

func (vs *validationSuite) SetUpSuite(c *C) {
	vs.ts = time.Now().Truncate(time.Second).UTC()
	vs.tsLine = "timestamp: " + vs.ts.Format(time.RFC3339) + "\n"
}

func (vs *validationSuite) makeValidEncoded() string {
	return "type: validation\n" +
		"authority-id: dev-id1\n" +
		"series: 16\n" +
		"snap-id: snap-id-1\n" +
		"approved-snap-id: snap-id-2\n" +
		"approved-snap-revision: 42\n" +
		"revision: 1\n" +
		vs.tsLine +
		"sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij" +
		"\n\n" +
		"AXNpZw=="
}

func (vs *validationSuite) makeHeaders(overrides map[string]interface{}) map[string]interface{} {
	headers := map[string]interface{}{
		"authority-id":           "dev-id1",
		"series":                 "16",
		"snap-id":                "snap-id-1",
		"approved-snap-id":       "snap-id-2",
		"approved-snap-revision": "42",
		"revision":               "1",
		"timestamp":              time.Now().Format(time.RFC3339),
	}
	for k, v := range overrides {
		headers[k] = v
	}
	return headers
}

func (vs *validationSuite) TestDecodeOK(c *C) {
	encoded := vs.makeValidEncoded()
	a := mylog.Check2(asserts.Decode([]byte(encoded)))

	c.Check(a.Type(), Equals, asserts.ValidationType)
	validation := a.(*asserts.Validation)
	c.Check(validation.AuthorityID(), Equals, "dev-id1")
	c.Check(validation.Timestamp(), Equals, vs.ts)
	c.Check(validation.Series(), Equals, "16")
	c.Check(validation.SnapID(), Equals, "snap-id-1")
	c.Check(validation.ApprovedSnapID(), Equals, "snap-id-2")
	c.Check(validation.ApprovedSnapRevision(), Equals, 42)
	c.Check(validation.Revoked(), Equals, false)
	c.Check(validation.Revision(), Equals, 1)
}

const (
	validationErrPrefix = "assertion validation: "
)

func (vs *validationSuite) TestDecodeInvalid(c *C) {
	encoded := vs.makeValidEncoded()

	invalidTests := []struct{ original, invalid, expectedErr string }{
		{"series: 16\n", "", `"series" header is mandatory`},
		{"series: 16\n", "series: \n", `"series" header should not be empty`},
		{"snap-id: snap-id-1\n", "", `"snap-id" header is mandatory`},
		{"snap-id: snap-id-1\n", "snap-id: \n", `"snap-id" header should not be empty`},
		{"approved-snap-id: snap-id-2\n", "", `"approved-snap-id" header is mandatory`},
		{"approved-snap-id: snap-id-2\n", "approved-snap-id: \n", `"approved-snap-id" header should not be empty`},
		{"approved-snap-revision: 42\n", "", `"approved-snap-revision" header is mandatory`},
		{"approved-snap-revision: 42\n", "approved-snap-revision: z\n", `"approved-snap-revision" header is not an integer: z`},
		{"approved-snap-revision: 42\n", "approved-snap-revision: 0\n", `"approved-snap-revision" header must be >=1: 0`},
		{"approved-snap-revision: 42\n", "approved-snap-revision: -1\n", `"approved-snap-revision" header must be >=1: -1`},
		{vs.tsLine, "", `"timestamp" header is mandatory`},
		{vs.tsLine, "timestamp: \n", `"timestamp" header should not be empty`},
		{vs.tsLine, "timestamp: 12:30\n", `"timestamp" header is not a RFC3339 date: .*`},
	}

	for _, test := range invalidTests {
		invalid := strings.Replace(encoded, test.original, test.invalid, 1)
		_ := mylog.Check2(asserts.Decode([]byte(invalid)))
		c.Check(err, ErrorMatches, validationErrPrefix+test.expectedErr)
	}
}

func prereqSnapDecl2(c *C, storeDB assertstest.SignerDB, db *asserts.Database) {
	snapDecl := mylog.Check2(storeDB.Sign(asserts.SnapDeclarationType, map[string]interface{}{
		"series":       "16",
		"snap-id":      "snap-id-2",
		"snap-name":    "bar",
		"publisher-id": "dev-id1",
		"timestamp":    time.Now().Format(time.RFC3339),
	}, nil, ""))

	mylog.Check(db.Add(snapDecl))

}

func (vs *validationSuite) TestValidationCheck(c *C) {
	storeDB, db := makeStoreAndCheckDB(c)
	devDB := setup3rdPartySigning(c, "dev-id1", storeDB, db)

	prereqSnapDecl(c, storeDB, db)
	prereqSnapDecl2(c, storeDB, db)

	headers := vs.makeHeaders(nil)
	validation := mylog.Check2(devDB.Sign(asserts.ValidationType, headers, nil, ""))

	mylog.Check(db.Check(validation))

}

func (vs *validationSuite) TestValidationCheckWrongAuthority(c *C) {
	storeDB, db := makeStoreAndCheckDB(c)

	prereqDevAccount(c, storeDB, db)
	prereqSnapDecl(c, storeDB, db)
	prereqSnapDecl2(c, storeDB, db)

	headers := vs.makeHeaders(map[string]interface{}{
		"authority-id": "canonical", // not the publisher
	})
	validation := mylog.Check2(storeDB.Sign(asserts.ValidationType, headers, nil, ""))

	mylog.Check(db.Check(validation))
	c.Assert(err, ErrorMatches, `validation assertion by snap "foo" \(id "snap-id-1"\) not signed by its publisher`)
}

func (vs *validationSuite) TestRevocation(c *C) {
	encoded := "type: validation\n" +
		"authority-id: dev-id1\n" +
		"series: 16\n" +
		"snap-id: snap-id-1\n" +
		"approved-snap-id: snap-id-2\n" +
		"approved-snap-revision: 42\n" +
		"revoked: true\n" +
		"revision: 1\n" +
		vs.tsLine +
		"sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij" +
		"\n\n" +
		"AXNpZw=="
	a := mylog.Check2(asserts.Decode([]byte(encoded)))

	validation := a.(*asserts.Validation)
	c.Check(validation.Revoked(), Equals, true)
}

func (vs *validationSuite) TestRevokedFalse(c *C) {
	encoded := "type: validation\n" +
		"authority-id: dev-id1\n" +
		"series: 16\n" +
		"snap-id: snap-id-1\n" +
		"approved-snap-id: snap-id-2\n" +
		"approved-snap-revision: 42\n" +
		"revoked: false\n" +
		"revision: 1\n" +
		vs.tsLine +
		"sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij" +
		"\n\n" +
		"AXNpZw=="
	a := mylog.Check2(asserts.Decode([]byte(encoded)))

	validation := a.(*asserts.Validation)
	c.Check(validation.Revoked(), Equals, false)
}

func (vs *validationSuite) TestRevokedInvalid(c *C) {
	encoded := "type: validation\n" +
		"authority-id: dev-id1\n" +
		"series: 16\n" +
		"snap-id: snap-id-1\n" +
		"approved-snap-id: snap-id-2\n" +
		"approved-snap-revision: 42\n" +
		"revoked: foo\n" +
		"revision: 1\n" +
		vs.tsLine +
		"sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij" +
		"\n\n" +
		"AXNpZw=="
	_ := mylog.Check2(asserts.Decode([]byte(encoded)))
	c.Check(err, ErrorMatches, `.*: "revoked" header must be 'true' or 'false'`)
}

func (vs *validationSuite) TestMissingGatedSnapDeclaration(c *C) {
	storeDB, db := makeStoreAndCheckDB(c)
	devDB := setup3rdPartySigning(c, "dev-id1", storeDB, db)

	headers := vs.makeHeaders(nil)
	a := mylog.Check2(devDB.Sign(asserts.ValidationType, headers, nil, ""))

	mylog.Check(db.Check(a))
	c.Assert(err, ErrorMatches, `validation assertion by snap-id "snap-id-1" does not have a matching snap-declaration assertion for approved-snap-id "snap-id-2"`)
}

func (vs *validationSuite) TestMissingGatingSnapDeclaration(c *C) {
	storeDB, db := makeStoreAndCheckDB(c)
	devDB := setup3rdPartySigning(c, "dev-id1", storeDB, db)

	prereqSnapDecl2(c, storeDB, db)

	headers := vs.makeHeaders(nil)
	a := mylog.Check2(devDB.Sign(asserts.ValidationType, headers, nil, ""))

	mylog.Check(db.Check(a))
	c.Assert(err, ErrorMatches, `validation assertion by snap-id "snap-id-1" does not have a matching snap-declaration assertion`)
}

func (vs *validationSuite) TestPrerequisites(c *C) {
	encoded := vs.makeValidEncoded()
	a := mylog.Check2(asserts.Decode([]byte(encoded)))


	prereqs := a.Prerequisites()
	c.Assert(prereqs, HasLen, 2)
	c.Check(prereqs[0], DeepEquals, &asserts.Ref{
		Type:       asserts.SnapDeclarationType,
		PrimaryKey: []string{"16", "snap-id-1"},
	})
	c.Check(prereqs[1], DeepEquals, &asserts.Ref{
		Type:       asserts.SnapDeclarationType,
		PrimaryKey: []string{"16", "snap-id-2"},
	})
}

type baseDeclSuite struct{}

func (s *baseDeclSuite) TestDecodeOK(c *C) {
	encoded := `type: base-declaration
authority-id: canonical
series: 16
plugs:
  interface1:
    deny-installation: false
    allow-auto-connection:
      slot-snap-type:
        - app
      slot-publisher-id:
        - acme
      slot-attributes:
        a1: /foo/.*
      plug-attributes:
        b1: B1
    deny-auto-connection:
      slot-attributes:
        a1: !A1
      plug-attributes:
        b1: !B1
  interface2:
    allow-installation: true
    allow-connection:
      plug-attributes:
        a2: A2
      slot-attributes:
        b2: B2
    deny-connection:
      slot-snap-id:
        - snapidsnapidsnapidsnapidsnapid01
        - snapidsnapidsnapidsnapidsnapid02
      plug-attributes:
        a2: !A2
      slot-attributes:
        b2: !B2
slots:
  interface3:
    deny-installation: false
    allow-auto-connection:
      plug-snap-type:
        - app
      plug-publisher-id:
        - acme
      slot-attributes:
        c1: /foo/.*
      plug-attributes:
        d1: C1
    deny-auto-connection:
      slot-attributes:
        c1: !C1
      plug-attributes:
        d1: !D1
  interface4:
    allow-connection:
      plug-attributes:
        c2: C2
      slot-attributes:
        d2: D2
    deny-connection:
      plug-snap-id:
        - snapidsnapidsnapidsnapidsnapid01
        - snapidsnapidsnapidsnapidsnapid02
      plug-attributes:
        c2: !D2
      slot-attributes:
        d2: !D2
    allow-installation:
      slot-snap-type:
        - app
      slot-attributes:
        e1: E1
timestamp: 2016-09-29T19:50:49Z
sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij

AXNpZw==`
	a := mylog.Check2(asserts.Decode([]byte(encoded)))

	baseDecl := a.(*asserts.BaseDeclaration)
	c.Check(baseDecl.Series(), Equals, "16")
	ts := mylog.Check2(time.Parse(time.RFC3339, "2016-09-29T19:50:49Z"))

	c.Check(baseDecl.Timestamp().Equal(ts), Equals, true)

	c.Check(baseDecl.PlugRule("interfaceX"), IsNil)
	c.Check(baseDecl.SlotRule("interfaceX"), IsNil)

	plug := emptyAttrerObject{}
	slot := emptyAttrerObject{}

	plugRule1 := baseDecl.PlugRule("interface1")
	c.Assert(plugRule1, NotNil)
	c.Assert(plugRule1.DenyInstallation, HasLen, 1)
	c.Check(plugRule1.DenyInstallation[0].PlugAttributes, Equals, asserts.NeverMatchAttributes)
	c.Assert(plugRule1.AllowAutoConnection, HasLen, 1)
	c.Check(plugRule1.AllowAutoConnection[0].SlotAttributes.Check(slot, nil), ErrorMatches, `attribute "a1".*`)
	c.Check(plugRule1.AllowAutoConnection[0].PlugAttributes.Check(plug, nil), ErrorMatches, `attribute "b1".*`)
	c.Check(plugRule1.AllowAutoConnection[0].SlotSnapTypes, DeepEquals, []string{"app"})
	c.Check(plugRule1.AllowAutoConnection[0].SlotPublisherIDs, DeepEquals, []string{"acme"})
	c.Assert(plugRule1.DenyAutoConnection, HasLen, 1)
	c.Check(plugRule1.DenyAutoConnection[0].SlotAttributes.Check(slot, nil), ErrorMatches, `attribute "a1".*`)
	c.Check(plugRule1.DenyAutoConnection[0].PlugAttributes.Check(plug, nil), ErrorMatches, `attribute "b1".*`)
	plugRule2 := baseDecl.PlugRule("interface2")
	c.Assert(plugRule2, NotNil)
	c.Assert(plugRule2.AllowInstallation, HasLen, 1)
	c.Check(plugRule2.AllowInstallation[0].PlugAttributes, Equals, asserts.AlwaysMatchAttributes)
	c.Assert(plugRule2.AllowConnection, HasLen, 1)
	c.Check(plugRule2.AllowConnection[0].PlugAttributes.Check(plug, nil), ErrorMatches, `attribute "a2".*`)
	c.Check(plugRule2.AllowConnection[0].SlotAttributes.Check(slot, nil), ErrorMatches, `attribute "b2".*`)
	c.Assert(plugRule2.DenyConnection, HasLen, 1)
	c.Check(plugRule2.DenyConnection[0].PlugAttributes.Check(plug, nil), ErrorMatches, `attribute "a2".*`)
	c.Check(plugRule2.DenyConnection[0].SlotAttributes.Check(slot, nil), ErrorMatches, `attribute "b2".*`)
	c.Check(plugRule2.DenyConnection[0].SlotSnapIDs, DeepEquals, []string{"snapidsnapidsnapidsnapidsnapid01", "snapidsnapidsnapidsnapidsnapid02"})

	slotRule3 := baseDecl.SlotRule("interface3")
	c.Assert(slotRule3, NotNil)
	c.Assert(slotRule3.DenyInstallation, HasLen, 1)
	c.Check(slotRule3.DenyInstallation[0].SlotAttributes, Equals, asserts.NeverMatchAttributes)
	c.Assert(slotRule3.AllowAutoConnection, HasLen, 1)
	c.Check(slotRule3.AllowAutoConnection[0].SlotAttributes.Check(slot, nil), ErrorMatches, `attribute "c1".*`)
	c.Check(slotRule3.AllowAutoConnection[0].PlugAttributes.Check(plug, nil), ErrorMatches, `attribute "d1".*`)
	c.Check(slotRule3.AllowAutoConnection[0].PlugSnapTypes, DeepEquals, []string{"app"})
	c.Check(slotRule3.AllowAutoConnection[0].PlugPublisherIDs, DeepEquals, []string{"acme"})
	c.Assert(slotRule3.DenyAutoConnection, HasLen, 1)
	c.Check(slotRule3.DenyAutoConnection[0].SlotAttributes.Check(slot, nil), ErrorMatches, `attribute "c1".*`)
	c.Check(slotRule3.DenyAutoConnection[0].PlugAttributes.Check(plug, nil), ErrorMatches, `attribute "d1".*`)
	slotRule4 := baseDecl.SlotRule("interface4")
	c.Assert(slotRule4, NotNil)
	c.Assert(slotRule4.AllowConnection, HasLen, 1)
	c.Check(slotRule4.AllowConnection[0].PlugAttributes.Check(plug, nil), ErrorMatches, `attribute "c2".*`)
	c.Check(slotRule4.AllowConnection[0].SlotAttributes.Check(slot, nil), ErrorMatches, `attribute "d2".*`)
	c.Assert(slotRule4.DenyConnection, HasLen, 1)
	c.Check(slotRule4.DenyConnection[0].PlugAttributes.Check(plug, nil), ErrorMatches, `attribute "c2".*`)
	c.Check(slotRule4.DenyConnection[0].SlotAttributes.Check(slot, nil), ErrorMatches, `attribute "d2".*`)
	c.Check(slotRule4.DenyConnection[0].PlugSnapIDs, DeepEquals, []string{"snapidsnapidsnapidsnapidsnapid01", "snapidsnapidsnapidsnapidsnapid02"})
	c.Assert(slotRule4.AllowInstallation, HasLen, 1)
	c.Check(slotRule4.AllowInstallation[0].SlotAttributes.Check(slot, nil), ErrorMatches, `attribute "e1".*`)
	c.Check(slotRule4.AllowInstallation[0].SlotSnapTypes, DeepEquals, []string{"app"})
}

func (s *baseDeclSuite) TestBaseDeclarationCheckUntrustedAuthority(c *C) {
	storeDB, db := makeStoreAndCheckDB(c)

	otherDB := setup3rdPartySigning(c, "other", storeDB, db)

	headers := map[string]interface{}{
		"series":    "16",
		"timestamp": time.Now().Format(time.RFC3339),
	}
	baseDecl := mylog.Check2(otherDB.Sign(asserts.BaseDeclarationType, headers, nil, ""))

	mylog.Check(db.Check(baseDecl))
	c.Assert(err, ErrorMatches, `base-declaration assertion for series 16 is not signed by a directly trusted authority: other`)
}

const (
	baseDeclErrPrefix = "assertion base-declaration: "
)

func (s *baseDeclSuite) TestDecodeInvalid(c *C) {
	tsLine := "timestamp: 2016-09-29T19:50:49Z\n"

	encoded := "type: base-declaration\n" +
		"authority-id: canonical\n" +
		"series: 16\n" +
		"plugs:\n  interface1: true\n" +
		"slots:\n  interface2: true\n" +
		tsLine +
		"sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij" +
		"\n\n" +
		"AXNpZw=="

	invalidTests := []struct{ original, invalid, expectedErr string }{
		{"series: 16\n", "", `"series" header is mandatory`},
		{"series: 16\n", "series: \n", `"series" header should not be empty`},
		{"plugs:\n  interface1: true\n", "plugs: \n", `"plugs" header must be a map`},
		{"plugs:\n  interface1: true\n", "plugs:\n  intf1:\n    foo: bar\n", `plug rule for interface "intf1" must specify at least one of.*`},
		{"slots:\n  interface2: true\n", "slots: \n", `"slots" header must be a map`},
		{"slots:\n  interface2: true\n", "slots:\n  intf1:\n    foo: bar\n", `slot rule for interface "intf1" must specify at least one of.*`},
		{tsLine, "", `"timestamp" header is mandatory`},
		{tsLine, "timestamp: 12:30\n", `"timestamp" header is not a RFC3339 date: .*`},
	}

	for _, test := range invalidTests {
		invalid := strings.Replace(encoded, test.original, test.invalid, 1)
		_ := mylog.Check2(asserts.Decode([]byte(invalid)))
		c.Check(err, ErrorMatches, baseDeclErrPrefix+test.expectedErr)
	}
}

func (s *baseDeclSuite) TestBuiltin(c *C) {
	baseDecl := asserts.BuiltinBaseDeclaration()
	c.Check(baseDecl, IsNil)

	defer asserts.InitBuiltinBaseDeclaration(nil)

	const headers = `
type: base-declaration
authority-id: canonical
series: 16
revision: 0
plugs:
  network: true
slots:
  network:
    allow-installation:
      slot-snap-type:
        - core
`
	mylog.Check(asserts.InitBuiltinBaseDeclaration([]byte(headers)))


	baseDecl = asserts.BuiltinBaseDeclaration()
	c.Assert(baseDecl, NotNil)

	cont, _ := baseDecl.Signature()
	c.Check(string(cont), Equals, strings.TrimSpace(headers))

	c.Check(baseDecl.AuthorityID(), Equals, "canonical")
	c.Check(baseDecl.Series(), Equals, "16")
	c.Check(baseDecl.PlugRule("network").AllowAutoConnection[0].SlotAttributes, Equals, asserts.AlwaysMatchAttributes)
	c.Check(baseDecl.SlotRule("network").AllowInstallation[0].SlotSnapTypes, DeepEquals, []string{"core"})

	enc := asserts.Encode(baseDecl)
	// it's expected that it cannot be decoded
	_ = mylog.Check2(asserts.Decode(enc))
	c.Check(err, NotNil)
}

func (s *baseDeclSuite) TestBuiltinInitErrors(c *C) {
	defer asserts.InitBuiltinBaseDeclaration(nil)

	tests := []struct {
		headers string
		err     string
	}{
		{"", `header entry missing ':' separator: ""`},
		{"type: foo\n", `the builtin base-declaration "type" header is not set to expected value "base-declaration"`},
		{"type: base-declaration", `the builtin base-declaration "authority-id" header is not set to expected value "canonical"`},
		{"type: base-declaration\nauthority-id: canonical", `the builtin base-declaration "series" header is not set to expected value "16"`},
		{"type: base-declaration\nauthority-id: canonical\nseries: 16\nrevision: zzz", `cannot assemble the builtin-base declaration: "revision" header is not an integer: zzz`},
		{"type: base-declaration\nauthority-id: canonical\nseries: 16\nplugs: foo", `cannot assemble the builtin base-declaration: "plugs" header must be a map`},
	}

	for _, t := range tests {
		mylog.Check(asserts.InitBuiltinBaseDeclaration([]byte(t.headers)))
		c.Check(err, ErrorMatches, t.err, Commentf(t.headers))
	}
}

type snapDevSuite struct {
	developersLines string
	validEncoded    string
}

func (sds *snapDevSuite) SetUpSuite(c *C) {
	sds.developersLines = "developers:\n  -\n    developer-id: dev-id2\n    since: 2017-01-01T00:00:00.0Z\n    until: 2017-02-01T00:00:00.0Z\n"
	sds.validEncoded = "type: snap-developer\n" +
		"authority-id: dev-id1\n" +
		"snap-id: snap-id-1\n" +
		"publisher-id: dev-id1\n" +
		sds.developersLines +
		"sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij" +
		"\n\n" +
		"AXNpZw=="
}

func (sds *snapDevSuite) TestDecodeOK(c *C) {
	encoded := sds.validEncoded
	a := mylog.Check2(asserts.Decode([]byte(encoded)))

	c.Check(a.Type(), Equals, asserts.SnapDeveloperType)
	snapDev := a.(*asserts.SnapDeveloper)
	c.Check(snapDev.AuthorityID(), Equals, "dev-id1")
	c.Check(snapDev.PublisherID(), Equals, "dev-id1")
	c.Check(snapDev.SnapID(), Equals, "snap-id-1")
}

func (sds *snapDevSuite) TestDevelopersOptional(c *C) {
	encoded := strings.Replace(sds.validEncoded, sds.developersLines, "", 1)
	_ := mylog.Check2(asserts.Decode([]byte(encoded)))
	c.Check(err, IsNil)
}

func (sds *snapDevSuite) TestDevelopersUntilOptional(c *C) {
	encoded := strings.Replace(
		sds.validEncoded, sds.developersLines,
		"developers:\n  -\n    developer-id: dev-id2\n    since: 2017-01-01T00:00:00.0Z\n", 1)
	_ := mylog.Check2(asserts.Decode([]byte(encoded)))
	c.Check(err, IsNil)
}

func (sds *snapDevSuite) TestDevelopersRevoked(c *C) {
	encoded := sds.validEncoded
	encoded = strings.Replace(
		encoded, sds.developersLines,
		"developers:\n  -\n    developer-id: dev-id2\n    since: 2017-01-01T00:00:00.0Z\n    until: 2017-01-01T00:00:00.0Z\n", 1)
	_ := mylog.Check2(asserts.Decode([]byte(encoded)))
	c.Check(err, IsNil)
	// TODO(matt): check actually revoked rather than just parsed
}

const (
	snapDevErrPrefix = "assertion snap-developer: "
)

func (sds *snapDevSuite) TestDecodeInvalid(c *C) {
	encoded := sds.validEncoded

	invalidTests := []struct{ original, invalid, expectedErr string }{
		{"publisher-id: dev-id1\n", "", `"publisher-id" header is mandatory`},
		{"publisher-id: dev-id1\n", "publisher-id: \n", `"publisher-id" header should not be empty`},
		{"snap-id: snap-id-1\n", "", `"snap-id" header is mandatory`},
		{"snap-id: snap-id-1\n", "snap-id: \n", `"snap-id" header should not be empty`},
		{sds.developersLines, "developers: \n", `"developers" must be a list of developer maps`},
		{sds.developersLines, "developers: foo\n", `"developers" must be a list of developer maps`},
		{sds.developersLines, "developers:\n  foo: bar\n", `"developers" must be a list of developer maps`},
		{sds.developersLines, "developers:\n  - foo\n", `"developers" must be a list of developer maps`},
		{sds.developersLines, "developers:\n  -\n    foo: bar\n", `"developer-id" in "developers" item 1 is mandatory`},
		{
			sds.developersLines, "developers:\n  -\n    developer-id: a\n",
			`"developer-id" in "developers" item 1 contains invalid characters: "a"`,
		},
		{
			sds.developersLines, "developers:\n  -\n    developer-id: dev-id2\n",
			`"since" in "developers" item 1 for developer "dev-id2" is mandatory`,
		},
		{
			sds.developersLines, "developers:\n  -\n    developer-id: dev-id2\n    since: \n",
			`"since" in "developers" item 1 for developer "dev-id2" should not be empty`,
		},
		{
			sds.developersLines, "developers:\n  -\n    developer-id: dev-id2\n    since: foo\n",
			`"since" in "developers" item 1 for developer "dev-id2" is not a RFC3339 date.*`,
		},
		{
			sds.developersLines, "developers:\n  -\n    developer-id: dev-id2\n    since: 2017-01-01T00:00:00.0Z\n    until: \n",
			`"until" in "developers" item 1 for developer "dev-id2" is not a RFC3339 date.*`,
		},
		{
			sds.developersLines, "developers:\n  -\n    developer-id: dev-id2\n    since: 2017-01-01T00:00:00.0Z\n    until: foo\n",
			`"until" in "developers" item 1 for developer "dev-id2" is not a RFC3339 date.*`,
		},
		{
			sds.developersLines, "developers:\n  -\n    developer-id: dev-id2\n    since: 2017-01-01T00:00:00.0Z\n  -\n    foo: bar\n",
			`"developer-id" in "developers" item 2 is mandatory`,
		},
		{
			sds.developersLines, "developers:\n  -\n    developer-id: dev-id2\n    since: 2017-01-02T00:00:00.0Z\n    until: 2017-01-01T00:00:00.0Z\n",
			`"since" in "developers" item 1 for developer "dev-id2" must be less than or equal to "until"`,
		},
	}

	for _, test := range invalidTests {
		invalid := strings.Replace(encoded, test.original, test.invalid, 1)
		_ := mylog.Check2(asserts.Decode([]byte(invalid)))
		c.Check(err, ErrorMatches, snapDevErrPrefix+test.expectedErr)
	}
}

func (sds *snapDevSuite) TestRevokedValidation(c *C) {
	// Multiple non-revoking items are fine.
	encoded := strings.Replace(sds.validEncoded, sds.developersLines,
		"developers:\n"+
			"  -\n    developer-id: dev-id2\n    since: 2017-01-01T00:00:00.0Z\n    until: 2017-02-01T00:00:00.0Z\n"+
			"  -\n    developer-id: dev-id2\n    since: 2017-03-01T00:00:00.0Z\n",
		1)
	_ := mylog.Check2(asserts.Decode([]byte(encoded)))
	c.Check(err, IsNil)

	// Multiple revocations for different developers are fine.
	encoded = strings.Replace(sds.validEncoded, sds.developersLines,
		"developers:\n"+
			"  -\n    developer-id: dev-id2\n    since: 2017-01-01T00:00:00.0Z\n    until: 2017-01-01T00:00:00.0Z\n"+
			"  -\n    developer-id: dev-id3\n    since: 2017-02-01T00:00:00.0Z\n    until: 2017-02-01T00:00:00.0Z\n",
		1)
	_ = mylog.Check2(asserts.Decode([]byte(encoded)))
	c.Check(err, IsNil)

	invalidTests := []string{
		// Multiple revocations.
		"developers:\n" +
			"  -\n    developer-id: dev-id2\n    since: 2017-01-01T00:00:00.0Z\n    until: 2017-01-01T00:00:00.0Z\n" +
			"  -\n    developer-id: dev-id2\n    since: 2017-02-01T00:00:00.0Z\n    until: 2017-02-01T00:00:00.0Z\n",
		// Revocation after non-revoking.
		"developers:\n" +
			"  -\n    developer-id: dev-id2\n    since: 2017-01-01T00:00:00.0Z\n" +
			"  -\n    developer-id: dev-id2\n    since: 2017-03-01T00:00:00.0Z\n    until: 2017-03-01T00:00:00.0Z\n",
		// Non-revoking after revocation.
		"developers:\n" +
			"  -\n    developer-id: dev-id2\n    since: 2017-01-01T00:00:00.0Z\n    until: 2017-01-01T00:00:00.0Z\n" +
			"  -\n    developer-id: dev-id2\n    since: 2017-02-01T00:00:00.0Z\n",
	}
	for _, test := range invalidTests {
		encoded := strings.Replace(sds.validEncoded, sds.developersLines, test, 1)
		_ := mylog.Check2(asserts.Decode([]byte(encoded)))
		c.Check(err, ErrorMatches, snapDevErrPrefix+`revocation for developer "dev-id2" must be standalone but found other "developers" items`)
	}
}

func (sds *snapDevSuite) TestAuthorityIsPublisher(c *C) {
	storeDB, db := makeStoreAndCheckDB(c)
	devDB := setup3rdPartySigning(c, "dev-id1", storeDB, db)

	snapDecl := mylog.Check2(storeDB.Sign(asserts.SnapDeclarationType, map[string]interface{}{
		"series":       "16",
		"snap-id":      "snap-id-1",
		"snap-name":    "snap-name-1",
		"publisher-id": "dev-id1",
		"timestamp":    time.Now().UTC().Format(time.RFC3339),
	}, nil, ""))

	mylog.Check(db.Add(snapDecl))


	snapDev := mylog.Check2(devDB.Sign(asserts.SnapDeveloperType, map[string]interface{}{
		"snap-id":      "snap-id-1",
		"publisher-id": "dev-id1",
	}, nil, ""))

	// Just to be super sure ...
	c.Assert(snapDev.HeaderString("authority-id"), Equals, "dev-id1")
	c.Assert(snapDev.HeaderString("publisher-id"), Equals, "dev-id1")
	mylog.Check(db.Check(snapDev))

}

func (sds *snapDevSuite) TestAuthorityIsNotPublisher(c *C) {
	storeDB, db := makeStoreAndCheckDB(c)
	devDB := setup3rdPartySigning(c, "dev-id1", storeDB, db)

	snapDecl := mylog.Check2(storeDB.Sign(asserts.SnapDeclarationType, map[string]interface{}{
		"series":       "16",
		"snap-id":      "snap-id-1",
		"snap-name":    "snap-name-1",
		"publisher-id": "dev-id1",
		"timestamp":    time.Now().UTC().Format(time.RFC3339),
	}, nil, ""))

	mylog.Check(db.Add(snapDecl))


	snapDev := mylog.Check2(devDB.Sign(asserts.SnapDeveloperType, map[string]interface{}{
		"authority-id": "dev-id1",
		"snap-id":      "snap-id-1",
		"publisher-id": "dev-id2",
	}, nil, ""))

	// Just to be super sure ...
	c.Assert(snapDev.HeaderString("authority-id"), Equals, "dev-id1")
	c.Assert(snapDev.HeaderString("publisher-id"), Equals, "dev-id2")
	mylog.Check(db.Check(snapDev))
	c.Assert(err, ErrorMatches, `snap-developer must be signed by the publisher or a trusted authority but got authority "dev-id1" and publisher "dev-id2"`)
}

func (sds *snapDevSuite) TestAuthorityIsNotPublisherButIsTrusted(c *C) {
	storeDB, db := makeStoreAndCheckDB(c)

	account := mylog.Check2(storeDB.Sign(asserts.AccountType, map[string]interface{}{
		"account-id":   "dev-id1",
		"display-name": "dev-id1",
		"validation":   "unknown",
		"timestamp":    time.Now().UTC().Format(time.RFC3339),
	}, nil, ""))

	mylog.Check(db.Add(account))


	snapDecl := mylog.Check2(storeDB.Sign(asserts.SnapDeclarationType, map[string]interface{}{
		"series":       "16",
		"snap-id":      "snap-id-1",
		"snap-name":    "snap-name-1",
		"publisher-id": "dev-id1",
		"timestamp":    time.Now().UTC().Format(time.RFC3339),
	}, nil, ""))

	mylog.Check(db.Add(snapDecl))


	snapDev := mylog.Check2(storeDB.Sign(asserts.SnapDeveloperType, map[string]interface{}{
		"snap-id":      "snap-id-1",
		"publisher-id": "dev-id1",
	}, nil, ""))

	// Just to be super sure ...
	c.Assert(snapDev.HeaderString("authority-id"), Equals, "canonical")
	c.Assert(snapDev.HeaderString("publisher-id"), Equals, "dev-id1")
	mylog.Check(db.Check(snapDev))

}

func (sds *snapDevSuite) TestCheckNewPublisherAccountExists(c *C) {
	storeDB, db := makeStoreAndCheckDB(c)

	account := mylog.Check2(storeDB.Sign(asserts.AccountType, map[string]interface{}{
		"account-id":   "dev-id1",
		"display-name": "dev-id1",
		"validation":   "unknown",
		"timestamp":    time.Now().UTC().Format(time.RFC3339),
	}, nil, ""))

	mylog.Check(db.Add(account))


	snapDecl := mylog.Check2(storeDB.Sign(asserts.SnapDeclarationType, map[string]interface{}{
		"series":       "16",
		"snap-id":      "snap-id-1",
		"snap-name":    "snap-name-1",
		"publisher-id": "dev-id1",
		"timestamp":    time.Now().UTC().Format(time.RFC3339),
	}, nil, ""))

	mylog.Check(db.Add(snapDecl))


	snapDev := mylog.Check2(storeDB.Sign(asserts.SnapDeveloperType, map[string]interface{}{
		"snap-id":      "snap-id-1",
		"publisher-id": "dev-id2",
	}, nil, ""))

	// Just to be super sure ...
	c.Assert(snapDev.HeaderString("authority-id"), Equals, "canonical")
	c.Assert(snapDev.HeaderString("publisher-id"), Equals, "dev-id2")
	mylog.

		// There's no account for dev-id2 yet so it should fail.
		Check(db.Check(snapDev))
	c.Assert(err, ErrorMatches, `snap-developer assertion for snap-id "snap-id-1" does not have a matching account assertion for the publisher "dev-id2"`)

	// But once the dev-id2 account is added the snap-developer is ok.
	account = mylog.Check2(storeDB.Sign(asserts.AccountType, map[string]interface{}{
		"account-id":   "dev-id2",
		"display-name": "dev-id2",
		"validation":   "unknown",
		"timestamp":    time.Now().UTC().Format(time.RFC3339),
	}, nil, ""))

	mylog.Check(db.Add(account))

	mylog.Check(db.Check(snapDev))

}

func (sds *snapDevSuite) TestCheckDeveloperAccountExists(c *C) {
	storeDB, db := makeStoreAndCheckDB(c)
	devDB := setup3rdPartySigning(c, "dev-id1", storeDB, db)

	snapDecl := mylog.Check2(storeDB.Sign(asserts.SnapDeclarationType, map[string]interface{}{
		"series":       "16",
		"snap-id":      "snap-id-1",
		"snap-name":    "snap-name-1",
		"publisher-id": "dev-id1",
		"timestamp":    time.Now().UTC().Format(time.RFC3339),
	}, nil, ""))

	mylog.Check(db.Add(snapDecl))


	snapDev := mylog.Check2(devDB.Sign(asserts.SnapDeveloperType, map[string]interface{}{
		"snap-id":      "snap-id-1",
		"publisher-id": "dev-id1",
		"developers": []interface{}{
			map[string]interface{}{
				"developer-id": "dev-id2",
				"since":        "2017-01-01T00:00:00.0Z",
			},
		},
	}, nil, ""))

	mylog.Check(db.Check(snapDev))
	c.Assert(err, ErrorMatches, `snap-developer assertion for snap-id "snap-id-1" does not have a matching account assertion for the developer "dev-id2"`)
}

func (sds *snapDevSuite) TestCheckMissingDeclaration(c *C) {
	storeDB, db := makeStoreAndCheckDB(c)
	devDB := setup3rdPartySigning(c, "dev-id1", storeDB, db)

	headers := map[string]interface{}{
		"authority-id": "dev-id1",
		"snap-id":      "snap-id-1",
		"publisher-id": "dev-id1",
	}
	snapDev := mylog.Check2(devDB.Sign(asserts.SnapDeveloperType, headers, nil, ""))

	mylog.Check(db.Check(snapDev))
	c.Assert(err, ErrorMatches, `snap-developer assertion for snap id "snap-id-1" does not have a matching snap-declaration assertion`)
}

func (sds *snapDevSuite) TestPrerequisitesNoDevelopers(c *C) {
	encoded := strings.Replace(sds.validEncoded, sds.developersLines, "", 1)
	assert := mylog.Check2(asserts.Decode([]byte(encoded)))

	prereqs := assert.Prerequisites()
	sort.Sort(RefSlice(prereqs))
	c.Assert(prereqs, DeepEquals, []*asserts.Ref{
		{Type: asserts.AccountType, PrimaryKey: []string{"dev-id1"}},
		{Type: asserts.SnapDeclarationType, PrimaryKey: []string{"16", "snap-id-1"}},
	})
}

func (sds *snapDevSuite) TestPrerequisitesWithDevelopers(c *C) {
	encoded := strings.Replace(
		sds.validEncoded, sds.developersLines,
		"developers:\n"+
			"  -\n    developer-id: dev-id2\n    since: 2017-01-01T00:00:00.0Z\n"+
			"  -\n    developer-id: dev-id3\n    since: 2017-01-01T00:00:00.0Z\n",
		1)
	assert := mylog.Check2(asserts.Decode([]byte(encoded)))

	prereqs := assert.Prerequisites()
	sort.Sort(RefSlice(prereqs))
	c.Assert(prereqs, DeepEquals, []*asserts.Ref{
		{Type: asserts.AccountType, PrimaryKey: []string{"dev-id1"}},
		{Type: asserts.AccountType, PrimaryKey: []string{"dev-id2"}},
		{Type: asserts.AccountType, PrimaryKey: []string{"dev-id3"}},
		{Type: asserts.SnapDeclarationType, PrimaryKey: []string{"16", "snap-id-1"}},
	})
}

func (sds *snapDevSuite) TestPrerequisitesWithDeveloperRepeated(c *C) {
	encoded := strings.Replace(
		sds.validEncoded, sds.developersLines,
		"developers:\n"+
			"  -\n    developer-id: dev-id2\n    since: 2015-01-01T00:00:00.0Z\n    until: 2016-01-01T00:00:00.0Z\n"+
			"  -\n    developer-id: dev-id2\n    since: 2017-01-01T00:00:00.0Z\n",
		1)
	assert := mylog.Check2(asserts.Decode([]byte(encoded)))

	prereqs := assert.Prerequisites()
	sort.Sort(RefSlice(prereqs))
	c.Assert(prereqs, DeepEquals, []*asserts.Ref{
		{Type: asserts.AccountType, PrimaryKey: []string{"dev-id1"}},
		{Type: asserts.AccountType, PrimaryKey: []string{"dev-id2"}},
		{Type: asserts.SnapDeclarationType, PrimaryKey: []string{"16", "snap-id-1"}},
	})
}

func (sds *snapDevSuite) TestPrerequisitesWithPublisherAsDeveloper(c *C) {
	encoded := strings.Replace(
		sds.validEncoded, sds.developersLines,
		"developers:\n  -\n    developer-id: dev-id1\n    since: 2017-01-01T00:00:00.0Z\n",
		1)
	assert := mylog.Check2(asserts.Decode([]byte(encoded)))

	prereqs := assert.Prerequisites()
	sort.Sort(RefSlice(prereqs))
	c.Assert(prereqs, DeepEquals, []*asserts.Ref{
		{Type: asserts.AccountType, PrimaryKey: []string{"dev-id1"}},
		{Type: asserts.SnapDeclarationType, PrimaryKey: []string{"16", "snap-id-1"}},
	})
}

type RefSlice []*asserts.Ref

func (s RefSlice) Len() int {
	return len(s)
}

func (s RefSlice) Less(i, j int) bool {
	iref, jref := s[i], s[j]
	if v := strings.Compare(iref.Type.Name, jref.Type.Name); v != 0 {
		return v == -1
	}
	for n, ipk := range iref.PrimaryKey {
		jpk := jref.PrimaryKey[n]
		if v := strings.Compare(ipk, jpk); v != 0 {
			return v == -1
		}
	}
	return false
}

func (s RefSlice) Swap(i, j int) {
	s[i], s[j] = s[j], s[i]
}
