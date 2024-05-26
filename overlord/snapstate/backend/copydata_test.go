// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2022 Canonical Ltd
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
	"os"
	"os/user"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	. "gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/osutil/sys"
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
		{snapDir: dirs.HiddenSnapDataHomeDir, opts: &dirs.SnapDirOptions{HiddenSnapDataDir: true}},
	} {
		s.testCopyData(c, t.snapDir, t.opts)
		c.Assert(os.RemoveAll(s.tempdir), IsNil)
		s.tempdir = c.MkDir()
		dirs.SetRootDir(s.tempdir)
	}
}

func (s *copydataSuite) testCopyData(c *C, snapDir string, opts *dirs.SnapDirOptions) {
	dirs.SetSnapHomeDirs("/home")
	homedir := filepath.Join(dirs.GlobalRootDir, "home", "user1", snapDir)
	homeData := filepath.Join(homedir, "hello/10")
	mylog.Check(os.MkdirAll(homeData, 0755))

	homeCommonData := filepath.Join(homedir, "hello/common")
	mylog.Check(os.MkdirAll(homeCommonData, 0755))


	canaryData := []byte("ni ni ni")

	v1 := snaptest.MockSnap(c, helloYaml1, &snap.SideInfo{Revision: snap.R(10)})
	mylog.
		// just creates data dirs in this case
		Check(s.be.CopySnapData(v1, nil, opts, progress.Null))


	canaryDataFile := filepath.Join(v1.DataDir(), "canary.txt")
	mylog.Check(os.WriteFile(canaryDataFile, canaryData, 0644))

	canaryDataFile = filepath.Join(v1.CommonDataDir(), "canary.common")
	mylog.Check(os.WriteFile(canaryDataFile, canaryData, 0644))

	mylog.Check(os.WriteFile(filepath.Join(homeData, "canary.home"), canaryData, 0644))

	mylog.Check(os.WriteFile(filepath.Join(homeCommonData, "canary.common_home"), canaryData, 0644))


	v2 := snaptest.MockSnap(c, helloYaml2, &snap.SideInfo{Revision: snap.R(20)})
	mylog.Check(s.be.CopySnapData(v2, v1, opts, progress.Null))


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

// same as TestCopyData but with multiple home directories
func (s *copydataSuite) TestCopyDataMulti(c *C) {
	for _, t := range []struct {
		snapDir string
		opts    *dirs.SnapDirOptions
	}{
		{snapDir: dirs.UserHomeSnapDir, opts: nil},
		{snapDir: dirs.UserHomeSnapDir, opts: &dirs.SnapDirOptions{}},
		{snapDir: dirs.HiddenSnapDataHomeDir, opts: &dirs.SnapDirOptions{HiddenSnapDataDir: true}},
	} {
		s.testCopyDataMulti(c, t.snapDir, t.opts)
		c.Assert(os.RemoveAll(s.tempdir), IsNil)
		s.tempdir = c.MkDir()
		dirs.SetRootDir(s.tempdir)
	}
}

func (s *copydataSuite) testCopyDataMulti(c *C, snapDir string, opts *dirs.SnapDirOptions) {
	homeDirs := []string{
		filepath.Join(dirs.GlobalRootDir, "home"),
		filepath.Join(dirs.GlobalRootDir, "home", "company"),
		filepath.Join(dirs.GlobalRootDir, "home", "department"),
		filepath.Join(dirs.GlobalRootDir, "office"),
	}
	dirs.SetSnapHomeDirs(strings.Join(homeDirs, ","))

	snapHomeDirs := []string{}
	snapHomeDataDirs := []string{}
	snapHomeCommonDirs := []string{}

	for _, v := range homeDirs {
		snapHomeDir := filepath.Join(v, "user1", snapDir)
		snapHomeData := filepath.Join(snapHomeDir, "hello/10")
		mylog.Check(os.MkdirAll(snapHomeData, 0755))

		homeCommonData := filepath.Join(snapHomeDir, "hello/common")
		mylog.Check(os.MkdirAll(homeCommonData, 0755))

		snapHomeDirs = append(snapHomeDirs, snapHomeDir)
		snapHomeDataDirs = append(snapHomeDataDirs, snapHomeData)
		snapHomeCommonDirs = append(snapHomeCommonDirs, homeCommonData)
	}

	canaryData := []byte("ni ni ni")

	v1 := snaptest.MockSnap(c, helloYaml1, &snap.SideInfo{Revision: snap.R(10)})
	mylog.
		// just creates data dirs in this case
		Check(s.be.CopySnapData(v1, nil, opts, progress.Null))


	canaryDataFile := filepath.Join(v1.DataDir(), "canary.txt")
	mylog.Check(os.WriteFile(canaryDataFile, canaryData, 0644))

	canaryDataFile = filepath.Join(v1.CommonDataDir(), "canary.common")
	mylog.Check(os.WriteFile(canaryDataFile, canaryData, 0644))


	for i := range snapHomeDataDirs {
		mylog.Check(os.WriteFile(filepath.Join(snapHomeDataDirs[i], "canary.home"), canaryData, 0644))

		mylog.Check(os.WriteFile(filepath.Join(snapHomeCommonDirs[i], "canary.common_home"), canaryData, 0644))

	}

	v2 := snaptest.MockSnap(c, helloYaml2, &snap.SideInfo{Revision: snap.R(20)})
	mylog.Check(s.be.CopySnapData(v2, v1, opts, progress.Null))


	newCanaryDataFile := filepath.Join(dirs.SnapDataDir, "hello/20", "canary.txt")
	c.Assert(newCanaryDataFile, testutil.FileEquals, canaryData)

	// ensure common data file is still there (even though it didn't get copied)
	newCanaryDataFile = filepath.Join(dirs.SnapDataDir, "hello", "common", "canary.common")
	c.Assert(newCanaryDataFile, testutil.FileEquals, canaryData)

	for _, v := range snapHomeDirs {
		newCanaryDataFile = filepath.Join(v, "hello/20", "canary.home")
		c.Assert(newCanaryDataFile, testutil.FileEquals, canaryData)

		// ensure home common data file is still there (even though it didn't get copied)
		newCanaryDataFile = filepath.Join(v, "hello", "common", "canary.common_home")
		c.Assert(newCanaryDataFile, testutil.FileEquals, canaryData)
	}
}

func (s *copydataSuite) TestCopyDataBails(c *C) {
	v1 := snaptest.MockSnap(c, helloYaml1, &snap.SideInfo{Revision: snap.R(10)})
	c.Assert(s.be.CopySnapData(v1, nil, nil, progress.Null), IsNil)
	c.Assert(os.Chmod(v1.DataDir(), 0), IsNil)

	v2 := snaptest.MockSnap(c, helloYaml2, &snap.SideInfo{Revision: snap.R(20)})
	mylog.Check(s.be.CopySnapData(v2, v1, nil, progress.Null))
	c.Check(err, ErrorMatches, "cannot copy .*")
}

// ensure that even with no home dir there is no error and the
// system data gets copied
func (s *copydataSuite) TestCopyDataNoUserHomes(c *C) {
	v1 := snaptest.MockSnap(c, helloYaml1, &snap.SideInfo{Revision: snap.R(10)})
	mylog.Check(s.be.CopySnapData(v1, nil, nil, progress.Null))


	canaryDataFile := filepath.Join(v1.DataDir(), "canary.txt")
	mylog.Check(os.WriteFile(canaryDataFile, []byte(""), 0644))

	canaryDataFile = filepath.Join(v1.CommonDataDir(), "canary.common")
	mylog.Check(os.WriteFile(canaryDataFile, []byte(""), 0644))


	v2 := snaptest.MockSnap(c, helloYaml2, &snap.SideInfo{Revision: snap.R(20)})
	mylog.Check(s.be.CopySnapData(v2, v1, nil, progress.Null))


	_ = mylog.Check2(os.Stat(filepath.Join(v2.DataDir(), "canary.txt")))

	_ = mylog.Check2(os.Stat(filepath.Join(v2.CommonDataDir(), "canary.common")))


	// validity atm
	c.Check(v1.DataDir(), Not(Equals), v2.DataDir())
	c.Check(v1.CommonDataDir(), Equals, v2.CommonDataDir())
}

func (s *copydataSuite) populateData(c *C, revision snap.Revision) {
	datadir := filepath.Join(dirs.SnapDataDir, "hello", revision.String())
	subdir := filepath.Join(datadir, "random-subdir")
	mylog.Check(os.MkdirAll(subdir, 0755))

	mylog.Check(os.WriteFile(filepath.Join(subdir, "canary"), []byte(fmt.Sprintln(revision)), 0644))

}

func (s *copydataSuite) populatedData(d string) string {
	bs := mylog.Check2(os.ReadFile(filepath.Join(dirs.SnapDataDir, "hello", d, "random-subdir", "canary")))
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
	dirs.SetSnapHomeDirs("/home")
	homedir = filepath.Join(dirs.GlobalRootDir, "home", user, snapDir)
	homeData := filepath.Join(homedir, "hello", revision.String())
	mylog.Check(os.MkdirAll(homeData, 0755))

	mylog.Check(os.WriteFile(filepath.Join(homeData, "canary.home"), []byte(fmt.Sprintln(revision)), 0644))


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
	mylog.

		// copy data
		Check(s.be.CopySnapData(v2, v1, opts, progress.Null))

	v2data := filepath.Join(dirs.SnapDataDir, "hello/20")
	l := mylog.Check2(filepath.Glob(filepath.Join(v2data, "*")))

	c.Assert(l, HasLen, 1)
	v2HomeData := filepath.Join(homedir, "hello/20")
	l = mylog.Check2(filepath.Glob(filepath.Join(v2HomeData, "*")))

	c.Assert(l, HasLen, 1)
	mylog.Check(s.be.UndoCopySnapData(v2, v1, opts, progress.Null))


	// now removed
	_ = mylog.Check2(os.Stat(v2data))
	c.Assert(os.IsNotExist(err), Equals, true)
	_ = mylog.Check2(os.Stat(v2HomeData))
	c.Assert(os.IsNotExist(err), Equals, true)
}

func (s *copydataSuite) TestCopyDataDoUndoNoUserHomes(c *C) {
	v1 := snaptest.MockSnap(c, helloYaml1, &snap.SideInfo{Revision: snap.R(10)})
	s.populateData(c, snap.R(10))

	// pretend we install a new version
	v2 := snaptest.MockSnap(c, helloYaml2, &snap.SideInfo{Revision: snap.R(20)})
	mylog.

		// copy data
		Check(s.be.CopySnapData(v2, v1, nil, progress.Null))

	v2data := filepath.Join(dirs.SnapDataDir, "hello/20")
	l := mylog.Check2(filepath.Glob(filepath.Join(v2data, "*")))

	c.Assert(l, HasLen, 1)
	mylog.Check(s.be.UndoCopySnapData(v2, v1, nil, progress.Null))


	// now removed
	_ = mylog.Check2(os.Stat(v2data))
	c.Assert(os.IsNotExist(err), Equals, true)
}

func (s *copydataSuite) TestCopyDataDoUndoFirstInstall(c *C) {
	v1 := snaptest.MockSnap(c, helloYaml1, &snap.SideInfo{Revision: snap.R(10)})
	mylog.

		// first install
		Check(s.be.CopySnapData(v1, nil, nil, progress.Null))

	_ = mylog.Check2(os.Stat(v1.DataDir()))

	_ = mylog.Check2(os.Stat(v1.CommonDataDir()))

	mylog.Check(s.be.UndoCopySnapData(v1, nil, nil, progress.Null))

	_ = mylog.Check2(os.Stat(v1.DataDir()))
	c.Check(os.IsNotExist(err), Equals, true)
	_ = mylog.Check2(os.Stat(v1.CommonDataDir()))
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
	c.Check(s.be.CopySnapData(v1, v2, opts, progress.Null), IsNil)

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
	c.Check(s.be.CopySnapData(v1, v2, nil, progress.Null), IsNil)

	// so v1 (r10) now has v2 (r20)'s data and we have trash
	c.Check(s.populatedData("10"), Equals, "20\n")
	c.Check(s.populatedData("10.old"), Equals, "10\n")

	// but oh no! we have to undo it!
	c.Check(s.be.UndoCopySnapData(v1, v2, nil, progress.Null), IsNil)

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
	mylog.

		// copy data
		Check(s.be.CopySnapData(v2, v1, nil, progress.Null))

	mylog.Check(s.be.CopySnapData(v2, v1, nil, progress.Null))


	v2data := filepath.Join(dirs.SnapDataDir, "hello/20")
	l := mylog.Check2(filepath.Glob(filepath.Join(v2data, "*")))

	c.Assert(l, HasLen, 1)
	v2HomeData := filepath.Join(homedir, "hello/20")
	l = mylog.Check2(filepath.Glob(filepath.Join(v2HomeData, "*")))

	c.Assert(l, HasLen, 1)
}

func (s *copydataSuite) TestCopyDataUndoIdempotent(c *C) {
	// make sure that a retry wouldn't stumble on partial work

	v1 := snaptest.MockSnap(c, helloYaml1, &snap.SideInfo{Revision: snap.R(10)})
	s.populateData(c, snap.R(10))
	homedir := s.populateHomeData(c, "user1", snap.R(10))

	// pretend we install a new version
	v2 := snaptest.MockSnap(c, helloYaml2, &snap.SideInfo{Revision: snap.R(20)})
	mylog.

		// copy data
		Check(s.be.CopySnapData(v2, v1, nil, progress.Null))


	v2data := filepath.Join(dirs.SnapDataDir, "hello/20")
	mylog.Check(s.be.UndoCopySnapData(v2, v1, nil, progress.Null))

	mylog.Check(s.be.UndoCopySnapData(v2, v1, nil, progress.Null))


	// now removed
	_ = mylog.Check2(os.Stat(v2data))
	c.Assert(os.IsNotExist(err), Equals, true)
	v2HomeData := filepath.Join(homedir, "hello/20")
	_ = mylog.Check2(os.Stat(v2HomeData))
	c.Assert(os.IsNotExist(err), Equals, true)
}

func (s *copydataSuite) TestCopyDataDoFirstInstallIdempotent(c *C) {
	v1 := snaptest.MockSnap(c, helloYaml1, &snap.SideInfo{Revision: snap.R(10)})
	mylog.

		// first install
		Check(s.be.CopySnapData(v1, nil, nil, progress.Null))

	mylog.Check(s.be.CopySnapData(v1, nil, nil, progress.Null))


	_ = mylog.Check2(os.Stat(v1.DataDir()))

	_ = mylog.Check2(os.Stat(v1.CommonDataDir()))

	mylog.Check(s.be.UndoCopySnapData(v1, nil, nil, progress.Null))

	_ = mylog.Check2(os.Stat(v1.DataDir()))
	c.Check(os.IsNotExist(err), Equals, true)
	_ = mylog.Check2(os.Stat(v1.CommonDataDir()))
	c.Check(os.IsNotExist(err), Equals, true)
}

func (s *copydataSuite) TestCopyDataUndoFirstInstallIdempotent(c *C) {
	v1 := snaptest.MockSnap(c, helloYaml1, &snap.SideInfo{Revision: snap.R(10)})
	mylog.

		// first install
		Check(s.be.CopySnapData(v1, nil, nil, progress.Null))

	_ = mylog.Check2(os.Stat(v1.DataDir()))

	_ = mylog.Check2(os.Stat(v1.CommonDataDir()))

	mylog.Check(s.be.UndoCopySnapData(v1, nil, nil, progress.Null))

	mylog.Check(s.be.UndoCopySnapData(v1, nil, nil, progress.Null))


	_ = mylog.Check2(os.Stat(v1.DataDir()))
	c.Check(os.IsNotExist(err), Equals, true)
	_ = mylog.Check2(os.Stat(v1.CommonDataDir()))
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
	mylog.

		// copy data will fail
		Check(s.be.CopySnapData(v2, v1, nil, progress.Null))
	c.Assert(err, ErrorMatches, fmt.Sprintf(`cannot copy %s to %s: .*: "cp: boom" \(3\)`, q(v1.DataDir()), q(v2.DataDir())))
}

func (s *copydataSuite) TestCopyDataPartialFailure(c *C) {
	v1 := snaptest.MockSnap(c, helloYaml1, &snap.SideInfo{Revision: snap.R(10)})

	s.populateData(c, snap.R(10))
	homedir1 := s.populateHomeData(c, "user1", snap.R(10))
	homedir2 := s.populateHomeData(c, "user2", snap.R(10))

	// pretend we install a new version
	v2 := snaptest.MockSnap(c, helloYaml2, &snap.SideInfo{Revision: snap.R(20)})

	// precondition check: the 20 dirs don't exist yet (but 10 do)
	for _, dir := range []string{dirs.SnapDataDir, homedir1, homedir2} {
		c.Assert(osutil.FileExists(filepath.Join(dir, "hello", "20")), Equals, false, Commentf(dir))
		c.Assert(osutil.FileExists(filepath.Join(dir, "hello", "10")), Equals, true, Commentf(dir))
	}

	c.Assert(os.Chmod(filepath.Join(homedir2, "hello", "10", "canary.home"), 0), IsNil)
	mylog.

		// try to copy data
		Check(s.be.CopySnapData(v2, v1, nil, progress.Null))
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
	c.Assert(os.WriteFile(filepath.Join(v1.DataDir(), "canary.txt"), nil, 0644), IsNil)
	c.Assert(os.WriteFile(filepath.Join(v1.CommonDataDir(), "canary.common"), nil, 0644), IsNil)

	// the data is there
	for _, fn := range []string{
		filepath.Join(v1.DataDir(), "canary.txt"),
		filepath.Join(v1.CommonDataDir(), "canary.common"),
		filepath.Join(homedir1, "hello", "10", "canary.home"),
		filepath.Join(homedir2, "hello", "10", "canary.home"),
	} {
		c.Assert(osutil.FileExists(fn), Equals, true, Commentf(fn))
	}
	mylog.

		// copy data works
		Check(s.be.CopySnapData(v1, v1, nil, progress.Null))


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
	c.Assert(os.WriteFile(filepath.Join(v1.DataDir(), "canary.txt"), nil, 0644), IsNil)
	c.Assert(os.WriteFile(filepath.Join(v1.CommonDataDir(), "canary.common"), nil, 0644), IsNil)

	// the data is there
	for _, fn := range []string{
		filepath.Join(v1.DataDir(), "canary.txt"),
		filepath.Join(v1.CommonDataDir(), "canary.common"),
		filepath.Join(homedir1, "hello", "10", "canary.home"),
		filepath.Join(homedir2, "hello", "10", "canary.home"),
	} {
		c.Assert(osutil.FileExists(fn), Equals, true, Commentf(fn))
	}
	mylog.

		// undo copy data works
		Check(s.be.UndoCopySnapData(v1, v1, nil, progress.Null))


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

const (
	mountRunMntUbuntuSaveFmt = `26 27 8:3 / %s/run/mnt/ubuntu-save rw,relatime shared:7 - ext4 /dev/fakedevice0p1 rw,data=ordered`
	mountSnapSaveFmt         = `26 27 8:3 / %s/var/lib/snapd/save rw,relatime shared:7 - ext4 /dev/fakedevice0p1 rw,data=ordered`
)

func (s *copydataSuite) TestSetupCommonSaveDataClassic(c *C) {
	v1 := snaptest.MockSnap(c, helloYaml1, &snap.SideInfo{Revision: snap.R(10)})
	mylog.

		// first install
		Check(s.be.SetupSnapSaveData(v1, mockClassicDev, progress.Null))

	_ = mylog.Check2(os.Stat(v1.CommonDataSaveDir()))
	c.Assert(err.Error(), Equals, fmt.Sprintf("stat %s/var/lib/snapd/save/snap/hello: no such file or directory", dirs.GlobalRootDir))
}

func (s *copydataSuite) TestSetupCommonSaveDataCoreNoMount(c *C) {
	restore := osutil.MockMountInfo("")
	defer restore()
	v1 := snaptest.MockSnap(c, helloYaml1, &snap.SideInfo{Revision: snap.R(10)})
	mylog.

		// first install
		Check(s.be.SetupSnapSaveData(v1, mockDev, progress.Null))

	_ = mylog.Check2(os.Stat(v1.CommonDataSaveDir()))
	c.Assert(err.Error(), Equals, fmt.Sprintf("stat %s/var/lib/snapd/save/snap/hello: no such file or directory", dirs.GlobalRootDir))
}

func (s *copydataSuite) TestSetupCommonSaveDataFirstInstall(c *C) {
	restore := osutil.MockMountInfo(fmt.Sprintf(mountRunMntUbuntuSaveFmt, dirs.GlobalRootDir) + "\n" +
		fmt.Sprintf(mountSnapSaveFmt, dirs.GlobalRootDir))
	defer restore()

	v1 := snaptest.MockSnap(c, helloYaml1, &snap.SideInfo{Revision: snap.R(10)})
	mylog.

		// first install
		Check(s.be.SetupSnapSaveData(v1, mockDev, progress.Null))

	_ = mylog.Check2(os.Stat(v1.CommonDataSaveDir()))


	// create a test file to make sure this also gets removed
	c.Assert(os.WriteFile(filepath.Join(v1.CommonDataSaveDir(), "canary.txt"), nil, 0644), IsNil)
	mylog.

		// removes correctly when no previous info is present
		Check(s.be.UndoSetupSnapSaveData(v1, nil, mockDev, progress.Null))

	_ = mylog.Check2(os.Stat(v1.CommonDataSaveDir()))
	c.Check(os.IsNotExist(err), Equals, true)

	// verify that the root (snap) folder has not been touched
	exists, isDir := mylog.Check3(osutil.DirExists(dirs.SnapDataSaveDir))
	c.Check(err, IsNil)
	c.Check(exists && isDir, Equals, true)
}

func (s *copydataSuite) TestSetupCommonSaveDataSameRevision(c *C) {
	restore := osutil.MockMountInfo(fmt.Sprintf(mountRunMntUbuntuSaveFmt, dirs.GlobalRootDir) + "\n" +
		fmt.Sprintf(mountSnapSaveFmt, dirs.GlobalRootDir))
	defer restore()

	v1 := snaptest.MockSnap(c, helloYaml1, &snap.SideInfo{Revision: snap.R(10)})

	c.Assert(os.MkdirAll(v1.CommonDataSaveDir(), 0755), IsNil)
	c.Assert(os.WriteFile(filepath.Join(v1.CommonDataSaveDir(), "canary.txt"), nil, 0644), IsNil)
	c.Assert(osutil.FileExists(filepath.Join(v1.CommonDataSaveDir(), "canary.txt")), Equals, true)
	mylog.

		// setup snap save data works
		Check(s.be.SetupSnapSaveData(v1, mockDev, progress.Null))


	// assert data still is there
	c.Assert(osutil.FileExists(filepath.Join(v1.CommonDataSaveDir(), "canary.txt")), Equals, true)
}

func (s *copydataSuite) TestUndoSetupCommonSaveDataClassic(c *C) {
	v1 := snaptest.MockSnap(c, helloYaml1, &snap.SideInfo{Revision: snap.R(10)})

	c.Assert(os.MkdirAll(v1.CommonDataSaveDir(), 0755), IsNil)
	c.Assert(os.WriteFile(filepath.Join(v1.CommonDataSaveDir(), "canary.txt"), nil, 0644), IsNil)
	c.Assert(osutil.FileExists(filepath.Join(v1.CommonDataSaveDir(), "canary.txt")), Equals, true)
	mylog.

		// make sure that undo doesn't do anything on a classic system
		Check(s.be.UndoSetupSnapSaveData(v1, v1, mockClassicDev, progress.Null))


	c.Assert(osutil.FileExists(filepath.Join(v1.CommonDataSaveDir(), "canary.txt")), Equals, true)
}

func (s *copydataSuite) TestUndoSetupCommonSaveDataSameRevision(c *C) {
	v1 := snaptest.MockSnap(c, helloYaml1, &snap.SideInfo{Revision: snap.R(10)})

	c.Assert(os.MkdirAll(v1.CommonDataSaveDir(), 0755), IsNil)
	c.Assert(os.WriteFile(filepath.Join(v1.CommonDataSaveDir(), "canary.txt"), nil, 0644), IsNil)
	c.Assert(osutil.FileExists(filepath.Join(v1.CommonDataSaveDir(), "canary.txt")), Equals, true)
	mylog.

		// make sure that undo doesn't do anything with a previous version present
		Check(s.be.UndoSetupSnapSaveData(v1, v1, mockDev, progress.Null))


	c.Assert(osutil.FileExists(filepath.Join(v1.CommonDataSaveDir(), "canary.txt")), Equals, true)
}

func (s *copydataSuite) TestHideSnapData(c *C) {
	info := snaptest.MockSnap(c, helloYaml1, &snap.SideInfo{Revision: snap.R(10)})

	// mock user home
	homedir := filepath.Join(dirs.GlobalRootDir, "home", "user")
	usr := mylog.Check2(user.Current())

	usr.HomeDir = homedir

	restore := backend.MockAllUsers(func(*dirs.SnapDirOptions) ([]*user.User, error) {
		return []*user.User{usr}, nil
	})
	defer restore()

	// writes a file canary.home file to the rev dir of the "hello" snap
	s.populateHomeData(c, "user", snap.R(10))
	mylog.

		// write file in common
		Check(os.MkdirAll(info.UserCommonDataDir(homedir, nil), 0770))


	commonFilePath := filepath.Join(info.UserCommonDataDir(homedir, nil), "file.txt")
	mylog.Check(os.WriteFile(commonFilePath, []byte("some content"), 0640))


	// make 'current' symlink
	revDir := snap.UserDataDir(homedir, "hello", snap.R(10), nil)
	mylog.
		// path must be relative, otherwise move would make it dangling
		Check(os.Symlink(filepath.Base(revDir), filepath.Join(revDir, "..", "current")))

	mylog.Check(s.be.HideSnapData("hello"))


	// check versioned file was moved
	opts := &dirs.SnapDirOptions{HiddenSnapDataDir: true}
	revFile := filepath.Join(info.UserDataDir(homedir, opts), "canary.home")
	data := mylog.Check2(os.ReadFile(revFile))

	c.Assert(data, DeepEquals, []byte("10\n"))

	// check common file was moved
	commonFile := filepath.Join(info.UserCommonDataDir(homedir, opts), "file.txt")
	data = mylog.Check2(os.ReadFile(commonFile))

	c.Assert(data, DeepEquals, []byte("some content"))

	// check 'current' symlink has correct attributes and target
	link := filepath.Join(homedir, dirs.HiddenSnapDataHomeDir, "hello", "current")
	linkInfo := mylog.Check2(os.Lstat(link))

	c.Assert(linkInfo.Mode()&os.ModeSymlink, Equals, os.ModeSymlink)

	target := mylog.Check2(os.Readlink(link))

	c.Assert(target, Equals, "10")

	// check old '~/snap' folder was removed
	_ = mylog.Check2(os.Stat(snap.SnapDir(homedir, nil)))
	c.Assert(errors.Is(err, os.ErrNotExist), Equals, true)
}

func (s *copydataSuite) TestHideSnapDataSkipNoData(c *C) {
	info := snaptest.MockSnap(c, helloYaml1, &snap.SideInfo{Revision: snap.R(10)})

	// mock user home
	homedir := filepath.Join(dirs.GlobalRootDir, "home", "user")
	usr := mylog.Check2(user.Current())

	usr.HomeDir = homedir

	// create user without snap dir (to be skipped)
	usrNoSnapDir := &user.User{
		HomeDir: filepath.Join(dirs.GlobalRootDir, "home", "other-user"),
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
	mylog.Check(os.Symlink(revDir, linkPath))

	mylog.

		// empty user dir is skipped
		Check(s.be.HideSnapData("hello"))


	// only the user with snap data was migrated
	newSnapDir := filepath.Join(homedir, dirs.HiddenSnapDataHomeDir)
	matches := mylog.Check2(filepath.Glob(dirs.DataHomeGlobs(&dirs.SnapDirOptions{HiddenSnapDataDir: true})[0]))

	c.Assert(matches, HasLen, 1)
	c.Assert(matches[0], Equals, newSnapDir)
}

func (s *copydataSuite) TestHideSnapDataOverwrite(c *C) {
	info := snaptest.MockSnap(c, helloYaml1, &snap.SideInfo{Revision: snap.R(10)})

	// mock user home
	homedir := filepath.Join(dirs.GlobalRootDir, "home", "user")
	usr := mylog.Check2(user.Current())

	usr.HomeDir = homedir

	restore := backend.MockAllUsers(func(*dirs.SnapDirOptions) ([]*user.User, error) {
		return []*user.User{usr}, nil
	})
	defer restore()

	// writes a file canary.home file to the rev dir of the "hello" snap
	s.populateHomeData(c, "user", snap.R(10))

	// write data to be overwritten
	opts := &dirs.SnapDirOptions{HiddenSnapDataDir: true}
	snapDir := info.UserDataDir(homedir, opts)
	c.Assert(os.MkdirAll(snapDir, 0700), IsNil)

	revFile := filepath.Join(snapDir, "canary.home")
	c.Assert(os.WriteFile(revFile, []byte("stuff"), 0600), IsNil)

	otherFile := filepath.Join(snapDir, "file")
	c.Assert(os.WriteFile(otherFile, []byte("stuff"), 0600), IsNil)

	c.Assert(s.be.HideSnapData("hello"), IsNil)

	// check versioned file was moved and previous contents were overwritten
	data := mylog.Check2(os.ReadFile(revFile))

	c.Assert(data, DeepEquals, []byte("10\n"))

	_ = mylog.Check2(os.Stat(otherFile))
	c.Assert(errors.Is(err, os.ErrNotExist), Equals, true)

	// check old '~/snap' folder was removed
	_ = mylog.Check2(os.Stat(snap.SnapDir(homedir, nil)))
	c.Assert(errors.Is(err, os.ErrNotExist), Equals, true)
}

func (s *copydataSuite) TestUndoHideSnapData(c *C) {
	info := snaptest.MockSnap(c, helloYaml1, &snap.SideInfo{Revision: snap.R(10)})

	// mock user home dir
	homedir := filepath.Join(dirs.GlobalRootDir, "home", "user")
	usr := mylog.Check2(user.Current())

	usr.HomeDir = homedir

	restore := backend.MockAllUsers(func(_ *dirs.SnapDirOptions) ([]*user.User, error) {
		return []*user.User{usr}, nil
	})
	defer restore()

	// write file in revisioned dir
	opts := &dirs.SnapDirOptions{HiddenSnapDataDir: true}
	mylog.Check(os.MkdirAll(info.UserDataDir(homedir, opts), 0770))


	hiddenRevFile := filepath.Join(info.UserDataDir(homedir, opts), "file.txt")
	mylog.Check(os.WriteFile(hiddenRevFile, []byte("some content"), 0640))

	mylog.

		// write file in common
		Check(os.MkdirAll(info.UserCommonDataDir(homedir, opts), 0770))


	hiddenCommonFile := filepath.Join(info.UserCommonDataDir(homedir, opts), "file.txt")
	mylog.Check(os.WriteFile(hiddenCommonFile, []byte("other content"), 0640))


	// make 'current' symlink
	revDir := info.UserDataDir(homedir, opts)
	mylog.
		// path must be relative otherwise the move would make it dangling
		Check(os.Symlink(filepath.Base(revDir), filepath.Join(revDir, "..", "current")))

	mylog.

		// undo migration
		Check(s.be.UndoHideSnapData("hello"))


	// check versioned file was restored
	revFile := filepath.Join(info.UserDataDir(homedir, nil), "file.txt")
	data := mylog.Check2(os.ReadFile(revFile))

	c.Assert(data, DeepEquals, []byte("some content"))

	// check common file was restored
	commonFile := filepath.Join(info.UserCommonDataDir(homedir, nil), "file.txt")
	data = mylog.Check2(os.ReadFile(commonFile))

	c.Assert(data, DeepEquals, []byte("other content"))

	// check symlink points to revisioned dir
	exposedDir := filepath.Join(homedir, dirs.UserHomeSnapDir)
	target := mylog.Check2(os.Readlink(filepath.Join(exposedDir, "hello", "current")))

	c.Assert(target, Equals, "10")

	// ~/.snap/data was removed
	hiddenDir := snap.SnapDir(homedir, opts)
	_ = mylog.Check2(os.Stat(hiddenDir))
	c.Assert(errors.Is(err, os.ErrNotExist), Equals, true)

	// ~/.snap was removed
	_ = mylog.Check2(os.Stat(filepath.Base(hiddenDir)))
	c.Assert(errors.Is(err, os.ErrNotExist), Equals, true)
}

func (s *copydataSuite) TestUndoHideDoesntRemoveIfDirHasFiles(c *C) {
	info := snaptest.MockSnap(c, helloYaml1, &snap.SideInfo{Revision: snap.R(10)})

	homedir := filepath.Join(dirs.GlobalRootDir, "home", "user")
	usr := mylog.Check2(user.Current())

	usr.HomeDir = homedir

	restore := backend.MockAllUsers(func(_ *dirs.SnapDirOptions) ([]*user.User, error) {
		return []*user.User{usr}, nil
	})
	defer restore()

	// set up state and create another dir 'bye' under ~/.snap/data/
	opts := &dirs.SnapDirOptions{HiddenSnapDataDir: true}
	c.Assert(os.MkdirAll(info.UserDataDir(homedir, opts), 0700), IsNil)
	c.Assert(os.MkdirAll(info.UserCommonDataDir(homedir, opts), 0700), IsNil)
	byeDirPath := filepath.Join(homedir, dirs.HiddenSnapDataHomeDir, "bye")
	c.Assert(os.MkdirAll(byeDirPath, 0700), IsNil)

	c.Assert(s.be.UndoHideSnapData("hello"), IsNil)

	exists, _ := mylog.Check3(osutil.DirExists(filepath.Join(homedir, dirs.HiddenSnapDataHomeDir, "hello")))

	c.Check(exists, Equals, false)

	// ~/.snap/data isn't deleted bc there's another dir 'bye'
	exists, _ = mylog.Check3(osutil.DirExists(filepath.Join(homedir, dirs.HiddenSnapDataHomeDir)))

	c.Check(exists, Equals, true)

	// reset state and create a file under ~/.snap
	c.Assert(os.MkdirAll(info.UserDataDir(homedir, opts), 0700), IsNil)
	c.Assert(os.MkdirAll(info.UserCommonDataDir(homedir, opts), 0700), IsNil)
	c.Assert(os.RemoveAll(byeDirPath), IsNil)
	c.Assert(os.RemoveAll(snap.UserSnapDir(homedir, "hello", nil)), IsNil)

	_ = mylog.Check2(os.Create(filepath.Join(homedir, ".snap", "other-file")))


	c.Assert(s.be.UndoHideSnapData("hello"), IsNil)

	// ~/.snap/data is deleted bc there's no dir other than 'hello'
	exists, _ = mylog.Check3(osutil.DirExists(filepath.Join(homedir, dirs.HiddenSnapDataHomeDir)))

	c.Check(exists, Equals, false)

	// ~/.snap/data isn't deleted bc there's a file 'other-file'
	exists, _ = mylog.Check3(osutil.DirExists(filepath.Join(homedir, ".snap")))

	c.Check(exists, Equals, true)
}

func (s *copydataSuite) TestCleanupAfterCopyAndMigration(c *C) {
	dirs.SetSnapHomeDirs("/home")
	homedir := filepath.Join(dirs.GlobalRootDir, "home", "user")
	usr := mylog.Check2(user.Current())

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
	exists, _ := mylog.Check3(osutil.DirExists(exposedTrash))

	c.Assert(exists, Equals, false)

	exists, _ = mylog.Check3(osutil.DirExists(hiddenTrash))

	c.Assert(exists, Equals, false)
}

func (s *copydataSuite) TestRemoveIfEmpty(c *C) {
	file := filepath.Join(dirs.GlobalRootDir, "random")
	c.Assert(os.WriteFile(file, []byte("stuff"), 0664), IsNil)

	// dir contains a file, shouldn't do anything
	c.Assert(backend.RemoveIfEmpty(dirs.GlobalRootDir), IsNil)
	files := mylog.Check2(os.ReadDir(dirs.GlobalRootDir))

	c.Check(files, HasLen, 1)
	c.Check(filepath.Join(dirs.GlobalRootDir, files[0].Name()), testutil.FileEquals, "stuff")

	c.Assert(os.Remove(file), IsNil)

	// dir is empty, should be removed
	c.Assert(backend.RemoveIfEmpty(dirs.GlobalRootDir), IsNil)
	c.Assert(osutil.FileExists(file), Equals, false)
}

func (s *copydataSuite) TestUndoHideKeepGoingPreserveFirstErr(c *C) {
	firstTime := true
	restore := backend.MockRemoveIfEmpty(func(dir string) error {
		if firstTime {
			mylog.Check(errors.New("first error"))
			firstTime = false
		} else {
			mylog.Check(errors.New("other error"))
		}

		return err
	})
	defer restore()

	info := snaptest.MockSnap(c, helloYaml1, &snap.SideInfo{Revision: snap.R(10)})

	// mock two users so that the undo is done twice
	var usrs []*user.User
	for _, usrName := range []string{"usr1", "usr2"} {
		homedir := filepath.Join(dirs.GlobalRootDir, "home", usrName)
		usr := mylog.Check2(user.Current())

		usr.HomeDir = homedir

		opts := &dirs.SnapDirOptions{HiddenSnapDataDir: true}
		mylog.Check(os.MkdirAll(info.UserDataDir(homedir, opts), 0770))


		usrs = append(usrs, usr)
	}

	restUsers := backend.MockAllUsers(func(_ *dirs.SnapDirOptions) ([]*user.User, error) {
		return usrs, nil
	})
	defer restUsers()

	buf, restLogger := logger.MockLogger()
	defer restLogger()
	mylog.Check(s.be.UndoHideSnapData("hello"))
	// the first error is returned
	c.Assert(err, ErrorMatches, `cannot remove dir ".*": first error`)
	// the undo keeps going and logs the next error
	c.Assert(buf, Matches, `.*cannot remove dir ".*": other error\n`)
}

func (s *copydataSuite) TestInitSnapUserHome(c *C) {
	homeDir := filepath.Join(dirs.GlobalRootDir, "user")
	usr := mylog.Check2(user.Current())

	usr.HomeDir = homeDir

	restore := backend.MockAllUsers(func(_ *dirs.SnapDirOptions) ([]*user.User, error) {
		return []*user.User{usr}, nil
	})
	defer restore()

	snapName := "some-snap"
	rev := mylog.Check2(snap.ParseRevision("2"))


	opts := &dirs.SnapDirOptions{HiddenSnapDataDir: true}
	revDir := snap.UserDataDir(usr.HomeDir, snapName, rev, opts)
	c.Assert(os.MkdirAll(revDir, 0700), IsNil)

	filePath := filepath.Join(revDir, "file")
	c.Assert(os.WriteFile(filePath, []byte("stuff"), 0664), IsNil)
	dirPath := filepath.Join(revDir, "dir")
	c.Assert(os.Mkdir(dirPath, 0775), IsNil)

	undoInfo := mylog.Check2(s.be.InitExposedSnapHome(snapName, rev, opts))

	exposedHome := filepath.Join(homeDir, dirs.ExposedSnapHomeDir, snapName)
	c.Check(undoInfo.Created, DeepEquals, []string{exposedHome})

	expectedFile := filepath.Join(exposedHome, "file")
	data := mylog.Check2(os.ReadFile(expectedFile))

	c.Check(string(data), Equals, "stuff")

	exists, isReg := mylog.Check3(osutil.RegularFileExists(filePath))

	c.Check(exists, Equals, true)
	c.Check(isReg, Equals, true)

	expectedDir := filepath.Join(exposedHome, "dir")
	exists, isDir := mylog.Check3(osutil.DirExists(expectedDir))

	c.Check(exists, Equals, true)
	c.Check(isDir, Equals, true)

	exists, isDir = mylog.Check3(osutil.DirExists(dirPath))

	c.Check(exists, Equals, true)
	c.Check(isDir, Equals, true)
}

func (s *copydataSuite) TestInitExposedHomeIgnoreXDGDirs(c *C) {
	homeDir := filepath.Join(dirs.GlobalRootDir, "user")
	usr := mylog.Check2(user.Current())

	usr.HomeDir = homeDir

	restore := backend.MockAllUsers(func(_ *dirs.SnapDirOptions) ([]*user.User, error) {
		return []*user.User{usr}, nil
	})
	defer restore()

	snapName := "some-snap"
	rev := mylog.Check2(snap.ParseRevision("2"))


	opts := &dirs.SnapDirOptions{HiddenSnapDataDir: true}
	revDir := snap.UserDataDir(homeDir, snapName, rev, opts)

	cachePath := filepath.Join(revDir, ".cache")
	c.Assert(os.MkdirAll(cachePath, 0700), IsNil)
	configPath := filepath.Join(revDir, ".config")
	c.Assert(os.MkdirAll(configPath, 0700), IsNil)
	localPath := filepath.Join(revDir, ".local", "share")
	c.Assert(os.MkdirAll(localPath, 0700), IsNil)

	undoInfo := mylog.Check2(s.be.InitExposedSnapHome(snapName, rev, opts))

	exposedHome := snap.UserExposedHomeDir(homeDir, snapName)
	c.Check(undoInfo.Created, DeepEquals, []string{exposedHome})

	cachePath = filepath.Join(exposedHome, ".cache")
	exists, _ := mylog.Check3(osutil.DirExists(cachePath))
	c.Check(err, IsNil)
	c.Check(exists, Equals, false)

	configPath = filepath.Join(exposedHome, ".config")
	exists, _ = mylog.Check3(osutil.DirExists(configPath))
	c.Check(err, IsNil)
	c.Check(exists, Equals, false)

	localPath = filepath.Join(exposedHome, ".local")
	exists, _ = mylog.Check3(osutil.DirExists(localPath))
	c.Check(err, IsNil)
	c.Check(exists, Equals, true)

	sharePath := filepath.Join(exposedHome, ".local", "share")
	exists, _ = mylog.Check3(osutil.DirExists(sharePath))
	c.Check(err, IsNil)
	c.Check(exists, Equals, false)
}

func (s *copydataSuite) TestInitSnapFailOnFirstErr(c *C) {
	usr1 := mylog.Check2(user.Current())

	usr1.HomeDir = filepath.Join(dirs.GlobalRootDir, "user1")

	usr2 := mylog.Check2(user.Current())

	usr2.HomeDir = filepath.Join(dirs.GlobalRootDir, "user2")

	restore := backend.MockAllUsers(func(_ *dirs.SnapDirOptions) ([]*user.User, error) {
		return []*user.User{usr1, usr2}, nil
	})
	defer restore()

	restore = backend.MockMkdirAllChown(func(string, os.FileMode, sys.UserID, sys.GroupID) error {
		return errors.New("boom")
	})
	defer restore()

	snapName := "some-snap"
	rev := mylog.Check2(snap.ParseRevision("2"))


	undoInfo := mylog.Check2(s.be.InitExposedSnapHome(snapName, rev, nil))
	c.Assert(err, ErrorMatches, ".*: boom")
	c.Check(undoInfo, IsNil)

	exists, _ := mylog.Check3(osutil.DirExists(filepath.Join(usr1.HomeDir, dirs.ExposedSnapHomeDir)))

	c.Check(exists, Equals, false)

	exists, _ = mylog.Check3(osutil.DirExists(filepath.Join(usr2.HomeDir, dirs.ExposedSnapHomeDir)))

	c.Check(exists, Equals, false)
}

func (s *copydataSuite) TestInitSnapUndoOnErr(c *C) {
	usr1 := mylog.Check2(user.Current())

	usr1.HomeDir = filepath.Join(dirs.GlobalRootDir, "user1")

	usr2 := mylog.Check2(user.Current())

	usr2.HomeDir = filepath.Join(dirs.GlobalRootDir, "user2")

	restore := backend.MockAllUsers(func(_ *dirs.SnapDirOptions) ([]*user.User, error) {
		return []*user.User{usr1, usr2}, nil
	})
	defer restore()

	first := true
	restore = backend.MockMkdirAllChown(func(string, os.FileMode, sys.UserID, sys.GroupID) error {
		if first {
			first = false
			return nil
		}

		return errors.New("boom")
	})
	defer restore()

	var calledRemove bool
	restore = backend.MockRemoveIfEmpty(func(dir string) error {
		calledRemove = true
		return backend.RemoveIfEmpty(dir)
	})
	defer restore()

	snapName := "some-snap"
	rev := mylog.Check2(snap.ParseRevision("2"))


	undoInfo := mylog.Check2(s.be.InitExposedSnapHome(snapName, rev, nil))
	c.Assert(err, ErrorMatches, ".*: boom")
	c.Check(undoInfo, IsNil)

	exists, _ := mylog.Check3(osutil.DirExists(filepath.Join(usr1.HomeDir, dirs.ExposedSnapHomeDir)))

	c.Check(exists, Equals, false)

	exists, _ = mylog.Check3(osutil.DirExists(filepath.Join(usr2.HomeDir, dirs.ExposedSnapHomeDir)))

	c.Check(exists, Equals, false)

	c.Check(calledRemove, Equals, true)
}

func (s *copydataSuite) TestInitSnapNothingToCopy(c *C) {
	usr := mylog.Check2(user.Current())

	usr.HomeDir = filepath.Join(dirs.GlobalRootDir, "user")

	restore := backend.MockAllUsers(func(_ *dirs.SnapDirOptions) ([]*user.User, error) {
		return []*user.User{usr}, nil
	})
	defer restore()

	snapName := "some-snap"
	rev := mylog.Check2(snap.ParseRevision("2"))


	undoInfo := mylog.Check2(s.be.InitExposedSnapHome(snapName, rev, nil))

	c.Check(undoInfo.Created, DeepEquals, []string{snap.UserExposedHomeDir(usr.HomeDir, snapName)})

	newHomeDir := filepath.Join(usr.HomeDir, dirs.ExposedSnapHomeDir, snapName)
	exists, _ := mylog.Check3(osutil.DirExists(newHomeDir))

	c.Check(exists, Equals, true)

	entries := mylog.Check2(os.ReadDir(newHomeDir))

	c.Check(entries, HasLen, 0)
}

func (s *copydataSuite) TestInitAlreadyExistsFile(c *C) {
	usr := mylog.Check2(user.Current())

	usr.HomeDir = filepath.Join(dirs.GlobalRootDir, "user")

	restore := backend.MockAllUsers(func(_ *dirs.SnapDirOptions) ([]*user.User, error) {
		return []*user.User{usr}, nil
	})
	defer restore()

	snapName := "some-snap"

	// ~/Snap/some-snap already exists but is file
	newHome := snap.UserExposedHomeDir(usr.HomeDir, snapName)
	parent := filepath.Dir(newHome)
	c.Assert(os.MkdirAll(parent, 0700), IsNil)
	c.Assert(os.WriteFile(newHome, nil, 0600), IsNil)

	rev := mylog.Check2(snap.ParseRevision("2"))


	undoInfo := mylog.Check2(s.be.InitExposedSnapHome(snapName, rev, nil))
	c.Assert(err, ErrorMatches, fmt.Sprintf("cannot initialize new user HOME %q: already exists but is not a directory", newHome))
	c.Check(undoInfo, IsNil)

	exists, isReg := mylog.Check3(osutil.RegularFileExists(newHome))

	c.Check(exists, Equals, true)
	c.Check(isReg, Equals, true)
}

func (s *copydataSuite) TestInitAlreadyExistsDir(c *C) {
	usr := mylog.Check2(user.Current())

	usr.HomeDir = filepath.Join(dirs.GlobalRootDir, "user")

	restore := backend.MockAllUsers(func(_ *dirs.SnapDirOptions) ([]*user.User, error) {
		return []*user.User{usr}, nil
	})
	defer restore()

	snapName := "some-snap"

	// ~/Snap/some-snap already exists but is file
	newHome := snap.UserExposedHomeDir(usr.HomeDir, snapName)
	c.Assert(os.MkdirAll(newHome, 0700), IsNil)
	c.Assert(os.WriteFile(filepath.Join(newHome, "file"), nil, 0600), IsNil)

	rev := mylog.Check2(snap.ParseRevision("2"))


	undoInfo := mylog.Check2(s.be.InitExposedSnapHome(snapName, rev, nil))

	c.Check(undoInfo.Created, HasLen, 0)

	exists, isDir := mylog.Check3(osutil.DirExists(newHome))

	c.Check(exists, Equals, true)
	c.Check(isDir, Equals, true)

	files := mylog.Check2(os.ReadDir(newHome))

	c.Check(files, HasLen, 1)
	c.Check(files[0].Name(), Equals, "file")
}

func (s *copydataSuite) TestRemoveExposedHome(c *C) {
	usr := mylog.Check2(user.Current())

	usr.HomeDir = filepath.Join(dirs.GlobalRootDir, "user")

	restore := backend.MockAllUsers(func(_ *dirs.SnapDirOptions) ([]*user.User, error) {
		return []*user.User{usr}, nil
	})
	defer restore()

	snapName := "some-snap"
	exposedDir := filepath.Join(usr.HomeDir, dirs.ExposedSnapHomeDir, snapName)
	c.Assert(os.MkdirAll(exposedDir, 0700), IsNil)
	c.Assert(os.WriteFile(filepath.Join(exposedDir, "file"), []byte("foo"), 0600), IsNil)
	var undoInfo backend.UndoInfo
	undoInfo.Created = append(undoInfo.Created, exposedDir)

	c.Assert(s.be.UndoInitExposedSnapHome(snapName, &undoInfo), IsNil)

	exists, _ := mylog.Check3(osutil.DirExists(exposedDir))

	c.Assert(exists, Equals, false)

	baseExposedDir := filepath.Base(exposedDir)
	exists, _ = mylog.Check3(osutil.DirExists(baseExposedDir))

	c.Assert(exists, Equals, false)
}

func (s *copydataSuite) TestRemoveExposedKeepGoingOnFail(c *C) {
	firstTime := true
	restore := backend.MockRemoveIfEmpty(func(dir string) error {
		if firstTime {
			mylog.Check(errors.New("first error"))
			firstTime = false
		} else {
			mylog.Check(errors.New("other error"))
		}

		return err
	})
	defer restore()

	snapName := "some-snap"
	var undoInfo backend.UndoInfo
	var usrs []*user.User
	for _, usrName := range []string{"usr1", "usr2"} {
		homedir := filepath.Join(dirs.GlobalRootDir, usrName)
		usr := mylog.Check2(user.Current())

		usr.HomeDir = homedir

		exposedDir := filepath.Join(homedir, dirs.ExposedSnapHomeDir, snapName)
		c.Assert(os.MkdirAll(exposedDir, 0700), IsNil)
		c.Assert(os.WriteFile(filepath.Join(exposedDir, "file"), []byte("foo"), 0700), IsNil)
		usrs = append(usrs, usr)
		undoInfo.Created = append(undoInfo.Created, exposedDir)
	}

	restUsers := backend.MockAllUsers(func(_ *dirs.SnapDirOptions) ([]*user.User, error) {
		return usrs, nil
	})
	defer restUsers()

	buf, restLogger := logger.MockLogger()
	defer restLogger()
	mylog.Check(s.be.UndoInitExposedSnapHome(snapName, &undoInfo))
	// the first error is returned
	c.Assert(err, ErrorMatches, fmt.Sprintf(`cannot remove %q: first error`, filepath.Join(dirs.GlobalRootDir, "usr1", "Snap")))
	// second error is logged
	c.Assert(buf, Matches, fmt.Sprintf(`.*cannot remove %q: other error\n`, filepath.Join(dirs.GlobalRootDir, "usr2", "Snap")))
}

func (s *copydataSuite) TestInitXDGDirsAlreadyExist(c *C) {
	homeDir := filepath.Join(dirs.GlobalRootDir, "user")
	usr := mylog.Check2(user.Current())

	usr.HomeDir = homeDir

	restore := backend.MockAllUsers(func(_ *dirs.SnapDirOptions) ([]*user.User, error) {
		return []*user.User{usr}, nil
	})
	defer restore()

	snapName := "some-snap"
	rev := mylog.Check2(snap.ParseRevision("2"))


	opts := &dirs.SnapDirOptions{HiddenSnapDataDir: true}
	revDir := snap.UserDataDir(homeDir, snapName, rev, opts)

	var srcDirs []string
	mkDir := func(dirs ...string) {
		srcDir := filepath.Join(append([]string{revDir}, dirs...)...)
		srcDirs = append(srcDirs, srcDir)
		c.Assert(os.MkdirAll(srcDir, 0700), IsNil)
	}

	for _, d := range [][]string{{".config"}, {".cache"}, {".local", "share"}} {
		mkDir(d...)
	}

	info := &snap.Info{SideInfo: snap.SideInfo{Revision: rev, RealName: snapName}}
	c.Assert(s.be.InitXDGDirs(info), IsNil)

	for _, d := range []string{"xdg-config", "xdg-cache", "xdg-data"} {
		dir := filepath.Join(revDir, d)

		exists, isDir := mylog.Check3(osutil.DirExists(dir))
		c.Check(err, IsNil)
		c.Check(exists, Equals, true)
		c.Check(isDir, Equals, true)
	}
}

func (s *copydataSuite) TestInitXDGDirsCreateNew(c *C) {
	homeDir := filepath.Join(dirs.GlobalRootDir, "user")
	usr := mylog.Check2(user.Current())

	usr.HomeDir = homeDir

	restore := backend.MockAllUsers(func(_ *dirs.SnapDirOptions) ([]*user.User, error) {
		return []*user.User{usr}, nil
	})
	defer restore()

	snapName := "some-snap"
	rev := mylog.Check2(snap.ParseRevision("2"))


	opts := &dirs.SnapDirOptions{HiddenSnapDataDir: true}
	revDir := snap.UserDataDir(homeDir, snapName, rev, opts)

	info := &snap.Info{SideInfo: snap.SideInfo{Revision: rev, RealName: snapName}}
	c.Assert(s.be.InitXDGDirs(info), IsNil)

	for _, d := range []string{"xdg-config", "xdg-cache", "xdg-data"} {
		dir := filepath.Join(revDir, d)

		exists, isDir := mylog.Check3(osutil.DirExists(dir))
		c.Check(err, IsNil)
		c.Check(exists, Equals, true)
		c.Check(isDir, Equals, true)
	}
}

func (s *copydataSuite) TestInitXDGDirsFailAlreadyExists(c *C) {
	homeDir := filepath.Join(dirs.GlobalRootDir, "user")
	usr := mylog.Check2(user.Current())

	usr.HomeDir = homeDir

	restore := backend.MockAllUsers(func(_ *dirs.SnapDirOptions) ([]*user.User, error) {
		return []*user.User{usr}, nil
	})
	defer restore()

	snapName := "some-snap"
	rev := mylog.Check2(snap.ParseRevision("2"))


	opts := &dirs.SnapDirOptions{HiddenSnapDataDir: true}
	revDir := snap.UserDataDir(homeDir, snapName, rev, opts)
	dst := filepath.Join(revDir, "xdg-config")
	src := filepath.Join(revDir, ".config")
	c.Assert(os.MkdirAll(dst, 0700), IsNil)
	c.Assert(os.MkdirAll(src, 0700), IsNil)

	info := &snap.Info{SideInfo: snap.SideInfo{Revision: rev, RealName: snapName}}
	mylog.Check(s.be.InitXDGDirs(info))
	c.Assert(err.Error(), Equals, fmt.Sprintf("cannot migrate XDG dir %q to %q because destination already exists", src, dst))
}
