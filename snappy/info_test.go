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

	"github.com/ubuntu-core/snappy/progress"
	"github.com/ubuntu-core/snappy/snap"
)

func (s *SnapTestSuite) TestActiveSnapByType(c *C) {
	yamlPath, err := makeInstalledMockSnap(`name: app1
version: 1.10
`, 11)

	c.Assert(err, IsNil)
	makeSnapActive(yamlPath)

	yamlPath, err = makeInstalledMockSnap(`name: os2
version: 1.0
type: os
`, 11)

	c.Assert(err, IsNil)
	makeSnapActive(yamlPath)

	snaps, err := ActiveSnapsByType(snap.TypeApp)
	c.Assert(err, IsNil)
	c.Assert(snaps, HasLen, 1)
	c.Assert(snaps[0].Name(), Equals, "app1")
}

func (s *SnapTestSuite) TestActiveSnapIterByType(c *C) {
	yamlPath, err := makeInstalledMockSnap(`name: app
version: 1.10`, 11)

	c.Assert(err, IsNil)
	makeSnapActive(yamlPath)

	yamlPath, err = makeInstalledMockSnap(`name: os2
version: 1.0
type: os`, 11)

	c.Assert(err, IsNil)
	makeSnapActive(yamlPath)

	type T struct {
		f func(*snap.Info) string
		t snap.Type
		n string
	}

	for _, t := range []T{
		{BareName, snap.TypeApp, "app"},
		{FullName, snap.TypeApp, "app." + testDeveloper},
		{fullNameWithChannel, snap.TypeApp, "app." + testDeveloper + "/remote-channel"},
	} {
		names, err := ActiveSnapIterByType(t.f, t.t)
		c.Check(err, IsNil)
		c.Check(names, DeepEquals, []string{t.n})
	}

	// now remove the channel
	si := snap.SideInfo{
		OfficialName:      "app",
		Revision:          11,
		Developer:         testDeveloper,
		Channel:           "",
		EditedSummary:     "hello",
		EditedDescription: "Hello.",
	}
	err = SaveManifest(&snap.Info{SideInfo: si, Version: "1.10"})
	c.Assert(err, IsNil)

	for _, t := range []T{
		{fullNameWithChannel, snap.TypeApp, "app." + testDeveloper + "/stable"},
	} {
		names, err := ActiveSnapIterByType(t.f, t.t)
		c.Check(err, IsNil)
		c.Check(names, DeepEquals, []string{t.n})
	}
}

func (s *SnapTestSuite) TestFindSnapsByNameNotAvailable(c *C) {
	_, err := makeInstalledMockSnap("", 11)
	repo := &Overlord{}
	installed, err := repo.Installed()
	c.Assert(err, IsNil)

	snaps := FindSnapsByName("not-available", installed)
	c.Assert(snaps, HasLen, 0)
}

func (s *SnapTestSuite) TestFindSnapsByNameFound(c *C) {
	_, err := makeInstalledMockSnap("", 11)
	repo := &Overlord{}
	installed, err := repo.Installed()
	c.Assert(err, IsNil)
	c.Assert(installed, HasLen, 1)

	snaps := FindSnapsByName("hello-snap", installed)
	c.Assert(snaps, HasLen, 1)
	c.Assert(snaps[0].Name(), Equals, "hello-snap")
}

func (s *SnapTestSuite) TestFindSnapsByNameWithDeveloper(c *C) {
	_, err := makeInstalledMockSnap("", 11)
	repo := &Overlord{}
	installed, err := repo.Installed()
	c.Assert(err, IsNil)
	c.Assert(installed, HasLen, 1)

	snaps := FindSnapsByName("hello-snap."+testDeveloper, installed)
	c.Assert(snaps, HasLen, 1)
	c.Assert(snaps[0].Name(), Equals, "hello-snap")
}

func (s *SnapTestSuite) TestFindSnapsByNameWithDeveloperNotThere(c *C) {
	_, err := makeInstalledMockSnap("", 11)
	repo := &Overlord{}
	installed, err := repo.Installed()
	c.Assert(err, IsNil)
	c.Assert(installed, HasLen, 1)

	snaps := FindSnapsByName("hello-snap.otherns", installed)
	c.Assert(snaps, HasLen, 0)
}

