// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2016 Canonical Ltd
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

package snappy

import (
	"os"

	"github.com/ubuntu-core/snappy/dirs"

	. "gopkg.in/check.v1"
)

type overlordTestSuite struct {
	tempdir string
}

var _ = Suite(&overlordTestSuite{})

func (o *overlordTestSuite) SetUpTest(c *C) {
	o.tempdir = c.MkDir()
	dirs.SetRootDir(o.tempdir)

	os.MkdirAll(dirs.SnapServicesDir, 0755)
	os.MkdirAll(dirs.SnapSeccompDir, 0755)

	makeMockSecurityEnv(c)
}

func (o *overlordTestSuite) TestInstall(c *C) {
	overlord := &Overlord{}

	snapFileName := makeTestSnapPackage(c, "name: foo\nversion: 1")
	name, err := overlord.Install(snapFileName, testOrigin, &MockProgressMeter{}, 0)
	c.Assert(err, IsNil)
	c.Assert(name, Equals, "foo")

	installed, err := NewMetaLocalRepository().Installed()
	c.Assert(err, IsNil)
	c.Assert(installed, HasLen, 1)
	c.Assert(installed[0].Name(), Equals, "foo")
	c.Assert(installed[0].Origin(), Equals, testOrigin)
}
