// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2015 Canonical Ltd
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

	"github.com/ubuntu-core/snappy/asserts"
)

type snapDeclSuite struct {
	ts     time.Time
	tsLine string
}

var (
	_ = Suite(&snapDeclSuite{})
	_ = Suite(&snapRevSuite{})
)

func (sds *snapDeclSuite) SetUpSuite(c *C) {
	sds.ts = time.Now().Truncate(time.Second).UTC()
	sds.tsLine = "timestamp: " + sds.ts.Format(time.RFC3339) + "\n"
}

func (sds *snapDeclSuite) TestDecodeOK(c *C) {
	encoded := "type: snap-declaration\n" +
		"authority-id: dev-id1\n" +
		"snap-id: snap-id-1\n" +
		"snap-digest: sha256 ...\n" +
		"grade: stable\n" +
		"snap-size: 10000\n" +
		sds.tsLine +
		"body-length: 0" +
		"\n\n" +
		"openpgp c2ln"
	a, err := asserts.Decode([]byte(encoded))
	c.Assert(err, IsNil)
	c.Check(a.Type(), Equals, asserts.SnapDeclarationType)
	snapDecl := a.(*asserts.SnapDeclaration)
	c.Check(snapDecl.AuthorityID(), Equals, "dev-id1")
	c.Check(snapDecl.Timestamp(), Equals, sds.ts)
	c.Check(snapDecl.SnapID(), Equals, "snap-id-1")
	c.Check(snapDecl.SnapDigest(), Equals, "sha256 ...")
	c.Check(snapDecl.SnapSize(), Equals, uint64(10000))
	c.Check(snapDecl.Grade(), Equals, "stable")
}

const (
	snapDeclErrPrefix = "assertion snap-declaration: "
)

func (sds *snapDeclSuite) TestDecodeInvalid(c *C) {
	encoded := "type: snap-declaration\n" +
		"authority-id: dev-id1\n" +
		"snap-id: snap-id-1\n" +
		"snap-digest: sha256 ...\n" +
		"grade: stable\n" +
		"snap-size: 10000\n" +
		sds.tsLine +
		"body-length: 0" +
		"\n\n" +
		"openpgp c2ln"

	invalidTests := []struct{ original, invalid, expectedErr string }{
		{"snap-id: snap-id-1\n", "", `"snap-id" header is mandatory`},
		{"snap-digest: sha256 ...\n", "", `"snap-digest" header is mandatory`},
		{"grade: stable\n", "", `"grade" header is mandatory`},
		{"snap-size: 10000\n", "", `"snap-size" header is mandatory`},
		{"snap-size: 10000\n", "snap-size: -1\n", `"snap-size" header is not an unsigned integer: -1`},
		{"snap-size: 10000\n", "snap-size: zzz\n", `"snap-size" header is not an unsigned integer: zzz`},
		{sds.tsLine, "timestamp: 12:30\n", `"timestamp" header is not a RFC3339 date: .*`},
	}

	for _, test := range invalidTests {
		invalid := strings.Replace(encoded, test.original, test.invalid, 1)
		_, err := asserts.Decode([]byte(invalid))
		c.Check(err, ErrorMatches, snapDeclErrPrefix+test.expectedErr)
	}
}

