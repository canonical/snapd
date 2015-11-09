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

package asserts

import (
	"encoding/hex"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"golang.org/x/crypto/openpgp/packet"
	. "gopkg.in/check.v1"

	"github.com/ubuntu-core/snappy/helpers"
)

func Test(t *testing.T) { TestingT(t) }

type openSuite struct{}

var _ = Suite(&openSuite{})

func (opens *openSuite) TestOpenStoreOK(c *C) {
	rootDir := filepath.Join(c.MkDir(), "astore")
	cfg := &StoreConfig{Path: rootDir}
	astore, err := OpenStore(cfg)
	c.Assert(err, IsNil)
	c.Assert(astore, NotNil)
	c.Check(astore.root, Equals, rootDir)
	info, err := os.Stat(rootDir)
	c.Assert(err, IsNil)
	c.Assert(info.IsDir(), Equals, true)
	c.Check(info.Mode().Perm(), Equals, os.FileMode(0775))
}

func (opens *openSuite) TestOpenStoreWorldWriteableFail(c *C) {
	rootDir := filepath.Join(c.MkDir(), "astore")
	oldUmask := syscall.Umask(0)
	os.MkdirAll(rootDir, 0777)
	syscall.Umask(oldUmask)
	cfg := &StoreConfig{Path: rootDir}
	astore, err := OpenStore(cfg)
	c.Assert(err, Equals, ErrStoreRootWorldReadable)
	c.Check(astore, IsNil)
}

type storeSuite struct {
	rootDir string
	astore  *AssertStore
}

var _ = Suite(&storeSuite{})

func (ss *storeSuite) SetUpTest(c *C) {
	ss.rootDir = filepath.Join(c.MkDir(), "astore")
	cfg := &StoreConfig{Path: ss.rootDir}
	astore, err := OpenStore(cfg)
	c.Assert(err, IsNil)
	ss.astore = astore
}

func (ss *storeSuite) TestAtomicWriteEntrySecret(c *C) {
	err := ss.astore.atomicWriteEntry([]byte("foobar"), true, "a", "b", "foo")
	c.Assert(err, IsNil)
	fooPath := filepath.Join(ss.rootDir, "a", "b", "foo")
	info, err := os.Stat(fooPath)
	c.Assert(err, IsNil)
	c.Check(info.Mode().Perm(), Equals, os.FileMode(0600))
	c.Check(info.Size(), Equals, int64(6))
}

func (ss *storeSuite) TestImportKey(c *C) {
	privk, err := generatePrivateKey()
	c.Assert(err, IsNil)
	expectedFingerprint := privk.PublicKey.Fingerprint[:]

	fingerp, err := ss.astore.ImportKey("account0", privk)
	c.Assert(err, IsNil)
	c.Check(fingerp, DeepEquals, expectedFingerprint)

	keyPath := filepath.Join(ss.rootDir, privateKeysRoot, "account0", hex.EncodeToString(fingerp))
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
	c.Check(privKeyFromDisk.PublicKey.Fingerprint[:], DeepEquals, expectedFingerprint)
}

func (ss *storeSuite) TestGenerateKey(c *C) {
	fingerp, err := ss.astore.GenerateKey("account0")
	c.Assert(err, IsNil)
	c.Check(fingerp, NotNil)
	keyPath := filepath.Join(ss.rootDir, privateKeysRoot, "account0", hex.EncodeToString(fingerp))
	c.Check(helpers.FileExists(keyPath), Equals, true)
}
