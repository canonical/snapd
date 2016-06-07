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

package backend_test

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"regexp"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/progress"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"

	"github.com/snapcore/snapd/overlord/snapstate/backend"
)

type copydataSuite struct {
	be           backend.Backend
	nullProgress progress.NullProgress
	tempdir      string
}

var _ = Suite(&copydataSuite{})

func (s *copydataSuite) SetUpTest(c *C) {
	s.tempdir = c.MkDir()
	dirs.SetRootDir(s.tempdir)
}

func (s *copydataSuite) TearDownTest(c *C) {
	dirs.SetRootDir("")
}

const (
	helloYaml1 = `name: hello
version: 1.0
`
	helloYaml2 = `name: hello
version: 2.0
`
)

func (s *copydataSuite) TestCopyData(c *C) {
	homedir := filepath.Join(s.tempdir, "home", "user1", "snap")
	homeData := filepath.Join(homedir, "hello/10")
	err := os.MkdirAll(homeData, 0755)
	c.Assert(err, IsNil)
	homeCommonData := filepath.Join(homedir, "hello/common")
	err = os.MkdirAll(homeCommonData, 0755)
	c.Assert(err, IsNil)

	canaryData := []byte("ni ni ni")

	v1 := snaptest.MockSnap(c, helloYaml1, &snap.SideInfo{Revision: snap.R(10)})
	// just creates data dirs in this case
	err = s.be.CopySnapData(v1, nil, &s.nullProgress)
	c.Assert(err, IsNil)

	canaryDataFile := filepath.Join(v1.DataDir(), "canary.txt")
	err = ioutil.WriteFile(canaryDataFile, canaryData, 0644)
	c.Assert(err, IsNil)
	canaryDataFile = filepath.Join(v1.CommonDataDir(), "canary.common")
	err = ioutil.WriteFile(canaryDataFile, canaryData, 0644)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(filepath.Join(homeData, "canary.home"), canaryData, 0644)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(filepath.Join(homeCommonData, "canary.common_home"), canaryData, 0644)
	c.Assert(err, IsNil)

	v2 := snaptest.MockSnap(c, helloYaml2, &snap.SideInfo{Revision: snap.R(20)})
	err = s.be.CopySnapData(v2, v1, &s.nullProgress)
	c.Assert(err, IsNil)

	newCanaryDataFile := filepath.Join(dirs.SnapDataDir, "hello/20", "canary.txt")
	content, err := ioutil.ReadFile(newCanaryDataFile)
	c.Assert(err, IsNil)
	c.Assert(content, DeepEquals, canaryData)

	// ensure common data file is still there (even though it didn't get copied)
	newCanaryDataFile = filepath.Join(dirs.SnapDataDir, "hello", "common", "canary.common")
	content, err = ioutil.ReadFile(newCanaryDataFile)
	c.Assert(err, IsNil)
	c.Assert(content, DeepEquals, canaryData)

	newCanaryDataFile = filepath.Join(homedir, "hello/20", "canary.home")
	content, err = ioutil.ReadFile(newCanaryDataFile)
	c.Assert(err, IsNil)
	c.Assert(content, DeepEquals, canaryData)

	// ensure home common data file is still there (even though it didn't get copied)
	newCanaryDataFile = filepath.Join(homedir, "hello", "common", "canary.common_home")
	content, err = ioutil.ReadFile(newCanaryDataFile)
	c.Assert(err, IsNil)
	c.Assert(content, DeepEquals, canaryData)
}

// ensure that even with no home dir there is no error and the
// system data gets copied
func (s *copydataSuite) TestCopyDataNoUserHomes(c *C) {
	// this home dir path does not exist
	oldSnapDataHomeGlob := dirs.SnapDataHomeGlob
	defer func() { dirs.SnapDataHomeGlob = oldSnapDataHomeGlob }()
	dirs.SnapDataHomeGlob = filepath.Join(s.tempdir, "no-such-home", "*", "snap")

	v1 := snaptest.MockSnap(c, helloYaml1, &snap.SideInfo{Revision: snap.R(10)})
	err := s.be.CopySnapData(v1, nil, &s.nullProgress)
	c.Assert(err, IsNil)

	canaryDataFile := filepath.Join(v1.DataDir(), "canary.txt")
	err = ioutil.WriteFile(canaryDataFile, []byte(""), 0644)
	c.Assert(err, IsNil)
	canaryDataFile = filepath.Join(v1.CommonDataDir(), "canary.common")
	err = ioutil.WriteFile(canaryDataFile, []byte(""), 0644)
	c.Assert(err, IsNil)

	v2 := snaptest.MockSnap(c, helloYaml2, &snap.SideInfo{Revision: snap.R(20)})
	err = s.be.CopySnapData(v2, v1, &s.nullProgress)
	c.Assert(err, IsNil)

	_, err = os.Stat(filepath.Join(v2.DataDir(), "canary.txt"))
	c.Assert(err, IsNil)
	_, err = os.Stat(filepath.Join(v2.CommonDataDir(), "canary.common"))
	c.Assert(err, IsNil)

	// sanity atm
	c.Check(v1.DataDir(), Not(Equals), v2.DataDir())
	c.Check(v1.CommonDataDir(), Equals, v2.CommonDataDir())
}

