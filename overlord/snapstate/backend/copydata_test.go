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
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"os/user"
	"path/filepath"
	"regexp"
	"strconv"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/snapstate/backend"
	"github.com/snapcore/snapd/progress"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/testutil"
)

type copydataSuite struct {
	be      backend.Backend
	tempdir string
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
	for _, t := range []struct {
		snapDir string
		opts    *dirs.SnapDirOptions
	}{
		{snapDir: dirs.UserHomeSnapDir, opts: nil},
		{snapDir: dirs.UserHomeSnapDir, opts: &dirs.SnapDirOptions{}},
		{snapDir: dirs.HiddenSnapDataHomeDir, opts: &dirs.SnapDirOptions{HiddenSnapDataDir: true}}} {
		s.testCopyData(c, t.snapDir, t.opts)
		c.Assert(os.RemoveAll(s.tempdir), IsNil)
		s.tempdir = c.MkDir()
		dirs.SetRootDir(s.tempdir)
	}
}

func (s *copydataSuite) testCopyData(c *C, snapDir string, opts *dirs.SnapDirOptions) {
	homedir := filepath.Join(s.tempdir, "home", "user1", snapDir)
	homeData := filepath.Join(homedir, "hello/10")
	err := os.MkdirAll(homeData, 0755)
	c.Assert(err, IsNil)
	homeCommonData := filepath.Join(homedir, "hello/common")
	err = os.MkdirAll(homeCommonData, 0755)
	c.Assert(err, IsNil)

	canaryData := []byte("ni ni ni")

	v1 := snaptest.MockSnap(c, helloYaml1, &snap.SideInfo{Revision: snap.R(10)})
	// just creates data dirs in this case
	err = s.be.CopySnapData(v1, nil, progress.Null, opts)
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
	err = s.be.CopySnapData(v2, v1, progress.Null, opts)
	c.Assert(err, IsNil)

	newCanaryDataFile := filepath.Join(dirs.SnapDataDir, "hello/20", "canary.txt")
	c.Assert(newCanaryDataFile, testutil.FileEquals, canaryData)

	// ensure common data file is still there (even though it didn't get copied)
	newCanaryDataFile = filepath.Join(dirs.SnapDataDir, "hello", "common", "canary.common")
	c.Assert(newCanaryDataFile, testutil.FileEquals, canaryData)

	newCanaryDataFile = filepath.Join(homedir, "hello/20", "canary.home")
	c.Assert(newCanaryDataFile, testutil.FileEquals, canaryData)

	// ensure home common data file is still there (even though it didn't get copied)
	newCanaryDataFile = filepath.Join(homedir, "hello", "common", "canary.common_home")
	c.Assert(newCanaryDataFile, testutil.FileEquals, canaryData)
}

func (s *copydataSuite) TestCopyDataBails(c *C) {
	oldSnapDataHomeGlob := dirs.SnapDataHomeGlob
	defer func() { dirs.SnapDataHomeGlob = oldSnapDataHomeGlob }()

	v1 := snaptest.MockSnap(c, helloYaml1, &snap.SideInfo{Revision: snap.R(10)})
	c.Assert(s.be.CopySnapData(v1, nil, progress.Null, nil), IsNil)
	c.Assert(os.Chmod(v1.DataDir(), 0), IsNil)

	v2 := snaptest.MockSnap(c, helloYaml2, &snap.SideInfo{Revision: snap.R(20)})
	err := s.be.CopySnapData(v2, v1, progress.Null, nil)
	c.Check(err, ErrorMatches, "cannot copy .*")
}

