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

package osutil_test

import (
	"errors"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"time"

	. "gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/osutil/sys"
	"github.com/snapcore/snapd/randutil"
	"github.com/snapcore/snapd/testutil"
)

type AtomicWriteTestSuite struct{}

var _ = Suite(&AtomicWriteTestSuite{})

func (ts *AtomicWriteTestSuite) TestAtomicWriteFile(c *C) {
	tmpdir := c.MkDir()

	p := filepath.Join(tmpdir, "foo")
	mylog.Check(osutil.AtomicWriteFile(p, []byte("canary"), 0644, 0))


	c.Check(p, testutil.FileEquals, "canary")

	// no files left behind!
	d := mylog.Check2(os.ReadDir(tmpdir))

	c.Assert(len(d), Equals, 1)
}

func (ts *AtomicWriteTestSuite) TestAtomicWriteFilePermissions(c *C) {
	tmpdir := c.MkDir()

	p := filepath.Join(tmpdir, "foo")
	mylog.Check(osutil.AtomicWriteFile(p, []byte(""), 0600, 0))


	st := mylog.Check2(os.Stat(p))

	c.Assert(st.Mode()&os.ModePerm, Equals, os.FileMode(0600))
}

func (ts *AtomicWriteTestSuite) TestAtomicWriteFileOverwrite(c *C) {
	tmpdir := c.MkDir()
	p := filepath.Join(tmpdir, "foo")
	c.Assert(os.WriteFile(p, []byte("hello"), 0644), IsNil)
	c.Assert(osutil.AtomicWriteFile(p, []byte("hi"), 0600, 0), IsNil)

	c.Assert(p, testutil.FileEquals, "hi")
}

func (ts *AtomicWriteTestSuite) TestAtomicWriteFileSymlinkNoFollow(c *C) {
	tmpdir := c.MkDir()
	rodir := filepath.Join(tmpdir, "ro")
	p := filepath.Join(rodir, "foo")
	s := filepath.Join(tmpdir, "foo")
	c.Assert(os.MkdirAll(rodir, 0755), IsNil)
	c.Assert(os.Symlink(s, p), IsNil)
	c.Assert(os.Chmod(rodir, 0500), IsNil)
	defer os.Chmod(rodir, 0700)
	mylog.Check(osutil.AtomicWriteFile(p, []byte("hi"), 0600, 0))
	c.Assert(err, NotNil)
}

func (ts *AtomicWriteTestSuite) TestAtomicWriteFileAbsoluteSymlinks(c *C) {
	tmpdir := c.MkDir()
	rodir := filepath.Join(tmpdir, "ro")
	p := filepath.Join(rodir, "foo")
	s := filepath.Join(tmpdir, "foo")
	c.Assert(os.MkdirAll(rodir, 0755), IsNil)
	c.Assert(os.Symlink(s, p), IsNil)
	c.Assert(os.Chmod(rodir, 0500), IsNil)
	defer os.Chmod(rodir, 0700)
	mylog.Check(osutil.AtomicWriteFile(p, []byte("hi"), 0600, osutil.AtomicWriteFollow))


	c.Assert(p, testutil.FileEquals, "hi")
}

func (ts *AtomicWriteTestSuite) TestAtomicWriteFileOverwriteAbsoluteSymlink(c *C) {
	tmpdir := c.MkDir()
	rodir := filepath.Join(tmpdir, "ro")
	p := filepath.Join(rodir, "foo")
	s := filepath.Join(tmpdir, "foo")
	c.Assert(os.MkdirAll(rodir, 0755), IsNil)
	c.Assert(os.Symlink(s, p), IsNil)
	c.Assert(os.Chmod(rodir, 0500), IsNil)
	defer os.Chmod(rodir, 0700)

	c.Assert(os.WriteFile(s, []byte("hello"), 0644), IsNil)
	c.Assert(osutil.AtomicWriteFile(p, []byte("hi"), 0600, osutil.AtomicWriteFollow), IsNil)

	c.Assert(p, testutil.FileEquals, "hi")
}

