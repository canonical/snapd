// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2020 Canonical Ltd
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
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/testutil"
)

type cpSuite struct {
	testutil.BaseTest

	dir  string
	f1   string
	f2   string
	data []byte
	log  []string
	errs []error
}

var _ = Suite(&cpSuite{})

func (s *cpSuite) mockCopyFile(fin, fout osutil.Fileish, fi os.FileInfo) error {
	return s.µ("copyfile")
}

func (s *cpSuite) mockOpenFile(name string, flag int, perm os.FileMode) (osutil.Fileish, error) {
	return &mockfile{s}, s.µ("open")
}

func (s *cpSuite) µ(msg string) (err error) {
	s.log = append(s.log, msg)
	if len(s.errs) > 0 {
		err = s.errs[0]
		if len(s.errs) > 1 {
			s.errs = s.errs[1:]
		}
	}

	return err
}

func (s *cpSuite) SetUpTest(c *C) {
	s.errs = nil
	s.log = nil
	s.dir = c.MkDir()
	s.f1 = filepath.Join(s.dir, "f1")
	s.f2 = filepath.Join(s.dir, "f2")
	s.data = []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
	c.Assert(os.WriteFile(s.f1, s.data, 0644), IsNil)
}

func (s *cpSuite) mock() {
	s.AddCleanup(osutil.MockCopyFile(s.mockCopyFile))
	s.AddCleanup(osutil.MockOpenFile(s.mockOpenFile))
}

func (s *cpSuite) TestCp(c *C) {
	c.Check(osutil.CopyFile(s.f1, s.f2, osutil.CopyFlagDefault), IsNil)
	c.Check(s.f2, testutil.FileEquals, s.data)
}

func (s *cpSuite) TestCpNoOverwrite(c *C) {
	_, err := os.Create(s.f2)
	c.Assert(err, IsNil)
	c.Check(osutil.CopyFile(s.f1, s.f2, osutil.CopyFlagDefault), NotNil)
}

func (s *cpSuite) TestCpOverwrite(c *C) {
	_, err := os.Create(s.f2)
	c.Assert(err, IsNil)
	c.Check(osutil.CopyFile(s.f1, s.f2, osutil.CopyFlagOverwrite), IsNil)
	c.Check(s.f2, testutil.FileEquals, s.data)
}

func (s *cpSuite) TestCpOverwriteTruncates(c *C) {
	c.Assert(os.WriteFile(s.f2, []byte("xxxxxxxxxxxxxxxx"), 0644), IsNil)
	c.Check(osutil.CopyFile(s.f1, s.f2, osutil.CopyFlagOverwrite), IsNil)
	c.Check(s.f2, testutil.FileEquals, s.data)
}

func (s *cpSuite) TestCpSync(c *C) {
	s.mock()
	c.Check(osutil.CopyFile(s.f1, s.f2, osutil.CopyFlagDefault), IsNil)
	c.Check(strings.Join(s.log, ":"), Not(Matches), `.*:sync(:.*)?`)

	s.log = nil
	c.Check(osutil.CopyFile(s.f1, s.f2, osutil.CopyFlagSync), IsNil)
	c.Check(strings.Join(s.log, ":"), Matches, `(.*:)?sync(:.*)?`)
}

func (s *cpSuite) TestCpCantOpen(c *C) {
	s.mock()
	s.errs = []error{errors.New("xyzzy"), nil}

	c.Check(osutil.CopyFile(s.f1, s.f2, osutil.CopyFlagSync), ErrorMatches, `unable to open \S+/f1: xyzzy`)
}

func (s *cpSuite) TestCpCantStat(c *C) {
	s.mock()
	s.errs = []error{nil, errors.New("xyzzy"), nil}

	c.Check(osutil.CopyFile(s.f1, s.f2, osutil.CopyFlagSync), ErrorMatches, `unable to stat \S+/f1: xyzzy`)
}

