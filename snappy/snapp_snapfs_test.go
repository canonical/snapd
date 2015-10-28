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

package snappy

import (
	"path/filepath"

	"github.com/ubuntu-core/snappy/dirs"
	"github.com/ubuntu-core/snappy/helpers"
	"github.com/ubuntu-core/snappy/pkg/snapfs"

	. "gopkg.in/check.v1"
)

type SnapfsTestSuite struct {
}

func (s *SnapfsTestSuite) SetUpTest(c *C) {
	// mocks
	aaClickHookCmd = "/bin/true"
	dirs.SetRootDir(c.MkDir())

	// ensure we use the right builder func (snapfs)
	snapBuilderFunc = BuildSnapfsSnap
}

func (s *SnapfsTestSuite) TearDownTest(c *C) {
	snapBuilderFunc = BuildLegacySnap
}

var _ = Suite(&SnapfsTestSuite{})

const packageHello = `name: hello-app
version: 1.10
vendor: Somebody
icon: meta/hello.svg
`

func (s *SnapfsTestSuite) TestMakeSnapMakesSnapfs(c *C) {
	snapPkg := makeTestSnapPackage(c, packageHello)
	part, err := NewSnapPartFromSnapFile(snapPkg, "origin", true)
	c.Assert(err, IsNil)

	// ensure the right backend got picked up
	c.Assert(part.deb, FitsTypeOf, &snapfs.Snap{})
}

func (s *SnapfsTestSuite) TestInstallViaSnapfsWorks(c *C) {
	snapPkg := makeTestSnapPackage(c, packageHello)
	part, err := NewSnapPartFromSnapFile(snapPkg, "origin", true)
	c.Assert(err, IsNil)

	_, err = part.Install(&MockProgressMeter{}, 0)
	c.Assert(err, IsNil)

	// after install its just on disk for now, note that this will
	// change once the mounting gets added
	base := filepath.Join(dirs.SnapAppsDir, "hello-app.origin", "1.10")
	for _, needle := range []string{
		"bin/foo",
		"meta/package.yaml",
		".click/info/hello-app.origin.manifest",
	} {
		println(needle)
		c.Assert(helpers.FileExists(filepath.Join(base, needle)), Equals, true)
	}
}
