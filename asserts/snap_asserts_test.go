// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2015-2016 Canonical Ltd
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
	"io/ioutil"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/crypto/sha3"
	. "gopkg.in/check.v1"

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
)

type snapDeclSuite struct {
	ts     time.Time
	tsLine string
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
		"body-length: 0\n" +
		"sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij" +
		"\n\n" +
		"AXNpZw=="
	a, err := asserts.Decode([]byte(encoded))
	c.Assert(err, IsNil)
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
	a, err := asserts.Decode([]byte(encoded))
	c.Assert(err, IsNil)
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
	a, err := asserts.Decode([]byte(encoded))
	c.Assert(err, IsNil)
	snapDecl := a.(*asserts.SnapDeclaration)
	c.Check(snapDecl.RefreshControl(), HasLen, 0)
	c.Check(snapDecl.AutoAliases(), HasLen, 0)
}

const (
	snapDeclErrPrefix = "assertion snap-declaration: "
)

func (sds *snapDeclSuite) TestDecodeInvalid(c *C) {
	encoded := "type: snap-declaration\n" +
		"authority-id: canonical\n" +
		"series: 16\n" +
		"snap-id: snap-id-1\n" +
		"snap-name: first\n" +
		"publisher-id: dev-id1\n" +
		"refresh-control:\n  - foo\n  - bar\n" +
		"auto-aliases:\n  - cmd1\n  - cmd2\n" +
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
		{sds.tsLine, "", `"timestamp" header is mandatory`},
		{sds.tsLine, "timestamp: \n", `"timestamp" header should not be empty`},
		{sds.tsLine, "timestamp: 12:30\n", `"timestamp" header is not a RFC3339 date: .*`},
	}

	for _, test := range invalidTests {
		invalid := strings.Replace(encoded, test.original, test.invalid, 1)
		_, err := asserts.Decode([]byte(invalid))
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
	a, err := asserts.Decode([]byte(encoded))
	c.Assert(err, IsNil)
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
	c.Check(plugRule1.AllowAutoConnection[0].SlotAttributes.Check(nil), ErrorMatches, `attribute "a1".*`)
	c.Check(plugRule1.AllowAutoConnection[0].PlugAttributes.Check(nil), ErrorMatches, `attribute "b1".*`)
	c.Check(plugRule1.AllowAutoConnection[0].SlotSnapTypes, DeepEquals, []string{"app"})
	c.Check(plugRule1.AllowAutoConnection[0].SlotPublisherIDs, DeepEquals, []string{"acme"})
	c.Assert(plugRule1.DenyAutoConnection, HasLen, 1)
	c.Check(plugRule1.DenyAutoConnection[0].SlotAttributes.Check(nil), ErrorMatches, `attribute "a1".*`)
	c.Check(plugRule1.DenyAutoConnection[0].PlugAttributes.Check(nil), ErrorMatches, `attribute "b1".*`)
	plugRule2 := snapDecl.PlugRule("interface2")
	c.Assert(plugRule2, NotNil)
	c.Assert(plugRule2.AllowInstallation, HasLen, 1)
	c.Check(plugRule2.AllowInstallation[0].PlugAttributes, Equals, asserts.AlwaysMatchAttributes)
	c.Assert(plugRule2.AllowConnection, HasLen, 1)
	c.Check(plugRule2.AllowConnection[0].PlugAttributes.Check(nil), ErrorMatches, `attribute "a2".*`)
	c.Check(plugRule2.AllowConnection[0].SlotAttributes.Check(nil), ErrorMatches, `attribute "b2".*`)
	c.Assert(plugRule2.DenyConnection, HasLen, 1)
	c.Check(plugRule2.DenyConnection[0].PlugAttributes.Check(nil), ErrorMatches, `attribute "a2".*`)
	c.Check(plugRule2.DenyConnection[0].SlotAttributes.Check(nil), ErrorMatches, `attribute "b2".*`)
	c.Check(plugRule2.DenyConnection[0].SlotSnapIDs, DeepEquals, []string{"snapidsnapidsnapidsnapidsnapid01", "snapidsnapidsnapidsnapidsnapid02"})

	slotRule3 := snapDecl.SlotRule("interface3")
	c.Assert(slotRule3, NotNil)
	c.Assert(slotRule3.DenyInstallation, HasLen, 1)
	c.Check(slotRule3.DenyInstallation[0].SlotAttributes, Equals, asserts.NeverMatchAttributes)
	c.Assert(slotRule3.AllowAutoConnection, HasLen, 1)
	c.Check(slotRule3.AllowAutoConnection[0].SlotAttributes.Check(nil), ErrorMatches, `attribute "c1".*`)
	c.Check(slotRule3.AllowAutoConnection[0].PlugAttributes.Check(nil), ErrorMatches, `attribute "d1".*`)
	c.Check(slotRule3.AllowAutoConnection[0].PlugSnapTypes, DeepEquals, []string{"app"})
	c.Check(slotRule3.AllowAutoConnection[0].PlugPublisherIDs, DeepEquals, []string{"acme"})
	c.Assert(slotRule3.DenyAutoConnection, HasLen, 1)
	c.Check(slotRule3.DenyAutoConnection[0].SlotAttributes.Check(nil), ErrorMatches, `attribute "c1".*`)
	c.Check(slotRule3.DenyAutoConnection[0].PlugAttributes.Check(nil), ErrorMatches, `attribute "d1".*`)
	slotRule4 := snapDecl.SlotRule("interface4")
	c.Assert(slotRule4, NotNil)
	c.Assert(slotRule4.AllowAutoConnection, HasLen, 1)
	c.Check(slotRule4.AllowConnection[0].PlugAttributes.Check(nil), ErrorMatches, `attribute "c2".*`)
	c.Check(slotRule4.AllowConnection[0].SlotAttributes.Check(nil), ErrorMatches, `attribute "d2".*`)
	c.Assert(slotRule4.DenyAutoConnection, HasLen, 1)
	c.Check(slotRule4.DenyConnection[0].PlugAttributes.Check(nil), ErrorMatches, `attribute "c2".*`)
	c.Check(slotRule4.DenyConnection[0].SlotAttributes.Check(nil), ErrorMatches, `attribute "d2".*`)
	c.Check(slotRule4.DenyConnection[0].PlugSnapIDs, DeepEquals, []string{"snapidsnapidsnapidsnapidsnapid01", "snapidsnapidsnapidsnapidsnapid02"})
	c.Assert(slotRule4.AllowInstallation, HasLen, 1)
	c.Check(slotRule4.AllowInstallation[0].SlotAttributes.Check(nil), ErrorMatches, `attribute "e1".*`)
	c.Check(slotRule4.AllowInstallation[0].SlotSnapTypes, DeepEquals, []string{"app"})
}

