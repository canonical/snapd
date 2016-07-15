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
	"strings"
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
)

var (
	_ = Suite(&snapDeclSuite{})
	_ = Suite(&snapBuildSuite{})
	_ = Suite(&snapRevSuite{})
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
		"gates: snap-id-3,snap-id-4\n" +
		sds.tsLine +
		"body-length: 0" +
		"\n\n" +
		"openpgp c2ln"
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
	c.Check(snapDecl.Gates(), DeepEquals, []string{"snap-id-3", "snap-id-4"})
}

func (sds *snapDeclSuite) TestEmptySnapName(c *C) {
	encoded := "type: snap-declaration\n" +
		"authority-id: canonical\n" +
		"series: 16\n" +
		"snap-id: snap-id-1\n" +
		"snap-name: \n" +
		"publisher-id: dev-id1\n" +
		"gates: snap-id-3,snap-id-4\n" +
		sds.tsLine +
		"body-length: 0" +
		"\n\n" +
		"openpgp c2ln"
	a, err := asserts.Decode([]byte(encoded))
	c.Assert(err, IsNil)
	snapDecl := a.(*asserts.SnapDeclaration)
	c.Check(snapDecl.SnapName(), Equals, "")
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
		"gates: snap-id-3,snap-id-4\n" +
		sds.tsLine +
		"body-length: 0" +
		"\n\n" +
		"openpgp c2ln"

	invalidTests := []struct{ original, invalid, expectedErr string }{
		{"series: 16\n", "", `"series" header is mandatory`},
		{"series: 16\n", "series: \n", `"series" header should not be empty`},
		{"snap-id: snap-id-1\n", "", `"snap-id" header is mandatory`},
		{"snap-id: snap-id-1\n", "snap-id: \n", `"snap-id" header should not be empty`},
		{"snap-name: first\n", "", `"snap-name" header is mandatory`},
		{"publisher-id: dev-id1\n", "", `"publisher-id" header is mandatory`},
		{"publisher-id: dev-id1\n", "publisher-id: \n", `"publisher-id" header should not be empty`},
		{sds.tsLine, "", `"timestamp" header is mandatory`},
		{sds.tsLine, "timestamp: \n", `"timestamp" header should not be empty`},
		{sds.tsLine, "timestamp: 12:30\n", `"timestamp" header is not a RFC3339 date: .*`},
		{"gates: snap-id-3,snap-id-4\n", "", `\"gates\" header is mandatory`},
		{"gates: snap-id-3,snap-id-4\n", "gates: foo,\n", `empty entry in comma separated "gates" header: "foo,"`},
	}

	for _, test := range invalidTests {
		invalid := strings.Replace(encoded, test.original, test.invalid, 1)
		_, err := asserts.Decode([]byte(invalid))
		c.Check(err, ErrorMatches, snapDeclErrPrefix+test.expectedErr)
	}

}

func prereqDevAccount(c *C, storeDB assertstest.SignerDB, db *asserts.Database) {
	dev1Acct := assertstest.NewAccount(storeDB, "developer1", map[string]string{
		"account-id": "dev-id1",
	}, "")
	err := db.Add(dev1Acct)
	c.Assert(err, IsNil)
}