func (s *cpSuite) TestCpCantCreate(c *C) {
	s.mock()
	s.errs = []error{nil, nil, errors.New("xyzzy"), nil}

	c.Check(osutil.CopyFile(s.f1, s.f2, osutil.CopyFlagSync), ErrorMatches, `unable to create \S+/f2: xyzzy`)
}

func (s *cpSuite) TestCpCantCopy(c *C) {
	s.mock()
	s.errs = []error{nil, nil, nil, errors.New("xyzzy"), nil}

	c.Check(osutil.CopyFile(s.f1, s.f2, osutil.CopyFlagSync), ErrorMatches, `unable to copy \S+/f1 to \S+/f2: xyzzy`)
}

func (s *cpSuite) TestCpCantSync(c *C) {
	s.mock()
	s.errs = []error{nil, nil, nil, nil, errors.New("xyzzy"), nil}

	c.Check(osutil.CopyFile(s.f1, s.f2, osutil.CopyFlagSync), ErrorMatches, `unable to sync \S+/f2: xyzzy`)
}

func (s *cpSuite) TestCpCantStop2(c *C) {
	s.mock()
	s.errs = []error{nil, nil, nil, nil, nil, errors.New("xyzzy"), nil}

	c.Check(osutil.CopyFile(s.f1, s.f2, osutil.CopyFlagSync), ErrorMatches, `when closing \S+/f2: xyzzy`)
}

func (s *cpSuite) TestCpCantStop1(c *C) {
	s.mock()
	s.errs = []error{nil, nil, nil, nil, nil, nil, errors.New("xyzzy"), nil}

	c.Check(osutil.CopyFile(s.f1, s.f2, osutil.CopyFlagSync), ErrorMatches, `when closing \S+/f1: xyzzy`)
}

type mockfile struct {
	s *cpSuite
}

var mockst = mockstat{}

func (f *mockfile) Close() error               { return f.s.µ("close") }
func (f *mockfile) Sync() error                { return f.s.µ("sync") }
func (f *mockfile) Fd() uintptr                { f.s.µ("fd"); return 42 }
func (f *mockfile) Read([]byte) (int, error)   { return 0, f.s.µ("read") }
func (f *mockfile) Write([]byte) (int, error)  { return 0, f.s.µ("write") }
func (f *mockfile) Stat() (os.FileInfo, error) { return mockst, f.s.µ("stat") }

type mockstat struct{}

func (mockstat) Name() string       { return "mockstat" }
func (mockstat) Size() int64        { return 42 }
func (mockstat) Mode() os.FileMode  { return 0644 }
func (mockstat) ModTime() time.Time { return time.Now() }
func (mockstat) IsDir() bool        { return false }
func (mockstat) Sys() interface{}   { return nil }

func (s *cpSuite) TestCopySpecialFileSimple(c *C) {
	sync := testutil.MockCommand(c, "sync", "")
	defer sync.Restore()

	src := filepath.Join(c.MkDir(), "fifo")
	err := syscall.Mkfifo(src, 0644)
	c.Assert(err, IsNil)
	dir := c.MkDir()
	dst := filepath.Join(dir, "copied-fifo")

	err = osutil.CopySpecialFile(src, dst)
	c.Assert(err, IsNil)

	st, err := os.Stat(dst)
	c.Assert(err, IsNil)
	c.Check((st.Mode() & os.ModeNamedPipe), Equals, os.ModeNamedPipe)
	c.Check(sync.Calls(), DeepEquals, [][]string{{"sync", dir}})
}

func (s *cpSuite) TestCopySpecialFileErrors(c *C) {
	err := osutil.CopySpecialFile("no-such-file", "no-such-target")
	c.Assert(err, ErrorMatches, "failed to copy device node:.*cp:.*stat.*no-such-file.*")
}