func (sds *snapDeclSuite) TestSuggestedFormat(c *C) {
	fmtnum, err := asserts.SuggestFormat(asserts.SnapDeclarationType, nil, nil)
	c.Assert(err, IsNil)
	c.Check(fmtnum, Equals, 0)

	headers := map[string]interface{}{
		"plugs": map[string]interface{}{
			"interface1": "true",
		},
	}
	fmtnum, err = asserts.SuggestFormat(asserts.SnapDeclarationType, headers, nil)
	c.Assert(err, IsNil)
	c.Check(fmtnum, Equals, 1)

	headers = map[string]interface{}{
		"slots": map[string]interface{}{
			"interface2": "true",
		},
	}
	fmtnum, err = asserts.SuggestFormat(asserts.SnapDeclarationType, headers, nil)
	c.Assert(err, IsNil)
	c.Check(fmtnum, Equals, 1)
}

func prereqDevAccount(c *C, storeDB assertstest.SignerDB, db *asserts.Database) {
	dev1Acct := assertstest.NewAccount(storeDB, "developer1", map[string]interface{}{
		"account-id": "dev-id1",
	}, "")
	err := db.Add(dev1Acct)
	c.Assert(err, IsNil)
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
	snapDecl, err := storeDB.Sign(asserts.SnapDeclarationType, headers, nil, "")
	c.Assert(err, IsNil)

	err = db.Check(snapDecl)
	c.Assert(err, IsNil)
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
	snapDecl, err := otherDB.Sign(asserts.SnapDeclarationType, headers, nil, "")
	c.Assert(err, IsNil)

	err = db.Check(snapDecl)
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
	snapDecl, err := storeDB.Sign(asserts.SnapDeclarationType, headers, nil, "")
	c.Assert(err, IsNil)

	err = db.Check(snapDecl)
	c.Assert(err, ErrorMatches, `snap-declaration assertion for "foo" \(id "snap-id-1"\) does not have a matching account assertion for the publisher "dev-id1"`)
}

