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
	"errors"
	"io/ioutil"
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/ubuntu-core/snappy/dirs"
	"github.com/ubuntu-core/snappy/osutil"
	"github.com/ubuntu-core/snappy/systemd"
)

type purgeSuite struct {
	tempdir string
}

var _ = Suite(&purgeSuite{})

func (s *purgeSuite) SetUpTest(c *C) {
	s.tempdir = c.MkDir()
	dirs.SetRootDir(s.tempdir)
	os.MkdirAll(dirs.SnapMetaDir, 0755)
	os.MkdirAll(filepath.Join(dirs.SnapServicesDir, "multi-user.target.wants"), 0755)
	systemd.SystemctlCmd = func(cmd ...string) ([]byte, error) {
		return []byte("ActiveState=inactive\n"), nil
	}

	dirs.SnapSeccompDir = c.MkDir()
	dirs.SnapAppArmorDir = c.MkDir()

	runAppArmorParser = mockRunAppArmorParser

	makeMockSecurityEnv(c)
}

func (s *purgeSuite) TestPurgeNonExistingRaisesError(c *C) {
	pkgName := "some-random-non-existing-stuff"
	inter := &MockProgressMeter{}
	err := Purge(pkgName, 0, inter)
	c.Check(err, Equals, ErrPackageNotFound)
	c.Check(inter.notified, HasLen, 0)
}

func (s *purgeSuite) mkpkg(c *C, args ...string) (dataDirs []string, part *Snap) {
	version := "1.10"
	extra := ""
	switch len(args) {
	case 2:
		extra = args[1]
		fallthrough
	case 1:
		version = args[0]
	case 0:
	default:
		panic("dunno what to do with args")
	}
	app := "hello-snap." + testDeveloper
	yaml := "version: 1.0\nname: hello-snap\nversion: " + version + "\n" + extra
	yamlFile, err := makeInstalledMockSnap(s.tempdir, yaml)
	c.Assert(err, IsNil)

	dataDir := filepath.Join(dirs.SnapDataDir, app, version)
	c.Assert(os.MkdirAll(dataDir, 0755), IsNil)
	canaryDataFile := filepath.Join(dataDir, "canary.txt")
	err = ioutil.WriteFile(canaryDataFile, []byte(""), 0644)
	c.Assert(err, IsNil)

	dataHomeDir := filepath.Join(s.tempdir, "home", "user1", "snaps", app, version)
	c.Assert(os.MkdirAll(dataHomeDir, 0755), IsNil)
	canaryDataFile = filepath.Join(dataHomeDir, "canary.txt")
	err = ioutil.WriteFile(canaryDataFile, []byte(""), 0644)
	c.Assert(err, IsNil)

	part, err = NewInstalledSnap(yamlFile, testDeveloper)
	c.Assert(err, IsNil)

	dataDirs = []string{dataDir, dataHomeDir}

	return dataDirs, part
}

func (s *purgeSuite) TestPurgeActiveRaisesError(c *C) {
	inter := &MockProgressMeter{}
	_, part := s.mkpkg(c)
	c.Assert(part.activate(true, inter), IsNil)

	err := Purge("hello-snap", 0, inter)
	c.Check(err, Equals, ErrStillActive)
	c.Check(inter.notified, HasLen, 0)
}

func (s *purgeSuite) TestPurgeInactiveOK(c *C) {
	inter := &MockProgressMeter{}
	ddirs, _ := s.mkpkg(c)

	err := Purge("hello-snap", 0, inter)
	c.Check(err, IsNil)

	for _, ddir := range ddirs {
		c.Check(osutil.FileExists(ddir), Equals, false)
	}

	c.Check(inter.notified, HasLen, 0)
}

func (s *purgeSuite) TestPurgeActiveExplicitOK(c *C) {
	inter := &MockProgressMeter{}
	ddirs, part := s.mkpkg(c)
	c.Assert(part.activate(true, inter), IsNil)

	for _, ddir := range ddirs {
		canary := filepath.Join(ddir, "canary")
		c.Assert(os.Mkdir(canary, 0755), IsNil)
	}

	err := Purge("hello-snap", DoPurgeActive, inter)
	c.Check(err, IsNil)

	for _, ddir := range ddirs {
		c.Check(osutil.FileExists(filepath.Join(ddir, "canary")), Equals, false)
	}

	c.Check(inter.notified, HasLen, 0)
}

