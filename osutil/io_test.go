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
	"io/ioutil"
	"math/rand"
	"os"
	"path/filepath"

	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/osutil/sys"
	"github.com/snapcore/snapd/strutil"
	"github.com/snapcore/snapd/testutil"

	. "gopkg.in/check.v1"
)

type AtomicWriteTestSuite struct{}

var _ = Suite(&AtomicWriteTestSuite{})

func (ts *AtomicWriteTestSuite) TestAtomicWriteFile(c *C) {
	tmpdir := c.MkDir()

	p := filepath.Join(tmpdir, "foo")
	err := osutil.AtomicWriteFile(p, []byte("canary"), 0644, 0)
	c.Assert(err, IsNil)

	c.Check(p, testutil.FileEquals, "canary")

	// no files left behind!
	d, err := ioutil.ReadDir(tmpdir)
	c.Assert(err, IsNil)
	c.Assert(len(d), Equals, 1)
}

func (ts *AtomicWriteTestSuite) TestAtomicWriteFilePermissions(c *C) {
	tmpdir := c.MkDir()

	p := filepath.Join(tmpdir, "foo")
	err := osutil.AtomicWriteFile(p, []byte(""), 0600, 0)
	c.Assert(err, IsNil)

	st, err := os.Stat(p)
	c.Assert(err, IsNil)
	c.Assert(st.Mode()&os.ModePerm, Equals, os.FileMode(0600))
}

func (ts *AtomicWriteTestSuite) TestAtomicWriteFileOverwrite(c *C) {
	tmpdir := c.MkDir()
	p := filepath.Join(tmpdir, "foo")
	c.Assert(ioutil.WriteFile(p, []byte("hello"), 0644), IsNil)
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

	err := osutil.AtomicWriteFile(p, []byte("hi"), 0600, 0)
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

	err := osutil.AtomicWriteFile(p, []byte("hi"), 0600, osutil.AtomicWriteFollow)
	c.Assert(err, IsNil)

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

	c.Assert(ioutil.WriteFile(s, []byte("hello"), 0644), IsNil)
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

	err := osutil.AtomicWriteFile(p, []byte("hi"), 0600, osutil.AtomicWriteFollow)
	c.Assert(err, IsNil)

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

	c.Assert(ioutil.WriteFile(s, []byte("hello"), 0644), IsNil)
	c.Assert(osutil.AtomicWriteFile(p, []byte("hi"), 0600, osutil.AtomicWriteFollow), IsNil)

	c.Assert(p, testutil.FileEquals, "hi")
}

func (ts *AtomicWriteTestSuite) TestAtomicWriteFileNoOverwriteTmpExisting(c *C) {
	tmpdir := c.MkDir()
	// ensure we always get the same result
	rand.Seed(1)
	expectedRandomness := strutil.MakeRandomString(12) + "~"
	// ensure we always get the same result
	rand.Seed(1)

	p := filepath.Join(tmpdir, "foo")
	err := ioutil.WriteFile(p+"."+expectedRandomness, []byte(""), 0644)
	c.Assert(err, IsNil)

	err = osutil.AtomicWriteFile(p, []byte(""), 0600, 0)
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

	aw, err := osutil.NewAtomicFile(p, 0644, 0, eUid, eGid)
	c.Assert(err, IsNil)
	defer aw.Cancel()

	_, err = aw.Write([]byte("hello"))
	c.Assert(err, IsNil)

	c.Check(aw.Commit(), Equals, eErr)
}

func (ts *AtomicWriteTestSuite) TestAtomicFileCancelError(c *C) {
	d := c.MkDir()
	p := filepath.Join(d, "foo")
	aw, err := osutil.NewAtomicFile(p, 0644, 0, osutil.NoChown, osutil.NoChown)
	c.Assert(err, IsNil)

	c.Assert(aw.File.Close(), IsNil)
	// Depending on golang version the error is one of the two.
	c.Check(aw.Cancel(), ErrorMatches, "invalid argument|file already closed")
}

func (ts *AtomicWriteTestSuite) TestAtomicFileCancelBadError(c *C) {
	d := c.MkDir()
	p := filepath.Join(d, "foo")
	aw, err := osutil.NewAtomicFile(p, 0644, 0, osutil.NoChown, osutil.NoChown)
	c.Assert(err, IsNil)
	defer aw.Close()

	osutil.SetAtomicFileRenamed(aw, true)

	c.Check(aw.Cancel(), Equals, osutil.ErrCannotCancel)
}

func (ts *AtomicWriteTestSuite) TestAtomicFileCancelNoClose(c *C) {
	d := c.MkDir()
	p := filepath.Join(d, "foo")
	aw, err := osutil.NewAtomicFile(p, 0644, 0, osutil.NoChown, osutil.NoChown)
	c.Assert(err, IsNil)
	c.Assert(aw.Close(), IsNil)

	c.Check(aw.Cancel(), IsNil)
}

