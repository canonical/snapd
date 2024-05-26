// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
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

package testutil

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/ddkwork/golibrary/mylog"
	"gopkg.in/check.v1"
)

type mockCommandSuite struct{}

var _ = check.Suite(&mockCommandSuite{})

const (
	UmountNoFollow = umountNoFollow
)

func (s *mockCommandSuite) TestMockCommand(c *check.C) {
	mock := MockCommand(c, "cmd", "true")
	defer mock.Restore()
	mylog.Check(exec.Command("cmd", "first-run", "--arg1", "arg2", "a space").Run())
	c.Assert(err, check.IsNil)
	mylog.Check(exec.Command("cmd", "second-run", "--arg1", "arg2", "a %s").Run())
	c.Assert(err, check.IsNil)
	c.Assert(mock.Calls(), check.DeepEquals, [][]string{
		{"cmd", "first-run", "--arg1", "arg2", "a space"},
		{"cmd", "second-run", "--arg1", "arg2", "a %s"},
	})
}

func (s *mockCommandSuite) TestMockCommandAlso(c *check.C) {
	mock := MockCommand(c, "fst", "")
	also := mock.Also("snd", "")
	defer mock.Restore()

	c.Assert(exec.Command("fst").Run(), check.IsNil)
	c.Assert(exec.Command("snd").Run(), check.IsNil)
	c.Check(mock.Calls(), check.DeepEquals, [][]string{{"fst"}, {"snd"}})
	c.Check(mock.Calls(), check.DeepEquals, also.Calls())
}

func (s *mockCommandSuite) TestMockCommandConflictEcho(c *check.C) {
	mock := MockCommand(c, "do-not-swallow-echo-args", "")
	defer mock.Restore()

	c.Assert(exec.Command("do-not-swallow-echo-args", "-E", "-n", "-e").Run(), check.IsNil)
	c.Assert(mock.Calls(), check.DeepEquals, [][]string{
		{"do-not-swallow-echo-args", "-E", "-n", "-e"},
	})
}

func (s *mockCommandSuite) TestMockShellchecksWhenAvailable(c *check.C) {
	tmpDir := c.MkDir()
	mockShellcheck := MockCommand(c, "shellcheck", fmt.Sprintf(`cat > %s/input`, tmpDir))
	defer mockShellcheck.Restore()

	restore := MockShellcheckPath(mockShellcheck.Exe())
	defer restore()

	mock := MockCommand(c, "some-command", "echo some-command")

	c.Assert(exec.Command("some-command").Run(), check.IsNil)

	c.Assert(mock.Calls(), check.DeepEquals, [][]string{
		{"some-command"},
	})
	c.Assert(mockShellcheck.Calls(), check.DeepEquals, [][]string{
		{"shellcheck", "-s", "bash", "-"},
	})

	scriptData := mylog.Check2(os.ReadFile(mock.Exe()))
	c.Assert(err, check.IsNil)
	c.Assert(string(scriptData), Contains, "\necho some-command\n")

	data := mylog.Check2(os.ReadFile(filepath.Join(tmpDir, "input")))
	c.Assert(err, check.IsNil)
	c.Assert(data, check.DeepEquals, scriptData)
}

func (s *mockCommandSuite) TestMockNoShellchecksWhenNotAvailable(c *check.C) {
	mockShellcheck := MockCommand(c, "shellcheck", `echo "i am not called"; exit 1`)
	defer mockShellcheck.Restore()

	restore := MockShellcheckPath("")
	defer restore()

	// This would fail with proper shellcheck due to SC2086: Double quote to
	// prevent globbing and word splitting.
	mock := MockCommand(c, "some-command", "echo $1")

	c.Assert(exec.Command("some-command").Run(), check.IsNil)

	c.Assert(mock.Calls(), check.DeepEquals, [][]string{
		{"some-command"},
	})
	c.Assert(mockShellcheck.Calls(), check.HasLen, 0)
}

func (s *mockCommandSuite) TestMockCreateAbsPathDir(c *check.C) {
	// this is an absolute path
	dir := c.MkDir()

	absPath := filepath.Join(dir, "this/is/nested/command")
	mock := MockCommand(c, absPath, "")

	c.Assert(exec.Command(absPath).Run(), check.IsNil)
	c.Assert(mock.Calls(), check.DeepEquals, [][]string{
		{"command"},
	})

	binDirRo := filepath.Join(dir, "ro")
	mylog.Check(os.MkdirAll(binDirRo, 0000))
	c.Assert(err, check.IsNil)
	absPathBad := filepath.Join(binDirRo, "this/fails/command")
	exp := fmt.Sprintf(`cannot create the directory for mocked command "%[1]s/ro/this/fails/command": mkdir %[1]s/ro/this: permission denied`, dir)
	c.Assert(func() { MockCommand(c, absPathBad, "") }, check.Panics, exp)
}
