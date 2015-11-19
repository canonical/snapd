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
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/ubuntu-core/snappy/dirs"
	"github.com/ubuntu-core/snappy/helpers"
	"github.com/ubuntu-core/snappy/pkg/squashfs"
	"github.com/ubuntu-core/snappy/systemd"

	. "gopkg.in/check.v1"
)

type SquashfsTestSuite struct {
}

func (s *SquashfsTestSuite) SetUpTest(c *C) {
	dirs.SetRootDir(c.MkDir())

	os.MkdirAll(filepath.Join(dirs.SnapServicesDir, "multi-user.target.wants"), 0755)

	// ensure we do not run a real systemd
	systemd.SystemctlCmd = func(cmd ...string) ([]byte, error) {
		return []byte("ActiveState=inactive\n"), nil
	}

	// ensure we use the right builder func (squashfs)
	snapBuilderFunc = BuildSquashfsSnap
}

func (s *SquashfsTestSuite) TearDownTest(c *C) {
	snapBuilderFunc = BuildLegacySnap
}

var _ = Suite(&SquashfsTestSuite{})

const packageHello = `name: hello-app
version: 1.10
icon: meta/hello.svg
`

func (s *SquashfsTestSuite) TestMakeSnapMakesSquashfs(c *C) {
	snapPkg := makeTestSnapPackage(c, packageHello)
	part, err := NewSnapPartFromSnapFile(snapPkg, "origin", true)
	c.Assert(err, IsNil)

	// ensure the right backend got picked up
	c.Assert(part.deb, FitsTypeOf, &squashfs.Snap{})
}

func (s *SquashfsTestSuite) TestInstallViaSquashfsWorks(c *C) {
	snapPkg := makeTestSnapPackage(c, packageHello)
	part, err := NewSnapPartFromSnapFile(snapPkg, "origin", true)
	c.Assert(err, IsNil)

	_, err = part.Install(&MockProgressMeter{}, 0)
	c.Assert(err, IsNil)

	// after install the blob is in the right dir
	c.Assert(helpers.FileExists(filepath.Join(dirs.SnapBlobDir, "hello-app.origin_1.10.snap")), Equals, true)

	// ensure the right unit is created
	mup := systemd.MountUnitPath("/apps/hello-app.origin/1.10", "mount")
	content, err := ioutil.ReadFile(mup)
	c.Assert(err, IsNil)
	c.Assert(string(content), Matches, "(?ms).*^Where=/apps/hello-app.origin/1.10")
	c.Assert(string(content), Matches, "(?ms).*^What=/var/lib/snappy/snaps/hello-app.origin_1.10.snap")
}

func (s *SquashfsTestSuite) TestAddSquashfsMount(c *C) {
	m := packageYaml{
		Name:          "foo.origin",
		Version:       "1.0",
		Architectures: []string{"all"},
	}
	inter := &MockProgressMeter{}
	err := m.addSquashfsMount(filepath.Join(dirs.SnapAppsDir, "foo.origin/1.0"), true, inter)
	c.Assert(err, IsNil)

	// ensure correct mount unit
	mount, err := ioutil.ReadFile(filepath.Join(dirs.SnapServicesDir, "apps-foo.origin-1.0.mount"))
	c.Assert(err, IsNil)
	c.Assert(string(mount), Equals, `[Unit]
Description=Squashfs mount unit for foo.origin

[Mount]
What=/var/lib/snappy/snaps/foo.origin_1.0.snap
Where=/apps/foo.origin/1.0
`)

}

func (s *SquashfsTestSuite) TestRemoveSquashfsMountUnit(c *C) {
	m := packageYaml{}
	inter := &MockProgressMeter{}
	err := m.addSquashfsMount(filepath.Join(dirs.SnapAppsDir, "foo.origin/1.0"), true, inter)
	c.Assert(err, IsNil)

	// ensure we have the files
	p := filepath.Join(dirs.SnapServicesDir, "apps-foo.origin-1.0.mount")
	c.Assert(helpers.FileExists(p), Equals, true)

	// now call remove and ensure they are gone
	err = m.removeSquashfsMount(filepath.Join(dirs.SnapAppsDir, "foo.origin/1.0"), inter)
	c.Assert(err, IsNil)
	p = filepath.Join(dirs.SnapServicesDir, "apps-foo.origin-1.0.mount")
	c.Assert(helpers.FileExists(p), Equals, false)
}

func (s *SquashfsTestSuite) TestRemoveViaSquashfsWorks(c *C) {
	snapPkg := makeTestSnapPackage(c, packageHello)
	part, err := NewSnapPartFromSnapFile(snapPkg, "origin", true)
	c.Assert(err, IsNil)

	_, err = part.Install(&MockProgressMeter{}, 0)
	c.Assert(err, IsNil)

	// after install the blob is in the right dir
	c.Assert(helpers.FileExists(filepath.Join(dirs.SnapBlobDir, "hello-app.origin_1.10.snap")), Equals, true)

	// now remove and ensure its gone
	err = part.Uninstall(&MockProgressMeter{})
	c.Assert(err, IsNil)
	c.Assert(helpers.FileExists(filepath.Join(dirs.SnapBlobDir, "hello-app.origin_1.10.snap")), Equals, false)

}
