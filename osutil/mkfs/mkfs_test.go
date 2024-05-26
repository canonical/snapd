// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019 Canonical Ltd
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

package mkfs_test

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/osutil/mkfs"
	"github.com/snapcore/snapd/testutil"
)

func TestRun(t *testing.T) { TestingT(t) }

type mkfsSuite struct {
	testutil.BaseTest
}

var _ = Suite(&mkfsSuite{})

func (m *mkfsSuite) SetUpTest(c *C) {
	m.BaseTest.SetUpTest(c)

	// fakeroot, mkfs.ext4, mkfs.vfat and mcopy are commonly installed in
	// the host system, set up some overrides so that we avoid calling the
	// host tools
	cmdFakeroot := testutil.MockCommand(c, "fakeroot", "echo 'override in test' ; exit 1")
	m.AddCleanup(cmdFakeroot.Restore)

	cmdMkfsExt4 := testutil.MockCommand(c, "mkfs.ext4", "echo 'override in test' ; exit 1")
	m.AddCleanup(cmdMkfsExt4.Restore)

	cmdMkfsVfat := testutil.MockCommand(c, "mkfs.vfat", "echo 'override in test'; exit 1")
	m.AddCleanup(cmdMkfsVfat.Restore)

	cmdMcopy := testutil.MockCommand(c, "mcopy", "echo 'override in test'; exit 1")
	m.AddCleanup(cmdMcopy.Restore)
}

func (m *mkfsSuite) TestMkfsExt4Happy(c *C) {
	cmd := testutil.MockCommand(c, "fakeroot", "")
	defer cmd.Restore()
	mylog.Check(mkfs.MakeWithContent("ext4", "foo.img", "my-label", "contents", 0, 0))

	c.Check(cmd.Calls(), DeepEquals, [][]string{
		{
			"fakeroot",
			"mkfs.ext4",
			"-d", "contents",
			"-L", "my-label",
			"foo.img",
		},
	})

	cmd.ForgetCalls()
	mylog.

		// empty label
		Check(mkfs.MakeWithContent("ext4", "foo.img", "", "contents", 0, 0))

	c.Check(cmd.Calls(), DeepEquals, [][]string{
		{
			"fakeroot",
			"mkfs.ext4",
			"-d", "contents",
			"foo.img",
		},
	})

	cmd.ForgetCalls()
	mylog.

		// no content
		Check(mkfs.Make("ext4", "foo.img", "my-label", 0, 0))

	c.Check(cmd.Calls(), DeepEquals, [][]string{
		{
			"fakeroot",
			"mkfs.ext4",
			"-L", "my-label",
			"foo.img",
		},
	})
}

func (m *mkfsSuite) TestMkfsExt4WithSize(c *C) {
	cmd := testutil.MockCommand(c, "fakeroot", "")
	defer cmd.Restore()
	mylog.Check(mkfs.MakeWithContent("ext4", "foo.img", "my-label", "contents", 250*1024*1024, 0))

	c.Check(cmd.Calls(), DeepEquals, [][]string{
		{
			"fakeroot",
			"mkfs.ext4",
			"-d", "contents",
			"-L", "my-label",
			"foo.img",
		},
	})

	cmd.ForgetCalls()
	mylog.

		// empty label
		Check(mkfs.MakeWithContent("ext4", "foo.img", "", "contents", 32*1024*1024, 0))

	c.Check(cmd.Calls(), DeepEquals, [][]string{
		{
			"fakeroot",
			"mkfs.ext4",
			"-b", "1024",
			"-d", "contents",
			"foo.img",
		},
	})

	cmd.ForgetCalls()
	mylog.

		// with sector size of 512
		Check(mkfs.MakeWithContent("ext4", "foo.img", "", "contents", 32*1024*1024, 512))

	c.Check(cmd.Calls(), DeepEquals, [][]string{
		{
			"fakeroot",
			"mkfs.ext4",
			"-b", "1024",
			"-d", "contents",
			"foo.img",
		},
	})

	cmd.ForgetCalls()
	mylog.

		// with sector size of 4096
		Check(mkfs.MakeWithContent("ext4", "foo.img", "", "contents", 32*1024*1024, 4096))

	c.Check(cmd.Calls(), DeepEquals, [][]string{
		{
			"fakeroot",
			"mkfs.ext4",
			"-b", "4096",
			"-d", "contents",
			"foo.img",
		},
	})

	cmd.ForgetCalls()
}

