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

package osutil

import (
	"io/ioutil"
	"os"

	. "gopkg.in/check.v1"
)

func (s *cpSuite) TestCpMulti(c *C) {
	maxcp = 2
	defer func() { maxcp = maxint }()

	c.Check(CopyFile(s.f1, s.f2, CopyFlagDefault), IsNil)
	bs, err := ioutil.ReadFile(s.f2)
	c.Check(err, IsNil)
	c.Check(bs, DeepEquals, s.data)
}

func (s *cpSuite) TestDoCpErr(c *C) {
	f1, err := os.Open(s.f1)
	c.Assert(err, IsNil)
	st, err := f1.Stat()
	c.Assert(err, IsNil)
	// force an error by asking it to write to a readonly stream
	c.Check(doCopyFile(f1, os.Stdin, st), NotNil)
}

func (s *cpSuite) TestCopyPreserveAll(c *C) {
	src := filepath.Join(c.MkDir(), "meep")
	dst := filepath.Join(c.MkDir(), "copied-meep")

	err := ioutil.WriteFile(src, []byte(nil), 0644)
	c.Assert(err, IsNil)

	// Give the file a different mtime to ensure CopyFlagPreserveAll
	// really works.
	//
	// You wonder why "touch" is used? And want to me about
	// syscall.Utime()? Well, syscall not implemented on armhf
	// Aha, syscall.Utimes() then? No, not implemented on arm64
	// Really, this is a just a test, touch is good enough!
	err = exec.Command("touch", src, "-d", "2007-08-23 08:21:42").Run()
	c.Assert(err, IsNil)

	err = CopyFile(src, dst, CopyFlagPreserveAll)
	c.Assert(err, IsNil)

	// ensure that the mtime got preserved
	st1, err := os.Stat(src)
	c.Assert(err, IsNil)
	st2, err := os.Stat(dst)
	c.Assert(err, IsNil)
	c.Assert(st1.ModTime(), Equals, st2.ModTime())
}