func (s *copydataSuite) populateData(c *C, revision snap.Revision) {
	datadir := filepath.Join(dirs.SnapDataDir, "hello/"+revision.String())
	subdir := filepath.Join(datadir, "random-subdir")
	err := os.MkdirAll(subdir, 0755)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(filepath.Join(subdir, "canary"), nil, 0644)
	c.Assert(err, IsNil)
}

func (s copydataSuite) populateHomeData(c *C, user string, revision snap.Revision) (homedir string) {
	homedir = filepath.Join(s.tempdir, "home", user, "snap")
	homeData := filepath.Join(homedir, "hello/"+revision.String())
	err := os.MkdirAll(homeData, 0755)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(filepath.Join(homeData, "canary.home"), nil, 0644)
	c.Assert(err, IsNil)
	return
}

func (s *copydataSuite) TestCopyDataDoUndo(c *C) {
	v1 := snaptest.MockSnap(c, helloYaml1, &snap.SideInfo{Revision: snap.R(10)})
	s.populateData(c, snap.R(10))
	homedir := s.populateHomeData(c, "user1", snap.R(10))

	// pretend we install a new version
	v2 := snaptest.MockSnap(c, helloYaml2, &snap.SideInfo{Revision: snap.R(20)})

	// copy data
	err := s.be.CopySnapData(v2, v1, &s.nullProgress)
	c.Assert(err, IsNil)
	v2data := filepath.Join(dirs.SnapDataDir, "hello/20")
	l, err := filepath.Glob(filepath.Join(v2data, "*"))
	c.Assert(err, IsNil)
	c.Assert(l, HasLen, 1)
	v2HomeData := filepath.Join(homedir, "hello/20")
	l, err = filepath.Glob(filepath.Join(v2HomeData, "*"))
	c.Assert(err, IsNil)
	c.Assert(l, HasLen, 1)

	err = s.be.UndoCopySnapData(v2, v1, &s.nullProgress)
	c.Assert(err, IsNil)

	// now removed
	_, err = os.Stat(v2data)
	c.Assert(os.IsNotExist(err), Equals, true)
	_, err = os.Stat(v2HomeData)
	c.Assert(os.IsNotExist(err), Equals, true)
}

func (s *copydataSuite) TestCopyDataDoUndoNoUserHomes(c *C) {
	// this home dir path does not exist
	oldSnapDataHomeGlob := dirs.SnapDataHomeGlob
	defer func() { dirs.SnapDataHomeGlob = oldSnapDataHomeGlob }()
	dirs.SnapDataHomeGlob = filepath.Join(s.tempdir, "no-such-home", "*", "snap")

	v1 := snaptest.MockSnap(c, helloYaml1, &snap.SideInfo{Revision: snap.R(10)})
	s.populateData(c, snap.R(10))

	// pretend we install a new version
	v2 := snaptest.MockSnap(c, helloYaml2, &snap.SideInfo{Revision: snap.R(20)})

	// copy data
	err := s.be.CopySnapData(v2, v1, &s.nullProgress)
	c.Assert(err, IsNil)
	v2data := filepath.Join(dirs.SnapDataDir, "hello/20")
	l, err := filepath.Glob(filepath.Join(v2data, "*"))
	c.Assert(err, IsNil)
	c.Assert(l, HasLen, 1)

	err = s.be.UndoCopySnapData(v2, v1, &s.nullProgress)
	c.Assert(err, IsNil)

	// now removed
	_, err = os.Stat(v2data)
	c.Assert(os.IsNotExist(err), Equals, true)
}

func (s *copydataSuite) TestCopyDataDoUndoFirstInstall(c *C) {
	v1 := snaptest.MockSnap(c, helloYaml1, &snap.SideInfo{Revision: snap.R(10)})

	// first install
	err := s.be.CopySnapData(v1, nil, &s.nullProgress)
	c.Assert(err, IsNil)
	_, err = os.Stat(v1.DataDir())
	c.Assert(err, IsNil)
	_, err = os.Stat(v1.CommonDataDir())
	c.Assert(err, IsNil)

	err = s.be.UndoCopySnapData(v1, nil, &s.nullProgress)
	c.Assert(err, IsNil)
	_, err = os.Stat(v1.DataDir())
	c.Check(os.IsNotExist(err), Equals, true)
	_, err = os.Stat(v1.CommonDataDir())
	c.Check(os.IsNotExist(err), Equals, true)
}

