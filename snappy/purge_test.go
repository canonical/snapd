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

	. "launchpad.net/gocheck"

	"launchpad.net/snappy/helpers"
	"launchpad.net/snappy/systemd"
)

type purgeSuite struct {
	tempdir string
}

var _ = Suite(&purgeSuite{})

func (s *purgeSuite) SetUpTest(c *C) {
	s.tempdir = c.MkDir()
	SetRootDir(s.tempdir)
	os.MkdirAll(filepath.Join(snapServicesDir, "multi-user.target.wants"), 0755)
	systemd.SystemctlCmd = func(cmd ...string) ([]byte, error) {
		return []byte("ActiveState=inactive\n"), nil
	}

	snapSeccompDir = c.MkDir()
	runScFilterGen = mockRunScFilterGen
}

func (s *purgeSuite) TestPurgeNonExistingRaisesError(c *C) {
	pkgName := "some-random-non-existing-stuff"
	inter := &MockProgressMeter{}
	err := Purge(pkgName, 0, inter)
	c.Check(err, Equals, ErrPackageNotFound)
	c.Check(inter.notified, HasLen, 0)
}

func (s *purgeSuite) mkpkg(c *C, args ...string) (string, string) {
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
	app := "hello-app." + testNamespace
	yaml := "name: hello-app\nversion: " + version + "\n" + extra
	yamlFile, err := makeInstalledMockSnap(s.tempdir, yaml)
	c.Assert(err, IsNil)
	pkgdir := filepath.Dir(filepath.Dir(yamlFile))
	c.Assert(os.MkdirAll(filepath.Join(pkgdir, ".click", "info"), 0755), IsNil)
	c.Assert(ioutil.WriteFile(filepath.Join(pkgdir, ".click", "info", app+".manifest"), []byte(`{"name": "`+app+`"}`), 0644), IsNil)

	ddir := filepath.Join(snapDataDir, app, version)
	c.Assert(os.MkdirAll(ddir, 0755), IsNil)
	canaryDataFile := filepath.Join(ddir, "canary.txt")
	err = ioutil.WriteFile(canaryDataFile, []byte(""), 0644)
	c.Assert(err, IsNil)

	return pkgdir, ddir
}

func (s *purgeSuite) TestPurgeActiveRaisesError(c *C) {
	inter := &MockProgressMeter{}
	pkgdir, _ := s.mkpkg(c)
	c.Assert(setActiveClick(pkgdir, true, inter), IsNil)

	err := Purge("hello-app", 0, inter)
	c.Check(err, Equals, ErrStillActive)
	c.Check(inter.notified, HasLen, 0)
}

func (s *purgeSuite) TestPurgeInactiveOK(c *C) {
	inter := &MockProgressMeter{}
	_, ddir := s.mkpkg(c)

	err := Purge("hello-app", 0, inter)
	c.Check(err, IsNil)
	c.Check(helpers.FileExists(ddir), Equals, false)
	c.Check(inter.notified, HasLen, 0)
}

func (s *purgeSuite) TestPurgeActiveExplicitOK(c *C) {
	inter := &MockProgressMeter{}
	pkgdir, ddir := s.mkpkg(c)
	c.Assert(setActiveClick(pkgdir, true, inter), IsNil)

	err := Purge("hello-app", DoPurgeActive, inter)
	c.Check(err, IsNil)
	c.Check(helpers.FileExists(ddir), Equals, false)
	c.Check(inter.notified, HasLen, 0)
}

func (s *purgeSuite) TestPurgeActiveRestartServices(c *C) {
	inter := &MockProgressMeter{}
	pkgdir, ddir := s.mkpkg(c, "v1", "services:\n - name: svc")
	c.Assert(setActiveClick(pkgdir, true, inter), IsNil)

	called := [][]string{}
	systemd.SystemctlCmd = func(cmd ...string) ([]byte, error) {
		called = append(called, cmd)
		return []byte("ActiveState=inactive\n"), nil
	}

	err := Purge("hello-app", DoPurgeActive, inter)
	c.Check(err, IsNil)
	c.Check(helpers.FileExists(ddir), Equals, false)
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
	_, ddir0 := s.mkpkg(c, "v0")
	_, ddir1 := s.mkpkg(c, "v1")

	err := Purge("hello-app", 0, inter)
	c.Check(err, IsNil)
	c.Check(helpers.FileExists(ddir0), Equals, false)
	c.Check(helpers.FileExists(ddir1), Equals, false)
	c.Check(inter.notified, HasLen, 0)
}

func (s *purgeSuite) TestPurgeMultiContinuesOnFail(c *C) {
	inter := &MockProgressMeter{}
	_, ddir0 := s.mkpkg(c, "v0")
	_, ddir1 := s.mkpkg(c, "v1")
	_, ddir2 := s.mkpkg(c, "v2")

	count := 0
	anError := errors.New("fail")
	remove = func(n, v string) error {
		count++
		if count == 2 {
			return anError
		}
		return removeSnapData(n, v)
	}
	defer func() { remove = removeSnapData }()

	err := Purge("hello-app", 0, inter)
	c.Check(err, Equals, anError)
	c.Check(count, Equals, 3)
	c.Check(helpers.FileExists(ddir0), Equals, false)
	c.Check(helpers.FileExists(ddir1), Equals, true)
	c.Check(helpers.FileExists(ddir2), Equals, false)
	c.Assert(inter.notified, HasLen, 1)
	c.Check(inter.notified[0], Matches, `unable to purge.*fail`)
}

func (s *purgeSuite) TestPurgeRemovedWorks(c *C) {
	inter := &MockProgressMeter{}
	pkgdir, ddir := s.mkpkg(c)
	c.Assert(removeClick(pkgdir, inter), IsNil)
	c.Check(helpers.FileExists(ddir), Equals, true)

	err := Purge("hello-app", 0, inter)
	c.Check(err, IsNil)
	c.Check(helpers.FileExists(ddir), Equals, false)
}

func (s *purgeSuite) TestPurgeBogusNameFails(c *C) {
}