func (s *cpSuite) TestCopyPreserveAll(c *C) {
	src := filepath.Join(c.MkDir(), "meep")
	dst := filepath.Join(c.MkDir(), "copied-meep")

	err := os.WriteFile(src, []byte(nil), 0644)
	c.Assert(err, IsNil)

	// Give the file a different mtime to ensure CopyFlagPreserveAll
	// really works.
	//
	// You wonder why "touch" is used? And want to me about
	// syscall.Utime()? Well, syscall not implemented on armhf
	// Aha, syscall.Utimes() then? No, not implemented on arm64
	// Really, this is a just a test, touch is good enough!
	err = exec.Command("touch", src, "-d", "2007-08-23 08:21:42").Run()
	c.Assert(err, IsNil)

	err = osutil.CopyFile(src, dst, osutil.CopyFlagPreserveAll)
	c.Assert(err, IsNil)

	// ensure that the mtime got preserved
	st1, err := os.Stat(src)
	c.Assert(err, IsNil)
	st2, err := os.Stat(dst)
	c.Assert(err, IsNil)
	c.Assert(st1.ModTime(), Equals, st2.ModTime())
}

func (s *cpSuite) TestCopyPreserveAllSync(c *C) {
	dir := c.MkDir()
	mocked := testutil.MockCommand(c, "cp", "").Also("sync", "")
	defer mocked.Restore()

	src := filepath.Join(dir, "meep")
	dst := filepath.Join(dir, "copied-meep")

	err := os.WriteFile(src, []byte(nil), 0644)
	c.Assert(err, IsNil)

	err = osutil.CopyFile(src, dst, osutil.CopyFlagPreserveAll|osutil.CopyFlagSync)
	c.Assert(err, IsNil)

	c.Check(mocked.Calls(), DeepEquals, [][]string{
		{"cp", "-av", src, dst},
		{"sync"},
	})
}

func (s *cpSuite) TestCopyPreserveAllSyncCpFailure(c *C) {
	dir := c.MkDir()
	mocked := testutil.MockCommand(c, "cp", "echo OUCH: cp failed.;exit 42").Also("sync", "")
	defer mocked.Restore()

	src := filepath.Join(dir, "meep")
	dst := filepath.Join(dir, "copied-meep")

	err := os.WriteFile(src, []byte(nil), 0644)
	c.Assert(err, IsNil)

	err = osutil.CopyFile(src, dst, osutil.CopyFlagPreserveAll|osutil.CopyFlagSync)
	c.Assert(err, ErrorMatches, `failed to copy all: "OUCH: cp failed." \(42\)`)
	c.Check(mocked.Calls(), DeepEquals, [][]string{
		{"cp", "-av", src, dst},
	})
}

func (s *cpSuite) TestCopyPreserveAllSyncSyncFailure(c *C) {
	dir := c.MkDir()
	mocked := testutil.MockCommand(c, "cp", "").Also("sync", "echo OUCH: sync failed.;exit 42")
	defer mocked.Restore()

	src := filepath.Join(dir, "meep")
	dst := filepath.Join(dir, "copied-meep")

	err := os.WriteFile(src, []byte(nil), 0644)
	c.Assert(err, IsNil)

	err = osutil.CopyFile(src, dst, osutil.CopyFlagPreserveAll|osutil.CopyFlagSync)
	c.Assert(err, ErrorMatches, `failed to sync: "OUCH: sync failed." \(42\)`)

	c.Check(mocked.Calls(), DeepEquals, [][]string{
		{"cp", "-av", src, dst},
		{"sync"},
	})
}

func (s *cpSuite) TestAtomicWriteFileCopySimple(c *C) {
	err := osutil.AtomicWriteFileCopy(s.f2, s.f1, 0)
	c.Assert(err, IsNil)
	c.Assert(s.f2, testutil.FileEquals, s.data)

}