func makeSignAndCheckDbWithAccountKey(c *C, accountID string) (accFingerp string, accSignDB, checkDB *asserts.Database) {
	trustedKey := testPrivKey0

	cfg1 := &asserts.DatabaseConfig{Path: filepath.Join(c.MkDir(), "asserts-db1")}
	accSignDB, err := asserts.OpenDatabase(cfg1)
	c.Assert(err, IsNil)
	accFingerp, err = accSignDB.ImportKey(accountID, asserts.OpenPGPPrivateKey(testPrivKey1))
	c.Assert(err, IsNil)
	pubKey, err := accSignDB.PublicKey(accountID, accFingerp)
	c.Assert(err, IsNil)
	pubKeyEncoded, err := asserts.EncodePublicKey(pubKey)
	c.Assert(err, IsNil)
	accPubKeyBody := string(pubKeyEncoded)

	headers := map[string]string{
		"authority-id": "canonical",
		"account-id":   accountID,
		"fingerprint":  accFingerp,
		"since":        "2015-11-20T15:04:00Z",
		"until":        "2500-11-20T15:04:00Z",
	}
	accKey, err := asserts.BuildAndSignInTest(asserts.AccountKeyType, headers, []byte(accPubKeyBody), asserts.OpenPGPPrivateKey(trustedKey))
	c.Assert(err, IsNil)

	rootDir := filepath.Join(c.MkDir(), "asserts-db")
	cfg := &asserts.DatabaseConfig{
		Path: rootDir,
		TrustedKeys: map[string][]asserts.PublicKey{
			"canonical": {asserts.OpenPGPPublicKey(&trustedKey.PublicKey)},
		},
	}
	checkDB, err = asserts.OpenDatabase(cfg)
	c.Assert(err, IsNil)

	err = checkDB.Add(accKey)
	c.Assert(err, IsNil)

	return accFingerp, accSignDB, checkDB
}

func (sds *snapDeclSuite) TestSnapDeclarationCheck(c *C) {
	accFingerp, accSignDB, db := makeSignAndCheckDbWithAccountKey(c, "dev-id1")

	headers := map[string]string{
		"authority-id": "dev-id1",
		"snap-id":      "snap-id-1",
		"snap-digest":  "sha256 ...",
		"grade":        "devel",
		"snap-size":    "1025",
		"timestamp":    "2015-11-25T20:00:00Z",
	}
	snapDecl, err := accSignDB.Sign(asserts.SnapDeclarationType, headers, nil, accFingerp)
	c.Assert(err, IsNil)

	err = db.Check(snapDecl)
	c.Assert(err, IsNil)
}

func (sds *snapDeclSuite) TestSnapDeclarationCheckInconsistentTimestamp(c *C) {
	accFingerp, accSignDB, db := makeSignAndCheckDbWithAccountKey(c, "dev-id1")

	headers := map[string]string{
		"authority-id": "dev-id1",
		"snap-id":      "snap-id-1",
		"snap-digest":  "sha256 ...",
		"grade":        "devel",
		"snap-size":    "1025",
		"timestamp":    "2013-01-01T14:00:00Z",
	}
	snapDecl, err := accSignDB.Sign(asserts.SnapDeclarationType, headers, nil, accFingerp)
	c.Assert(err, IsNil)

	err = db.Check(snapDecl)
	c.Assert(err, ErrorMatches, "signature verifies but assertion violates other knowledge: snap-declaration timestamp outside of signing key validity")
}

type snapRevSuite struct {
	ts           time.Time
	tsLine       string
	validEncoded string
}

func (suite *snapRevSuite) SetUpSuite(c *C) {
	suite.ts = time.Now().Truncate(time.Second).UTC()
	suite.tsLine = "timestamp: " + suite.ts.Format(time.RFC3339) + "\n"
}

func (suite *snapRevSuite) makeValidEncoded() string {
	return "type: snap-revision\n" +
		"authority-id: store-id1\n" +
		"snap-id: snap-id-1\n" +
		"snap-digest: sha256 ...\n" +
		"sequence: 1\n" +
		"snap-declaration: sha256 ...\n" +
		"developer-id: dev-id1\n" +
		"revision: 1\n" +
		suite.tsLine +
		"body-length: 0" +
		"\n\n" +
		"openpgp c2ln"
}

func (suite *snapRevSuite) makeHeaders(overrides map[string]string) map[string]string {
	headers := map[string]string{
		"authority-id":     "store-id1",
		"snap-id":          "snap-id-1",
		"snap-digest":      "sha256 ...",
		"sequence":         "1",
		"snap-declaration": "sha256 ...",
		"developer-id":     "dev-id1",
		"revision":         "1",
		"timestamp":        "2015-11-25T20:00:00Z",
	}
	for k, v := range overrides {
		headers[k] = v
	}
	return headers
}