func (ts *AtomicWriteTestSuite) TestAtomicFileCancel(c *C) {
	d := c.MkDir()
	p := filepath.Join(d, "foo")

	aw, err := osutil.NewAtomicFile(p, 0644, 0, osutil.NoChown, osutil.NoChown)
	c.Assert(err, IsNil)
	fn := aw.File.Name()
	c.Check(osutil.FileExists(fn), Equals, true)
	c.Check(aw.Cancel(), IsNil)
	c.Check(osutil.FileExists(fn), Equals, false)
}

type AtomicSymlinkTestSuite struct{}

var _ = Suite(&AtomicSymlinkTestSuite{})

func (ts *AtomicSymlinkTestSuite) TestAtomicSymlink(c *C) {
	mustReadSymlink := func(p, exp string) {
		target, err := os.Readlink(p)
		c.Assert(err, IsNil)
		c.Check(exp, Equals, target)
	}

	d := c.MkDir()
	err := osutil.AtomicSymlink("target", filepath.Join(d, "bar"))
	c.Assert(err, IsNil)
	mustReadSymlink(filepath.Join(d, "bar"), "target")

	// no nested directory
	nested := filepath.Join(d, "nested")
	err = osutil.AtomicSymlink("target", filepath.Join(nested, "bar"))
	c.Assert(err, ErrorMatches, `symlink target /.*/nested/bar\..*~: no such file or directory`)

	// create a dir without write permission
	err = os.MkdirAll(nested, 0644)
	c.Assert(err, IsNil)

	// no permission to write in dir
	err = osutil.AtomicSymlink("target", filepath.Join(nested, "bar"))
	c.Assert(err, ErrorMatches, `symlink target /.*/nested/bar\..*~: permission denied`)

	err = os.Chmod(nested, 0755)
	c.Assert(err, IsNil)

	err = osutil.AtomicSymlink("target", filepath.Join(nested, "bar"))
	c.Assert(err, IsNil)
	mustReadSymlink(filepath.Join(nested, "bar"), "target")

	// symlink gets replaced
	err = osutil.AtomicSymlink("new-target", filepath.Join(nested, "bar"))
	c.Assert(err, IsNil)
	mustReadSymlink(filepath.Join(nested, "bar"), "new-target")

	// don't care about symlink target
	err = osutil.AtomicSymlink("/this/is/some/funny/path", filepath.Join(nested, "bar"))
	c.Assert(err, IsNil)
	mustReadSymlink(filepath.Join(nested, "bar"), "/this/is/some/funny/path")

}

type AtomicRenameTestSuite struct{}

var _ = Suite(&AtomicRenameTestSuite{})

func (ts *AtomicRenameTestSuite) TestAtomicRename(c *C) {
	d := c.MkDir()

	err := ioutil.WriteFile(filepath.Join(d, "foo"), []byte("foobar"), 0644)
	c.Assert(err, IsNil)

	err = osutil.AtomicRename(filepath.Join(d, "foo"), filepath.Join(d, "bar"))
	c.Assert(err, IsNil)
	c.Check(filepath.Join(d, "bar"), testutil.FileEquals, "foobar")

	// no nested directory
	nested := filepath.Join(d, "nested")
	err = osutil.AtomicRename(filepath.Join(d, "bar"), filepath.Join(nested, "bar"))
	if !osutil.GetUnsafeIO() {
		// with safe IO first op is to open the source and target directories
		c.Assert(err, ErrorMatches, "open /.*/nested: no such file or directory")
	} else {
		c.Assert(err, ErrorMatches, "rename /.*/bar /.*/nested/bar: no such file or directory")
	}

	// create a dir without write permission
	err = os.MkdirAll(nested, 0644)
	c.Assert(err, IsNil)

	// no permission to write in dir
	err = osutil.AtomicRename(filepath.Join(d, "bar"), filepath.Join(nested, "bar"))
	c.Assert(err, ErrorMatches, "rename /.*/bar /.*/nested/bar: permission denied")

	err = os.Chmod(nested, 0755)
	c.Assert(err, IsNil)

	// all good now
	err = osutil.AtomicRename(filepath.Join(d, "bar"), filepath.Join(nested, "bar"))
	c.Assert(err, IsNil)

	err = ioutil.WriteFile(filepath.Join(nested, "new-bar"), []byte("barbar"), 0644)
	c.Assert(err, IsNil)

	// target is overwritten
	err = osutil.AtomicRename(filepath.Join(nested, "new-bar"), filepath.Join(nested, "bar"))
	c.Assert(err, IsNil)
	c.Check(filepath.Join(nested, "bar"), testutil.FileEquals, "barbar")

	// no source
	err = osutil.AtomicRename(filepath.Join(d, "does-not-exist"), filepath.Join(nested, "bar"))
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
