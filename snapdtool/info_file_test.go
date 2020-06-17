// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019-2020 Canonical Ltd
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

package snapdtool_test

import (
	"io/ioutil"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/snapdtool"
)

type infoFileSuite struct{}

var _ = Suite(&infoFileSuite{})

func (s *infoFileSuite) TestNoVersionFile(c *C) {
	_, err := snapdtool.SnapdVersionFromInfoFile("/non-existing-file")
	c.Assert(err, ErrorMatches, `cannot open snapd info file "/non-existing-file":.*`)
}

func (s *infoFileSuite) TestNoVersionData(c *C) {
	top := c.MkDir()
	infoFile := filepath.Join(top, "info")
	c.Assert(ioutil.WriteFile(infoFile, []byte("foo"), 0644), IsNil)

	_, err := snapdtool.SnapdVersionFromInfoFile(infoFile)
	c.Assert(err, ErrorMatches, `cannot find snapd version information in "foo"`)
}

func (s *infoFileSuite) TestVersionHappy(c *C) {
	top := c.MkDir()
	infoFile := filepath.Join(top, "info")
	c.Assert(ioutil.WriteFile(infoFile, []byte("VERSION=1.2.3"), 0644), IsNil)

	ver, err := snapdtool.SnapdVersionFromInfoFile(infoFile)
	c.Assert(err, IsNil)
	c.Check(ver, Equals, "1.2.3")
}