// ensure that even with no home dir there is no error and the
// system data gets copied
func (s *copydataSuite) TestCopyDataNoUserHomes(c *C) {
	// this home dir path does not exist
	oldSnapDataHomeGlob := dirs.SnapDataHomeGlob
	defer func() { dirs.SnapDataHomeGlob = oldSnapDataHomeGlob }()
	dirs.SnapDataHomeGlob = filepath.Join(s.tempdir, "no-such-home", "*", "snap")

	v1 := snaptest.MockSnap(c, helloYaml1, &snap.SideInfo{Revision: snap.R(10)})
	err := s.be.CopySnapData(v1, nil, progress.Null, nil)
	c.Assert(err, IsNil)

	canaryDataFile := filepath.Join(v1.DataDir(), "canary.txt")
	err = ioutil.WriteFile(canaryDataFile, []byte(""), 0644)
	c.Assert(err, IsNil)
	canaryDataFile = filepath.Join(v1.CommonDataDir(), "canary.common")
	err = ioutil.WriteFile(canaryDataFile, []byte(""), 0644)
	c.Assert(err, IsNil)

	v2 := snaptest.MockSnap(c, helloYaml2, &snap.SideInfo{Revision: snap.R(20)})
	err = s.be.CopySnapData(v2, v1, progress.Null, nil)
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
	datadir := filepath.Join(dirs.SnapDataDir, "hello", revision.String())
	subdir := filepath.Join(datadir, "random-subdir")
	err := os.MkdirAll(subdir, 0755)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(filepath.Join(subdir, "canary"), []byte(fmt.Sprintln(revision)), 0644)
	c.Assert(err, IsNil)
}

func (s *copydataSuite) populatedData(d string) string {
	bs, err := ioutil.ReadFile(filepath.Join(dirs.SnapDataDir, "hello", d, "random-subdir", "canary"))
	if err == nil {
		return string(bs)
	}
	if os.IsNotExist(err) {
		return ""
	}
	panic(err)
}

func (s copydataSuite) populateHomeData(c *C, user string, revision snap.Revision) (homedir string) {
	return s.populateHomeDataWithSnapDir(c, user, dirs.UserHomeSnapDir, revision)
}

func (s copydataSuite) populateHomeDataWithSnapDir(c *C, user string, snapDir string, revision snap.Revision) (homedir string) {
	homedir = filepath.Join(s.tempdir, "home", user, snapDir)
	homeData := filepath.Join(homedir, "hello", revision.String())
	err := os.MkdirAll(homeData, 0755)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(filepath.Join(homeData, "canary.home"), []byte(fmt.Sprintln(revision)), 0644)
	c.Assert(err, IsNil)

	return homedir
}

func (s *copydataSuite) TestCopyDataDoUndo(c *C) {
	for _, t := range []struct {
		snapDir string
		opts    *dirs.SnapDirOptions
	}{
		{snapDir: dirs.UserHomeSnapDir},
		{snapDir: dirs.UserHomeSnapDir, opts: &dirs.SnapDirOptions{}},
		{snapDir: dirs.HiddenSnapDataHomeDir, opts: &dirs.SnapDirOptions{HiddenSnapDataDir: true}},
	} {
		s.testCopyDataUndo(c, t.snapDir, t.opts)
		c.Assert(os.RemoveAll(s.tempdir), IsNil)
		s.tempdir = c.MkDir()
		dirs.SetRootDir(s.tempdir)
	}
}

func (s *copydataSuite) testCopyDataUndo(c *C, snapDir string, opts *dirs.SnapDirOptions) {
	v1 := snaptest.MockSnap(c, helloYaml1, &snap.SideInfo{Revision: snap.R(10)})
	s.populateData(c, snap.R(10))
	homedir := s.populateHomeDataWithSnapDir(c, "user1", snapDir, snap.R(10))

	// pretend we install a new version
	v2 := snaptest.MockSnap(c, helloYaml2, &snap.SideInfo{Revision: snap.R(20)})

	// copy data
	err := s.be.CopySnapData(v2, v1, progress.Null, opts)
	c.Assert(err, IsNil)
	v2data := filepath.Join(dirs.SnapDataDir, "hello/20")
	l, err := filepath.Glob(filepath.Join(v2data, "*"))
	c.Assert(err, IsNil)
	c.Assert(l, HasLen, 1)
	v2HomeData := filepath.Join(homedir, "hello/20")
	l, err = filepath.Glob(filepath.Join(v2HomeData, "*"))
	c.Assert(err, IsNil)
	c.Assert(l, HasLen, 1)

	err = s.be.UndoCopySnapData(v2, v1, progress.Null, opts)
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
	err := s.be.CopySnapData(v2, v1, progress.Null, nil)
	c.Assert(err, IsNil)
	v2data := filepath.Join(dirs.SnapDataDir, "hello/20")
	l, err := filepath.Glob(filepath.Join(v2data, "*"))
	c.Assert(err, IsNil)
	c.Assert(l, HasLen, 1)

	err = s.be.UndoCopySnapData(v2, v1, progress.Null, nil)
	c.Assert(err, IsNil)

	// now removed
	_, err = os.Stat(v2data)
	c.Assert(os.IsNotExist(err), Equals, true)
}

