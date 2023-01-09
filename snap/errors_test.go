// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2023 Canonical Ltd
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

package snap_test

import (
	"io/ioutil"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/snap"
)

type errorsSuite struct{}

var _ = Suite(&errorsSuite{})

func (s *errorsSuite) TestNotSnapErrorNoSuchFile(c *C) {
	err := snap.NewNotSnapErrorWithContext("non-existing-file")
	c.Check(err, ErrorMatches, `cannot process snap or snapdir: open non-existing-file: no such file or directory`)
}

func (s *errorsSuite) TestNotSnapErrorEmptyDir(c *C) {
	err := snap.NewNotSnapErrorWithContext(c.MkDir())
	c.Check(err, ErrorMatches, `cannot process snap or snapdir: directory ".*" is empty`)
}

func (s *errorsSuite) TestNotSnapErrorInvalidDir(c *C) {
	tmpdir := c.MkDir()
	err := ioutil.WriteFile(filepath.Join(tmpdir, "foo"), nil, 0644)
	c.Assert(err, IsNil)
	err = snap.NewNotSnapErrorWithContext(tmpdir)
	c.Check(err, ErrorMatches, `cannot process snap or snapdir: directory ".*" is invalid`)
}

func (s *errorsSuite) TestNotSnapErrorInvalidFile(c *C) {
	badFile := filepath.Join(c.MkDir(), "foo")
	err := ioutil.WriteFile(badFile, []byte("not-a-snap-and-much-much-more-content"), 0644)
	c.Assert(err, IsNil)
	err = snap.NewNotSnapErrorWithContext(badFile)
	c.Check(err, ErrorMatches, `cannot process snap or snapdir: file ".*" is invalid \(header \[110 111 116 45 97 45 115 110 97 112\]\)`)
}
