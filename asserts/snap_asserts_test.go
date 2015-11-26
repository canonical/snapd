// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2015 Canonical Ltd
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
	"bytes"
	"encoding/base64"
	"encoding/hex"
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

var _ = Suite(&snapDeclSuite{})

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
		{"grade: stable\n", "grade: zzz\n", `grade must be either "stable" or "devel"`},
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

func (sds *snapDeclSuite) TestSnapDeclarationCheck(c *C) {
	trustedKey := testPrivKey0
	accPrivKey := testPrivKey1

	// TODO: crypto.go should have helpers for this related to exporting
	buf := new(bytes.Buffer)
	err := accPrivKey.PublicKey.Serialize(buf)
	c.Assert(err, IsNil)
	accPubKeyBody := "openpgp " + base64.StdEncoding.EncodeToString(buf.Bytes())
	accFingerp := hex.EncodeToString(testPrivKey1.PublicKey.Fingerprint[:])

	headers := map[string]string{
		"authority-id": "canonical",
		"account-id":   "dev-id1",
		"fingerprint":  accFingerp,
		"since":        "2015-11-20T15:04:00Z",
		"until":        "2500-11-20T15:04:00Z",
	}
	accKey, err := asserts.BuildAndSignInTest(asserts.AccountKeyType, headers, []byte(accPubKeyBody), trustedKey)
	c.Assert(err, IsNil)

	rootDir := filepath.Join(c.MkDir(), "asserts-db")
	cfg := &asserts.DatabaseConfig{
		Path: rootDir,
		TrustedKeys: map[string][]asserts.PublicKey{
			"canonical": {asserts.WrapPublicKey(&trustedKey.PublicKey)},
		},
	}
	db, err := asserts.OpenDatabase(cfg)
	c.Assert(err, IsNil)

	err = db.Add(accKey)
	c.Assert(err, IsNil)

	headers = map[string]string{
		"authority-id": "dev-id1",
		"snap-id":      "snap-id-1",
		"snap-digest":  "sha256 ...",
		"grade":        "devel",
		"snap-size":    "1025",
		"timestamp":    "2015-11-25T20:00:00Z",
	}
	snapDecl, err := asserts.BuildAndSignInTest(asserts.SnapDeclarationType, headers, nil, accPrivKey)
	c.Assert(err, IsNil)

	err = db.Check(snapDecl)
	c.Assert(err, IsNil)
}
