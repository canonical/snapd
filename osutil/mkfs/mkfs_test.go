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
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	. "gopkg.in/check.v1"

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
	useFakeroot := os.Getuid() != 0
	var cmd *testutil.MockCmd
	if useFakeroot {
		cmd = testutil.MockCommand(c, "fakeroot", "")
	} else {
		cmd = testutil.MockCommand(c, "mkfs.ext4", "")
	}
	defer cmd.Restore()

	err := mkfs.Make(context.Background(), "ext4", "foo.img", &mkfs.MakeOptions{Label: "my-label", ContentRootDir: "contents"})
	c.Assert(err, IsNil)
	expectedCall := []string{
		"mkfs.ext4",
		"-d", "contents",
		"-L", "my-label",
		"foo.img",
	}
	if useFakeroot {
		expectedCall = append([]string{"fakeroot"}, expectedCall...)
	}
	c.Check(cmd.Calls(), DeepEquals, [][]string{expectedCall})

	cmd.ForgetCalls()

	// empty label
	err = mkfs.Make(context.Background(), "ext4", "foo.img", &mkfs.MakeOptions{ContentRootDir: "contents"})
	c.Assert(err, IsNil)
	expectedCall = []string{
		"mkfs.ext4",
		"-d", "contents",
		"foo.img",
	}
	if useFakeroot {
		expectedCall = append([]string{"fakeroot"}, expectedCall...)
	}
	c.Check(cmd.Calls(), DeepEquals, [][]string{expectedCall})

	cmd.ForgetCalls()

	// no content
	err = mkfs.Make(context.Background(), "ext4", "foo.img", &mkfs.MakeOptions{Label: "my-label"})
	c.Assert(err, IsNil)
	expectedCall = []string{
		"mkfs.ext4",
		"-L", "my-label",
		"foo.img",
	}
	if useFakeroot {
		expectedCall = append([]string{"fakeroot"}, expectedCall...)
	}
	c.Check(cmd.Calls(), DeepEquals, [][]string{expectedCall})

	cmd.ForgetCalls()

	// nil options.
	err = mkfs.Make(context.Background(), "ext4", "foo.img", nil)
	c.Assert(err, IsNil)
	expectedCall = []string{
		"mkfs.ext4",
		"foo.img",
	}
	if useFakeroot {
		expectedCall = append([]string{"fakeroot"}, expectedCall...)
	}
	c.Check(cmd.Calls(), DeepEquals, [][]string{expectedCall})
}

func (m *mkfsSuite) TestMkfsExt4WithSize(c *C) {
	useFakeroot := os.Getuid() != 0
	var cmd *testutil.MockCmd
	if useFakeroot {
		cmd = testutil.MockCommand(c, "fakeroot", "")
	} else {
		cmd = testutil.MockCommand(c, "mkfs.ext4", "")
	}
	defer cmd.Restore()

	err := mkfs.Make(context.Background(), "ext4", "foo.img", &mkfs.MakeOptions{Label: "my-label", ContentRootDir: "contents", DeviceSize: 250 * 1024 * 1024})
	c.Assert(err, IsNil)
	expectedCall := []string{
		"mkfs.ext4",
		"-d", "contents",
		"-L", "my-label",
		"foo.img",
	}
	if useFakeroot {
		expectedCall = append([]string{"fakeroot"}, expectedCall...)
	}
	c.Check(cmd.Calls(), DeepEquals, [][]string{expectedCall})

	cmd.ForgetCalls()

	// empty label
	err = mkfs.Make(context.Background(), "ext4", "foo.img", &mkfs.MakeOptions{ContentRootDir: "contents", DeviceSize: 32 * 1024 * 1024})
	c.Assert(err, IsNil)
	expectedCall = []string{
		"mkfs.ext4",
		"-b", "1024",
		"-d", "contents",
		"foo.img",
	}
	if useFakeroot {
		expectedCall = append([]string{"fakeroot"}, expectedCall...)
	}
	c.Check(cmd.Calls(), DeepEquals, [][]string{expectedCall})

	cmd.ForgetCalls()

	// with sector size of 512
	err = mkfs.Make(context.Background(), "ext4", "foo.img", &mkfs.MakeOptions{ContentRootDir: "contents", DeviceSize: 32 * 1024 * 1024, SectorSize: 512})
	c.Assert(err, IsNil)
	expectedCall = []string{
		"mkfs.ext4",
		"-b", "1024",
		"-d", "contents",
		"foo.img",
	}
	if useFakeroot {
		expectedCall = append([]string{"fakeroot"}, expectedCall...)
	}
	c.Check(cmd.Calls(), DeepEquals, [][]string{expectedCall})

	cmd.ForgetCalls()

	// with sector size of 4096
	err = mkfs.Make(context.Background(), "ext4", "foo.img", &mkfs.MakeOptions{ContentRootDir: "contents", DeviceSize: 32 * 1024 * 1024, SectorSize: 4096})
	c.Assert(err, IsNil)
	expectedCall = []string{
		"mkfs.ext4",
		"-b", "4096",
		"-d", "contents",
		"foo.img",
	}
	if useFakeroot {
		expectedCall = append([]string{"fakeroot"}, expectedCall...)
	}
	c.Check(cmd.Calls(), DeepEquals, [][]string{expectedCall})

	cmd.ForgetCalls()
}

