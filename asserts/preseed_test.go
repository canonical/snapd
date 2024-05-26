// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2023 Canonical Ltd
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

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/asserts"
)

type preseedSuite struct {
	ts     time.Time
	tsLine string
}

var _ = Suite(&preseedSuite{})

func (ps *preseedSuite) SetUpSuite(c *C) {
	ps.ts = time.Now().Truncate(time.Second).UTC()
	ps.tsLine = "timestamp: " + ps.ts.Format(time.RFC3339) + "\n"
}

const (
	preseedExample = `type: preseed
authority-id: brand-id1
series: 16
brand-id: brand-id1
model: baz-3000
system-label: 20220210
artifact-sha3-384: KPIl7M4vQ9d4AUjkoU41TGAwtOMLc_bWUCeW8AvdRWD4_xcP60Oo4ABs1No7BtXj
snaps:
  -
    name: baz-linux
    id: bazlinuxidididididididididididid
    revision: 99
OTHER` + "TSLINE" +
		"body-length: 0\n" +
		"sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij" +
		"\n\n" +
		"AXNpZw=="
)

func (ps *preseedSuite) TestValidateSeedSystemLabel(c *C) {
	valid := []string{
		"a",
		"ab",
		"a-a",
		"a-123",
		"a-a-a",
		"20191119",
		"foobar",
		"my-system",
		"brand-system-date-1234",
	}
	for _, label := range valid {
		c.Logf("trying valid label: %q", label)
		mylog.Check(asserts.IsValidSystemLabel(label))
		c.Check(err, IsNil)
	}

	invalid := []string{
		"",
		"/bin",
		"../../bin/bar",
		":invalid:",
		"日本語",
		"-invalid",
		"invalid-",
		"MYSYSTEM",
		"mySystem",
	}
	for _, label := range invalid {
		c.Logf("trying invalid label: %q", label)
		mylog.Check(asserts.IsValidSystemLabel(label))
		c.Check(err, ErrorMatches, fmt.Sprintf("invalid seed system label: %q", label))
	}
}

func (ps *preseedSuite) TestDecodeOK(c *C) {
	encoded := strings.Replace(preseedExample, "TSLINE", ps.tsLine, 1)
	encoded = strings.Replace(encoded, "OTHER", "", 1)

	a := mylog.Check2(asserts.Decode([]byte(encoded)))

	c.Check(a.Type(), Equals, asserts.PreseedType)
	preseed := a.(*asserts.Preseed)
	c.Check(preseed.AuthorityID(), Equals, "brand-id1")
	c.Check(preseed.Timestamp(), Equals, ps.ts)
	c.Check(preseed.Series(), Equals, "16")
	c.Check(preseed.BrandID(), Equals, "brand-id1")
	c.Check(preseed.Model(), Equals, "baz-3000")
	c.Check(preseed.SystemLabel(), Equals, "20220210")
	c.Check(preseed.ArtifactSHA3_384(), Equals, "KPIl7M4vQ9d4AUjkoU41TGAwtOMLc_bWUCeW8AvdRWD4_xcP60Oo4ABs1No7BtXj")
	snaps := preseed.Snaps()
	c.Assert(snaps, DeepEquals, []*asserts.PreseedSnap{
		{
			Name:     "baz-linux",
			SnapID:   "bazlinuxidididididididididididid",
			Revision: 99,
		},
	})
	c.Check(snaps[0].SnapName(), Equals, "baz-linux")
	c.Check(snaps[0].ID(), Equals, "bazlinuxidididididididididididid")
}