func (ts *AtomicWriteTestSuite) TestAtomicWriteFileRelativeSymlinks(c *C) {
	tmpdir := c.MkDir()
	rodir := filepath.Join(tmpdir, "ro")
	p := filepath.Join(rodir, "foo")
	c.Assert(os.MkdirAll(rodir, 0755), IsNil)
	c.Assert(os.Symlink("../foo", p), IsNil)
	c.Assert(os.Chmod(rodir, 0500), IsNil)
	defer os.Chmod(rodir, 0700)
	mylog.Check(osutil.AtomicWriteFile(p, []byte("hi"), 0600, osutil.AtomicWriteFollow))


	c.Assert(p, testutil.FileEquals, "hi")
}

func (ts *AtomicWriteTestSuite) TestAtomicWriteFileOverwriteRelativeSymlink(c *C) {
	tmpdir := c.MkDir()
	rodir := filepath.Join(tmpdir, "ro")
	p := filepath.Join(rodir, "foo")
	s := filepath.Join(tmpdir, "foo")
	c.Assert(os.MkdirAll(rodir, 0755), IsNil)
	c.Assert(os.Symlink("../foo", p), IsNil)
	c.Assert(os.Chmod(rodir, 0500), IsNil)
	defer os.Chmod(rodir, 0700)

	c.Assert(os.WriteFile(s, []byte("hello"), 0644), IsNil)
	c.Assert(osutil.AtomicWriteFile(p, []byte("hi"), 0600, osutil.AtomicWriteFollow), IsNil)

	c.Assert(p, testutil.FileEquals, "hi")
}

func (ts *AtomicWriteTestSuite) TestAtomicWriteFileNoOverwriteTmpExisting(c *C) {
	tmpdir := c.MkDir()
	// ensure we always get the same result
	rand.Seed(1)
	expectedRandomness := randutil.RandomString(12) + "~"
	// ensure we always get the same result
	rand.Seed(1)

	p := filepath.Join(tmpdir, "foo")
	mylog.Check(os.WriteFile(p+"."+expectedRandomness, []byte(""), 0644))

	mylog.Check(osutil.AtomicWriteFile(p, []byte(""), 0600, 0))
	c.Assert(err, ErrorMatches, "open .*: file exists")
}

func (ts *AtomicWriteTestSuite) TestAtomicFileChownError(c *C) {
	eUid := sys.UserID(42)
	eGid := sys.GroupID(74)
	eErr := errors.New("this didn't work")
	defer osutil.MockChown(func(fd *os.File, uid sys.UserID, gid sys.GroupID) error {
		c.Check(uid, Equals, eUid)
		c.Check(gid, Equals, eGid)
		return eErr
	})()

	d := c.MkDir()
	p := filepath.Join(d, "foo")

	aw := mylog.Check2(osutil.NewAtomicFile(p, 0644, 0, eUid, eGid))

	defer aw.Cancel()

	_ = mylog.Check2(aw.Write([]byte("hello")))


	c.Check(aw.Commit(), Equals, eErr)
}

func (ts *AtomicWriteTestSuite) TestAtomicFileCancelError(c *C) {
	d := c.MkDir()
	p := filepath.Join(d, "foo")
	aw := mylog.Check2(osutil.NewAtomicFile(p, 0644, 0, osutil.NoChown, osutil.NoChown))


	c.Assert(aw.File.Close(), IsNil)
	// Depending on golang version the error is one of the two.
	c.Check(aw.Cancel(), ErrorMatches, "invalid argument|file already closed")
}

func (ts *AtomicWriteTestSuite) TestAtomicFileCancelBadError(c *C) {
	d := c.MkDir()
	p := filepath.Join(d, "foo")
	aw := mylog.Check2(osutil.NewAtomicFile(p, 0644, 0, osutil.NoChown, osutil.NoChown))

	defer aw.Close()

	osutil.SetAtomicFileRenamed(aw, true)

	c.Check(aw.Cancel(), Equals, osutil.ErrCannotCancel)
}