func (m *mkfsSuite) TestMkfsExt4Error(c *C) {
	useFakeroot := os.Getuid() != 0
	var cmd *testutil.MockCmd
	if useFakeroot {
		cmd = testutil.MockCommand(c, "fakeroot", "echo 'command failed'; exit 1")
	} else {
		cmd = testutil.MockCommand(c, "mkfs.ext4", "echo 'command failed'; exit 1")
	}
	defer cmd.Restore()

	err := mkfs.Make(context.Background(), "ext4", "foo.img", &mkfs.MakeOptions{Label: "my-label", ContentRootDir: "contents"})
	c.Assert(err, ErrorMatches, "command failed")
}

func (m *mkfsSuite) TestMkfsExt4ContextPreCanceled(c *C) {
	useFakeroot := os.Getuid() != 0
	var cmd *testutil.MockCmd
	if useFakeroot {
		cmd = testutil.MockCommand(c, "fakeroot", "exit 0")
	} else {
		cmd = testutil.MockCommand(c, "mkfs.ext4", "exit 0")
	}
	defer cmd.Restore()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := mkfs.Make(ctx, "ext4", "foo.img", &mkfs.MakeOptions{Label: "my-label"})
	c.Assert(err, testutil.ErrorIs, context.Canceled)
	c.Assert(cmd.Calls(), HasLen, 0)
}

func (m *mkfsSuite) TestMkfsExt4ContextCanceledDuringExecution(c *C) {
	useFakeroot := os.Getuid() != 0
	var cmd *testutil.MockCmd
	if useFakeroot {
		cmd = testutil.MockCommand(c, "fakeroot", "sleep 10")
	} else {
		cmd = testutil.MockCommand(c, "mkfs.ext4", "sleep 10")
	}
	defer cmd.Restore()

	ctx, cancel := context.WithCancel(context.Background())
	// Cancel only once the command has been invoked, avoiding a race between
	// process startup and the cancel call. The mock logs its call before
	// running the script body, so Calls() is non-empty before sleep starts.
	go func() {
		for {
			if len(cmd.Calls()) > 0 {
				cancel()
				return
			}
		}
	}()

	err := mkfs.Make(ctx, "ext4", "foo.img", &mkfs.MakeOptions{Label: "my-label"})
	c.Assert(err, ErrorMatches, "signal: killed")
	c.Assert(cmd.Calls(), HasLen, 1)
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

	err := mkfs.Make(context.Background(), fatType, "foo.img", &mkfs.MakeOptions{Label: "my-label", ContentRootDir: d})
	c.Assert(err, IsNil)
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

	// empty label
	err = mkfs.Make(context.Background(), fatType, "foo.img", &mkfs.MakeOptions{ContentRootDir: d})
	c.Assert(err, IsNil)
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

	// no content
	err = mkfs.Make(context.Background(), fatType, "foo.img", &mkfs.MakeOptions{Label: "my-label"})
	c.Assert(err, IsNil)
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

	// nil options.
	err = mkfs.Make(context.Background(), fatType, "foo.img", nil)
	c.Assert(err, IsNil)
	c.Check(cmd.Calls(), DeepEquals, [][]string{
		{
			"mkfs.vfat",
			"-S", "512",
			"-s", "1",
			"-F", fatBits,
			"foo.img",
		},
	})
}

