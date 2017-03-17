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

package systemd_test

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"
	"time"

	. "gopkg.in/check.v1"
	"gopkg.in/yaml.v2"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/testutil"

	. "github.com/snapcore/snapd/systemd"
)

type testreporter struct {
	msgs []string
}

func (tr *testreporter) Notify(msg string) {
	tr.msgs = append(tr.msgs, msg)
}

// Hook up check.v1 into the "go test" runner
func Test(t *testing.T) { TestingT(t) }

// systemd's testsuite
type SystemdTestSuite struct {
	i      int
	argses [][]string
	errors []error
	outs   [][]byte

	j     int
	jsvcs [][]string
	jouts [][]byte
	jerrs []error

	rep *testreporter
}

var _ = Suite(&SystemdTestSuite{})

func (s *SystemdTestSuite) SetUpTest(c *C) {
	dirs.SetRootDir(c.MkDir())
	err := os.MkdirAll(dirs.SnapServicesDir, 0755)
	c.Assert(err, IsNil)

	// force UTC timezone, for reproducible timestamps
	os.Setenv("TZ", "")

	SystemctlCmd = s.myRun
	s.i = 0
	s.argses = nil
	s.errors = nil
	s.outs = nil

	JournalctlCmd = s.myJctl
	s.j = 0
	s.jsvcs = nil
	s.jouts = nil
	s.jerrs = nil

	s.rep = new(testreporter)
}

func (s *SystemdTestSuite) TearDownTest(c *C) {
	SystemctlCmd = SystemdRun
	JournalctlCmd = Jctl
}

func (s *SystemdTestSuite) myRun(args ...string) (out []byte, err error) {
	s.argses = append(s.argses, args)
	if s.i < len(s.outs) {
		out = s.outs[s.i]
	}
	if s.i < len(s.errors) {
		err = s.errors[s.i]
	}
	s.i++
	return out, err
}

func (s *SystemdTestSuite) myJctl(svcs []string) (out []byte, err error) {
	s.jsvcs = append(s.jsvcs, svcs)

	if s.j < len(s.jouts) {
		out = s.jouts[s.j]
	}
	if s.j < len(s.jerrs) {
		err = s.jerrs[s.j]
	}
	s.j++

	return out, err
}

func (s *SystemdTestSuite) TestDaemonReload(c *C) {
	err := New("", s.rep).DaemonReload()
	c.Assert(err, IsNil)
	c.Assert(s.argses, DeepEquals, [][]string{{"daemon-reload"}})
}

func (s *SystemdTestSuite) TestStart(c *C) {
	err := New("", s.rep).Start("foo")
	c.Assert(err, IsNil)
	c.Check(s.argses, DeepEquals, [][]string{{"start", "foo"}})
}

func (s *SystemdTestSuite) TestStop(c *C) {
	restore := MockStopDelays(time.Millisecond, 25*time.Second)
	defer restore()
	s.outs = [][]byte{
		nil, // for the "stop" itself
		[]byte("ActiveState=whatever\n"),
		[]byte("ActiveState=active\n"),
		[]byte("ActiveState=inactive\n"),
	}
	s.errors = []error{nil, nil, nil, nil, &Timeout{}}
	err := New("", s.rep).Stop("foo", 1*time.Second)
	c.Assert(err, IsNil)
	c.Assert(s.argses, HasLen, 4)
	c.Check(s.argses[0], DeepEquals, []string{"stop", "foo"})
	c.Check(s.argses[1], DeepEquals, []string{"show", "--property=ActiveState", "foo"})
	c.Check(s.argses[1], DeepEquals, s.argses[2])
	c.Check(s.argses[1], DeepEquals, s.argses[3])
}

func (s *SystemdTestSuite) TestStatus(c *C) {
	s.outs = [][]byte{
		[]byte("Id=Thing\nLoadState=LoadState\nActiveState=ActiveState\nSubState=SubState\nUnitFileState=UnitFileState\n"),
	}
	s.errors = []error{nil}
	out, err := New("", s.rep).Status("foo")
	c.Assert(err, IsNil)
	c.Check(out, Equals, "UnitFileState; LoadState; ActiveState (SubState)")
}

func (s *SystemdTestSuite) TestStatusObj(c *C) {
	s.outs = [][]byte{
		[]byte("Id=Thing\nLoadState=LoadState\nActiveState=ActiveState\nSubState=SubState\nUnitFileState=UnitFileState\n"),
	}
	s.errors = []error{nil}
	out, err := New("", s.rep).ServiceStatus("foo")
	c.Assert(err, IsNil)
	c.Check(out, DeepEquals, &ServiceStatus{
		ServiceFileName: "foo",
		LoadState:       "LoadState",
		ActiveState:     "ActiveState",
		SubState:        "SubState",
		UnitFileState:   "UnitFileState",
	})
}