func (s *cpSuite) TestAtomicWriteFileCopyPreservesModTime(c *C) {
	t := time.Date(2010, time.January, 1, 13, 0, 0, 0, time.UTC)
	c.Assert(os.Chtimes(s.f1, t, t), IsNil)

	err := osutil.AtomicWriteFileCopy(s.f2, s.f1, 0)
	c.Assert(err, IsNil)
	c.Assert(s.f2, testutil.FileEquals, s.data)

	finfo, err := os.Stat(s.f1)
	c.Assert(err, IsNil)
	m1 := finfo.ModTime()
	finfo, err = os.Stat(s.f2)
	c.Assert(err, IsNil)
	m2 := finfo.ModTime()
	c.Assert(m1.Equal(m2), Equals, true)
}

func (s *cpSuite) TestAtomicWriteFileCopyOverwrites(c *C) {
	err := os.WriteFile(s.f2, []byte("this is f2 content"), 0644)
	c.Assert(err, IsNil)

	err = osutil.AtomicWriteFileCopy(s.f2, s.f1, 0)
	c.Assert(err, IsNil)
	c.Assert(s.f2, testutil.FileEquals, s.data)
}

func (s *cpSuite) TestAtomicWriteFileCopySymlinks(c *C) {
	f2Symlink := filepath.Join(s.dir, "f2-symlink")
	err := os.Symlink(s.f2, f2Symlink)
	c.Assert(err, IsNil)

	f2SymlinkNoFollow := filepath.Join(s.dir, "f2-symlink-no-follow")
	err = os.Symlink(s.f2, f2SymlinkNoFollow)
	c.Assert(err, IsNil)

	// follows symlink, dst is f2
	err = osutil.AtomicWriteFileCopy(f2Symlink, s.f1, osutil.AtomicWriteFollow)
	c.Assert(err, IsNil)
	c.Check(osutil.IsSymlink(f2Symlink), Equals, true, Commentf("%q is not a symlink", f2Symlink))
	c.Check(s.f2, testutil.FileEquals, s.data)
	c.Check(f2SymlinkNoFollow, testutil.FileEquals, s.data)

	// when not following, copy overwrites the symlink
	err = osutil.AtomicWriteFileCopy(f2SymlinkNoFollow, s.f1, 0)
	c.Assert(err, IsNil)
	c.Check(osutil.IsSymlink(f2SymlinkNoFollow), Equals, false, Commentf("%q is not a file", f2SymlinkNoFollow))
	c.Check(f2SymlinkNoFollow, testutil.FileEquals, s.data)
}

func (s *cpSuite) TestAtomicWriteFileCopyErrReal(c *C) {
	err := osutil.AtomicWriteFileCopy(s.f2, filepath.Join(s.dir, "random-file"), 0)
	c.Assert(err, ErrorMatches, "unable to open source file .*/random-file: open .* no such file or directory")

	dir := c.MkDir()

	err = osutil.AtomicWriteFileCopy(filepath.Join(dir, "random-dir", "f3"), s.f1, 0)
	c.Assert(err, ErrorMatches, `cannot create atomic file: open .*/random-dir/f3\.[a-zA-Z0-9]+~: no such file or directory`)

	err = os.MkdirAll(filepath.Join(dir, "read-only"), 0000)
	c.Assert(err, IsNil)
	err = osutil.AtomicWriteFileCopy(filepath.Join(dir, "read-only", "f3"), s.f1, 0)
	c.Assert(err, ErrorMatches, `cannot create atomic file: open .*/read-only/f3\.[a-zA-Z0-9]+~: permission denied`)
}

func (s *cpSuite) TestAtomicWriteFileCopyErrMockedCopy(c *C) {
	s.mock()
	s.errs = []error{
		nil, // openFile
		nil, // src.Stat()
		errors.New("copy fail"),
	}

	err := osutil.AtomicWriteFileCopy(s.f2, s.f1, 0)
	c.Assert(err, ErrorMatches, `unable to copy .*/f1 to .*/f2\.[a-zA-Z0-9]+~: copy fail`)
	entries, err := filepath.Glob(filepath.Join(s.dir, "*"))
	c.Assert(err, IsNil)
	c.Assert(entries, DeepEquals, []string{
		filepath.Join(s.dir, "f1"),
	})
}