func (s *copydataSuite) TestCopyDataDoUndoFirstInstall(c *C) {
	v1 := snaptest.MockSnap(c, helloYaml1, &snap.SideInfo{Revision: snap.R(10)})

	// first install
	err := s.be.CopySnapData(v1, nil, progress.Null, nil)
	c.Assert(err, IsNil)
	_, err = os.Stat(v1.DataDir())
	c.Assert(err, IsNil)
	_, err = os.Stat(v1.CommonDataDir())
	c.Assert(err, IsNil)

	err = s.be.UndoCopySnapData(v1, nil, progress.Null, nil)
	c.Assert(err, IsNil)
	_, err = os.Stat(v1.DataDir())
	c.Check(os.IsNotExist(err), Equals, true)
	_, err = os.Stat(v1.CommonDataDir())
	c.Check(os.IsNotExist(err), Equals, true)
}

func (s *copydataSuite) TestCopyDataDoABA(c *C) {
	for _, opts := range []*dirs.SnapDirOptions{nil, {}, {HiddenSnapDataDir: true}} {
		s.testCopyDataDoABA(c, opts)
	}
}

func (s *copydataSuite) testCopyDataDoABA(c *C, opts *dirs.SnapDirOptions) {
	v1 := snaptest.MockSnap(c, helloYaml1, &snap.SideInfo{Revision: snap.R(10)})
	s.populateData(c, snap.R(10))
	c.Check(s.populatedData("10"), Equals, "10\n")

	// pretend we install a new version
	v2 := snaptest.MockSnap(c, helloYaml2, &snap.SideInfo{Revision: snap.R(20)})
	// and write our own data to it
	s.populateData(c, snap.R(20))
	c.Check(s.populatedData("20"), Equals, "20\n")

	// and now we pretend to refresh back to v1 (r10)
	c.Check(s.be.CopySnapData(v1, v2, progress.Null, opts), IsNil)

	// so 10 now has 20's data
	c.Check(s.populatedData("10"), Equals, "20\n")

	// but we still have the trash
	c.Check(s.populatedData("10.old"), Equals, "10\n")

	// but cleanup cleans it up, huzzah
	s.be.ClearTrashedData(v1)
	c.Check(s.populatedData("10.old"), Equals, "")
}

func (s *copydataSuite) TestCopyDataDoUndoABA(c *C) {
	v1 := snaptest.MockSnap(c, helloYaml1, &snap.SideInfo{Revision: snap.R(10)})
	s.populateData(c, snap.R(10))
	c.Check(s.populatedData("10"), Equals, "10\n")

	// pretend we install a new version
	v2 := snaptest.MockSnap(c, helloYaml2, &snap.SideInfo{Revision: snap.R(20)})
	// and write our own data to it
	s.populateData(c, snap.R(20))
	c.Check(s.populatedData("20"), Equals, "20\n")

	// and now we pretend to refresh back to v1 (r10)
	c.Check(s.be.CopySnapData(v1, v2, progress.Null, nil), IsNil)

	// so v1 (r10) now has v2 (r20)'s data and we have trash
	c.Check(s.populatedData("10"), Equals, "20\n")
	c.Check(s.populatedData("10.old"), Equals, "10\n")

	// but oh no! we have to undo it!
	c.Check(s.be.UndoCopySnapData(v1, v2, progress.Null, nil), IsNil)

	// so now v1 (r10) has v1 (r10)'s data and v2 (r20) has v2 (r20)'s data and we have no trash
	c.Check(s.populatedData("10"), Equals, "10\n")
	c.Check(s.populatedData("20"), Equals, "20\n")
	c.Check(s.populatedData("10.old"), Equals, "")
}