func (m *mkfsSuite) TestMkfsExt4Error(c *C) {
	cmd := testutil.MockCommand(c, "fakeroot", "echo 'command failed'; exit 1")
	defer cmd.Restore()
	mylog.Check(mkfs.MakeWithContent("ext4", "foo.img", "my-label", "contents", 0, 0))
	c.Assert(err, ErrorMatches, "command failed")
}

func (m *mkfsSuite) TestMkfsFat16HappySimple(c *C) {
	m.testMkfsVfatHappySimple(c, "vfat-16", "16")
}

func (m *mkfsSuite) TestMkfsFat32HappySimple(c *C) {
	m.testMkfsVfatHappySimple(c, "vfat", "32")
	m.testMkfsVfatHappySimple(c, "vfat-32", "32")
}

func (m *mkfsSuite) testMkfsVfatHappySimple(c *C, fatType, fatBits string) {
	// no contents, should not fail
	d := c.MkDir()

	cmd := testutil.MockCommand(c, "mkfs.vfat", "")
	defer cmd.Restore()
	mylog.Check(mkfs.MakeWithContent(fatType, "foo.img", "my-label", d, 0, 0))

	c.Check(cmd.Calls(), DeepEquals, [][]string{
		{
			"mkfs.vfat",
			"-S", "512",
			"-s", "1",
			"-F", fatBits,
			"-n", "my-label",
			"foo.img",
		},
	})

	cmd.ForgetCalls()
	mylog.

		// empty label
		Check(mkfs.MakeWithContent(fatType, "foo.img", "", d, 0, 0))

	c.Check(cmd.Calls(), DeepEquals, [][]string{
		{
			"mkfs.vfat",
			"-S", "512",
			"-s", "1",
			"-F", fatBits,
			"foo.img",
		},
	})

	cmd.ForgetCalls()
	mylog.

		// no content
		Check(mkfs.Make(fatType, "foo.img", "my-label", 0, 0))

	c.Check(cmd.Calls(), DeepEquals, [][]string{
		{
			"mkfs.vfat",
			"-S", "512",
			"-s", "1",
			"-F", fatBits,
			"-n", "my-label",
			"foo.img",
		},
	})
}

func (m *mkfsSuite) TestMkfsVfatWithSize(c *C) {
	d := c.MkDir()

	cmd := testutil.MockCommand(c, "mkfs.vfat", "")
	defer cmd.Restore()
	mylog.Check(mkfs.MakeWithContent("vfat", "foo.img", "my-label", d, 32*1024*1024, 0))

	c.Check(cmd.Calls(), DeepEquals, [][]string{
		{
			"mkfs.vfat",
			"-S", "512",
			"-s", "1",
			"-F", "32",
			"-n", "my-label",
			"foo.img",
		},
	})

	cmd.ForgetCalls()
	mylog.

		// with sector size of 512
		Check(mkfs.MakeWithContent("vfat", "foo.img", "my-label", d, 32*1024*1024, 512))

	c.Check(cmd.Calls(), DeepEquals, [][]string{
		{
			"mkfs.vfat",
			"-S", "512",
			"-s", "1",
			"-F", "32",
			"-n", "my-label",
			"foo.img",
		},
	})

	cmd.ForgetCalls()
	mylog.

		// with sector size of 4096
		Check(mkfs.MakeWithContent("vfat", "foo.img", "my-label", d, 32*1024*1024, 4096))

	c.Check(cmd.Calls(), DeepEquals, [][]string{
		{
			"mkfs.vfat",
			"-S", "4096",
			"-s", "1",
			"-F", "32",
			"-n", "my-label",
			"foo.img",
		},
	})
}

