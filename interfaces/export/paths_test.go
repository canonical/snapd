// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) Canonical Ltd
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

package export_test

import (
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/interfaces/export"
)

type pathsSuite struct {
	testRoot string
}

var _ = Suite(&pathsSuite{})

func (s *pathsSuite) SetUpTest(c *C) {
	s.testRoot = c.MkDir()
	dirs.SetRootDir(s.testRoot)
}

func (s *pathsSuite) TearDownTest(c *C) {
	dirs.SetRootDir("/")
}

func (s *pathsSuite) TestInterfaceRoot(c *C) {
	root := export.InterfaceRoot("egl-driver-libs")
	c.Check(root, Equals, filepath.Join(s.testRoot, "var/lib/snapd/export/system/egl-driver-libs"))
}

func (s *pathsSuite) TestInterfaceRootFollowsRootDir(c *C) {
	before := export.InterfaceRoot("egl-driver-libs")
	otherRoot := c.MkDir()
	dirs.SetRootDir(otherRoot)
	after := export.InterfaceRoot("egl-driver-libs")
	c.Check(before, Not(Equals), after)
	c.Check(after, Equals, filepath.Join(otherRoot, "var/lib/snapd/export/system/egl-driver-libs"))
}

func (s *pathsSuite) TestUnitDir(c *C) {
	dir := export.UnitDir("egl-driver-libs", "foo_egl-slot_5")
	c.Check(dir, Equals, filepath.Join(export.InterfaceRoot("egl-driver-libs"), "foo_egl-slot_5"))
}

func (s *pathsSuite) TestUnitTmpDirIsSiblingOfUnitDir(c *C) {
	unit := export.UnitDir("egl-driver-libs", "foo_egl-slot_5")
	tmp := export.UnitTmpDir("egl-driver-libs", "foo_egl-slot_5")

	c.Check(tmp, Equals, unit+".tmp")
	c.Check(filepath.Dir(tmp), Equals, filepath.Dir(unit))
}

func (s *pathsSuite) TestManifestPath(c *C) {
	manifest := export.ManifestPath("egl-driver-libs")
	c.Check(manifest, Equals, filepath.Join(export.InterfaceRoot("egl-driver-libs"), "export.sources"))
}

func (s *pathsSuite) TestPathsForDifferentInterfacesDoNotCollide(c *C) {
	c.Check(export.InterfaceRoot("egl-driver-libs"), Not(Equals), export.InterfaceRoot("vulkan-driver-libs"))
	c.Check(export.ManifestPath("egl-driver-libs"), Not(Equals), export.ManifestPath("vulkan-driver-libs"))
}