func (s *copydataSuite) TestCopyDataDoIdempotent(c *C) {
	// make sure that a retry wouldn't stumble on partial work

	v1 := snaptest.MockSnap(c, helloYaml1, &snap.SideInfo{Revision: snap.R(10)})

	s.populateData(c, snap.R(10))
	homedir := s.populateHomeData(c, "user1", snap.R(10))

	// pretend we install a new version
	v2 := snaptest.MockSnap(c, helloYaml2, &snap.SideInfo{Revision: snap.R(20)})

	// copy data
	err := s.be.CopySnapData(v2, v1, progress.Null, nil)
	c.Assert(err, IsNil)

	err = s.be.CopySnapData(v2, v1, progress.Null, nil)
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
	err := s.be.CopySnapData(v2, v1, progress.Null, nil)
	c.Assert(err, IsNil)

	v2data := filepath.Join(dirs.SnapDataDir, "hello/20")

	err = s.be.UndoCopySnapData(v2, v1, progress.Null, nil)
	c.Assert(err, IsNil)

	err = s.be.UndoCopySnapData(v2, v1, progress.Null, nil)
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
	err := s.be.CopySnapData(v1, nil, progress.Null, nil)
	c.Assert(err, IsNil)

	err = s.be.CopySnapData(v1, nil, progress.Null, nil)
	c.Assert(err, IsNil)

	_, err = os.Stat(v1.DataDir())
	c.Assert(err, IsNil)
	_, err = os.Stat(v1.CommonDataDir())
	c.Assert(err, IsNil)

	err = s.be.UndoCopySnapData(v1, nil, progress.Null, nil)
	c.Assert(err, IsNil)
	_, err = os.Stat(v1.DataDir())
	c.Check(os.IsNotExist(err), Equals, true)
	_, err = os.Stat(v1.CommonDataDir())
	c.Check(os.IsNotExist(err), Equals, true)
}

func (s *copydataSuite) TestCopyDataUndoFirstInstallIdempotent(c *C) {
	v1 := snaptest.MockSnap(c, helloYaml1, &snap.SideInfo{Revision: snap.R(10)})

	// first install
	err := s.be.CopySnapData(v1, nil, progress.Null, nil)
	c.Assert(err, IsNil)
	_, err = os.Stat(v1.DataDir())
	c.Assert(err, IsNil)
	_, err = os.Stat(v1.CommonDataDir())
	c.Assert(err, IsNil)

	err = s.be.UndoCopySnapData(v1, nil, progress.Null, nil)
	c.Assert(err, IsNil)

	err = s.be.UndoCopySnapData(v1, nil, progress.Null, nil)
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

	defer testutil.MockCommand(c, "cp", "echo cp: boom; exit 3").Restore()

	q := func(s string) string {
		return regexp.QuoteMeta(strconv.Quote(s))
	}

	// copy data will fail
	err := s.be.CopySnapData(v2, v1, progress.Null, nil)
	c.Assert(err, ErrorMatches, fmt.Sprintf(`cannot copy %s to %s: .*: "cp: boom" \(3\)`, q(v1.DataDir()), q(v2.DataDir())))
}