func (s *purgeSuite) TestPurgeActiveRestartServices(c *C) {
	inter := &MockProgressMeter{}
	ddirs, part := s.mkpkg(c, "v1", `apps:
 svc:
  command: foo
  daemon: forking
`)
	c.Assert(part.activate(true, inter), IsNil)
	for _, ddir := range ddirs {
		canary := filepath.Join(ddir, "canary")
		c.Assert(os.Mkdir(canary, 0755), IsNil)
	}

	called := [][]string{}
	systemd.SystemctlCmd = func(cmd ...string) ([]byte, error) {
		called = append(called, cmd)
		return []byte("ActiveState=inactive\n"), nil
	}

	err := Purge("hello-snap", DoPurgeActive, inter)
	c.Check(err, IsNil)
	for _, ddir := range ddirs {
		c.Check(osutil.FileExists(filepath.Join(ddir, "canary")), Equals, false)
	}
	c.Assert(inter.notified, HasLen, 1)
	c.Check(inter.notified[0], Matches, `Waiting for .* to stop.`)
	rv := make(map[string]int)
	for i, c := range called {
		rv[c[0]] = i + 1
	}
	c.Check(rv["stop"] > 0 && rv["start"] > rv["stop"], Equals, true)
}

func (s *purgeSuite) TestPurgeMultiOK(c *C) {
	inter := &MockProgressMeter{}
	ddirs0, _ := s.mkpkg(c, "v0")
	ddirs1, _ := s.mkpkg(c, "v1")

	err := Purge("hello-snap", 0, inter)
	c.Check(err, IsNil)

	for _, ddir := range ddirs0 {
		c.Check(osutil.FileExists(ddir), Equals, false)
	}
	for _, ddir := range ddirs1 {
		c.Check(osutil.FileExists(ddir), Equals, false)
	}
	c.Check(inter.notified, HasLen, 0)
}

func (s *purgeSuite) TestPurgeMultiContinuesOnFail(c *C) {
	inter := &MockProgressMeter{}
	ddirs0, _ := s.mkpkg(c, "v0")
	ddirs1, _ := s.mkpkg(c, "v1")
	ddirs2, _ := s.mkpkg(c, "v2")

	count := 0
	anError := errors.New("fail")
	remove = func(n, v string) error {
		count++

		// Fail to remove v1
		if v == "v1" {
			return anError
		}
		return removeSnapData(n, v)
	}
	defer func() { remove = removeSnapData }()

	err := Purge("hello-snap", 0, inter)
	c.Check(err, Equals, anError)
	c.Check(count, Equals, 6)
	for _, ddir := range ddirs0 {
		c.Check(osutil.FileExists(ddir), Equals, false)
	}
	for _, ddir := range ddirs1 {
		c.Check(osutil.FileExists(ddir), Equals, true)
	}
	for _, ddir := range ddirs2 {
		c.Check(osutil.FileExists(ddir), Equals, false)
	}
	c.Assert(inter.notified, HasLen, 2)
	c.Check(inter.notified[0], Matches, `unable to purge.*fail`)
	c.Check(inter.notified[1], Matches, `unable to purge.*fail`)
}

func (s *purgeSuite) TestPurgeRemovedWorks(c *C) {
	inter := &MockProgressMeter{}
	ddirs, part := s.mkpkg(c)

	err := (&Overlord{}).Uninstall(part, &MockProgressMeter{})
	c.Assert(err, IsNil)
	for _, ddir := range ddirs {
		c.Check(osutil.FileExists(ddir), Equals, true)
	}

	err = Purge("hello-snap", 0, inter)
	c.Check(err, IsNil)
	for _, ddir := range ddirs {
		c.Check(osutil.FileExists(ddir), Equals, false)
	}
}

func (s *purgeSuite) TestPurgeBogusNameFails(c *C) {
}
