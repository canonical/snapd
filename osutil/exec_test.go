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
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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

func (s *execSuite) makeMockLdSoConf(c *C) {
	ldSoConf := filepath.Join(dirs.SnapMountDir, "/core/current/etc/ld.so.conf")
	ldSoConfD := ldSoConf + ".d"

	err := os.MkdirAll(filepath.Dir(ldSoConf), 0755)
	c.Assert(err, IsNil)
	err = os.MkdirAll(ldSoConfD, 0755)
	c.Assert(err, IsNil)

	err = ioutil.WriteFile(ldSoConf, []byte("include /etc/ld.so.conf.d/*.conf"), 0644)
	c.Assert(err, IsNil)

	ldSoConf1 := filepath.Join(ldSoConfD, "x86_64-linux-gnu.conf")

	err = ioutil.WriteFile(ldSoConf1, []byte(`
# Multiarch support
/lib/x86_64-linux-gnu
/usr/lib/x86_64-linux-gnu`), 0644)
	c.Assert(err, IsNil)
}

func (s *execSuite) TestCommandFromCore(c *C) {
	s.makeMockLdSoConf(c)
	root := filepath.Join(dirs.SnapMountDir, "/core/current")

	os.MkdirAll(filepath.Join(root, "/usr/bin"), 0755)
	osutil.CopyFile(truePath, filepath.Join(root, "/usr/bin/xdelta3"), 0)
	cmd, err := osutil.CommandFromCore("/usr/bin/xdelta3", "--some-xdelta-arg")
	c.Assert(err, IsNil)

	out, err := exec.Command("/bin/sh", "-c", fmt.Sprintf("readelf -l %s |grep interpreter:|cut -f2 -d:|cut -f1 -d]", truePath)).Output()
	c.Assert(err, IsNil)
	interp := strings.TrimSpace(string(out))

	c.Check(cmd.Args, DeepEquals, []string{
		filepath.Join(root, interp),
		"--library-path",
		fmt.Sprintf("%s/lib/x86_64-linux-gnu:%s/usr/lib/x86_64-linux-gnu", root, root),
		filepath.Join(dirs.SnapMountDir, "/core/current/usr/bin/xdelta3"),
		"--some-xdelta-arg",
	})
}

func (s *execSuite) TestCommandFromCoreSymlinkCycle(c *C) {
	s.makeMockLdSoConf(c)
	root := filepath.Join(dirs.SnapMountDir, "/core/current")

	os.MkdirAll(filepath.Join(root, "/usr/bin"), 0755)
	osutil.CopyFile(truePath, filepath.Join(root, "/usr/bin/xdelta3"), 0)

	out, err := exec.Command("/bin/sh", "-c", "readelf -l /bin/true |grep interpreter:|cut -f2 -d:|cut -f1 -d]").Output()
	c.Assert(err, IsNil)
	interp := strings.TrimSpace(string(out))

	coreInterp := filepath.Join(root, interp)
	c.Assert(os.MkdirAll(filepath.Dir(coreInterp), 0755), IsNil)
	c.Assert(os.Symlink(filepath.Base(coreInterp), coreInterp), IsNil)

	_, err = osutil.CommandFromCore("/usr/bin/xdelta3", "--some-xdelta-arg")
	c.Assert(err, ErrorMatches, "cannot run command from core: symlink cycle found")

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