func (s *copydataSuite) TestCopyDataDoIdempotent(c *C) {
	// make sure that a retry wouldn't stumble on partial work

	v1 := snaptest.MockSnap(c, helloYaml1, &snap.SideInfo{Revision: snap.R(10)})

	s.populateData(c, snap.R(10))
	homedir := s.populateHomeData(c, "user1", snap.R(10))

	// pretend we install a new version
	v2 := snaptest.MockSnap(c, helloYaml2, &snap.SideInfo{Revision: snap.R(20)})

	// copy data
	err := s.be.CopySnapData(v2, v1, &s.nullProgress)
	c.Assert(err, IsNil)

	err = s.be.CopySnapData(v2, v1, &s.nullProgress)
	c.Assert(err, IsNil)

	v2data := filepath.Join(dirs.SnapDataDir, "hello/20")
	l, err := filepath.Glob(filepath.Join(v2data, "*"))
	c.Assert(err, IsNil)
	c.Assert(l, HasLen, 1)
	v2HomeData := filepath.Join(homedir, "hello/20")
	l, err = filepath.Glob(filepath.Join(v2HomeData, "*"))
	c.Assert(err, IsNil)
	c.Assert(l, HasLen, 1)
}

func (s *copydataSuite) TestCopyDataUndoIdempotent(c *C) {
	// make sure that a retry wouldn't stumble on partial work

	v1 := snaptest.MockSnap(c, helloYaml1, &snap.SideInfo{Revision: snap.R(10)})
	s.populateData(c, snap.R(10))
	homedir := s.populateHomeData(c, "user1", snap.R(10))

	// pretend we install a new version
	v2 := snaptest.MockSnap(c, helloYaml2, &snap.SideInfo{Revision: snap.R(20)})

	// copy data
	err := s.be.CopySnapData(v2, v1, &s.nullProgress)
	c.Assert(err, IsNil)

	v2data := filepath.Join(dirs.SnapDataDir, "hello/20")

	err = s.be.UndoCopySnapData(v2, v1, &s.nullProgress)
	c.Assert(err, IsNil)

	err = s.be.UndoCopySnapData(v2, v1, &s.nullProgress)
	c.Assert(err, IsNil)

	// now removed
	_, err = os.Stat(v2data)
	c.Assert(os.IsNotExist(err), Equals, true)
	v2HomeData := filepath.Join(homedir, "hello/20")
	_, err = os.Stat(v2HomeData)
	c.Assert(os.IsNotExist(err), Equals, true)
}

func (s *copydataSuite) TestCopyDataDoFirstInstallIdempotent(c *C) {
	v1 := snaptest.MockSnap(c, helloYaml1, &snap.SideInfo{Revision: snap.R(10)})

	// first install
	err := s.be.CopySnapData(v1, nil, &s.nullProgress)
	c.Assert(err, IsNil)

	err = s.be.CopySnapData(v1, nil, &s.nullProgress)
	c.Assert(err, IsNil)

	_, err = os.Stat(v1.DataDir())
	c.Assert(err, IsNil)
	_, err = os.Stat(v1.CommonDataDir())
	c.Assert(err, IsNil)

	err = s.be.UndoCopySnapData(v1, nil, &s.nullProgress)
	c.Assert(err, IsNil)
	_, err = os.Stat(v1.DataDir())
	c.Check(os.IsNotExist(err), Equals, true)
	_, err = os.Stat(v1.CommonDataDir())
	c.Check(os.IsNotExist(err), Equals, true)
}

func (s *copydataSuite) TestCopyDataUndoFirstInstallIdempotent(c *C) {
	v1 := snaptest.MockSnap(c, helloYaml1, &snap.SideInfo{Revision: snap.R(10)})

	// first install
	err := s.be.CopySnapData(v1, nil, &s.nullProgress)
	c.Assert(err, IsNil)
	_, err = os.Stat(v1.DataDir())
	c.Assert(err, IsNil)
	_, err = os.Stat(v1.CommonDataDir())
	c.Assert(err, IsNil)

	err = s.be.UndoCopySnapData(v1, nil, &s.nullProgress)
	c.Assert(err, IsNil)

	err = s.be.UndoCopySnapData(v1, nil, &s.nullProgress)
	c.Assert(err, IsNil)

	_, err = os.Stat(v1.DataDir())
	c.Check(os.IsNotExist(err), Equals, true)
	_, err = os.Stat(v1.CommonDataDir())
	c.Check(os.IsNotExist(err), Equals, true)
}

func (s *copydataSuite) TestCopyDataCopyFailure(c *C) {
	v1 := snaptest.MockSnap(c, helloYaml1, &snap.SideInfo{Revision: snap.R(10)})
	s.populateData(c, snap.R(10))

	// pretend we install a new version
	v2 := snaptest.MockSnap(c, helloYaml2, &snap.SideInfo{Revision: snap.R(20)})

	fakeBinDir := filepath.Join(s.tempdir, "bin")
	err := os.MkdirAll(fakeBinDir, 0755)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(filepath.Join(fakeBinDir, "cp"), []byte(
		`#!/bin/sh
echo cp: boom
exit 3
`), 0755)
	c.Assert(err, IsNil)

	oldPATH := os.Getenv("PATH")
	defer os.Setenv("PATH", oldPATH)
	os.Setenv("PATH", fakeBinDir+":"+oldPATH)

	// copy data will fail
	err = s.be.CopySnapData(v2, v1, &s.nullProgress)
	c.Assert(err, ErrorMatches, regexp.QuoteMeta(fmt.Sprintf("cannot copy %s to %s: cp: boom", v1.DataDir(), v2.DataDir())))
}