func (s *copydataSuite) TestCopyDataPartialFailure(c *C) {
	v1 := snaptest.MockSnap(c, helloYaml1, &snap.SideInfo{Revision: snap.R(10)})

	s.populateData(c, snap.R(10))
	homedir1 := s.populateHomeData(c, "user1", snap.R(10))
	homedir2 := s.populateHomeData(c, "user2", snap.R(10))

	// pretend we install a new version
	v2 := snaptest.MockSnap(c, helloYaml2, &snap.SideInfo{Revision: snap.R(20)})

	// sanity check: the 20 dirs don't exist yet (but 10 do)
	for _, dir := range []string{dirs.SnapDataDir, homedir1, homedir2} {
		c.Assert(osutil.FileExists(filepath.Join(dir, "hello", "20")), Equals, false, Commentf(dir))
		c.Assert(osutil.FileExists(filepath.Join(dir, "hello", "10")), Equals, true, Commentf(dir))
	}

	c.Assert(os.Chmod(filepath.Join(homedir2, "hello", "10", "canary.home"), 0), IsNil)

	// try to copy data
	err := s.be.CopySnapData(v2, v1, progress.Null, nil)
	c.Assert(err, NotNil)

	// the copy data failed, so check it cleaned up after itself (but not too much!)
	for _, dir := range []string{dirs.SnapDataDir, homedir1, homedir2} {
		c.Check(osutil.FileExists(filepath.Join(dir, "hello", "20")), Equals, false, Commentf(dir))
		c.Check(osutil.FileExists(filepath.Join(dir, "hello", "10")), Equals, true, Commentf(dir))
	}
}

func (s *copydataSuite) TestCopyDataSameRevision(c *C) {
	v1 := snaptest.MockSnap(c, helloYaml1, &snap.SideInfo{Revision: snap.R(10)})

	homedir1 := s.populateHomeData(c, "user1", snap.R(10))
	homedir2 := s.populateHomeData(c, "user2", snap.R(10))
	c.Assert(os.MkdirAll(v1.DataDir(), 0755), IsNil)
	c.Assert(os.MkdirAll(v1.CommonDataDir(), 0755), IsNil)
	c.Assert(ioutil.WriteFile(filepath.Join(v1.DataDir(), "canary.txt"), nil, 0644), IsNil)
	c.Assert(ioutil.WriteFile(filepath.Join(v1.CommonDataDir(), "canary.common"), nil, 0644), IsNil)

	// the data is there
	for _, fn := range []string{
		filepath.Join(v1.DataDir(), "canary.txt"),
		filepath.Join(v1.CommonDataDir(), "canary.common"),
		filepath.Join(homedir1, "hello", "10", "canary.home"),
		filepath.Join(homedir2, "hello", "10", "canary.home"),
	} {
		c.Assert(osutil.FileExists(fn), Equals, true, Commentf(fn))
	}

	// copy data works
	err := s.be.CopySnapData(v1, v1, progress.Null, nil)
	c.Assert(err, IsNil)

	// the data is still there :-)
	for _, fn := range []string{
		filepath.Join(v1.DataDir(), "canary.txt"),
		filepath.Join(v1.CommonDataDir(), "canary.common"),
		filepath.Join(homedir1, "hello", "10", "canary.home"),
		filepath.Join(homedir2, "hello", "10", "canary.home"),
	} {
		c.Check(osutil.FileExists(fn), Equals, true, Commentf(fn))
	}

}

func (s *copydataSuite) TestUndoCopyDataSameRevision(c *C) {
	v1 := snaptest.MockSnap(c, helloYaml1, &snap.SideInfo{Revision: snap.R(10)})

	homedir1 := s.populateHomeData(c, "user1", snap.R(10))
	homedir2 := s.populateHomeData(c, "user2", snap.R(10))
	c.Assert(os.MkdirAll(v1.DataDir(), 0755), IsNil)
	c.Assert(os.MkdirAll(v1.CommonDataDir(), 0755), IsNil)
	c.Assert(ioutil.WriteFile(filepath.Join(v1.DataDir(), "canary.txt"), nil, 0644), IsNil)
	c.Assert(ioutil.WriteFile(filepath.Join(v1.CommonDataDir(), "canary.common"), nil, 0644), IsNil)

	// the data is there
	for _, fn := range []string{
		filepath.Join(v1.DataDir(), "canary.txt"),
		filepath.Join(v1.CommonDataDir(), "canary.common"),
		filepath.Join(homedir1, "hello", "10", "canary.home"),
		filepath.Join(homedir2, "hello", "10", "canary.home"),
	} {
		c.Assert(osutil.FileExists(fn), Equals, true, Commentf(fn))
	}

	// undo copy data works
	err := s.be.UndoCopySnapData(v1, v1, progress.Null, nil)
	c.Assert(err, IsNil)

	// the data is still there :-)
	for _, fn := range []string{
		filepath.Join(v1.DataDir(), "canary.txt"),
		filepath.Join(v1.CommonDataDir(), "canary.common"),
		filepath.Join(homedir1, "hello", "10", "canary.home"),
		filepath.Join(homedir2, "hello", "10", "canary.home"),
	} {
		c.Check(osutil.FileExists(fn), Equals, true, Commentf(fn))
	}
}

