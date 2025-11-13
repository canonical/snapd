// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2018 Canonical Ltd
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

package strace_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil/strace"
	"github.com/snapcore/snapd/osutil/user"
	"github.com/snapcore/snapd/testutil"
)

// Hook up check.v1 into the "go test" runner
func Test(t *testing.T) { TestingT(t) }

type straceSuite struct {
	rootdir    string
	mockSudo   *testutil.MockCmd
	mockStrace *testutil.MockCmd
}

var _ = Suite(&straceSuite{})

func (s *straceSuite) SetUpTest(c *C) {
	s.rootdir = c.MkDir()
	dirs.SetRootDir(s.rootdir)

	s.mockSudo = testutil.MockCommand(c, "sudo", "")
	s.mockStrace = testutil.MockCommand(c, "strace", "")
}

func (s *straceSuite) TearDownTest(c *C) {
	dirs.SetRootDir("/")
	s.mockSudo.Restore()
	s.mockStrace.Restore()
}

func (s *straceSuite) TestStraceCommandHappy(c *C) {
	cmd, err := strace.Command(nil)
	c.Assert(err, IsNil)
	c.Assert(cmd.Path, Equals, s.mockSudo.Exe())
	c.Assert(cmd.Args, DeepEquals, []string{
		s.mockSudo.Exe(), "--",
		s.mockStrace.Exe(),
		"-f",
		"-e", strace.ExcludedSyscalls,
	})

	cmd, err = strace.CommandWithTraceePid(123, nil)
	c.Assert(err, IsNil)
	c.Assert(cmd.Path, Equals, s.mockSudo.Exe())
	c.Assert(cmd.Args, DeepEquals, []string{
		s.mockSudo.Exe(), "--",
		s.mockStrace.Exe(),
		"-f",
		"-e", strace.ExcludedSyscalls,
		"-p", "123",
	})
}

func (s *straceSuite) TestStraceCommandHappyFromSnap(c *C) {
	straceStaticPath := filepath.Join(dirs.SnapMountDir, "strace-static", "current", "bin", "strace")
	err := os.MkdirAll(filepath.Dir(straceStaticPath), 0755)
	c.Assert(err, IsNil)
	mockStraceStatic := testutil.MockCommand(c, straceStaticPath, "")
	defer mockStraceStatic.Restore()

	cmd, err := strace.Command(nil)
	c.Assert(err, IsNil)
	c.Check(cmd.Path, Equals, s.mockSudo.Exe())
	c.Check(cmd.Args, DeepEquals, []string{
		s.mockSudo.Exe(), "--",
		mockStraceStatic.Exe(),
		"-f",
		"-e", strace.ExcludedSyscalls,
	})
}

func (s *straceSuite) TestStraceCommandNoSudo(c *C) {
	tmp := c.MkDir()

	if user.GetentBased {
		getEntPath, err := exec.LookPath("getent")
		c.Assert(err, IsNil)
		err = os.Symlink(getEntPath, filepath.Join(tmp, "getent"))
		c.Assert(err, IsNil)
	}

	origPath := os.Getenv("PATH")
	defer func() { os.Setenv("PATH", origPath) }()
	os.Setenv("PATH", tmp)

	_, err := strace.Command(nil)
	c.Assert(err, ErrorMatches, `cannot use strace without sudo: exec: "sudo": executable file not found in \$PATH`)
}

func (s *straceSuite) TestStraceCommandNoStrace(c *C) {
	tmp := c.MkDir()

	if user.GetentBased {
		getEntPath, err := exec.LookPath("getent")
		c.Assert(err, IsNil)
		err = os.Symlink(getEntPath, filepath.Join(tmp, "getent"))
		c.Assert(err, IsNil)
	}

	origPath := os.Getenv("PATH")
	defer func() { os.Setenv("PATH", origPath) }()

	os.Setenv("PATH", tmp)
	err := os.WriteFile(filepath.Join(tmp, "sudo"), nil, 0755)
	c.Assert(err, IsNil)

	_, err = strace.Command(nil)
	c.Assert(err, ErrorMatches, `cannot find an installed strace, please try 'snap install strace-static'`)
}

func (s *straceSuite) TestTraceExecCommand(c *C) {
	cmd, err := strace.TraceExecCommandForPid(123, "/run/snapd/strace.log")
	c.Assert(err, IsNil)
	c.Assert(cmd.Path, Equals, s.mockSudo.Exe())
	c.Assert(cmd.Args, DeepEquals, []string{
		s.mockSudo.Exe(), "--",
		s.mockStrace.Exe(),
		"-f",
		"-e", strace.ExcludedSyscalls,
		// timing specific trace
		"-ttt",
		"-e", "trace=execve,execveat",
		"-o", "/run/snapd/strace.log",
		"-p", "123",
	})

}