func (ts *AtomicWriteTestSuite) TestAtomicFileCancelNoClose(c *C) {
	d := c.MkDir()
	p := filepath.Join(d, "foo")
	aw := mylog.Check2(osutil.NewAtomicFile(p, 0644, 0, osutil.NoChown, osutil.NoChown))

	c.Assert(aw.Close(), IsNil)

	c.Check(aw.Cancel(), IsNil)
}

func (ts *AtomicWriteTestSuite) TestAtomicFileCancel(c *C) {
	d := c.MkDir()
	p := filepath.Join(d, "foo")

	aw := mylog.Check2(osutil.NewAtomicFile(p, 0644, 0, osutil.NoChown, osutil.NoChown))

	fn := aw.File.Name()
	c.Check(osutil.FileExists(fn), Equals, true)
	c.Check(aw.Cancel(), IsNil)
	c.Check(osutil.FileExists(fn), Equals, false)
}

func (ts *AtomicWriteTestSuite) TestAtomicFileModTime(c *C) {
	d := c.MkDir()
	p := filepath.Join(d, "foo")

	aw := mylog.Check2(osutil.NewAtomicFile(p, 0644, 0, osutil.NoChown, osutil.NoChown))

	t := time.Date(2010, time.January, 1, 13, 0, 0, 0, time.UTC)
	aw.SetModTime(t)
	c.Assert(aw.Commit(), IsNil)

	finfo := mylog.Check2(os.Stat(p))

	c.Assert(finfo.ModTime().Equal(t), Equals, true)
}

func (ts *AtomicWriteTestSuite) TestAtomicFileCommitAs(c *C) {
	d := c.MkDir()
	initialTarget := filepath.Join(d, "foo")
	actualTarget := filepath.Join(d, "bar")

	aw := mylog.Check2(osutil.NewAtomicFile(initialTarget, 0644, 0, osutil.NoChown, osutil.NoChown))

	defer aw.Cancel()
	fn := aw.File.Name()
	c.Check(osutil.FileExists(fn), Equals, true)
	c.Check(strings.HasPrefix(fn, initialTarget), Equals, true, Commentf("unexpected temporary file name prefix: %q", fn))
	_ = mylog.Check2(aw.WriteString("this is test data"))

	mylog.Check(aw.CommitAs(actualTarget))

	c.Check(fn, testutil.FileAbsent)
	c.Check(actualTarget, testutil.FileEquals, "this is test data")
	c.Check(initialTarget, testutil.FileAbsent)

	// not confused when CommitAs uses the same name as initially
	sameNameTarget := filepath.Join(d, "baz")
	aw = mylog.Check2(osutil.NewAtomicFile(sameNameTarget, 0644, 0, osutil.NoChown, osutil.NoChown))

	defer aw.Cancel()
	_ = mylog.Check2(aw.WriteString("this is baz"))

	mylog.Check(aw.CommitAs(sameNameTarget))

	c.Check(sameNameTarget, testutil.FileEquals, "this is baz")

	// overwrites any existing file on CommitAs (same as Commit)
	overwrittenTarget := filepath.Join(d, "will-overwrite")
	mylog.Check(os.WriteFile(overwrittenTarget, []byte("overwritten"), 0644))

	aw = mylog.Check2(osutil.NewAtomicFile(filepath.Join(d, "temp-name"), 0644, 0, osutil.NoChown, osutil.NoChown))

	defer aw.Cancel()
	_ = mylog.Check2(aw.WriteString("this will overwrite existing file"))

	mylog.Check(aw.CommitAs(overwrittenTarget))

	c.Check(overwrittenTarget, testutil.FileEquals, "this will overwrite existing file")
}

func (ts *AtomicWriteTestSuite) TestAtomicFileCommitAsDifferentDirErr(c *C) {
	d := c.MkDir()
	initialTarget := filepath.Join(d, "foo")
	differentDirTarget := filepath.Join(c.MkDir(), "bar")

	aw := mylog.Check2(osutil.NewAtomicFile(initialTarget, 0644, 0, osutil.NoChown, osutil.NoChown))

	_ = mylog.Check2(aw.WriteString("this is test data"))

	mylog.Check(aw.CommitAs(differentDirTarget))
	c.Assert(err, ErrorMatches, `cannot commit as "bar" to a different directory .*`)
}