func (ps *preseedSuite) TestDecodeInvalid(c *C) {
	const errPrefix = "assertion preseed: "

	encoded := strings.Replace(preseedExample, "TSLINE", ps.tsLine, 1)

	snapsStanza := encoded[strings.Index(encoded, "snaps:"):strings.Index(encoded, "timestamp:")]

	invalidTests := []struct{ original, invalid, expectedErr string }{
		{"series: 16\n", "", `"series" header is mandatory`},
		{"series: 16\n", "series: \n", `"series" header should not be empty`},
		{"model: baz-3000\n", "model: \n", `"model" header should not be empty`},
		{"model: baz-3000\n", "model: -\n", `"model" header contains invalid characters: "-"`},
		{"brand-id: brand-id1\n", "", `"brand-id" header is mandatory`},
		{"brand-id: brand-id1\n", "brand-id: \n", `"brand-id" header should not be empty`},
		{"system-label: 20220210\n", "system-label: \n", `"system-label" header should not be empty`},
		{"system-label: 20220210\n", "system-label: -x\n", `"system-label" header contains invalid characters: "-x"`},
		{ps.tsLine, "timestamp: 12:30\n", `"timestamp" header is not a RFC3339 date: .*`},
		{"artifact-sha3-384: KPIl7M4vQ9d4AUjkoU41TGAwtOMLc_bWUCeW8AvdRWD4_xcP60Oo4ABs1No7BtXj\n", "artifact-sha3-384: 1\n", `"artifact-sha3-384" header cannot be decoded: illegal base64 data at input byte 0`},
		{"revision: 99\n", "revision: 0\n", `"revision" of snap "baz-linux" must be >=1: 0`},
		{snapsStanza, "", `"snaps" header is mandatory`},
		{snapsStanza, "snaps: snap\n", `"snaps" header must be a list of maps`},
		{snapsStanza, "snaps:\n  - snap\n", `"snaps" header must be a list of maps`},
		{"name: baz-linux\n", "other: 1\n", `"name" of snap is mandatory`},
		{"name: baz-linux\n", "name: linux_2\n", `invalid snap name "linux_2"`},
		{"id: bazlinuxidididididididididididid\n", "id: 2\n", `"id" of snap "baz-linux" contains invalid characters: "2"`},
		{"OTHER", "  -\n    name: baz-linux\n    id: bazlinuxidididididididididididid\n    revision: 1\n", `cannot list the same snap "baz-linux" multiple times`},
		{"OTHER", "  -\n    name: baz-linux2\n    id: bazlinuxidididididididididididid\n    revision: 1\n", `cannot specify the same snap id "bazlinuxidididididididididididid" multiple times, specified for snaps "baz-linux" and "baz-linux2"`},
	}

	for _, test := range invalidTests {
		invalid := strings.Replace(encoded, test.original, test.invalid, 1)
		invalid = strings.Replace(invalid, "OTHER", "", 1)
		_ := mylog.Check2(asserts.Decode([]byte(invalid)))
		c.Check(err, ErrorMatches, errPrefix+test.expectedErr)
	}
}

func (ps *preseedSuite) TestSnapRevisionImpliesSnapId(c *C) {
	encoded := strings.Replace(preseedExample, "TSLINE", ps.tsLine, 1)
	encoded = strings.Replace(encoded, "OTHER", "", 1)
	encoded = strings.Replace(encoded, "    revision: 99\n", "", 1)

	_ := mylog.Check2(asserts.Decode([]byte(encoded)))
	c.Assert(err, ErrorMatches, `assertion preseed: snap revision is required when snap id is set`)
}

func (ps *preseedSuite) TestSnapIdImpliesRevision(c *C) {
	encoded := strings.Replace(preseedExample, "TSLINE", ps.tsLine, 1)
	encoded = strings.Replace(encoded, "OTHER", "", 1)
	encoded = strings.Replace(encoded, "    id: bazlinuxidididididididididididid\n", "", 1)

	_ := mylog.Check2(asserts.Decode([]byte(encoded)))
	c.Assert(err, ErrorMatches, `assertion preseed: snap id is required when revision is set`)
}

func (ps *preseedSuite) TestSnapIdOptional(c *C) {
	encoded := strings.Replace(preseedExample, "TSLINE", ps.tsLine, 1)
	encoded = strings.Replace(encoded, "OTHER", "  -\n    name: foo-linux\n", 1)
	encoded = strings.Replace(encoded, "    revision: 99\n", "", 1)
	encoded = strings.Replace(encoded, "    id: bazlinuxidididididididididididid\n", "", 1)

	a := mylog.Check2(asserts.Decode([]byte(encoded)))

	snaps := a.(*asserts.Preseed).Snaps()
	c.Assert(snaps, HasLen, 2)
	c.Check(snaps[0].Name, Equals, "baz-linux")
	c.Check(snaps[1].Name, Equals, "foo-linux")
}
