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
	"bytes"
	"crypto"
	"encoding/base64"
	"encoding/hex"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"golang.org/x/crypto/openpgp/packet"
	. "gopkg.in/check.v1"

	"github.com/ubuntu-core/snappy/asserts"
	"github.com/ubuntu-core/snappy/helpers"
)

func Test(t *testing.T) { TestingT(t) }

type openSuite struct{}

var _ = Suite(&openSuite{})

func (opens *openSuite) TestOpenDatabaseOK(c *C) {
	// ensure umask has clean when creating the DB dir
	oldUmask := syscall.Umask(0)
	defer func() { syscall.Umask(oldUmask) }()

	rootDir := filepath.Join(c.MkDir(), "asserts-db")
	cfg := &asserts.DatabaseConfig{Path: rootDir}
	db, err := asserts.OpenDatabase(cfg)
	c.Assert(err, IsNil)
	c.Assert(db, NotNil)
	info, err := os.Stat(rootDir)
	c.Assert(err, IsNil)
	c.Assert(info.IsDir(), Equals, true)
	c.Check(info.Mode().Perm(), Equals, os.FileMode(0775))
}

func (opens *openSuite) TestOpenDatabaseRootCreateFail(c *C) {
	parent := filepath.Join(c.MkDir(), "var")
	// make it not writable
	os.MkdirAll(parent, 555)
	rootDir := filepath.Join(parent, "asserts-db")
	cfg := &asserts.DatabaseConfig{Path: rootDir}
	db, err := asserts.OpenDatabase(cfg)
	c.Assert(err, ErrorMatches, "failed to create assert database root: .*")
	c.Check(db, IsNil)
}

func (opens *openSuite) TestOpenDatabaseWorldWritableFail(c *C) {
	rootDir := filepath.Join(c.MkDir(), "asserts-db")
	oldUmask := syscall.Umask(0)
	os.MkdirAll(rootDir, 0777)
	syscall.Umask(oldUmask)
	cfg := &asserts.DatabaseConfig{Path: rootDir}
	db, err := asserts.OpenDatabase(cfg)
	c.Assert(err, ErrorMatches, "assert database root unexpectedly world-writable: .*")
	c.Check(db, IsNil)
}

type databaseSuite struct {
	rootDir string
	db      *asserts.Database
}

var _ = Suite(&databaseSuite{})

func (dbs *databaseSuite) SetUpTest(c *C) {
	dbs.rootDir = filepath.Join(c.MkDir(), "asserts-db")
	cfg := &asserts.DatabaseConfig{Path: dbs.rootDir}
	db, err := asserts.OpenDatabase(cfg)
	c.Assert(err, IsNil)
	dbs.db = db
}

func (dbs *databaseSuite) TestImportKey(c *C) {
	expectedFingerprint := hex.EncodeToString(testPrivKey1.PublicKey.Fingerprint[:])

	fingerp, err := dbs.db.ImportKey("account0", testPrivKey1)
	c.Assert(err, IsNil)
	c.Check(fingerp, Equals, expectedFingerprint)

	keyPath := filepath.Join(dbs.rootDir, "private-keys-v0/account0", fingerp)
	info, err := os.Stat(keyPath)
	c.Assert(err, IsNil)
	c.Check(info.Mode().Perm(), Equals, os.FileMode(0600)) // secret
	// too white box? ok at least until we have more functionality
	fpriv, err := os.Open(keyPath)
	c.Assert(err, IsNil)
	pk, err := packet.Read(fpriv)
	c.Assert(err, IsNil)
	privKeyFromDisk, ok := pk.(*packet.PrivateKey)
	c.Assert(ok, Equals, true)
	c.Check(hex.EncodeToString(privKeyFromDisk.PublicKey.Fingerprint[:]), Equals, expectedFingerprint)
}

func (dbs *databaseSuite) TestGenerateKey(c *C) {
	fingerp, err := dbs.db.GenerateKey("account0")
	c.Assert(err, IsNil)
	c.Check(fingerp, NotNil)
	keyPath := filepath.Join(dbs.rootDir, "private-keys-v0/account0", fingerp)
	c.Check(helpers.FileExists(keyPath), Equals, true)
}

type checkSuite struct {
	rootDir string
	a       asserts.Assertion
}

var _ = Suite(&checkSuite{})

func (chks *checkSuite) SetUpTest(c *C) {
	var err error

	chks.rootDir = filepath.Join(c.MkDir(), "asserts-db")

	headers := map[string]string{
		"authority-id": "canonical",
		"primary-key":  "0",
	}
	chks.a, err = asserts.BuildAndSignInTest(asserts.AssertionType("test-only"), headers, nil, testPrivKey0)
	c.Assert(err, IsNil)
}

func (chks *checkSuite) TestCheckNoPubKey(c *C) {
	cfg := &asserts.DatabaseConfig{Path: chks.rootDir}
	db, err := asserts.OpenDatabase(cfg)
	c.Assert(err, IsNil)

	err = db.Check(chks.a)
	c.Assert(err, ErrorMatches, "no valid known public key verifies assertion")
}

