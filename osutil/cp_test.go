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

package osutil

import (
	"errors"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/testutil"
)

type cpSuite struct {
	dir  string
	f1   string
	f2   string
	data []byte
	log  []string
	errs []error
	idx  int
}

var _ = Suite(&cpSuite{})

func (s *cpSuite) mockCopyFile(fin, fout fileish, fi os.FileInfo) error {
	return s.µ("copyfile")
}

func (s *cpSuite) mockOpenFile(name string, flag int, perm os.FileMode) (fileish, error) {
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
	c.Assert(ioutil.WriteFile(s.f1, s.data, 0644), IsNil)
}

func (s *cpSuite) mock() {
	copyfile = s.mockCopyFile
	openfile = s.mockOpenFile
}

func (s *cpSuite) TearDownTest(c *C) {
	copyfile = doCopyFile
	openfile = doOpenFile
}

func (s *cpSuite) TestCp(c *C) {
	c.Check(CopyFile(s.f1, s.f2, CopyFlagDefault), IsNil)
	c.Check(s.f2, testutil.FileEquals, s.data)
}

func (s *cpSuite) TestCpNoOverwrite(c *C) {
	_, err := os.Create(s.f2)
	c.Assert(err, IsNil)
	c.Check(CopyFile(s.f1, s.f2, CopyFlagDefault), NotNil)
}

func (s *cpSuite) TestCpOverwrite(c *C) {
	_, err := os.Create(s.f2)
	c.Assert(err, IsNil)
	c.Check(CopyFile(s.f1, s.f2, CopyFlagOverwrite), IsNil)
	c.Check(s.f2, testutil.FileEquals, s.data)
}

func (s *cpSuite) TestCpOverwriteTruncates(c *C) {
	c.Assert(ioutil.WriteFile(s.f2, []byte("xxxxxxxxxxxxxxxx"), 0644), IsNil)
	c.Check(CopyFile(s.f1, s.f2, CopyFlagOverwrite), IsNil)
	c.Check(s.f2, testutil.FileEquals, s.data)
}

func (s *cpSuite) TestCpSync(c *C) {
	s.mock()
	c.Check(CopyFile(s.f1, s.f2, CopyFlagDefault), IsNil)
	c.Check(strings.Join(s.log, ":"), Not(Matches), `.*:sync(:.*)?`)

	s.log = nil
	c.Check(CopyFile(s.f1, s.f2, CopyFlagSync), IsNil)
	c.Check(strings.Join(s.log, ":"), Matches, `(.*:)?sync(:.*)?`)
}

func (s *cpSuite) TestCpCantOpen(c *C) {
	s.mock()
	s.errs = []error{errors.New("xyzzy"), nil}

	c.Check(CopyFile(s.f1, s.f2, CopyFlagSync), ErrorMatches, `unable to open \S+/f1: xyzzy`)
}

func (s *cpSuite) TestCpCantStat(c *C) {
	s.mock()
	s.errs = []error{nil, errors.New("xyzzy"), nil}

	c.Check(CopyFile(s.f1, s.f2, CopyFlagSync), ErrorMatches, `unable to stat \S+/f1: xyzzy`)
}

func (s *cpSuite) TestCpCantCreate(c *C) {
	s.mock()
	s.errs = []error{nil, nil, errors.New("xyzzy"), nil}

	c.Check(CopyFile(s.f1, s.f2, CopyFlagSync), ErrorMatches, `unable to create \S+/f2: xyzzy`)
}

func (s *cpSuite) TestCpCantCopy(c *C) {
	s.mock()
	s.errs = []error{nil, nil, nil, errors.New("xyzzy"), nil}

	c.Check(CopyFile(s.f1, s.f2, CopyFlagSync), ErrorMatches, `unable to copy \S+/f1 to \S+/f2: xyzzy`)
}

func (s *cpSuite) TestCpCantSync(c *C) {
	s.mock()
	s.errs = []error{nil, nil, nil, nil, errors.New("xyzzy"), nil}

	c.Check(CopyFile(s.f1, s.f2, CopyFlagSync), ErrorMatches, `unable to sync \S+/f2: xyzzy`)
}

func (s *cpSuite) TestCpCantStop2(c *C) {
	s.mock()
	s.errs = []error{nil, nil, nil, nil, nil, errors.New("xyzzy"), nil}

	c.Check(CopyFile(s.f1, s.f2, CopyFlagSync), ErrorMatches, `when closing \S+/f2: xyzzy`)
}

func (s *cpSuite) TestCpCantStop1(c *C) {
	s.mock()
	s.errs = []error{nil, nil, nil, nil, nil, nil, errors.New("xyzzy"), nil}

	c.Check(CopyFile(s.f1, s.f2, CopyFlagSync), ErrorMatches, `when closing \S+/f1: xyzzy`)
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

	err = CopySpecialFile(src, dst)
	c.Assert(err, IsNil)

	st, err := os.Stat(dst)
	c.Assert(err, IsNil)
	c.Check((st.Mode() & os.ModeNamedPipe), Equals, os.ModeNamedPipe)
	c.Check(sync.Calls(), DeepEquals, [][]string{{"sync", dir}})
}

func (s *cpSuite) TestCopySpecialFileErrors(c *C) {
	err := CopySpecialFile("no-such-file", "no-such-target")
	c.Assert(err, ErrorMatches, "failed to copy device node:.*cp:.*stat.*no-such-file.*")
}

func (s *cpSuite) TestCopyPreserveAll(c *C) {
	src := filepath.Join(c.MkDir(), "meep")
	dst := filepath.Join(c.MkDir(), "copied-meep")

	err := ioutil.WriteFile(src, []byte(nil), 0644)
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

	err = CopyFile(src, dst, CopyFlagPreserveAll)
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

	err := ioutil.WriteFile(src, []byte(nil), 0644)
	c.Assert(err, IsNil)

	err = CopyFile(src, dst, CopyFlagPreserveAll|CopyFlagSync)
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

	err := ioutil.WriteFile(src, []byte(nil), 0644)
	c.Assert(err, IsNil)

	err = CopyFile(src, dst, CopyFlagPreserveAll|CopyFlagSync)
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

	err := ioutil.WriteFile(src, []byte(nil), 0644)
	c.Assert(err, IsNil)

	err = CopyFile(src, dst, CopyFlagPreserveAll|CopyFlagSync)
	c.Assert(err, ErrorMatches, `failed to sync: "OUCH: sync failed." \(42\)`)

	c.Check(mocked.Calls(), DeepEquals, [][]string{
		{"cp", "-av", src, dst},
		{"sync"},
	})
}