type snapFileDigestSuite struct{}

func (s *snapFileDigestSuite) TestSnapFileSHA3_384(c *C) {
	exData := []byte("hashmeplease")

	tempdir := c.MkDir()
	snapFn := filepath.Join(tempdir, "ex.snap")
	err := ioutil.WriteFile(snapFn, exData, 0644)
	c.Assert(err, IsNil)

	encDgst, size, err := asserts.SnapFileSHA3_384(snapFn)
	c.Assert(err, IsNil)
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
	a, err := asserts.Decode([]byte(encoded))
	c.Assert(err, IsNil)

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
	a, err := asserts.Decode([]byte(encoded))
	c.Assert(err, IsNil)
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
		{"grade: stable\n", "", `"grade" header is mandatory`},
		{"grade: stable\n", "grade: \n", `"grade" header should not be empty`},
		{sbs.tsLine, "", `"timestamp" header is mandatory`},
		{sbs.tsLine, "timestamp: \n", `"timestamp" header should not be empty`},
		{sbs.tsLine, "timestamp: 12:30\n", `"timestamp" header is not a RFC3339 date: .*`},
	}

	for _, test := range invalidTests {
		invalid := strings.Replace(encoded, test.original, test.invalid, 1)
		_, err := asserts.Decode([]byte(invalid))
		c.Check(err, ErrorMatches, snapBuildErrPrefix+test.expectedErr)
	}
}

func makeStoreAndCheckDB(c *C) (storeDB *assertstest.SigningDB, checkDB *asserts.Database) {
	trustedPrivKey := testPrivKey0
	storePrivKey := testPrivKey1

	store := assertstest.NewStoreStack("canonical", trustedPrivKey, storePrivKey)
	cfg := &asserts.DatabaseConfig{
		Backstore: asserts.NewMemoryBackstore(),
		Trusted:   store.Trusted,
	}
	checkDB, err := asserts.OpenDatabase(cfg)
	c.Assert(err, IsNil)

	// add store key
	err = checkDB.Add(store.StoreAccountKey(""))
	c.Assert(err, IsNil)

	return store.SigningDB, checkDB
}

func setup3rdPartySigning(c *C, username string, storeDB *assertstest.SigningDB, checkDB *asserts.Database) (signingDB *assertstest.SigningDB) {
	privKey := testPrivKey2

	acct := assertstest.NewAccount(storeDB, username, map[string]interface{}{
		"account-id": username,
	}, "")
	accKey := assertstest.NewAccountKey(storeDB, acct, nil, privKey.PublicKey(), "")

	err := checkDB.Add(acct)
	c.Assert(err, IsNil)
	err = checkDB.Add(accKey)
	c.Assert(err, IsNil)

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
	snapBuild, err := devDB.Sign(asserts.SnapBuildType, headers, nil, "")
	c.Assert(err, IsNil)

	err = db.Check(snapBuild)
	c.Assert(err, IsNil)
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
	snapBuild, err := devDB.Sign(asserts.SnapBuildType, headers, nil, "")
	c.Assert(err, IsNil)

	err = db.Check(snapBuild)
	c.Assert(err, ErrorMatches, "snap-build assertion timestamp outside of signing key validity")
}

