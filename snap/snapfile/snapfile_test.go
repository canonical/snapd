// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020 Canonical Ltd
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

package snapfile_test

import (
	"os"
	"path/filepath"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snapfile"
	"github.com/snapcore/snapd/snap/squashfs"
	"github.com/snapcore/snapd/testutil"
)

func TestSnapfileTest(t *testing.T) { TestingT(t) }

type snapFileTestSuite struct {
	testutil.BaseTest
}

var _ = Suite(&snapFileTestSuite{})

func (s *snapFileTestSuite) SetUpTest(c *C) {
	dirs.SetRootDir(c.MkDir())

	restore := osutil.MockMountInfo("")
	s.AddCleanup(restore)
}

func (s *snapFileTestSuite) TestOpenSquashfs(c *C) {
	// make a squashfs snap and try to open it with just the filename, then
	// install it somewhere
	tmp := c.MkDir()
	mylog.Check(os.MkdirAll(filepath.Join(tmp, "meta"), 0755))

	mylog.

		// our regular snap.yaml
		Check(os.WriteFile(filepath.Join(tmp, "meta", "snap.yaml"), []byte("name: foo"), 0644))


	// build it
	dir := c.MkDir()
	snFilename := filepath.Join(dir, "foo.snap")
	buildSn := squashfs.New(snFilename)
	mylog.Check(buildSn.Build(tmp, &squashfs.BuildOpts{SnapType: "app"}))


	sn := mylog.Check2(snapfile.Open(snFilename))


	targetPath := filepath.Join(c.MkDir(), "target.snap")
	mountDir := c.MkDir()
	// we should have copied it
	didNothing := mylog.Check2(sn.Install(targetPath, mountDir, nil))

	c.Assert(didNothing, Equals, false)
	c.Check(osutil.FileExists(targetPath), Equals, true)

	r := mylog.Check2(sn.RandomAccessFile("meta/snap.yaml"))

	defer r.Close()

	b := make([]byte, 5)
	n := mylog.Check2(r.ReadAt(b, 4))

	c.Assert(n, Equals, 5)
	c.Check(string(b), Equals, ": foo")
}

func (s *snapFileTestSuite) TestOpenSnapdir(c *C) {
	// make a snapdir snap and try to open it with just the filename, then
	// install it somewhere
	tmp := c.MkDir()
	mylog.Check(os.MkdirAll(filepath.Join(tmp, "meta"), 0755))

	mylog.

		// our regular snap.yaml
		Check(os.WriteFile(filepath.Join(tmp, "meta", "snap.yaml"), []byte("name: foo"), 0644))


	sn := mylog.Check2(snapfile.Open(tmp))


	targetPath := filepath.Join(c.MkDir(), "target.snap")
	mountDir := c.MkDir()
	// we should have copied it
	didNothing := mylog.Check2(sn.Install(targetPath, mountDir, nil))

	c.Assert(didNothing, Equals, false)
	c.Check(osutil.FileExists(targetPath), Equals, true)

	r := mylog.Check2(sn.RandomAccessFile("meta/snap.yaml"))

	defer r.Close()

	b := make([]byte, 5)
	n := mylog.Check2(r.ReadAt(b, 4))

	c.Assert(n, Equals, 5)
	c.Check(string(b), Equals, ": foo")
}

func (s *snapFileTestSuite) TestOpenSnapdirUnsupportedFormat(c *C) {
	// make a file with garbage data
	tmp := c.MkDir()
	fn := filepath.Join(tmp, "some-format")
	mylog.Check(os.WriteFile(fn, []byte("not-a-real-header"), 0644))


	_ = mylog.Check2(snapfile.Open(fn))
	c.Assert(err, FitsTypeOf, snap.NotSnapError{})
	c.Check(err, ErrorMatches, `cannot process snap or snapdir: file ".*" is invalid \(header \[110 111 116 45 97 45 114 101 97 108 45 104 101 97 100\] "not-a-real-head"\)`)
}

func (s *snapFileTestSuite) TestOpenSnapdirFileNoExists(c *C) {
	dir := c.MkDir()
	_ := mylog.Check2(snapfile.Open(filepath.Join(dir, "non-existing-file")))
	c.Assert(err, FitsTypeOf, snap.NotSnapError{})
	c.Check(err, ErrorMatches, `cannot process snap or snapdir: open /.*/non-existing-file: no such file or directory`)
}

func (s *snapFileTestSuite) TestOpenSnapdirFileEmpty(c *C) {
	emptyFile := filepath.Join(c.MkDir(), "foo")
	mylog.Check(os.WriteFile(emptyFile, nil, 0644))

	_ = mylog.Check2(snapfile.Open(emptyFile))
	c.Assert(err, FitsTypeOf, snap.NotSnapError{})
	c.Check(err, ErrorMatches, `cannot process snap or snapdir: cannot read "/.*/foo": EOF`)
}

func (s *snapFileTestSuite) TestFileOpenForSnapDirErrors(c *C) {
	// no snap.yaml file
	_ := mylog.Check2(snapfile.Open(c.MkDir()))
	c.Assert(err, FitsTypeOf, snap.NotSnapError{})
	c.Assert(err, ErrorMatches, `cannot process snap or snapdir: directory ".*" is empty`)
}

func (s *snapFileTestSuite) TestNotSnapErrorInvalidDir(c *C) {
	tmpdir := c.MkDir()
	mylog.Check(os.WriteFile(filepath.Join(tmpdir, "foo"), nil, 0644))

	_ = mylog.Check2(snapfile.Open(tmpdir))
	c.Assert(err, FitsTypeOf, snap.NotSnapError{})
	c.Check(err, ErrorMatches, `cannot process snap or snapdir: directory ".*" is invalid`)
}