func (m *mkfsSuite) TestMkfsVfatHappyContents(c *C) {
	d := c.MkDir()
	makeSizedFile(c, filepath.Join(d, "foo"), 128, []byte("foo foo foo"))
	makeSizedFile(c, filepath.Join(d, "bar/bar-content"), 128, []byte("bar bar bar"))

	cmdMkfs := testutil.MockCommand(c, "mkfs.vfat", "")
	defer cmdMkfs.Restore()

	cmdMcopy := testutil.MockCommand(c, "mcopy", "")
	defer cmdMcopy.Restore()
	mylog.Check(mkfs.MakeWithContent("vfat", "foo.img", "my-label", d, 0, 0))

	c.Assert(cmdMkfs.Calls(), HasLen, 1)

	c.Assert(cmdMcopy.Calls(), DeepEquals, [][]string{
		{"mcopy", "-s", "-i", "foo.img", filepath.Join(d, "bar"), filepath.Join(d, "foo"), "::"},
	})
}

func (m *mkfsSuite) TestMkfsVfatErrorSimpleFail(c *C) {
	d := c.MkDir()

	cmd := testutil.MockCommand(c, "mkfs.vfat", "echo 'failed'; false")
	defer cmd.Restore()
	mylog.Check(mkfs.MakeWithContent("vfat", "foo.img", "my-label", d, 0, 0))
	c.Assert(err, ErrorMatches, "failed")
}

func (m *mkfsSuite) TestMkfsVfatErrorUnreadableDir(c *C) {
	cmd := testutil.MockCommand(c, "mkfs.vfat", "")
	defer cmd.Restore()
	mylog.Check(mkfs.MakeWithContent("vfat", "foo.img", "my-label", "dir-does-not-exist", 0, 0))
	c.Assert(err, ErrorMatches, "cannot list directory contents: .* no such file or directory")
	c.Assert(cmd.Calls(), HasLen, 1)
}

func (m *mkfsSuite) TestMkfsVfatErrorInMcopy(c *C) {
	d := c.MkDir()
	makeSizedFile(c, filepath.Join(d, "foo"), 128, []byte("foo foo foo"))

	cmdMkfs := testutil.MockCommand(c, "mkfs.vfat", "")
	defer cmdMkfs.Restore()

	cmdMcopy := testutil.MockCommand(c, "mcopy", "echo 'hard fail'; exit 1")
	defer cmdMcopy.Restore()
	mylog.Check(mkfs.MakeWithContent("vfat", "foo.img", "my-label", d, 0, 0))
	c.Assert(err, ErrorMatches, "cannot populate vfat filesystem with contents: hard fail")
	c.Assert(cmdMkfs.Calls(), HasLen, 1)
	c.Assert(cmdMcopy.Calls(), HasLen, 1)
}

func (m *mkfsSuite) TestMkfsVfatHappyNoContents(c *C) {
	cmdMkfs := testutil.MockCommand(c, "mkfs.vfat", "")
	defer cmdMkfs.Restore()

	cmdMcopy := testutil.MockCommand(c, "mcopy", "")
	defer cmdMcopy.Restore()
	mylog.Check(mkfs.MakeWithContent("vfat", "foo.img", "my-label", "", 0, 0))

	c.Assert(cmdMkfs.Calls(), HasLen, 1)
	// mcopy was not called
	c.Assert(cmdMcopy.Calls(), HasLen, 0)
}

func (m *mkfsSuite) TestMkfsInvalidFs(c *C) {
	mylog.Check(mkfs.MakeWithContent("no-fs", "foo.img", "my-label", "", 0, 0))
	c.Assert(err, ErrorMatches, `cannot create unsupported filesystem "no-fs"`)
	mylog.Check(mkfs.Make("no-fs", "foo.img", "my-label", 0, 0))
	c.Assert(err, ErrorMatches, `cannot create unsupported filesystem "no-fs"`)
}

func makeSizedFile(c *C, path string, size int64, content []byte) {
	mylog.Check(os.MkdirAll(filepath.Dir(path), 0755))


	f := mylog.Check2(os.Create(path))

	defer f.Close()
	if size != 0 {
		mylog.Check(f.Truncate(size))

	}
	if content != nil {
		_ := mylog.Check2(io.Copy(f, bytes.NewReader(content)))

	}
}