func (sds *snapDeclSuite) TestSnapDeclarationCheck(c *C) {
	storeDB, db := makeStoreAndCheckDB(c)

	prereqDevAccount(c, storeDB, db)

	headers := map[string]string{
		"series":       "16",
		"snap-id":      "snap-id-1",
		"snap-name":    "foo",
		"publisher-id": "dev-id1",
		"gates":        "",
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

	headers := map[string]string{
		"series":       "16",
		"snap-id":      "snap-id-1",
		"snap-name":    "foo",
		"publisher-id": "dev-id1",
		"gates":        "",
		"timestamp":    time.Now().Format(time.RFC3339),
	}
	snapDecl, err := otherDB.Sign(asserts.SnapDeclarationType, headers, nil, "")
	c.Assert(err, IsNil)

	err = db.Check(snapDecl)
	c.Assert(err, ErrorMatches, `snap-declaration assertion for "foo" \(id "snap-id-1"\) is not signed by a directly trusted authority:.*`)
}

func (sds *snapDeclSuite) TestSnapDeclarationCheckMissingPublisherAccount(c *C) {
	storeDB, db := makeStoreAndCheckDB(c)

	headers := map[string]string{
		"series":       "16",
		"snap-id":      "snap-id-1",
		"snap-name":    "foo",
		"publisher-id": "dev-id1",
		"gates":        "",
		"timestamp":    time.Now().Format(time.RFC3339),
	}
	snapDecl, err := storeDB.Sign(asserts.SnapDeclarationType, headers, nil, "")
	c.Assert(err, IsNil)

	err = db.Check(snapDecl)
	c.Assert(err, ErrorMatches, `snap-declaration assertion for "foo" \(id "snap-id-1"\) does not have a matching account assertion for the publisher "dev-id1"`)
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
		"gates: snap-id-3,snap-id-4\n" +
		sds.tsLine +
		"body-length: 0" +
		"\n\n" +
		"openpgp c2ln"
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

func (sbs *snapBuildSuite) TestDecodeOK(c *C) {
	encoded := "type: snap-build\n" +
		"authority-id: dev-id1\n" +
		"series: 16\n" +
		"snap-id: snap-id-1\n" +
		"snap-digest: sha256 ...\n" +
		"grade: stable\n" +
		"snap-size: 10000\n" +
		sbs.tsLine +
		"body-length: 0" +
		"\n\n" +
		"openpgp c2ln"
	a, err := asserts.Decode([]byte(encoded))
	c.Assert(err, IsNil)
	c.Check(a.Type(), Equals, asserts.SnapBuildType)
	snapBuild := a.(*asserts.SnapBuild)
	c.Check(snapBuild.AuthorityID(), Equals, "dev-id1")
	c.Check(snapBuild.Timestamp(), Equals, sbs.ts)
	c.Check(snapBuild.Series(), Equals, "16")
	c.Check(snapBuild.SnapID(), Equals, "snap-id-1")
	c.Check(snapBuild.SnapDigest(), Equals, "sha256 ...")
	c.Check(snapBuild.SnapSize(), Equals, uint64(10000))
	c.Check(snapBuild.Grade(), Equals, "stable")
}

const (
	snapBuildErrPrefix = "assertion snap-build: "
)

func (sbs *snapBuildSuite) TestDecodeInvalid(c *C) {
	encoded := "type: snap-build\n" +
		"authority-id: dev-id1\n" +
		"series: 16\n" +
		"snap-id: snap-id-1\n" +
		"snap-digest: sha256 ...\n" +
		"grade: stable\n" +
		"snap-size: 10000\n" +
		sbs.tsLine +
		"body-length: 0" +
		"\n\n" +
		"openpgp c2ln"

	invalidTests := []struct{ original, invalid, expectedErr string }{
		{"series: 16\n", "", `"series" header is mandatory`},
		{"series: 16\n", "series: \n", `"series" header should not be empty`},
		{"snap-id: snap-id-1\n", "", `"snap-id" header is mandatory`},
		{"snap-id: snap-id-1\n", "snap-id: \n", `"snap-id" header should not be empty`},
		{"snap-digest: sha256 ...\n", "", `"snap-digest" header is mandatory`},
		{"snap-digest: sha256 ...\n", "snap-digest: \n", `"snap-digest" header should not be empty`},
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
		Backstore:      asserts.NewMemoryBackstore(),
		KeypairManager: asserts.NewMemoryKeypairManager(),
		Trusted:        store.Trusted,
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

	acct := assertstest.NewAccount(storeDB, username, nil, "")
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

	headers := map[string]string{
		"authority-id": devDB.AuthorityID,
		"series":       "16",
		"snap-id":      "snap-id-1",
		"snap-digest":  "sha256 ...",
		"grade":        "devel",
		"snap-size":    "1025",
		"timestamp":    time.Now().Format(time.RFC3339),
	}
	snapBuild, err := devDB.Sign(asserts.SnapBuildType, headers, nil, "")
	c.Assert(err, IsNil)

	err = db.Check(snapBuild)
	c.Assert(err, IsNil)
}

func (sbs *snapBuildSuite) TestSnapBuildCheckInconsistentTimestamp(c *C) {
	storeDB, db := makeStoreAndCheckDB(c)
	devDB := setup3rdPartySigning(c, "devel1", storeDB, db)

	headers := map[string]string{
		"series":      "16",
		"snap-id":     "snap-id-1",
		"snap-digest": "sha256 ...",
		"grade":       "devel",
		"snap-size":   "1025",
		"timestamp":   "2013-01-01T14:00:00Z",
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
		"series: 16\n" +
		"snap-id: snap-id-1\n" +
		"snap-digest: sha256 ...\n" +
		"snap-size: 123\n" +
		"snap-revision: 1\n" +
		"developer-id: dev-id1\n" +
		"revision: 1\n" +
		srs.tsLine +
		"body-length: 0" +
		"\n\n" +
		"openpgp c2ln"
}

func (srs *snapRevSuite) makeHeaders(overrides map[string]string) map[string]string {
	headers := map[string]string{
		"authority-id":  "canonical",
		"series":        "16",
		"snap-id":       "snap-id-1",
		"snap-digest":   "sha256 ...",
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
	c.Check(snapRev.Series(), Equals, "16")
	c.Check(snapRev.SnapID(), Equals, "snap-id-1")
	c.Check(snapRev.SnapDigest(), Equals, "sha256 ...")
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
	invalidTests := []struct{ original, invalid, expectedErr string }{
		{"series: 16\n", "", `"series" header is mandatory`},
		{"series: 16\n", "series: \n", `"series" header should not be empty`},
		{"snap-id: snap-id-1\n", "", `"snap-id" header is mandatory`},
		{"snap-id: snap-id-1\n", "snap-id: \n", `"snap-id" header should not be empty`},
		{"snap-digest: sha256 ...\n", "", `"snap-digest" header is mandatory`},
		{"snap-digest: sha256 ...\n", "snap-digest: \n", `"snap-digest" header should not be empty`},
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
	snapDecl, err := storeDB.Sign(asserts.SnapDeclarationType, map[string]string{
		"series":       "16",
		"snap-id":      "snap-id-1",
		"snap-name":    "foo",
		"publisher-id": "dev-id1",
		"gates":        "",
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

	headers := srs.makeHeaders(map[string]string{
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
		"series":      "16",
		"snap-id":     headers["snap-id"],
		"snap-digest": headers["snap-digest"],
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