func (s *copydataSuite) TestHideSnapData(c *C) {
	info := snaptest.MockSnap(c, helloYaml1, &snap.SideInfo{Revision: snap.R(10)})

	// mock user home
	homedir := filepath.Join(s.tempdir, "home", "user")
	usr, err := user.Current()
	c.Assert(err, IsNil)
	usr.HomeDir = homedir

	restore := backend.MockAllUsers(func(*dirs.SnapDirOptions) ([]*user.User, error) {
		return []*user.User{usr}, nil
	})
	defer restore()

	// writes a file canary.home file to the rev dir of the "hello" snap
	s.populateHomeData(c, "user", snap.R(10))

	// write file in common
	err = os.MkdirAll(info.UserCommonDataDir(homedir, nil), 0770)
	c.Assert(err, IsNil)

	commonFilePath := filepath.Join(info.UserCommonDataDir(homedir, nil), "file.txt")
	err = ioutil.WriteFile(commonFilePath, []byte("some content"), 0640)
	c.Assert(err, IsNil)

	// make 'current' symlink
	revDir := snap.UserDataDir(homedir, "hello", snap.R(10), nil)
	// path must be relative, otherwise move would make it dangling
	err = os.Symlink(filepath.Base(revDir), filepath.Join(revDir, "..", "current"))
	c.Assert(err, IsNil)

	err = s.be.HideSnapData("hello")
	c.Assert(err, IsNil)

	// check versioned file was moved
	opts := &dirs.SnapDirOptions{HiddenSnapDataDir: true}
	revFile := filepath.Join(info.UserDataDir(homedir, opts), "canary.home")
	data, err := ioutil.ReadFile(revFile)
	c.Assert(err, IsNil)
	c.Assert(data, DeepEquals, []byte("10\n"))

	// check common file was moved
	commonFile := filepath.Join(info.UserCommonDataDir(homedir, opts), "file.txt")
	data, err = ioutil.ReadFile(commonFile)
	c.Assert(err, IsNil)
	c.Assert(data, DeepEquals, []byte("some content"))

	// check 'current' symlink has correct attributes and target
	link := filepath.Join(homedir, dirs.HiddenSnapDataHomeDir, "hello", "current")
	linkInfo, err := os.Lstat(link)
	c.Assert(err, IsNil)
	c.Assert(linkInfo.Mode()&os.ModeSymlink, Equals, os.ModeSymlink)

	target, err := os.Readlink(link)
	c.Assert(err, IsNil)
	c.Assert(target, Equals, "10")

	// check old '~/snap' folder was removed
	_, err = os.Stat(snap.SnapDir(homedir, nil))
	c.Assert(errors.Is(err, os.ErrNotExist), Equals, true)
}