func (s *SystemdTestSuite) TestStopTimeout(c *C) {
	restore := MockStopDelays(time.Millisecond, 25*time.Second)
	defer restore()
	err := New("", s.rep).Stop("foo", 10*time.Millisecond)
	c.Assert(err, FitsTypeOf, &Timeout{})
	c.Assert(len(s.rep.msgs) > 0, Equals, true)
	c.Check(s.rep.msgs[0], Equals, "Waiting for foo to stop.")
}

func (s *SystemdTestSuite) TestDisable(c *C) {
	err := New("xyzzy", s.rep).Disable("foo")
	c.Assert(err, IsNil)
	c.Check(s.argses, DeepEquals, [][]string{{"--root", "xyzzy", "disable", "foo"}})
}

func (s *SystemdTestSuite) TestEnable(c *C) {
	err := New("xyzzy", s.rep).Enable("foo")
	c.Assert(err, IsNil)
	c.Check(s.argses, DeepEquals, [][]string{{"--root", "xyzzy", "enable", "foo"}})
}

func (s *SystemdTestSuite) TestRestart(c *C) {
	restore := MockStopDelays(time.Millisecond, 25*time.Second)
	defer restore()
	s.outs = [][]byte{
		nil, // for the "stop" itself
		[]byte("ActiveState=inactive\n"),
		nil, // for the "start"
	}
	s.errors = []error{nil, nil, nil, nil, &Timeout{}}
	err := New("", s.rep).Restart("foo", 100*time.Millisecond)
	c.Assert(err, IsNil)
	c.Check(s.argses, HasLen, 3)
	c.Check(s.argses[0], DeepEquals, []string{"stop", "foo"})
	c.Check(s.argses[1], DeepEquals, []string{"show", "--property=ActiveState", "foo"})
	c.Check(s.argses[2], DeepEquals, []string{"start", "foo"})
}

func (s *SystemdTestSuite) TestKill(c *C) {
	c.Assert(New("", s.rep).Kill("foo", "HUP"), IsNil)
	c.Check(s.argses, DeepEquals, [][]string{{"kill", "foo", "-s", "HUP"}})
}

func (s *SystemdTestSuite) TestIsTimeout(c *C) {
	c.Check(IsTimeout(os.ErrInvalid), Equals, false)
	c.Check(IsTimeout(&Timeout{}), Equals, true)
}

func (s *SystemdTestSuite) TestLogErrJctl(c *C) {
	s.jerrs = []error{&Timeout{}}

	logs, err := New("", s.rep).Logs([]string{"foo"})
	c.Check(err, NotNil)
	c.Check(logs, IsNil)
	c.Check(s.jsvcs, DeepEquals, [][]string{{"foo"}})
	c.Check(s.j, Equals, 1)
}

func (s *SystemdTestSuite) TestLogErrJSON(c *C) {
	s.jouts = [][]byte{[]byte("this is not valid json.")}

	logs, err := New("", s.rep).Logs([]string{"foo"})
	c.Check(err, NotNil)
	c.Check(logs, IsNil)
	c.Check(s.jsvcs, DeepEquals, [][]string{{"foo"}})
	c.Check(s.j, Equals, 1)
}

func (s *SystemdTestSuite) TestLogs(c *C) {
	s.jouts = [][]byte{[]byte(`{"a": 1}
{"a": 2}
`)}

	logs, err := New("", s.rep).Logs([]string{"foo"})
	c.Check(err, IsNil)
	c.Check(logs, DeepEquals, []Log{{"a": 1.}, {"a": 2.}})
	c.Check(s.jsvcs, DeepEquals, [][]string{{"foo"}})
	c.Check(s.j, Equals, 1)
}

func (s *SystemdTestSuite) TestLogString(c *C) {
	c.Check(Log{}.String(), Equals, "-(no timestamp!)- - -")
	c.Check(Log{
		"__REALTIME_TIMESTAMP": 42,
	}.String(), Equals, "-(timestamp not a string: 42)- - -")
	c.Check(Log{
		"__REALTIME_TIMESTAMP": "what",
	}.String(), Equals, "-(timestamp not a decimal number: \"what\")- - -")
	c.Check(Log{
		"__REALTIME_TIMESTAMP": "0",
		"MESSAGE":              "hi",
	}.String(), Equals, "1970-01-01T00:00:00.000000Z - hi")
	c.Check(Log{
		"__REALTIME_TIMESTAMP": "42",
		"MESSAGE":              "hi",
		"SYSLOG_IDENTIFIER":    "me",
	}.String(), Equals, "1970-01-01T00:00:00.000042Z me hi")

}

func (s *SystemdTestSuite) TestMountUnitPath(c *C) {
	c.Assert(MountUnitPath("/apps/hello/1.1"), Equals, filepath.Join(dirs.SnapServicesDir, "apps-hello-1.1.mount"))
}