type AtomicSymlinkTestSuite struct{}

var _ = Suite(&AtomicSymlinkTestSuite{})

func (ts *AtomicSymlinkTestSuite) TestAtomicSymlink(c *C) {
	mustReadSymlink := func(p, exp string) {
		target := mylog.Check2(os.Readlink(p))

		c.Check(exp, Equals, target)
	}

	checkLeftoverFiles := func(sym string, exp []string) {
		res := mylog.Check2(filepath.Glob(sym + "*"))

		if len(exp) != 0 {
			c.Assert(res, DeepEquals, exp)
		} else {
			c.Assert(res, HasLen, 0)
		}
	}

	d := c.MkDir()
	barSymlink := filepath.Join(d, "bar")
	mylog.Check(osutil.AtomicSymlink("target", barSymlink))

	mustReadSymlink(barSymlink, "target")
	checkLeftoverFiles(barSymlink, []string{barSymlink})

	// no nested directory
	nested := filepath.Join(d, "nested")
	nestedBarSymlink := filepath.Join(nested, "bar")
	mylog.Check(osutil.AtomicSymlink("target", nestedBarSymlink))
	c.Assert(err, ErrorMatches, `symlink target /.*/nested/bar\..*~: no such file or directory`)
	checkLeftoverFiles(nestedBarSymlink, nil)

	if os.Geteuid() != 0 {
		mylog.
			// create a dir without write permission
			Check(os.MkdirAll(nested, 0644))

		mylog.Check(

			// no permission to write in dir
			osutil.AtomicSymlink("target", nestedBarSymlink))
		c.Assert(err, ErrorMatches, `symlink target /.*/nested/bar\..*~: permission denied`)
		checkLeftoverFiles(nestedBarSymlink, nil)
		mylog.Check(os.Chmod(nested, 0755))

	}
	mylog.Check(osutil.AtomicSymlink("target", nestedBarSymlink))

	mustReadSymlink(nestedBarSymlink, "target")
	checkLeftoverFiles(nestedBarSymlink, []string{nestedBarSymlink})
	mylog.

		// symlink gets replaced
		Check(osutil.AtomicSymlink("new-target", nestedBarSymlink))

	mustReadSymlink(nestedBarSymlink, "new-target")
	checkLeftoverFiles(nestedBarSymlink, []string{nestedBarSymlink})
	mylog.

		// don't care about symlink target
		Check(osutil.AtomicSymlink("/this/is/some/funny/path", nestedBarSymlink))

	mustReadSymlink(nestedBarSymlink, "/this/is/some/funny/path")
	checkLeftoverFiles(nestedBarSymlink, []string{nestedBarSymlink})
}

func (ts *AtomicSymlinkTestSuite) createCollisionSequence(c *C, baseName string, many int) {
	for i := 0; i < many; i++ {
		expectedRandomness := randutil.RandomString(12) + "~"
		mylog.
			// ensure we always get the same result
			Check(os.WriteFile(baseName+"."+expectedRandomness, []byte(""), 0644))

	}
}

func (ts *AtomicSymlinkTestSuite) TestAtomicSymlinkCollisionError(c *C) {
	tmpdir := c.MkDir()
	// ensure we always get the same result
	rand.Seed(1)
	p := filepath.Join(tmpdir, "foo")
	ts.createCollisionSequence(c, p, osutil.MaxSymlinkTries)
	// restart random number sequence
	rand.Seed(1)
	mylog.Check(osutil.AtomicSymlink("target", p))
	c.Assert(err, ErrorMatches, "cannot create a temporary symlink")
}

func (ts *AtomicSymlinkTestSuite) TestAtomicSymlinkCollisionHappy(c *C) {
	tmpdir := c.MkDir()
	// ensure we always get the same result
	rand.Seed(1)
	p := filepath.Join(tmpdir, "foo")
	ts.createCollisionSequence(c, p, osutil.MaxSymlinkTries/2)
	// restart random number sequence
	rand.Seed(1)
	mylog.Check(osutil.AtomicSymlink("target", p))

}