func (s *copydataSuite) TestHideSnapDataSkipNoData(c *C) {
	info := snaptest.MockSnap(c, helloYaml1, &snap.SideInfo{Revision: snap.R(10)})

	// mock user home
	homedir := filepath.Join(s.tempdir, "home", "user")
	usr, err := user.Current()
	c.Assert(err, IsNil)
	usr.HomeDir = homedir

	// create user without snap dir (to be skipped)
	usrNoSnapDir := &user.User{
		HomeDir: filepath.Join(s.tempdir, "home", "other-user"),
		Name:    "other-user",
		Uid:     "1001",
		Gid:     "1001",
	}
	restore := backend.MockAllUsers(func(_ *dirs.SnapDirOptions) ([]*user.User, error) {
		return []*user.User{usr, usrNoSnapDir}, nil
	})
	defer restore()

	s.populateHomeData(c, "user", snap.R(10))

	// make 'current' symlink
	revDir := info.UserDataDir(homedir, nil)
	linkPath := filepath.Join(revDir, "..", "current")
	err = os.Symlink(revDir, linkPath)
	c.Assert(err, IsNil)

	// empty user dir is skipped
	err = s.be.HideSnapData("hello")
	c.Assert(err, IsNil)

	// only the user with snap data was migrated
	newSnapDir := filepath.Join(homedir, dirs.HiddenSnapDataHomeDir)
	matches, err := filepath.Glob(dirs.HiddenSnapDataHomeGlob)
	c.Assert(err, IsNil)
	c.Assert(matches, HasLen, 1)
	c.Assert(matches[0], Equals, newSnapDir)
}

func (s *copydataSuite) TestUndoHideSnapData(c *C) {
	info := snaptest.MockSnap(c, helloYaml1, &snap.SideInfo{Revision: snap.R(10)})

	// mock user home dir
	homedir := filepath.Join(s.tempdir, "home", "user")
	usr, err := user.Current()
	c.Assert(err, IsNil)
	usr.HomeDir = homedir

	restore := backend.MockAllUsers(func(_ *dirs.SnapDirOptions) ([]*user.User, error) {
		return []*user.User{usr}, nil
	})
	defer restore()

	// write file in revisioned dir
	opts := &dirs.SnapDirOptions{HiddenSnapDataDir: true}
	err = os.MkdirAll(info.UserDataDir(homedir, opts), 0770)
	c.Assert(err, IsNil)

	hiddenRevFile := filepath.Join(info.UserDataDir(homedir, opts), "file.txt")
	err = ioutil.WriteFile(hiddenRevFile, []byte("some content"), 0640)
	c.Assert(err, IsNil)

	// write file in common
	err = os.MkdirAll(info.UserCommonDataDir(homedir, opts), 0770)
	c.Assert(err, IsNil)

	hiddenCommonFile := filepath.Join(info.UserCommonDataDir(homedir, opts), "file.txt")
	err = ioutil.WriteFile(hiddenCommonFile, []byte("other content"), 0640)
	c.Assert(err, IsNil)

	// make 'current' symlink
	revDir := info.UserDataDir(homedir, opts)
	// path must be relative otherwise the move would make it dangling
	err = os.Symlink(filepath.Base(revDir), filepath.Join(revDir, "..", "current"))
	c.Assert(err, IsNil)

	// undo migration
	err = s.be.UndoHideSnapData("hello")
	c.Assert(err, IsNil)

	// check versioned file was restored
	revFile := filepath.Join(info.UserDataDir(homedir, nil), "file.txt")
	data, err := ioutil.ReadFile(revFile)
	c.Assert(err, IsNil)
	c.Assert(data, DeepEquals, []byte("some content"))

	// check common file was restored
	commonFile := filepath.Join(info.UserCommonDataDir(homedir, nil), "file.txt")
	data, err = ioutil.ReadFile(commonFile)
	c.Assert(err, IsNil)
	c.Assert(data, DeepEquals, []byte("other content"))

	// check symlink points to revisioned dir
	exposedDir := filepath.Join(homedir, dirs.UserHomeSnapDir)
	target, err := os.Readlink(filepath.Join(exposedDir, "hello", "current"))
	c.Assert(err, IsNil)
	c.Assert(target, Equals, "10")

	// ~/.snap/data was removed
	_, err = os.Stat(snap.SnapDir(homedir, opts))
	c.Assert(errors.Is(err, os.ErrNotExist), Equals, true)
}