func (s *SystemdTestSuite) TestWriteMountUnit(c *C) {
	mockSnapPath := filepath.Join(c.MkDir(), "/var/lib/snappy/snaps/foo_1.0.snap")
	err := os.MkdirAll(filepath.Dir(mockSnapPath), 0755)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(mockSnapPath, nil, 0644)
	c.Assert(err, IsNil)

	mountUnitName, err := New("", nil).WriteMountUnitFile("foo", mockSnapPath, "/apps/foo/1.0", "squashfs")
	c.Assert(err, IsNil)
	defer os.Remove(mountUnitName)

	mount, err := ioutil.ReadFile(filepath.Join(dirs.SnapServicesDir, mountUnitName))
	c.Assert(err, IsNil)
	c.Assert(string(mount), Equals, fmt.Sprintf(`[Unit]
Description=Mount unit for foo

[Mount]
What=%s
Where=/apps/foo/1.0
Type=squashfs
Options=nodev

[Install]
WantedBy=multi-user.target
`, mockSnapPath))
}

func (s *SystemdTestSuite) TestWriteMountUnitForDirs(c *C) {
	// a directory instead of a file produces a different output
	snapDir := c.MkDir()
	mountUnitName, err := New("", nil).WriteMountUnitFile("foodir", snapDir, "/apps/foo/1.0", "squashfs")
	c.Assert(err, IsNil)
	defer os.Remove(mountUnitName)

	mount, err := ioutil.ReadFile(filepath.Join(dirs.SnapServicesDir, mountUnitName))
	c.Assert(err, IsNil)
	c.Assert(string(mount), Equals, fmt.Sprintf(`[Unit]
Description=Mount unit for foodir

[Mount]
What=%s
Where=/apps/foo/1.0
Type=none
Options=nodev,bind

[Install]
WantedBy=multi-user.target
`, snapDir))
}

func (s *SystemdTestSuite) TestRestartCondUnmarshal(c *C) {
	for cond := range RestartMap {
		bs := []byte(cond)
		var rc RestartCondition

		c.Check(yaml.Unmarshal(bs, &rc), IsNil)
		c.Check(rc, Equals, RestartMap[cond], Commentf(cond))
	}
}

func (s *SystemdTestSuite) TestRestartCondString(c *C) {
	for name, cond := range RestartMap {
		c.Check(cond.String(), Equals, name, Commentf(name))
	}
}

func (s *SystemdTestSuite) TestFuseInContainer(c *C) {
	if !osutil.FileExists("/dev/fuse") {
		c.Skip("No /dev/fuse on the system")
	}

	systemdCmd := testutil.MockCommand(c, "systemd-detect-virt", `
echo lxc
exit 0
	`)
	defer systemdCmd.Restore()

	fuseCmd := testutil.MockCommand(c, "squashfuse", `
exit 0
	`)
	defer fuseCmd.Restore()

	mockSnapPath := filepath.Join(c.MkDir(), "/var/lib/snappy/snaps/foo_1.0.snap")
	err := os.MkdirAll(filepath.Dir(mockSnapPath), 0755)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(mockSnapPath, nil, 0644)
	c.Assert(err, IsNil)

	mountUnitName, err := New("", nil).WriteMountUnitFile("foo", mockSnapPath, "/apps/foo/1.0", "squashfs")
	c.Assert(err, IsNil)
	defer os.Remove(mountUnitName)

	mount, err := ioutil.ReadFile(filepath.Join(dirs.SnapServicesDir, mountUnitName))
	c.Assert(err, IsNil)
	c.Assert(string(mount), Equals, fmt.Sprintf(`[Unit]
Description=Mount unit for foo

[Mount]
What=%s
Where=/apps/foo/1.0
Type=fuse.squashfuse
Options=nodev,ro,allow_other

[Install]
WantedBy=multi-user.target
`, mockSnapPath))
}

func (s *SystemdTestSuite) TestFuseOutsideContainer(c *C) {
	systemdCmd := testutil.MockCommand(c, "systemd-detect-virt", `
echo none
exit 0
	`)
	defer systemdCmd.Restore()

	fuseCmd := testutil.MockCommand(c, "squashfuse", `
exit 0
	`)
	defer fuseCmd.Restore()

	mockSnapPath := filepath.Join(c.MkDir(), "/var/lib/snappy/snaps/foo_1.0.snap")
	err := os.MkdirAll(filepath.Dir(mockSnapPath), 0755)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(mockSnapPath, nil, 0644)
	c.Assert(err, IsNil)

	mountUnitName, err := New("", nil).WriteMountUnitFile("foo", mockSnapPath, "/apps/foo/1.0", "squashfs")
	c.Assert(err, IsNil)
	defer os.Remove(mountUnitName)

	mount, err := ioutil.ReadFile(filepath.Join(dirs.SnapServicesDir, mountUnitName))
	c.Assert(err, IsNil)
	c.Assert(string(mount), Equals, fmt.Sprintf(`[Unit]
Description=Mount unit for foo

[Mount]
What=%s
Where=/apps/foo/1.0
Type=squashfs
Options=nodev

[Install]
WantedBy=multi-user.target
`, mockSnapPath))
}