func (m *mkfsSuite) TestMkfsVfatWithSize(c *C) {
	d := c.MkDir()

	cmd := testutil.MockCommand(c, "mkfs.vfat", "")
	defer cmd.Restore()

	err := mkfs.Make(context.Background(), "vfat", "foo.img", &mkfs.MakeOptions{Label: "my-label", ContentRootDir: d, DeviceSize: 32 * 1024 * 1024})
	c.Assert(err, IsNil)
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

	// with sector size of 512
	err = mkfs.Make(context.Background(), "vfat", "foo.img", &mkfs.MakeOptions{Label: "my-label", ContentRootDir: d, DeviceSize: 32 * 1024 * 1024, SectorSize: 512})
	c.Assert(err, IsNil)
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

	// with sector size of 4096
	err = mkfs.Make(context.Background(), "vfat", "foo.img", &mkfs.MakeOptions{Label: "my-label", ContentRootDir: d, DeviceSize: 32 * 1024 * 1024, SectorSize: 4096})
	c.Assert(err, IsNil)
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

	err := mkfs.Make(context.Background(), "vfat", "foo.img", &mkfs.MakeOptions{Label: "my-label", ContentRootDir: d})
	c.Assert(err, IsNil)
	c.Assert(cmdMkfs.Calls(), HasLen, 1)

	c.Assert(cmdMcopy.Calls(), DeepEquals, [][]string{
		{"mcopy", "-s", "-i", "foo.img", filepath.Join(d, "bar"), filepath.Join(d, "foo"), "::"},
	})
}

func (m *mkfsSuite) TestMkfsVfatContextPreCanceled(c *C) {
	cmd := testutil.MockCommand(c, "mkfs.vfat", "exit 0")
	defer cmd.Restore()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := mkfs.Make(ctx, "vfat", "foo.img", &mkfs.MakeOptions{Label: "my-label"})
	c.Assert(err, testutil.ErrorIs, context.Canceled)
	c.Assert(cmd.Calls(), HasLen, 0)
}

func (m *mkfsSuite) TestMkfsVfatContextCanceledDuringMkfsExecution(c *C) {
	cmd := testutil.MockCommand(c, "mkfs.vfat", "sleep 10")
	defer cmd.Restore()

	ctx, cancel := context.WithCancel(context.Background())
	// Cancel only once the command has been invoked, avoiding a race between
	// process startup and the cancel call. The mock logs its call before
	// running the script body, so Calls() is non-empty before sleep starts.
	go func() {
		for {
			if len(cmd.Calls()) > 0 {
				cancel()
				return
			}
		}
	}()

	err := mkfs.Make(ctx, "vfat", "foo.img", &mkfs.MakeOptions{Label: "my-label"})
	c.Assert(err, ErrorMatches, "signal: killed")
	c.Assert(cmd.Calls(), HasLen, 1)
}