type snapRevSuite struct {
	ts           time.Time
	tsLine       string
	validEncoded string
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

func (srs *snapRevSuite) makeHeaders(overrides map[string]interface{}) map[string]interface{} {
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

func (srs *snapRevSuite) TestDecodeOK(c *C) {
	encoded := srs.makeValidEncoded()
	a, err := asserts.Decode([]byte(encoded))
	c.Assert(err, IsNil)
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
		_, err := asserts.Decode([]byte(invalid))
		c.Check(err, ErrorMatches, snapRevErrPrefix+test.expectedErr)
	}
}

func prereqSnapDecl(c *C, storeDB assertstest.SignerDB, db *asserts.Database) {
	snapDecl, err := storeDB.Sign(asserts.SnapDeclarationType, map[string]interface{}{
		"series":       "16",
		"snap-id":      "snap-id-1",
		"snap-name":    "foo",
		"publisher-id": "dev-id1",
		"timestamp":    time.Now().Format(time.RFC3339),
	}, nil, "")
	c.Assert(err, IsNil)
	err = db.Add(snapDecl)
	c.Assert(err, IsNil)
}

func (srs *snapRevSuite) TestSnapRevisionCheck(c *C) {
	storeDB, db := makeStoreAndCheckDB(c)

	prereqDevAccount(c, storeDB, db)
	prereqSnapDecl(c, storeDB, db)

	headers := srs.makeHeaders(nil)
	snapRev, err := storeDB.Sign(asserts.SnapRevisionType, headers, nil, "")
	c.Assert(err, IsNil)

	err = db.Check(snapRev)
	c.Assert(err, IsNil)
}

func (srs *snapRevSuite) TestSnapRevisionCheckInconsistentTimestamp(c *C) {
	storeDB, db := makeStoreAndCheckDB(c)

	headers := srs.makeHeaders(map[string]interface{}{
		"timestamp": "2013-01-01T14:00:00Z",
	})
	snapRev, err := storeDB.Sign(asserts.SnapRevisionType, headers, nil, "")
	c.Assert(err, IsNil)

	err = db.Check(snapRev)
	c.Assert(err, ErrorMatches, "snap-revision assertion timestamp outside of signing key validity")
}

func (srs *snapRevSuite) TestSnapRevisionCheckUntrustedAuthority(c *C) {
	storeDB, db := makeStoreAndCheckDB(c)

	otherDB := setup3rdPartySigning(c, "other", storeDB, db)

	headers := srs.makeHeaders(nil)
	snapRev, err := otherDB.Sign(asserts.SnapRevisionType, headers, nil, "")
	c.Assert(err, IsNil)

	err = db.Check(snapRev)
	c.Assert(err, ErrorMatches, `snap-revision assertion for snap id "snap-id-1" is not signed by a store:.*`)
}

func (srs *snapRevSuite) TestSnapRevisionCheckMissingDeveloperAccount(c *C) {
	storeDB, db := makeStoreAndCheckDB(c)

	headers := srs.makeHeaders(nil)
	snapRev, err := storeDB.Sign(asserts.SnapRevisionType, headers, nil, "")
	c.Assert(err, IsNil)

	err = db.Check(snapRev)
	c.Assert(err, ErrorMatches, `snap-revision assertion for snap id "snap-id-1" does not have a matching account assertion for the developer "dev-id1"`)
}

func (srs *snapRevSuite) TestSnapRevisionCheckMissingDeclaration(c *C) {
	storeDB, db := makeStoreAndCheckDB(c)

	prereqDevAccount(c, storeDB, db)

	headers := srs.makeHeaders(nil)
	snapRev, err := storeDB.Sign(asserts.SnapRevisionType, headers, nil, "")
	c.Assert(err, IsNil)

	err = db.Check(snapRev)
	c.Assert(err, ErrorMatches, `snap-revision assertion for snap id "snap-id-1" does not have a matching snap-declaration assertion`)
}

func (srs *snapRevSuite) TestPrimaryKey(c *C) {
	storeDB, db := makeStoreAndCheckDB(c)

	prereqDevAccount(c, storeDB, db)
	prereqSnapDecl(c, storeDB, db)

	headers := srs.makeHeaders(nil)
	snapRev, err := storeDB.Sign(asserts.SnapRevisionType, headers, nil, "")
	c.Assert(err, IsNil)
	err = db.Add(snapRev)
	c.Assert(err, IsNil)

	_, err = db.Find(asserts.SnapRevisionType, map[string]string{
		"snap-sha3-384": headers["snap-sha3-384"].(string),
	})
	c.Assert(err, IsNil)
}

func (srs *snapRevSuite) TestPrerequisites(c *C) {
	encoded := srs.makeValidEncoded()
	a, err := asserts.Decode([]byte(encoded))
	c.Assert(err, IsNil)

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
	a, err := asserts.Decode([]byte(encoded))
	c.Assert(err, IsNil)
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
		_, err := asserts.Decode([]byte(invalid))
		c.Check(err, ErrorMatches, validationErrPrefix+test.expectedErr)
	}
}