func (s *SnapTestSuite) TestPackageNameInstalled(c *C) {
	c.Check(PackageNameActive("hello-snap"), Equals, false)

	yamlFile, err := makeInstalledMockSnap("", 11)
	c.Assert(err, IsNil)
	pkgdir := filepath.Dir(filepath.Dir(yamlFile))

	c.Assert(os.MkdirAll(filepath.Join(pkgdir, ".click", "info"), 0755), IsNil)
	c.Assert(ioutil.WriteFile(filepath.Join(pkgdir, ".click", "info", "hello-snap.manifest"), []byte(`{"name": "hello-snap"}`), 0644), IsNil)
	ag := &progress.NullProgress{}

	snap, err := NewInstalledSnap(yamlFile)
	c.Assert(err, IsNil)

	c.Assert(ActivateSnap(snap, ag), IsNil)

	c.Check(PackageNameActive("hello-snap"), Equals, true)
	c.Assert(UnlinkSnap(snap, ag), IsNil)
	c.Check(PackageNameActive("hello-snap"), Equals, false)
}

func (s *SnapTestSuite) TestFindSnapsByNameAndVersion(c *C) {
	_, err := makeInstalledMockSnap("", 11)
	repo := &Overlord{}
	installed, err := repo.Installed()
	c.Assert(err, IsNil)

	snaps := FindSnapsByNameAndVersion("hello-snap."+testDeveloper, "1.10", installed)
	c.Check(snaps, HasLen, 1)
	snaps = FindSnapsByNameAndVersion("bad-app."+testDeveloper, "1.10", installed)
	c.Check(snaps, HasLen, 0)
	snaps = FindSnapsByNameAndVersion("hello-snap.badDeveloper", "1.10", installed)
	c.Check(snaps, HasLen, 0)
	snaps = FindSnapsByNameAndVersion("hello-snap."+testDeveloper, "2.20", installed)
	c.Check(snaps, HasLen, 0)

	snaps = FindSnapsByNameAndVersion("hello-snap", "1.10", installed)
	c.Check(snaps, HasLen, 1)
	snaps = FindSnapsByNameAndVersion("bad-app", "1.10", installed)
	c.Check(snaps, HasLen, 0)
	snaps = FindSnapsByNameAndVersion("hello-snap", "2.20", installed)
	c.Check(snaps, HasLen, 0)
}

func (s *SnapTestSuite) TestFindSnapsByNameAndVersionFmk(c *C) {
	_, err := makeInstalledMockSnap("name: os2\ntype: os\nversion: 1", 11)
	repo := &Overlord{}
	installed, err := repo.Installed()
	c.Assert(err, IsNil)

	snaps := FindSnapsByNameAndVersion("os2."+testDeveloper, "1", installed)
	c.Check(snaps, HasLen, 1)
	snaps = FindSnapsByNameAndVersion("os2.badDeveloper", "1", installed)
	c.Check(snaps, HasLen, 0)

	snaps = FindSnapsByNameAndVersion("os2", "1", installed)
	c.Check(snaps, HasLen, 1)
	snaps = FindSnapsByNameAndVersion("not-os2", "1", installed)
	c.Check(snaps, HasLen, 0)
	snaps = FindSnapsByNameAndVersion("os2", "2", installed)
	c.Check(snaps, HasLen, 0)
}

func (s *SnapTestSuite) TestFindSnapsByNameAndRevision(c *C) {
	_, err := makeInstalledMockSnap("", 11)
	repo := &Overlord{}
	installed, err := repo.Installed()
	c.Assert(err, IsNil)

	snaps := FindSnapsByNameAndRevision("hello-snap."+testDeveloper, 11, installed)
	c.Check(snaps, HasLen, 1)
	snaps = FindSnapsByNameAndRevision("bad-app."+testDeveloper, 11, installed)
	c.Check(snaps, HasLen, 0)
	snaps = FindSnapsByNameAndRevision("hello-snap.badDeveloper", 11, installed)
	c.Check(snaps, HasLen, 0)
	snaps = FindSnapsByNameAndRevision("hello-snap."+testDeveloper, 22, installed)
	c.Check(snaps, HasLen, 0)

	snaps = FindSnapsByNameAndRevision("hello-snap", 11, installed)
	c.Check(snaps, HasLen, 1)
	snaps = FindSnapsByNameAndRevision("bad-app", 11, installed)
	c.Check(snaps, HasLen, 0)
	snaps = FindSnapsByNameAndRevision("hello-snap", 22, installed)
	c.Check(snaps, HasLen, 0)
}
