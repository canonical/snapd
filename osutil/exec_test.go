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

package osutil_test

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"syscall"
	"time"

	. "gopkg.in/check.v1"
	"gopkg.in/tomb.v2"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
)

type execSuite struct{}

var _ = Suite(&execSuite{})

func (s *execSuite) SetUpTest(c *C) {
	dirs.SetRootDir(c.MkDir())
}

func (s *execSuite) TearDownTest(c *C) {
	dirs.SetRootDir("")
}

func (s *execSuite) TestRunAndWaitRunsAndWaits(c *C) {
	buf, err := osutil.RunAndWait([]string{"sh", "-c", "echo hello; sleep .1"}, nil, time.Second, &tomb.Tomb{})
	c.Assert(err, IsNil)
	c.Check(string(buf), Equals, "hello\n")
}

func (s *execSuite) TestRunAndWaitRunsSetsEnviron(c *C) {
	buf, err := osutil.RunAndWait([]string{"sh", "-c", "echo $FOO"}, []string{"FOO=42"}, time.Second, &tomb.Tomb{})
	c.Assert(err, IsNil)
	c.Check(string(buf), Equals, "42\n")
}

func (s *execSuite) TestRunAndWaitRunsAndKillsOnTimeout(c *C) {
	buf, err := osutil.RunAndWait([]string{"sleep", "1s"}, nil, time.Millisecond, &tomb.Tomb{})
	c.Check(err, ErrorMatches, "exceeded maximum runtime.*")
	c.Check(string(buf), Matches, "(?s).*exceeded maximum runtime.*")
}

func (s *execSuite) TestRunAndWaitRunsAndKillsOnAbort(c *C) {
	tmb := &tomb.Tomb{}
	go func() {
		time.Sleep(10 * time.Millisecond)
		tmb.Kill(nil)
	}()
	buf, err := osutil.RunAndWait([]string{"sleep", "1s"}, nil, time.Second, tmb)
	c.Check(err, ErrorMatches, "aborted.*")
	c.Check(string(buf), Matches, "(?s).*aborted.*")
}

func (s *execSuite) TestRunAndWaitKillImpatient(c *C) {
	defer osutil.MockSyscallKill(func(int, syscall.Signal) error { return nil })()
	defer osutil.MockCmdWaitTimeout(time.Millisecond)()

	buf, err := osutil.RunAndWait([]string{"sleep", "1s"}, nil, time.Millisecond, &tomb.Tomb{})
	c.Check(err, ErrorMatches, ".* did not stop")
	c.Check(string(buf), Equals, "")
}

func (s *execSuite) TestRunAndWaitExposesKillallError(c *C) {
	defer osutil.MockSyscallKill(func(p int, s syscall.Signal) error {
		syscall.Kill(p, s)
		return fmt.Errorf("xyzzy")
	})()
	defer osutil.MockCmdWaitTimeout(time.Millisecond)()

	_, err := osutil.RunAndWait([]string{"sleep", "1s"}, nil, time.Millisecond, &tomb.Tomb{})
	c.Check(err, ErrorMatches, "cannot abort: xyzzy")
}

func (s *execSuite) TestKillProcessGroupKillsProcessGroup(c *C) {
	pid := 0
	ppid := 0
	defer osutil.MockSyscallGetpgid(func(p int) (int, error) {
		ppid = p
		return syscall.Getpgid(p)
	})()
	defer osutil.MockSyscallKill(func(p int, s syscall.Signal) error {
		pid = p
		return syscall.Kill(p, s)
	})()

	cmd := exec.Command("sleep", "1m")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Start()
	defer cmd.Process.Kill()

	err := osutil.KillProcessGroup(cmd)
	c.Assert(err, IsNil)
	// process groups are passed to kill as negative numbers
	c.Check(pid, Equals, -ppid)
}

func (s *execSuite) TestKillProcessGroupShyOfInit(c *C) {
	defer osutil.MockSyscallGetpgid(func(int) (int, error) { return 1, nil })()

	cmd := exec.Command("sleep", "1m")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Start()
	defer cmd.Process.Kill()

	err := osutil.KillProcessGroup(cmd)
	c.Assert(err, ErrorMatches, "cannot kill pgid 1")
}

func (s *execSuite) TestStreamCommandHappy(c *C) {
	var buf bytes.Buffer
	stdout, err := osutil.StreamCommand("sh", "-c", "echo hello; sleep .1; echo bye")
	c.Assert(err, IsNil)
	_, err = io.Copy(&buf, stdout)
	c.Assert(err, IsNil)
	c.Check(buf.String(), Equals, "hello\nbye\n")

	wrf, wrc := osutil.WaitingReaderGuts(stdout)
	c.Assert(wrf, FitsTypeOf, &os.File{})
	// Depending on golang version the error is one of the two.
	c.Check(wrf.(*os.File).Close(), ErrorMatches, "invalid argument|file already closed")
	c.Check(wrc.ProcessState, NotNil) // i.e. already waited for
}

func (s *execSuite) TestStreamCommandSad(c *C) {
	var buf bytes.Buffer
	stdout, err := osutil.StreamCommand("false")
	c.Assert(err, IsNil)
	_, err = io.Copy(&buf, stdout)
	c.Assert(err, ErrorMatches, "exit status 1")
	c.Check(buf.String(), Equals, "")

	wrf, wrc := osutil.WaitingReaderGuts(stdout)
	c.Assert(wrf, FitsTypeOf, &os.File{})
	// Depending on golang version the error is one of the two.
	c.Check(wrf.(*os.File).Close(), ErrorMatches, "invalid argument|file already closed")
	c.Check(wrc.ProcessState, NotNil) // i.e. already waited for
}
