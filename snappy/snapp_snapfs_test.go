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

	"launchpad.net/snappy/dirs"
	"launchpad.net/snappy/helpers"
	"launchpad.net/snappy/pkg/snapfs"
	"launchpad.net/snappy/systemd"

	. "gopkg.in/check.v1"
)

type SnapfsTestSuite struct {
}

func (s *SnapfsTestSuite) SetUpTest(c *C) {
	// mocks
	aaClickHookCmd = "/bin/true"
	dirs.SetRootDir(c.MkDir())
	os.MkdirAll(filepath.Join(dirs.SnapServicesDir, "multi-user.target.wants"), 0755)

	// ensure we do not run a real systemd (slows down tests)
	systemd.SystemctlCmd = func(cmd ...string) ([]byte, error) {
		return []byte("ActiveState=inactive\n"), nil
	}

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

	// after install the blob is in the right dir
	c.Assert(helpers.FileExists(filepath.Join(dirs.SnapBlobDir, "hello-app.origin_1.10.snap")), Equals, true)

	// ensure the right unit is created
	mup := systemd.MountUnitPath("/apps/hello-app.origin/1.10", "mount")
	content, err := ioutil.ReadFile(mup)
	c.Assert(err, IsNil)
	c.Assert(string(content), Matches, "(?ms).*^Where=/apps/hello-app.origin/1.10")
	c.Assert(string(content), Matches, "(?ms).*^What=/var/lib/snappy/snaps/hello-app.origin_1.10.snap")
}

func (s *SnapfsTestSuite) TestAddSnapfsAutomount(c *C) {
	m := packageYaml{
		Name:          "foo.origin",
		Version:       "1.0",
		Architectures: []string{"all"},
	}
	inter := &MockProgressMeter{}
	err := m.addSnapfsAutomount(filepath.Join(dirs.SnapAppsDir, "foo.origin/1.0"), true, inter)
	c.Assert(err, IsNil)

	// ensure correct mount unit
	mount, err := ioutil.ReadFile(filepath.Join(dirs.SnapServicesDir, "apps-foo.origin-1.0.mount"))
	c.Assert(err, IsNil)
	c.Assert(string(mount), Equals, `[Unit]
Description=Snapfs mount unit for foo.origin

[Mount]
What=/var/lib/snappy/snaps/foo.origin_1.0.snap
Where=/apps/foo.origin/1.0
`)

	// and correct automount unit
	automount, err := ioutil.ReadFile(filepath.Join(dirs.SnapServicesDir, "apps-foo.origin-1.0.automount"))
	c.Assert(err, IsNil)
	c.Assert(string(automount), Equals, `[Unit]
Description=Snapfs automount unit for foo.origin

[Automount]
Where=/apps/foo.origin/1.0
TimeoutIdleSec=30

[Install]
WantedBy=multi-user.target
`)
}

func (s *SnapfsTestSuite) TestRemoveSnapfsAutomount(c *C) {
	m := packageYaml{}
	inter := &MockProgressMeter{}
	err := m.addSnapfsAutomount(filepath.Join(dirs.SnapAppsDir, "foo.origin/1.0"), true, inter)
	c.Assert(err, IsNil)

	// ensure we have the files
	for _, ext := range []string{"mount", "automount"} {
		p := filepath.Join(dirs.SnapServicesDir, "apps-foo.origin-1.0.") + ext
		c.Assert(helpers.FileExists(p), Equals, true)
	}

	// now call remove and ensure they are gone
	err = m.removeSnapfsAutomount(filepath.Join(dirs.SnapAppsDir, "foo.origin/1.0"), inter)
	c.Assert(err, IsNil)
	for _, ext := range []string{"mount", "automount"} {
		p := filepath.Join(dirs.SnapServicesDir, "apps-foo.origin-1.0.") + ext
		c.Assert(helpers.FileExists(p), Equals, false)
	}
}

func (s *SnapfsTestSuite) TestRemoveViaSnapfsWorks(c *C) {
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
