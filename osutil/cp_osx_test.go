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
	"os/exec"
	"path/filepath"

	. "gopkg.in/check.v1"
)

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
	err = exec.Command("touch", "-t", "200708230821.42", src).Run()
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
