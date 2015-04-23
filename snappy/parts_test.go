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

	. "launchpad.net/gocheck"

	"launchpad.net/snappy/progress"
)

func (s *SnapTestSuite) TestActiveSnapByType(c *C) {
	yamlPath, err := makeInstalledMockSnap(s.tempdir, `name: app1
version: 1.10
vendor: Michael Vogt <mvo@ubuntu.com>
icon: meta/hello.svg`)
	c.Assert(err, IsNil)
	makeSnapActive(yamlPath)

	yamlPath, err = makeInstalledMockSnap(s.tempdir, `name: framework1
version: 1.0
type: framework
vendor: Michael Vogt <mvo@ubuntu.com>
icon: meta/hello.svg`)
	c.Assert(err, IsNil)
	makeSnapActive(yamlPath)

	parts, err := ActiveSnapsByType(SnapTypeApp)
	c.Assert(err, IsNil)
	c.Assert(parts, HasLen, 1)
	c.Assert(parts[0].Name(), Equals, "app1")

	parts, err = ActiveSnapsByType(SnapTypeFramework)
	c.Assert(err, IsNil)
	c.Assert(parts, HasLen, 1)
	c.Assert(parts[0].Name(), Equals, "framework1")
}

func (s *SnapTestSuite) TestMetaRepositoryDetails(c *C) {
	_, err := makeInstalledMockSnap(s.tempdir, "")
	c.Assert(err, IsNil)

	m := NewMetaRepository()
	c.Assert(m, NotNil)

	parts, err := m.Details("hello-app")
	c.Assert(err, IsNil)
	c.Assert(parts, HasLen, 1)
	c.Assert(parts[0].Name(), Equals, "hello-app")
	c.Assert(parts[0].Namespace(), Equals, testNamespace)
}

func (s *SnapTestSuite) TestFindSnapsByNameNotAvailable(c *C) {
	_, err := makeInstalledMockSnap(s.tempdir, "")
	repo := NewLocalSnapRepository(snapAppsDir)
	installed, err := repo.Installed()
	c.Assert(err, IsNil)

	parts := FindSnapsByName("not-available", installed)
	c.Assert(parts, HasLen, 0)
}

func (s *SnapTestSuite) TestFindSnapsByNameFound(c *C) {
	_, err := makeInstalledMockSnap(s.tempdir, "")
	repo := NewLocalSnapRepository(snapAppsDir)
	installed, err := repo.Installed()
	c.Assert(err, IsNil)
	c.Assert(installed, HasLen, 1)

	parts := FindSnapsByName("hello-app", installed)
	c.Assert(parts, HasLen, 1)
	c.Assert(parts[0].Name(), Equals, "hello-app")
}

func (s *SnapTestSuite) TestFindSnapsByNameWithNamespace(c *C) {
	_, err := makeInstalledMockSnap(s.tempdir, "")
	repo := NewLocalSnapRepository(snapAppsDir)
	installed, err := repo.Installed()
	c.Assert(err, IsNil)
	c.Assert(installed, HasLen, 1)

	parts := FindSnapsByName("hello-app."+testNamespace, installed)
	c.Assert(parts, HasLen, 1)
	c.Assert(parts[0].Name(), Equals, "hello-app")
}

func (s *SnapTestSuite) TestFindSnapsByNameWithNamespaceNotThere(c *C) {
	_, err := makeInstalledMockSnap(s.tempdir, "")
	repo := NewLocalSnapRepository(snapAppsDir)
	installed, err := repo.Installed()
	c.Assert(err, IsNil)
	c.Assert(installed, HasLen, 1)

	parts := FindSnapsByName("hello-app.otherns", installed)
	c.Assert(parts, HasLen, 0)
}

func (s *SnapTestSuite) TestPackageNameInstalled(c *C) {
	c.Check(PackageNameActive("hello-app"), Equals, false)

	yamlFile, err := makeInstalledMockSnap(s.tempdir, "")
	c.Assert(err, IsNil)
	pkgdir := filepath.Dir(filepath.Dir(yamlFile))

	c.Assert(os.MkdirAll(filepath.Join(pkgdir, ".click", "info"), 0755), IsNil)
	c.Assert(ioutil.WriteFile(filepath.Join(pkgdir, ".click", "info", "hello-app.manifest"), []byte(`{"name": "hello-app"}`), 0644), IsNil)
	ag := &progress.NullProgress{}

	c.Assert(setActiveClick(pkgdir, true, ag), IsNil)

	c.Check(PackageNameActive("hello-app"), Equals, true)
	c.Assert(unsetActiveClick(pkgdir, true, ag), IsNil)
	c.Check(PackageNameActive("hello-app"), Equals, false)
}

func (s *SnapTestSuite) TestFindSnapsByNameAndVersion(c *C) {
	_, err := makeInstalledMockSnap(s.tempdir, "")
	repo := NewLocalSnapRepository(snapAppsDir)
	installed, err := repo.Installed()
	c.Assert(err, IsNil)

	parts := FindSnapsByNameAndVersion("hello-app."+testNamespace, "1.10", installed)
	c.Check(parts, HasLen, 1)
	parts = FindSnapsByNameAndVersion("bad-app."+testNamespace, "1.10", installed)
	c.Check(parts, HasLen, 0)
	parts = FindSnapsByNameAndVersion("hello-app.badNamespace", "1.10", installed)
	c.Check(parts, HasLen, 0)
	parts = FindSnapsByNameAndVersion("hello-app."+testNamespace, "2.20", installed)
	c.Check(parts, HasLen, 0)

	parts = FindSnapsByNameAndVersion("hello-app", "1.10", installed)
	c.Check(parts, HasLen, 1)
	parts = FindSnapsByNameAndVersion("bad-app", "1.10", installed)
	c.Check(parts, HasLen, 0)
	parts = FindSnapsByNameAndVersion("hello-app", "2.20", installed)
	c.Check(parts, HasLen, 0)
}

func (s *SnapTestSuite) TestFindSnapsByNameAndVersionFmk(c *C) {
	_, err := makeInstalledMockSnap(s.tempdir, "name: fmk\ntype: framework\nversion: 1")
	repo := NewLocalSnapRepository(snapAppsDir)
	installed, err := repo.Installed()
	c.Assert(err, IsNil)

	parts := FindSnapsByNameAndVersion("fmk."+testNamespace, "1", installed)
	c.Check(parts, HasLen, 0)
	parts = FindSnapsByNameAndVersion("fmk.badNamespace", "1", installed)
	c.Check(parts, HasLen, 0)

	parts = FindSnapsByNameAndVersion("fmk", "1", installed)
	c.Check(parts, HasLen, 1)
	parts = FindSnapsByNameAndVersion("not-fmk", "1", installed)
	c.Check(parts, HasLen, 0)
	parts = FindSnapsByNameAndVersion("fmk", "2", installed)
	c.Check(parts, HasLen, 0)
}