func (chks *checkSuite) TestCheckForgery(c *C) {
	dbTrustedKey := asserts.WrapPublicKey(&testPrivKey0.PublicKey)

	cfg := &asserts.DatabaseConfig{
		Path: chks.rootDir,
		TrustedKeys: map[string][]asserts.PublicKey{
			"canonical": {dbTrustedKey},
		},
	}
	db, err := asserts.OpenDatabase(cfg)
	c.Assert(err, IsNil)

	encoded := asserts.Encode(chks.a)
	content, encodedSig := chks.a.Signature()
	// forgery
	forgedSig := new(packet.Signature)
	forgedSig.PubKeyAlgo = testPrivKey1.PubKeyAlgo
	forgedSig.Hash = crypto.SHA256
	forgedSig.CreationTime = time.Now()
	forgedSig.IssuerKeyId = &testPrivKey0.KeyId
	h := crypto.SHA256.New()
	h.Write(content)
	err = forgedSig.Sign(h, testPrivKey1, &packet.Config{DefaultHash: crypto.SHA256})
	c.Assert(err, IsNil)
	buf := new(bytes.Buffer)
	forgedSig.Serialize(buf)
	forgedSigEncoded := "openpgp " + base64.StdEncoding.EncodeToString(buf.Bytes())
	forgedEncoded := bytes.Replace(encoded, encodedSig, []byte(forgedSigEncoded), 1)
	c.Assert(forgedEncoded, Not(DeepEquals), encoded)

	forgedAssert, err := asserts.Decode(forgedEncoded)
	c.Assert(err, IsNil)

	err = db.Check(forgedAssert)
	c.Assert(err, ErrorMatches, "failed signature verification: .*")
}

type addFindSuite struct {
	db *asserts.Database
}

var _ = Suite(&addFindSuite{})

func (afs *addFindSuite) SetUpTest(c *C) {
	rootDir := filepath.Join(c.MkDir(), "asserts-db")
	dbTrustedKey := asserts.WrapPublicKey(&testPrivKey0.PublicKey)
	cfg := &asserts.DatabaseConfig{
		Path: rootDir,
		TrustedKeys: map[string][]asserts.PublicKey{
			"canonical": {dbTrustedKey},
		},
	}
	db, err := asserts.OpenDatabase(cfg)
	c.Assert(err, IsNil)
	afs.db = db
}

func (afs *addFindSuite) TestAddSuperseding(c *C) {
	headers := map[string]string{
		"authority-id": "canonical",
		"primary-key":  "a",
	}
	a1, err := asserts.BuildAndSignInTest(asserts.AssertionType("test-only"), headers, nil, testPrivKey0)
	c.Assert(err, IsNil)

	err = afs.db.Add(a1)
	c.Assert(err, IsNil)

	retrieved1, err := afs.db.Find(asserts.AssertionType("test-only"), map[string]string{
		"primary-key": "a",
	})
	c.Assert(err, IsNil)
	c.Check(retrieved1, NotNil)
	c.Check(retrieved1.Revision(), Equals, 0)

	headers["revision"] = "1"
	a2, err := asserts.BuildAndSignInTest(asserts.AssertionType("test-only"), headers, nil, testPrivKey0)
	c.Assert(err, IsNil)

	err = afs.db.Add(a2)
	c.Assert(err, IsNil)

	retrieved2, err := afs.db.Find(asserts.AssertionType("test-only"), map[string]string{
		"primary-key": "a",
	})
	c.Assert(err, IsNil)
	c.Check(retrieved2, NotNil)
	c.Check(retrieved2.Revision(), Equals, 1)

	err = afs.db.Add(a1)
	c.Check(err, ErrorMatches, "assertion added must have more recent revision than current one.*")
}

func (afs *addFindSuite) TestFindNotFound(c *C) {
	headers := map[string]string{
		"authority-id": "canonical",
		"primary-key":  "a",
	}
	a1, err := asserts.BuildAndSignInTest(asserts.AssertionType("test-only"), headers, nil, testPrivKey0)
	c.Assert(err, IsNil)

	err = afs.db.Add(a1)
	c.Assert(err, IsNil)

	retrieved1, err := afs.db.Find(asserts.AssertionType("test-only"), map[string]string{
		"primary-key": "b",
	})
	c.Assert(err, Equals, asserts.ErrNotFound)
	c.Check(retrieved1, IsNil)

	// checking also extra headers
	retrieved1, err = afs.db.Find(asserts.AssertionType("test-only"), map[string]string{
		"primary-key":  "a",
		"authority-id": "other-auth-id",
	})
	c.Assert(err, Equals, asserts.ErrNotFound)
	c.Check(retrieved1, IsNil)
}

func (afs *addFindSuite) TestFindPrimaryLeftOut(c *C) {
	retrieved1, err := afs.db.Find(asserts.AssertionType("test-only"), map[string]string{})
	c.Assert(err, ErrorMatches, "must provide primary key: primary-key")
	c.Check(retrieved1, IsNil)
}
