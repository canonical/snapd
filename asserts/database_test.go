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
	"io/ioutil"
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

	fingerp, err := dbs.db.ImportKey("account0", asserts.WrapPrivateKey(testPrivKey1))
	c.Assert(err, IsNil)
	c.Check(fingerp, Equals, expectedFingerprint)

	keyPath := filepath.Join(dbs.rootDir, "private-keys-v0/account0", fingerp)
	info, err := os.Stat(keyPath)
	c.Assert(err, IsNil)
	c.Check(info.Mode().Perm(), Equals, os.FileMode(0600)) // secret
	// too white box? ok at least until we have more functionality
	privKey, err := ioutil.ReadFile(keyPath)
	c.Assert(err, IsNil)

	privKeyFromDisk, err := asserts.ParsePrivateKeyInTest(privKey)
	c.Assert(err, IsNil)

	c.Check(privKeyFromDisk.PublicKey().Fingerprint(), Equals, expectedFingerprint)
}

func (dbs *databaseSuite) TestGenerateKey(c *C) {
	fingerp, err := dbs.db.GenerateKey("account0")
	c.Assert(err, IsNil)
	c.Check(fingerp, NotNil)
	keyPath := filepath.Join(dbs.rootDir, "private-keys-v0/account0", fingerp)
	c.Check(helpers.FileExists(keyPath), Equals, true)
}

func (dbs *databaseSuite) TestExportPublicKey(c *C) {
	fingerp, err := dbs.db.ImportKey("account0", asserts.WrapPrivateKey(testPrivKey1))
	c.Assert(err, IsNil)

	pubk, err := dbs.db.ExportPublicKey("account0", fingerp[len(fingerp)-8:])
	c.Assert(err, IsNil)
	c.Check(pubk.Fingerprint(), Equals, fingerp)

	// usual pattern is to then encode it
	encoded, err := asserts.EncodePublicKey(pubk)
	c.Assert(err, IsNil)
	c.Check(bytes.HasPrefix(encoded, []byte("openpgp ")), Equals, true)
	data, err := base64.StdEncoding.DecodeString(string(encoded[len("openpgp "):]))
	c.Assert(err, IsNil)
	pkt, err := packet.Read(bytes.NewBuffer(data))
	c.Assert(err, IsNil)
	pubKey, ok := pkt.(*packet.PublicKey)
	c.Assert(ok, Equals, true)
	c.Assert(pubKey.Fingerprint, DeepEquals, testPrivKey1.PublicKey.Fingerprint)
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
	chks.a, err = asserts.BuildAndSignInTest(asserts.AssertionType("test-only"), headers, nil, asserts.WrapPrivateKey(testPrivKey0))
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

type signAddFindSuite struct {
	signingDB          *asserts.Database
	signingFingerprint string
	db                 *asserts.Database
}

var _ = Suite(&signAddFindSuite{})

func (safs *signAddFindSuite) SetUpTest(c *C) {
	cfg0 := &asserts.DatabaseConfig{Path: filepath.Join(c.MkDir(), "asserts-db0")}
	db0, err := asserts.OpenDatabase(cfg0)
	c.Assert(err, IsNil)
	safs.signingDB = db0

	safs.signingFingerprint, err = db0.ImportKey("canonical", asserts.WrapPrivateKey(testPrivKey0))
	c.Assert(err, IsNil)

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
	safs.db = db
}

func (safs *signAddFindSuite) TestSign(c *C) {
	headers := map[string]string{
		"authority-id": "canonical",
		"primary-key":  "a",
	}
	a1, err := safs.signingDB.Sign(asserts.AssertionType("test-only"), headers, nil, safs.signingFingerprint)
	c.Assert(err, IsNil)

	err = safs.db.Check(a1)
	c.Check(err, IsNil)
}

func (safs *signAddFindSuite) TestSignPickTheOneKeyPair(c *C) {
	headers := map[string]string{
		"authority-id": "canonical",
		"primary-key":  "a",
	}
	a1, err := safs.signingDB.Sign(asserts.AssertionType("test-only"), headers, nil, "")
	c.Assert(err, IsNil)

	err = safs.db.Check(a1)
	c.Check(err, IsNil)
}

func (safs *signAddFindSuite) TestAddSuperseding(c *C) {
	headers := map[string]string{
		"authority-id": "canonical",
		"primary-key":  "a",
	}
	a1, err := safs.signingDB.Sign(asserts.AssertionType("test-only"), headers, nil, "")
	c.Assert(err, IsNil)

	err = safs.db.Add(a1)
	c.Assert(err, IsNil)

	retrieved1, err := safs.db.Find(asserts.AssertionType("test-only"), map[string]string{
		"primary-key": "a",
	})
	c.Assert(err, IsNil)
	c.Check(retrieved1, NotNil)
	c.Check(retrieved1.Revision(), Equals, 0)

	headers["revision"] = "1"
	a2, err := safs.signingDB.Sign(asserts.AssertionType("test-only"), headers, nil, "")
	c.Assert(err, IsNil)

	err = safs.db.Add(a2)
	c.Assert(err, IsNil)

	retrieved2, err := safs.db.Find(asserts.AssertionType("test-only"), map[string]string{
		"primary-key": "a",
	})
	c.Assert(err, IsNil)
	c.Check(retrieved2, NotNil)
	c.Check(retrieved2.Revision(), Equals, 1)

	err = safs.db.Add(a1)
	c.Check(err, ErrorMatches, "assertion added must have more recent revision than current one.*")
}

func (safs *signAddFindSuite) TestFindNotFound(c *C) {
	headers := map[string]string{
		"authority-id": "canonical",
		"primary-key":  "a",
	}
	a1, err := safs.signingDB.Sign(asserts.AssertionType("test-only"), headers, nil, "")
	c.Assert(err, IsNil)

	err = safs.db.Add(a1)
	c.Assert(err, IsNil)

	retrieved1, err := safs.db.Find(asserts.AssertionType("test-only"), map[string]string{
		"primary-key": "b",
	})
	c.Assert(err, Equals, asserts.ErrNotFound)
	c.Check(retrieved1, IsNil)

	// checking also extra headers
	retrieved1, err = safs.db.Find(asserts.AssertionType("test-only"), map[string]string{
		"primary-key":  "a",
		"authority-id": "other-auth-id",
	})
	c.Assert(err, Equals, asserts.ErrNotFound)
	c.Check(retrieved1, IsNil)
}

func (safs *signAddFindSuite) TestFindPrimaryLeftOut(c *C) {
	retrieved1, err := safs.db.Find(asserts.AssertionType("test-only"), map[string]string{})
	c.Assert(err, ErrorMatches, "must provide primary key: primary-key")
	c.Check(retrieved1, IsNil)
}
