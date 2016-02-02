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

	. "gopkg.in/check.v1"

	"github.com/ubuntu-core/snappy/dirs"
	"github.com/ubuntu-core/snappy/progress"
	"github.com/ubuntu-core/snappy/snap"
)

func (s *SnapTestSuite) TestActiveSnapByType(c *C) {
	yamlPath, err := makeInstalledMockSnap(s.tempdir, `name: app1
version: 1.10
`)
	c.Assert(err, IsNil)
	makeSnapActive(yamlPath)

	yamlPath, err = makeInstalledMockSnap(s.tempdir, `name: framework1
version: 1.0
type: framework
`)
	c.Assert(err, IsNil)
	makeSnapActive(yamlPath)

	parts, err := ActiveSnapsByType(snap.TypeApp)
	c.Assert(err, IsNil)
	c.Assert(parts, HasLen, 1)
	c.Assert(parts[0].Name(), Equals, "app1")

	parts, err = ActiveSnapsByType(snap.TypeFramework)
	c.Assert(err, IsNil)
	c.Assert(parts, HasLen, 1)
	c.Assert(parts[0].Name(), Equals, "framework1")
}

func (s *SnapTestSuite) TestActiveSnapIterByType(c *C) {
	yamlPath, err := makeInstalledMockSnap(s.tempdir, `name: app
version: 1.10`)
	c.Assert(err, IsNil)
	makeSnapActive(yamlPath)

	yamlPath, err = makeInstalledMockSnap(s.tempdir, `name: fwk
version: 1.0
type: framework`)
	c.Assert(err, IsNil)
	makeSnapActive(yamlPath)

	type T struct {
		f func(Part) string
		t snap.Type
		n string
	}

	for _, t := range []T{
		{BareName, snap.TypeApp, "app"},
		{BareName, snap.TypeFramework, "fwk"},
		{QualifiedName, snap.TypeApp, "app." + testOrigin},
		{QualifiedName, snap.TypeFramework, "fwk"},
		{FullName, snap.TypeApp, "app." + testOrigin},
		{FullName, snap.TypeFramework, "fwk." + testOrigin},
		{fullNameWithChannel, snap.TypeApp, "app." + testOrigin + "/remote-channel"},
		{fullNameWithChannel, snap.TypeFramework, "fwk." + testOrigin + "/remote-channel"},
	} {
		names, err := ActiveSnapIterByType(t.f, t.t)
		c.Check(err, IsNil)
		c.Check(names, DeepEquals, []string{t.n})
	}

	// now remove the channel
	storeMinimalRemoteManifest("app."+testOrigin, "app", testOrigin, "1.10", "Hello.", "")
	storeMinimalRemoteManifest("fwk", "fwk", testOrigin, "1.0", "Hello.", "")
	for _, t := range []T{
		{fullNameWithChannel, snap.TypeApp, "app." + testOrigin},
		{fullNameWithChannel, snap.TypeFramework, "fwk." + testOrigin},
	} {
		names, err := ActiveSnapIterByType(t.f, t.t)
		c.Check(err, IsNil)
		c.Check(names, DeepEquals, []string{t.n})
	}

	nm := make(map[string]bool, 2)
	names, err := ActiveSnapIterByType(QualifiedName, snap.TypeApp, snap.TypeFramework)
	c.Check(err, IsNil)
	c.Assert(names, HasLen, 2)
	for i := range names {
		nm[names[i]] = true
	}

	c.Check(nm, DeepEquals, map[string]bool{"fwk": true, "app." + testOrigin: true})
}

func (s *SnapTestSuite) TestMetaRepositoryDetails(c *C) {
	_, err := makeInstalledMockSnap(s.tempdir, "")
	c.Assert(err, IsNil)

	m := NewMetaRepository()
	c.Assert(m, NotNil)

	parts, err := m.Details("hello-app", "")
	c.Assert(err, IsNil)
	c.Assert(parts, HasLen, 1)
	c.Assert(parts[0].Name(), Equals, "hello-app")
	c.Assert(parts[0].Origin(), Equals, testOrigin)
}

