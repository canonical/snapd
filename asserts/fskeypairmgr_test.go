// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
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
	"os"
	"path/filepath"
	"syscall"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
)

type fsKeypairMgrSuite struct{}

var _ = Suite(&fsKeypairMgrSuite{})

func (fsbss *fsKeypairMgrSuite) TestOpenOK(c *C) {
	// ensure umask is clean when creating the DB dir
	oldUmask := syscall.Umask(0)
	defer syscall.Umask(oldUmask)

	topDir := filepath.Join(c.MkDir(), "asserts-db")
	err := os.MkdirAll(topDir, 0775)
	c.Assert(err, IsNil)

	bs, err := asserts.OpenFSKeypairManager(topDir)
	c.Check(err, IsNil)
	c.Check(bs, NotNil)

	info, err := os.Stat(filepath.Join(topDir, "private-keys-v1"))
	c.Assert(err, IsNil)
	c.Assert(info.IsDir(), Equals, true)
	c.Check(info.Mode().Perm(), Equals, os.FileMode(0775))
}

func (fsbss *fsKeypairMgrSuite) TestOpenWorldWritableFail(c *C) {
	topDir := filepath.Join(c.MkDir(), "asserts-db")
	// make it world-writable
	oldUmask := syscall.Umask(0)
	os.MkdirAll(filepath.Join(topDir, "private-keys-v1"), 0777)
	syscall.Umask(oldUmask)

	bs, err := asserts.OpenFSKeypairManager(topDir)
	c.Assert(err, ErrorMatches, "assert storage root unexpectedly world-writable: .*")
	c.Check(bs, IsNil)
}

func (fsbss *fsKeypairMgrSuite) TestDelete(c *C) {
	// ensure umask is clean when creating the DB dir
	oldUmask := syscall.Umask(0)
	defer syscall.Umask(oldUmask)

	topDir := filepath.Join(c.MkDir(), "asserts-db")
	err := os.MkdirAll(topDir, 0775)
	c.Assert(err, IsNil)

	keypairMgr, err := asserts.OpenFSKeypairManager(topDir)
	c.Check(err, IsNil)

	pk1 := testPrivKey1
	keyID := pk1.PublicKey().ID()
	err = keypairMgr.Put(pk1)
	c.Assert(err, IsNil)

	_, err = keypairMgr.Get(keyID)
	c.Assert(err, IsNil)

	err = keypairMgr.Delete(keyID)
	c.Assert(err, IsNil)

	err = keypairMgr.Delete(keyID)
	c.Check(err, ErrorMatches, "cannot find key pair")
	c.Check(asserts.IsKeyNotFound(err), Equals, true)

	_, err = keypairMgr.Get(keyID)
	c.Check(err, ErrorMatches, "cannot find key pair")
	c.Check(asserts.IsKeyNotFound(err), Equals, true)
}