func prereqSnapDecl2(c *C, storeDB assertstest.SignerDB, db *asserts.Database) {
	snapDecl, err := storeDB.Sign(asserts.SnapDeclarationType, map[string]interface{}{
		"series":       "16",
		"snap-id":      "snap-id-2",
		"snap-name":    "bar",
		"publisher-id": "dev-id1",
		"timestamp":    time.Now().Format(time.RFC3339),
	}, nil, "")
	c.Assert(err, IsNil)
	err = db.Add(snapDecl)
	c.Assert(err, IsNil)
}

func (vs *validationSuite) TestValidationCheck(c *C) {
	storeDB, db := makeStoreAndCheckDB(c)
	devDB := setup3rdPartySigning(c, "dev-id1", storeDB, db)

	prereqSnapDecl(c, storeDB, db)
	prereqSnapDecl2(c, storeDB, db)

	headers := vs.makeHeaders(nil)
	validation, err := devDB.Sign(asserts.ValidationType, headers, nil, "")
	c.Assert(err, IsNil)

	err = db.Check(validation)
	c.Assert(err, IsNil)
}

func (vs *validationSuite) TestValidationCheckWrongAuthority(c *C) {
	storeDB, db := makeStoreAndCheckDB(c)

	prereqDevAccount(c, storeDB, db)
	prereqSnapDecl(c, storeDB, db)
	prereqSnapDecl2(c, storeDB, db)

	headers := vs.makeHeaders(nil)
	validation, err := storeDB.Sign(asserts.ValidationType, headers, nil, "")
	c.Assert(err, IsNil)

	err = db.Check(validation)
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
	a, err := asserts.Decode([]byte(encoded))
	c.Assert(err, IsNil)
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
	a, err := asserts.Decode([]byte(encoded))
	c.Assert(err, IsNil)
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
	_, err := asserts.Decode([]byte(encoded))
	c.Check(err, ErrorMatches, `.*: "revoked" header must be 'true' or 'false'`)
}

func (vs *validationSuite) TestMissingGatedSnapDeclaration(c *C) {
	storeDB, db := makeStoreAndCheckDB(c)

	prereqDevAccount(c, storeDB, db)

	headers := vs.makeHeaders(nil)
	a, err := storeDB.Sign(asserts.ValidationType, headers, nil, "")
	c.Assert(err, IsNil)

	err = db.Check(a)
	c.Assert(err, ErrorMatches, `validation assertion by snap-id "snap-id-1" does not have a matching snap-declaration assertion for approved-snap-id "snap-id-2"`)
}

