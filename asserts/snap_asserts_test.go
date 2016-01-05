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

type snapBuildSuite struct {
	ts     time.Time
	tsLine string
}

var _ = Suite(&snapBuildSuite{})

func (sds *snapBuildSuite) SetUpSuite(c *C) {
	sds.ts = time.Now().Truncate(time.Second).UTC()
	sds.tsLine = "timestamp: " + sds.ts.Format(time.RFC3339) + "\n"
}

func (sds *snapBuildSuite) TestDecodeOK(c *C) {
	encoded := "type: snap-build\n" +
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
	c.Check(a.Type(), Equals, asserts.SnapBuildType)
	snapBuild := a.(*asserts.SnapBuild)
	c.Check(snapBuild.AuthorityID(), Equals, "dev-id1")
	c.Check(snapBuild.Timestamp(), Equals, sds.ts)
	c.Check(snapBuild.SnapID(), Equals, "snap-id-1")
	c.Check(snapBuild.SnapDigest(), Equals, "sha256 ...")
	c.Check(snapBuild.SnapSize(), Equals, uint64(10000))
	c.Check(snapBuild.Grade(), Equals, "stable")
}

const (
	snapBuildErrPrefix = "assertion snap-build: "
)

func (sds *snapBuildSuite) TestDecodeInvalid(c *C) {
	encoded := "type: snap-build\n" +
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
		c.Check(err, ErrorMatches, snapBuildErrPrefix+test.expectedErr)
	}
}

func makeSignAndCheckDbWithAccountKey(c *C) (accFingerp string, accSignDB, checkDB *asserts.Database) {
	trustedKey := testPrivKey0

	cfg1 := &asserts.DatabaseConfig{Path: filepath.Join(c.MkDir(), "asserts-db1")}
	accSignDB, err := asserts.OpenDatabase(cfg1)
	c.Assert(err, IsNil)
	accFingerp, err = accSignDB.ImportKey("dev-id1", asserts.OpenPGPPrivateKey(testPrivKey1))
	c.Assert(err, IsNil)
	pubKey, err := accSignDB.PublicKey("dev-id1", accFingerp)
	c.Assert(err, IsNil)
	pubKeyEncoded, err := asserts.EncodePublicKey(pubKey)
	c.Assert(err, IsNil)
	accPubKeyBody := string(pubKeyEncoded)

	headers := map[string]string{
		"authority-id": "canonical",
		"account-id":   "dev-id1",
		"fingerprint":  accFingerp,
		"since":        "2015-11-20T15:04:00Z",
		"until":        "2500-11-20T15:04:00Z",
	}
	accKey, err := asserts.BuildAndSignInTest(asserts.AccountKeyType, headers, []byte(accPubKeyBody), asserts.OpenPGPPrivateKey(trustedKey))
	c.Assert(err, IsNil)

	rootDir := filepath.Join(c.MkDir(), "asserts-db")
	cfg := &asserts.DatabaseConfig{
		Path:        rootDir,
		TrustedKeys: []*asserts.AccountKey{asserts.BuildBootstrapAccountKeyForTest("canonical", &trustedKey.PublicKey)},
	}
	checkDB, err = asserts.OpenDatabase(cfg)
	c.Assert(err, IsNil)

	err = checkDB.Add(accKey)
	c.Assert(err, IsNil)

	return accFingerp, accSignDB, checkDB
}

func (sds *snapBuildSuite) TestSnapBuildCheck(c *C) {
	accFingerp, accSignDB, db := makeSignAndCheckDbWithAccountKey(c)

	headers := map[string]string{
		"authority-id": "dev-id1",
		"snap-id":      "snap-id-1",
		"snap-digest":  "sha256 ...",
		"grade":        "devel",
		"snap-size":    "1025",
		"timestamp":    "2015-11-25T20:00:00Z",
	}
	snapBuild, err := accSignDB.Sign(asserts.SnapBuildType, headers, nil, accFingerp)
	c.Assert(err, IsNil)

	err = db.Check(snapBuild)
	c.Assert(err, IsNil)
}

func (sds *snapBuildSuite) TestSnapBuildCheckInconsistentTimestamp(c *C) {
	accFingerp, accSignDB, db := makeSignAndCheckDbWithAccountKey(c)

	headers := map[string]string{
		"authority-id": "dev-id1",
		"snap-id":      "snap-id-1",
		"snap-digest":  "sha256 ...",
		"grade":        "devel",
		"snap-size":    "1025",
		"timestamp":    "2013-01-01T14:00:00Z",
	}
	snapBuild, err := accSignDB.Sign(asserts.SnapBuildType, headers, nil, accFingerp)
	c.Assert(err, IsNil)

	err = db.Check(snapBuild)
	c.Assert(err, ErrorMatches, "signature verifies but assertion violates other knowledge: snap-build timestamp outside of signing key validity")
}
