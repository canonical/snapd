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

	"github.com/ubuntu-core/snappy/asserts"
)

type fsKeypairMgrSuite struct{}

var _ = Suite(&fsKeypairMgrSuite{})

func (fsbss *fsKeypairMgrSuite) TestOpenOK(c *C) {
	rootDir := filepath.Join(c.MkDir(), "asserts-db")
	err := os.MkdirAll(rootDir, 0775)
	c.Assert(err, IsNil)

	bs, err := asserts.OpenFilesystemKeypairManager(rootDir)
	c.Check(err, IsNil)
	c.Check(bs, NotNil)
}

func (fsbss *fsKeypairMgrSuite) TestOpenRootNotThere(c *C) {
	parent := filepath.Join(c.MkDir(), "var")
	rootDir := filepath.Join(parent, "asserts-db")
	bs, err := asserts.OpenFilesystemKeypairManager(rootDir)
	// xxx special case not there as error
	c.Assert(err, ErrorMatches, "failed to check assert storage root: .*")
	c.Check(bs, IsNil)
}

func (fsbss *fsKeypairMgrSuite) TestOpenWorldWritableFail(c *C) {
	rootDir := filepath.Join(c.MkDir(), "asserts-db")
	oldUmask := syscall.Umask(0)
	os.MkdirAll(rootDir, 0777)
	syscall.Umask(oldUmask)
	bs, err := asserts.OpenFilesystemKeypairManager(rootDir)
	c.Assert(err, ErrorMatches, "assert storage root unexpectedly world-writable: .*")
	c.Check(bs, IsNil)
}