type AtomicRenameTestSuite struct{}

var _ = Suite(&AtomicRenameTestSuite{})

func (ts *AtomicRenameTestSuite) TestAtomicRenameFile(c *C) {
	d := c.MkDir()
	mylog.Check(os.WriteFile(filepath.Join(d, "foo"), []byte("foobar"), 0644))

	mylog.Check(osutil.AtomicRename(filepath.Join(d, "foo"), filepath.Join(d, "bar")))

	c.Check(filepath.Join(d, "bar"), testutil.FileEquals, "foobar")

	// no nested directory
	nested := filepath.Join(d, "nested")
	mylog.Check(osutil.AtomicRename(filepath.Join(d, "bar"), filepath.Join(nested, "bar")))
	if !osutil.GetUnsafeIO() {
		// with safe IO first op is to open the source and target directories
		c.Assert(err, ErrorMatches, "open /.*/nested: no such file or directory")
	} else {
		c.Assert(err, ErrorMatches, "rename /.*/bar /.*/nested/bar: no such file or directory")
	}

	if os.Geteuid() != 0 {
		mylog.
			// create a dir without write permission
			Check(os.MkdirAll(nested, 0644))

		mylog.Check(

			// no permission to write in dir
			osutil.AtomicRename(filepath.Join(d, "bar"), filepath.Join(nested, "bar")))
		c.Assert(err, ErrorMatches, "rename /.*/bar /.*/nested/bar: permission denied")
		mylog.Check(os.Chmod(nested, 0755))

	}
	mylog.

		// all good now
		Check(osutil.AtomicRename(filepath.Join(d, "bar"), filepath.Join(nested, "bar")))

	mylog.Check(os.WriteFile(filepath.Join(nested, "new-bar"), []byte("barbar"), 0644))

	mylog.

		// target is overwritten
		Check(osutil.AtomicRename(filepath.Join(nested, "new-bar"), filepath.Join(nested, "bar")))

	c.Check(filepath.Join(nested, "bar"), testutil.FileEquals, "barbar")
	mylog.

		// no source
		Check(osutil.AtomicRename(filepath.Join(d, "does-not-exist"), filepath.Join(nested, "bar")))
	c.Assert(err, ErrorMatches, "rename /.*/does-not-exist /.*/nested/bar: no such file or directory")
}

// SafeIoAtomicTestSuite runs all Atomic* tests with safe
// io enabled
type SafeIoAtomicTestSuite struct {
	AtomicWriteTestSuite
	AtomicSymlinkTestSuite
	AtomicRenameTestSuite

	restoreUnsafeIO func()
}

var _ = Suite(&SafeIoAtomicTestSuite{})

func (s *SafeIoAtomicTestSuite) SetUpSuite(c *C) {
	s.restoreUnsafeIO = osutil.SetUnsafeIO(false)
}

func (s *SafeIoAtomicTestSuite) TearDownSuite(c *C) {
	s.restoreUnsafeIO()
}

func (ts *AtomicWriteTestSuite) TestAtomicRenameDir(c *C) {
	// create a source directory
	srcParentDir := c.MkDir()
	src := filepath.Join(srcParentDir, "foo")
	mylog.Check(os.MkdirAll(src, 0755))


	// put a file in the source directory
	srcFile := filepath.Join(src, "file")
	contents := []byte("contents")
	mylog.Check(osutil.AtomicWriteFile(srcFile, contents, 0644, 0))


	// the parent dir of the destination
	dstParentDir := c.MkDir()
	dst := filepath.Join(dstParentDir, "bar")
	mylog.

		// ensure it works even with trailing '/'
		Check(osutil.AtomicRename(src+"/", dst+"/"))


	d := mylog.Check2(os.ReadDir(dst))

	c.Assert(len(d), Equals, 1)
	c.Assert(d[0].Name(), Equals, "file")

	data := mylog.Check2(os.ReadFile(filepath.Join(dst, "file")))

	c.Assert(data, DeepEquals, contents)

	exists, _ := mylog.Check3(osutil.DirExists(src))

	c.Assert(exists, Equals, false)
}