func (vs *validationSuite) TestMissingGatingSnapDeclaration(c *C) {
	storeDB, db := makeStoreAndCheckDB(c)

	prereqDevAccount(c, storeDB, db)
	prereqSnapDecl2(c, storeDB, db)

	headers := vs.makeHeaders(nil)
	a, err := storeDB.Sign(asserts.ValidationType, headers, nil, "")
	c.Assert(err, IsNil)

	err = db.Check(a)
	c.Assert(err, ErrorMatches, `validation assertion by snap-id "snap-id-1" does not have a matching snap-declaration assertion`)
}

func (vs *validationSuite) TestPrerequisites(c *C) {
	encoded := vs.makeValidEncoded()
	a, err := asserts.Decode([]byte(encoded))
	c.Assert(err, IsNil)

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
	a, err := asserts.Decode([]byte(encoded))
	c.Assert(err, IsNil)
	baseDecl := a.(*asserts.BaseDeclaration)
	c.Check(baseDecl.Series(), Equals, "16")
	ts, err := time.Parse(time.RFC3339, "2016-09-29T19:50:49Z")
	c.Assert(err, IsNil)
	c.Check(baseDecl.Timestamp().Equal(ts), Equals, true)

	c.Check(baseDecl.PlugRule("interfaceX"), IsNil)
	c.Check(baseDecl.SlotRule("interfaceX"), IsNil)

	plugRule1 := baseDecl.PlugRule("interface1")
	c.Assert(plugRule1, NotNil)
	c.Assert(plugRule1.DenyInstallation, HasLen, 1)
	c.Check(plugRule1.DenyInstallation[0].PlugAttributes, Equals, asserts.NeverMatchAttributes)
	c.Assert(plugRule1.AllowAutoConnection, HasLen, 1)
	c.Check(plugRule1.AllowAutoConnection[0].SlotAttributes.Check(nil), ErrorMatches, `attribute "a1".*`)
	c.Check(plugRule1.AllowAutoConnection[0].PlugAttributes.Check(nil), ErrorMatches, `attribute "b1".*`)
	c.Check(plugRule1.AllowAutoConnection[0].SlotSnapTypes, DeepEquals, []string{"app"})
	c.Check(plugRule1.AllowAutoConnection[0].SlotPublisherIDs, DeepEquals, []string{"acme"})
	c.Assert(plugRule1.DenyAutoConnection, HasLen, 1)
	c.Check(plugRule1.DenyAutoConnection[0].SlotAttributes.Check(nil), ErrorMatches, `attribute "a1".*`)
	c.Check(plugRule1.DenyAutoConnection[0].PlugAttributes.Check(nil), ErrorMatches, `attribute "b1".*`)
	plugRule2 := baseDecl.PlugRule("interface2")
	c.Assert(plugRule2, NotNil)
	c.Assert(plugRule2.AllowInstallation, HasLen, 1)
	c.Check(plugRule2.AllowInstallation[0].PlugAttributes, Equals, asserts.AlwaysMatchAttributes)
	c.Assert(plugRule2.AllowConnection, HasLen, 1)
	c.Check(plugRule2.AllowConnection[0].PlugAttributes.Check(nil), ErrorMatches, `attribute "a2".*`)
	c.Check(plugRule2.AllowConnection[0].SlotAttributes.Check(nil), ErrorMatches, `attribute "b2".*`)
	c.Assert(plugRule2.DenyConnection, HasLen, 1)
	c.Check(plugRule2.DenyConnection[0].PlugAttributes.Check(nil), ErrorMatches, `attribute "a2".*`)
	c.Check(plugRule2.DenyConnection[0].SlotAttributes.Check(nil), ErrorMatches, `attribute "b2".*`)
	c.Check(plugRule2.DenyConnection[0].SlotSnapIDs, DeepEquals, []string{"snapidsnapidsnapidsnapidsnapid01", "snapidsnapidsnapidsnapidsnapid02"})

	slotRule3 := baseDecl.SlotRule("interface3")
	c.Assert(slotRule3, NotNil)
	c.Assert(slotRule3.DenyInstallation, HasLen, 1)
	c.Check(slotRule3.DenyInstallation[0].SlotAttributes, Equals, asserts.NeverMatchAttributes)
	c.Assert(slotRule3.AllowAutoConnection, HasLen, 1)
	c.Check(slotRule3.AllowAutoConnection[0].SlotAttributes.Check(nil), ErrorMatches, `attribute "c1".*`)
	c.Check(slotRule3.AllowAutoConnection[0].PlugAttributes.Check(nil), ErrorMatches, `attribute "d1".*`)
	c.Check(slotRule3.AllowAutoConnection[0].PlugSnapTypes, DeepEquals, []string{"app"})
	c.Check(slotRule3.AllowAutoConnection[0].PlugPublisherIDs, DeepEquals, []string{"acme"})
	c.Assert(slotRule3.DenyAutoConnection, HasLen, 1)
	c.Check(slotRule3.DenyAutoConnection[0].SlotAttributes.Check(nil), ErrorMatches, `attribute "c1".*`)
	c.Check(slotRule3.DenyAutoConnection[0].PlugAttributes.Check(nil), ErrorMatches, `attribute "d1".*`)
	slotRule4 := baseDecl.SlotRule("interface4")
	c.Assert(slotRule4, NotNil)
	c.Assert(slotRule4.AllowConnection, HasLen, 1)
	c.Check(slotRule4.AllowConnection[0].PlugAttributes.Check(nil), ErrorMatches, `attribute "c2".*`)
	c.Check(slotRule4.AllowConnection[0].SlotAttributes.Check(nil), ErrorMatches, `attribute "d2".*`)
	c.Assert(slotRule4.DenyConnection, HasLen, 1)
	c.Check(slotRule4.DenyConnection[0].PlugAttributes.Check(nil), ErrorMatches, `attribute "c2".*`)
	c.Check(slotRule4.DenyConnection[0].SlotAttributes.Check(nil), ErrorMatches, `attribute "d2".*`)
	c.Check(slotRule4.DenyConnection[0].PlugSnapIDs, DeepEquals, []string{"snapidsnapidsnapidsnapidsnapid01", "snapidsnapidsnapidsnapidsnapid02"})
	c.Assert(slotRule4.AllowInstallation, HasLen, 1)
	c.Check(slotRule4.AllowInstallation[0].SlotAttributes.Check(nil), ErrorMatches, `attribute "e1".*`)
	c.Check(slotRule4.AllowInstallation[0].SlotSnapTypes, DeepEquals, []string{"app"})

}

func (s *baseDeclSuite) TestBaseDeclarationCheckUntrustedAuthority(c *C) {
	storeDB, db := makeStoreAndCheckDB(c)

	otherDB := setup3rdPartySigning(c, "other", storeDB, db)

	headers := map[string]interface{}{
		"series":    "16",
		"timestamp": time.Now().Format(time.RFC3339),
	}
	baseDecl, err := otherDB.Sign(asserts.BaseDeclarationType, headers, nil, "")
	c.Assert(err, IsNil)

	err = db.Check(baseDecl)
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
		_, err := asserts.Decode([]byte(invalid))
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

	err := asserts.InitBuiltinBaseDeclaration([]byte(headers))
	c.Assert(err, IsNil)

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
	_, err = asserts.Decode(enc)
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
		err := asserts.InitBuiltinBaseDeclaration([]byte(t.headers))
		c.Check(err, ErrorMatches, t.err, Commentf(t.headers))
	}
}
