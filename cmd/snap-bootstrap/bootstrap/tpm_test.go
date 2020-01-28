// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020 Canonical Ltd
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
package bootstrap_test

import (
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/cmd/snap-bootstrap/bootstrap"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/testutil"
)

type bootstrapTPMSuite struct {
	testutil.BaseTest

	dir string
}

var _ = Suite(&bootstrapTPMSuite{})

func (s *bootstrapTPMSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)

	s.dir = c.MkDir()
	dirs.SetRootDir(s.dir)
	s.AddCleanup(func() { dirs.SetRootDir("/") })
}

func (s *bootstrapTPMSuite) TestSetFiles(c *C) {
	t := &bootstrap.TPMSupport{}

	p1 := filepath.Join(s.dir, "f1")
	f, err := os.Create(p1)
	f.Close()

	// set shim files
	err = t.SetShimFiles("foo")
	c.Assert(err, ErrorMatches, "file foo does not exist")
	err = t.SetShimFiles(p1, "bar")
	c.Assert(err, ErrorMatches, "file bar does not exist")
	err = t.SetShimFiles(p1)
	c.Assert(err, IsNil)

	// set bootloader
	err = t.SetBootloaderFiles("foo")
	c.Assert(err, ErrorMatches, "file foo does not exist")
	err = t.SetBootloaderFiles(p1, "bar")
	c.Assert(err, ErrorMatches, "file bar does not exist")
	err = t.SetBootloaderFiles(p1)
	c.Assert(err, IsNil)

	// set kernel files
	err = t.SetKernelFiles("foo")
	c.Assert(err, ErrorMatches, "file foo does not exist")
	err = t.SetKernelFiles(p1, "bar")
	c.Assert(err, ErrorMatches, "file bar does not exist")
	err = t.SetKernelFiles(p1)
	c.Assert(err, IsNil)
}