func (s *copydataSuite) TestCleanupAfterCopyAndMigration(c *C) {
	homedir := filepath.Join(s.tempdir, "home", "user")
	usr, err := user.Current()
	c.Assert(err, IsNil)
	usr.HomeDir = homedir

	restore := backend.MockAllUsers(func(_ *dirs.SnapDirOptions) ([]*user.User, error) {
		return []*user.User{usr}, nil
	})
	defer restore()

	// add trashed data in exposed dir
	s.populateHomeData(c, "user", snap.R(10))
	v1 := snaptest.MockSnap(c, helloYaml1, &snap.SideInfo{Revision: snap.R(10)})
	exposedTrash := filepath.Join(homedir, "snap", "hello", "10.old")
	c.Assert(os.MkdirAll(exposedTrash, 0770), IsNil)

	// add trashed data in hidden dir
	s.populateHomeDataWithSnapDir(c, "user", dirs.HiddenSnapDataHomeDir, snap.R(10))
	hiddenTrash := filepath.Join(homedir, ".snap", "data", "hello", "10.old")
	c.Assert(os.MkdirAll(exposedTrash, 0770), IsNil)

	s.be.ClearTrashedData(v1)

	// clear should remove both
	exists, _, err := osutil.DirExists(exposedTrash)
	c.Assert(err, IsNil)
	c.Assert(exists, Equals, false)

	exists, _, err = osutil.DirExists(hiddenTrash)
	c.Assert(err, IsNil)
	c.Assert(exists, Equals, false)
}

func (s *copydataSuite) TestRemoveIfEmpty(c *C) {
	file := filepath.Join(s.tempdir, "random")
	c.Assert(ioutil.WriteFile(file, []byte("stuff"), 0664), IsNil)

	// dir contains a file, shouldn't do anything
	c.Assert(backend.RemoveIfEmpty(s.tempdir), IsNil)
	files, err := ioutil.ReadDir(s.tempdir)
	c.Assert(err, IsNil)
	c.Check(files, HasLen, 1)
	c.Check(filepath.Join(s.tempdir, files[0].Name()), testutil.FileEquals, "stuff")

	c.Assert(os.Remove(file), IsNil)

	// dir is empty, should be removed
	c.Assert(backend.RemoveIfEmpty(s.tempdir), IsNil)
	c.Assert(osutil.FileExists(file), Equals, false)
}

func (s *copydataSuite) TestUndoHideKeepGoingPreserveFirstErr(c *C) {
	firstTime := true
	restore := backend.MockRemoveIfEmpty(func(dir string) error {
		var err error
		if firstTime {
			err = errors.New("first error")
			firstTime = false
		} else {
			err = errors.New("other error")
		}

		return err
	})
	defer restore()

	info := snaptest.MockSnap(c, helloYaml1, &snap.SideInfo{Revision: snap.R(10)})

	// mock two users so that the undo is done twice
	var usrs []*user.User
	for _, usrName := range []string{"usr1", "usr2"} {
		homedir := filepath.Join(s.tempdir, "home", usrName)
		usr, err := user.Current()
		c.Assert(err, IsNil)
		usr.HomeDir = homedir

		opts := &dirs.SnapDirOptions{HiddenSnapDataDir: true}
		err = os.MkdirAll(info.UserDataDir(homedir, opts), 0770)
		c.Assert(err, IsNil)

		usrs = append(usrs, usr)
	}

	restUsers := backend.MockAllUsers(func(_ *dirs.SnapDirOptions) ([]*user.User, error) {
		return usrs, nil
	})
	defer restUsers()

	buf, restLogger := logger.MockLogger()
	defer restLogger()

	err := s.be.UndoHideSnapData("hello")
	// the first error is returned
	c.Assert(err, ErrorMatches, `cannot remove dir ".*": first error`)
	// the undo keeps going and logs the next error
	c.Assert(buf, Matches, `.*cannot remove dir ".*": other error\n`)
}