func (s *SnapTestSuite) TestFindSnapsByNameNotAvailable(c *C) {
	_, err := makeInstalledMockSnap(s.tempdir, "")
	repo := NewLocalSnapRepository(dirs.SnapSnapsDir)
	installed, err := repo.Installed()
	c.Assert(err, IsNil)

	parts := FindSnapsByName("not-available", installed)
	c.Assert(parts, HasLen, 0)
}

func (s *SnapTestSuite) TestFindSnapsByNameFound(c *C) {
	_, err := makeInstalledMockSnap(s.tempdir, "")
	repo := NewLocalSnapRepository(dirs.SnapSnapsDir)
	installed, err := repo.Installed()
	c.Assert(err, IsNil)
	c.Assert(installed, HasLen, 1)

	parts := FindSnapsByName("hello-app", installed)
	c.Assert(parts, HasLen, 1)
	c.Assert(parts[0].Name(), Equals, "hello-app")
}

func (s *SnapTestSuite) TestFindSnapsByNameWithOrigin(c *C) {
	_, err := makeInstalledMockSnap(s.tempdir, "")
	repo := NewLocalSnapRepository(dirs.SnapSnapsDir)
	installed, err := repo.Installed()
	c.Assert(err, IsNil)
	c.Assert(installed, HasLen, 1)

	parts := FindSnapsByName("hello-app."+testOrigin, installed)
	c.Assert(parts, HasLen, 1)
	c.Assert(parts[0].Name(), Equals, "hello-app")
}

func (s *SnapTestSuite) TestFindSnapsByNameWithOriginNotThere(c *C) {
	_, err := makeInstalledMockSnap(s.tempdir, "")
	repo := NewLocalSnapRepository(dirs.SnapSnapsDir)
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

	part, err := NewInstalledSnapPart(yamlFile, testOrigin)
	c.Assert(err, IsNil)

	c.Assert(part.activate(true, ag), IsNil)

	c.Check(PackageNameActive("hello-app"), Equals, true)
	c.Assert(part.deactivate(true, ag), IsNil)
	c.Check(PackageNameActive("hello-app"), Equals, false)
}

func (s *SnapTestSuite) TestFindSnapsByNameAndVersion(c *C) {
	_, err := makeInstalledMockSnap(s.tempdir, "")
	repo := NewLocalSnapRepository(dirs.SnapSnapsDir)
	installed, err := repo.Installed()
	c.Assert(err, IsNil)

	parts := FindSnapsByNameAndVersion("hello-app."+testOrigin, "1.10", installed)
	c.Check(parts, HasLen, 1)
	parts = FindSnapsByNameAndVersion("bad-app."+testOrigin, "1.10", installed)
	c.Check(parts, HasLen, 0)
	parts = FindSnapsByNameAndVersion("hello-app.badOrigin", "1.10", installed)
	c.Check(parts, HasLen, 0)
	parts = FindSnapsByNameAndVersion("hello-app."+testOrigin, "2.20", installed)
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
	repo := NewLocalSnapRepository(dirs.SnapSnapsDir)
	installed, err := repo.Installed()
	c.Assert(err, IsNil)

	parts := FindSnapsByNameAndVersion("fmk."+testOrigin, "1", installed)
	c.Check(parts, HasLen, 1)
	parts = FindSnapsByNameAndVersion("fmk.badOrigin", "1", installed)
	c.Check(parts, HasLen, 0)

	parts = FindSnapsByNameAndVersion("fmk", "1", installed)
	c.Check(parts, HasLen, 1)
	parts = FindSnapsByNameAndVersion("not-fmk", "1", installed)
	c.Check(parts, HasLen, 0)
	parts = FindSnapsByNameAndVersion("fmk", "2", installed)
	c.Check(parts, HasLen, 0)
}