func (m *mkfsSuite) TestMkfsVfatContextCanceledDuringMcopyExecution(c *C) {
	d := c.MkDir()
	makeSizedFile(c, filepath.Join(d, "foo"), 128, []byte("foo foo foo"))

	cmdMkfs := testutil.MockCommand(c, "mkfs.vfat", "exit 0")
	defer cmdMkfs.Restore()

	cmdMcopy := testutil.MockCommand(c, "mcopy", "sleep 10")
	defer cmdMcopy.Restore()

	ctx, cancel := context.WithCancel(context.Background())
	// Cancel only once the command has been invoked, avoiding a race between
	// process startup and the cancel call. The mock logs its call before
	// running the script body, so Calls() is non-empty before sleep starts.
	go func() {
		for {
			if len(cmdMcopy.Calls()) > 0 {
				cancel()
				return
			}
		}
	}()

	err := mkfs.Make(ctx, "vfat", "foo.img", &mkfs.MakeOptions{Label: "my-label", ContentRootDir: d})
	c.Assert(err, ErrorMatches, "cannot populate vfat filesystem with contents: signal: killed")
	c.Assert(cmdMkfs.Calls(), HasLen, 1)
	c.Assert(cmdMcopy.Calls(), HasLen, 1)
}

func (m *mkfsSuite) TestMkfsVfatErrorSimpleFail(c *C) {
	d := c.MkDir()

	cmd := testutil.MockCommand(c, "mkfs.vfat", "echo 'failed'; false")
	defer cmd.Restore()

	err := mkfs.Make(context.Background(), "vfat", "foo.img", &mkfs.MakeOptions{Label: "my-label", ContentRootDir: d})
	c.Assert(err, ErrorMatches, "failed")
}

func (m *mkfsSuite) TestMkfsVfatErrorUnreadableDir(c *C) {
	cmd := testutil.MockCommand(c, "mkfs.vfat", "")
	defer cmd.Restore()

	err := mkfs.Make(context.Background(), "vfat", "foo.img", &mkfs.MakeOptions{Label: "my-label", ContentRootDir: "dir-does-not-exist"})
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

	err := mkfs.Make(context.Background(), "vfat", "foo.img", &mkfs.MakeOptions{Label: "my-label", ContentRootDir: d})
	c.Assert(err, ErrorMatches, "cannot populate vfat filesystem with contents: hard fail")
	c.Assert(cmdMkfs.Calls(), HasLen, 1)
	c.Assert(cmdMcopy.Calls(), HasLen, 1)
}

func (m *mkfsSuite) TestMkfsVfatHappyNoContents(c *C) {
	cmdMkfs := testutil.MockCommand(c, "mkfs.vfat", "")
	defer cmdMkfs.Restore()

	cmdMcopy := testutil.MockCommand(c, "mcopy", "")
	defer cmdMcopy.Restore()

	err := mkfs.Make(context.Background(), "vfat", "foo.img", &mkfs.MakeOptions{Label: "my-label"})
	c.Assert(err, IsNil)
	c.Assert(cmdMkfs.Calls(), HasLen, 1)
	// mcopy was not called
	c.Assert(cmdMcopy.Calls(), HasLen, 0)
}

func (m *mkfsSuite) TestMkfsInvalidFs(c *C) {
	err := mkfs.Make(context.Background(), "no-fs", "foo.img", &mkfs.MakeOptions{Label: "my-label"})
	c.Assert(err, ErrorMatches, `cannot create unsupported filesystem "no-fs"`)

	err = mkfs.Make(context.Background(), "no-fs", "foo.img", nil)
	c.Assert(err, ErrorMatches, `cannot create unsupported filesystem "no-fs"`)
}

func makeSizedFile(c *C, path string, size int64, content []byte) {
	err := os.MkdirAll(filepath.Dir(path), 0755)
	c.Assert(err, IsNil)

	f, err := os.Create(path)
	c.Assert(err, IsNil)
	defer f.Close()
	if size != 0 {
		err = f.Truncate(size)
		c.Assert(err, IsNil)
	}
	if content != nil {
		_, err := io.Copy(f, bytes.NewReader(content))
		c.Assert(err, IsNil)
	}
}