func (suite *snapRevSuite) TestDecodeOK(c *C) {
	encoded := suite.makeValidEncoded()
	a, err := asserts.Decode([]byte(encoded))
	c.Assert(err, IsNil)
	c.Check(a.Type(), Equals, asserts.SnapRevisionType)
	snapRev := a.(*asserts.SnapRevision)
	c.Check(snapRev.AuthorityID(), Equals, "store-id1")
	c.Check(snapRev.Timestamp(), Equals, suite.ts)
	c.Check(snapRev.SnapID(), Equals, "snap-id-1")
	c.Check(snapRev.SnapDigest(), Equals, "sha256 ...")
	c.Check(snapRev.Sequence(), Equals, uint64(1))
	c.Check(snapRev.SnapDeclaration(), Equals, "sha256 ...")
	c.Check(snapRev.DeveloperID(), Equals, "dev-id1")
	c.Check(snapRev.Revision(), Equals, 1)
}

const (
	snapRevErrPrefix = "assertion snap-revision: "
)

func (suite *snapRevSuite) TestDecodeInvalid(c *C) {
	encoded := suite.makeValidEncoded()
	invalidTests := []struct{ original, invalid, expectedErr string }{
		{"snap-id: snap-id-1\n", "", `"snap-id" header is mandatory`},
		{"snap-digest: sha256 ...\n", "", `"snap-digest" header is mandatory`},
		{"sequence: 1\n", "", `"sequence" header is mandatory`},
		{"sequence: 1\n", "sequence: -1\n", `"sequence" header is not an unsigned integer: -1`},
		{"sequence: 1\n", "sequence: zzz\n", `"sequence" header is not an unsigned integer: zzz`},
		{"snap-declaration: sha256 ...\n", "", `"snap-declaration" header is mandatory`},
		{"developer-id: dev-id1\n", "", `"developer-id" header is mandatory`},
		{suite.tsLine, "timestamp: 12:30\n", `"timestamp" header is not a RFC3339 date: .*`},
	}

	for _, test := range invalidTests {
		invalid := strings.Replace(encoded, test.original, test.invalid, 1)
		_, err := asserts.Decode([]byte(invalid))
		c.Check(err, ErrorMatches, snapRevErrPrefix+test.expectedErr)
	}
}

func (suite *snapRevSuite) TestSnapRevisionCheck(c *C) {
	accFingerp, accSignDB, db := makeSignAndCheckDbWithAccountKey(c, "store-id1")

	headers := suite.makeHeaders(nil)
	snapRev, err := accSignDB.Sign(asserts.SnapRevisionType, headers, nil, accFingerp)
	c.Assert(err, IsNil)

	err = db.Check(snapRev)
	c.Assert(err, IsNil)
}

func (suite *snapRevSuite) TestSnapRevisionCheckInconsistentTimestamp(c *C) {
	accFingerp, accSignDB, db := makeSignAndCheckDbWithAccountKey(c, "store-id1")

	headers := suite.makeHeaders(map[string]string{
		"timestamp": "2013-01-01T14:00:00Z",
	})
	snapRev, err := accSignDB.Sign(asserts.SnapRevisionType, headers, nil, accFingerp)
	c.Assert(err, IsNil)

	err = db.Check(snapRev)
	c.Assert(err, ErrorMatches, "signature verifies but assertion violates other knowledge: snap-revision timestamp outside of signing key validity")
}

func (suite *snapRevSuite) TestPrimaryKey(c *C) {
	headers := suite.makeHeaders(nil)

	accFingerp, accSignDB, db := makeSignAndCheckDbWithAccountKey(c, "store-id1")
	snapRev, err := accSignDB.Sign(asserts.SnapRevisionType, headers, nil, accFingerp)
	c.Assert(err, IsNil)
	err = db.Add(snapRev)
	c.Assert(err, IsNil)

	_, err = db.Find(asserts.SnapRevisionType, map[string]string{
		"snap-id":     headers["snap-id"],
		"snap-digest": headers["snap-digest"],
	})
	c.Assert(err, IsNil)
}
