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
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"
	"time"

	. "gopkg.in/check.v1"

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

	j        int
	jns      []string
	jsvcs    [][]string
	jouts    [][]byte
	jerrs    []error
	jfollows []bool

	rep *testreporter

	restore func()
}

var _ = Suite(&SystemdTestSuite{})

func (s *SystemdTestSuite) SetUpTest(c *C) {
	dirs.SetRootDir(c.MkDir())
	err := os.MkdirAll(dirs.SnapServicesDir, 0755)
	c.Assert(err, IsNil)

	// force UTC timezone, for reproducible timestamps
	os.Setenv("TZ", "")

	s.restore = MockSystemctl(s.myRun)
	s.i = 0
	s.argses = nil
	s.errors = nil
	s.outs = nil

	JournalctlCmd = s.myJctl
	s.j = 0
	s.jns = nil
	s.jsvcs = nil
	s.jouts = nil
	s.jerrs = nil
	s.jfollows = nil

	s.rep = new(testreporter)
}

func (s *SystemdTestSuite) TearDownTest(c *C) {
	JournalctlCmd = Jctl
	s.restore()
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

func (s *SystemdTestSuite) myJctl(svcs []string, n string, follow bool) (io.ReadCloser, error) {
	var err error
	var out []byte

	s.jns = append(s.jns, n)
	s.jsvcs = append(s.jsvcs, svcs)
	s.jfollows = append(s.jfollows, follow)

	if s.j < len(s.jouts) {
		out = s.jouts[s.j]
	}
	if s.j < len(s.jerrs) {
		err = s.jerrs[s.j]
	}
	s.j++

	if out == nil {
		return nil, err
	}

	return ioutil.NopCloser(bytes.NewReader(out)), err
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
		[]byte(`
Type=simple
Id=foo.service
ActiveState=active
UnitFileState=enabled

Type=simple
Id=bar.service
ActiveState=reloading
UnitFileState=static

Type=potato
Id=baz.service
ActiveState=inactive
UnitFileState=disabled
`[1:]),
	}
	s.errors = []error{nil}
	out, err := New("", s.rep).Status("foo.service", "bar.service", "baz.service")
	c.Assert(err, IsNil)
	c.Check(out, DeepEquals, []*ServiceStatus{
		{
			Daemon:          "simple",
			ServiceFileName: "foo.service",
			Active:          true,
			Enabled:         true,
		}, {
			Daemon:          "simple",
			ServiceFileName: "bar.service",
			Active:          true,
			Enabled:         true,
		}, {
			Daemon:          "potato",
			ServiceFileName: "baz.service",
			Active:          false,
			Enabled:         false,
		},
	})
	c.Check(s.rep.msgs, IsNil)
	c.Assert(s.argses, DeepEquals, [][]string{{"show", "--property=Id,Type,ActiveState,UnitFileState", "foo.service", "bar.service", "baz.service"}})
}

func (s *SystemdTestSuite) TestStatusBadNumberOfValues(c *C) {
	s.outs = [][]byte{
		[]byte(`
Type=simple
Id=foo.service
ActiveState=active
UnitFileState=enabled

Type=simple
Id=foo.service
ActiveState=active
UnitFileState=enabled
`[1:]),
	}
	s.errors = []error{nil}
	out, err := New("", s.rep).Status("foo.service")
	c.Check(err, ErrorMatches, "cannot get service status: expected 1 results, got 2")
	c.Check(out, IsNil)
	c.Check(s.rep.msgs, IsNil)
}

func (s *SystemdTestSuite) TestStatusBadLine(c *C) {
	s.outs = [][]byte{
		[]byte(`
Type=simple
Id=foo.service
ActiveState=active
UnitFileState=enabled
Potatoes
`[1:]),
	}
	s.errors = []error{nil}
	out, err := New("", s.rep).Status("foo.service")
	c.Assert(err, ErrorMatches, `.* bad line "Potatoes" .*`)
	c.Check(out, IsNil)
}

func (s *SystemdTestSuite) TestStatusBadId(c *C) {
	s.outs = [][]byte{
		[]byte(`
Type=simple
Id=bar.service
ActiveState=active
UnitFileState=enabled
`[1:]),
	}
	s.errors = []error{nil}
	out, err := New("", s.rep).Status("foo.service")
	c.Assert(err, ErrorMatches, `.* queried status of "foo.service" but got status of "bar.service"`)
	c.Check(out, IsNil)
}

func (s *SystemdTestSuite) TestStatusBadField(c *C) {
	s.outs = [][]byte{
		[]byte(`
Type=simple
Id=foo.service
ActiveState=active
UnitFileState=enabled
Potatoes=false
`[1:]),
	}
	s.errors = []error{nil}
	out, err := New("", s.rep).Status("foo.service")
	c.Assert(err, ErrorMatches, `.* unexpected field "Potatoes" .*`)
	c.Check(out, IsNil)
}

func (s *SystemdTestSuite) TestStatusMissingField(c *C) {
	s.outs = [][]byte{
		[]byte(`
Type=simple
Id=foo.service
ActiveState=active
`[1:]),
	}
	s.errors = []error{nil}
	out, err := New("", s.rep).Status("foo.service")
	c.Assert(err, ErrorMatches, `.* missing UnitFileState .*`)
	c.Check(out, IsNil)
}

func (s *SystemdTestSuite) TestStatusDupeField(c *C) {
	s.outs = [][]byte{
		[]byte(`
Type=simple
Id=foo.service
ActiveState=active
ActiveState=active
UnitFileState=enabled
`[1:]),
	}
	s.errors = []error{nil}
	out, err := New("", s.rep).Status("foo.service")
	c.Assert(err, ErrorMatches, `.* duplicate field "ActiveState" .*`)
	c.Check(out, IsNil)
}

func (s *SystemdTestSuite) TestStatusEmptyField(c *C) {
	s.outs = [][]byte{
		[]byte(`
Type=simple
Id=
ActiveState=active
UnitFileState=enabled
`[1:]),
	}
	s.errors = []error{nil}
	out, err := New("", s.rep).Status("foo.service")
	c.Assert(err, ErrorMatches, `.* empty field "Id" .*`)
	c.Check(out, IsNil)
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

	reader, err := New("", s.rep).LogReader([]string{"foo"}, "24", false)
	c.Check(err, NotNil)
	c.Check(reader, IsNil)
	c.Check(s.jns, DeepEquals, []string{"24"})
	c.Check(s.jsvcs, DeepEquals, [][]string{{"foo"}})
	c.Check(s.jfollows, DeepEquals, []bool{false})
	c.Check(s.j, Equals, 1)
}

func (s *SystemdTestSuite) TestLogs(c *C) {
	expected := `{"a": 1}
{"a": 2}
`
	s.jouts = [][]byte{[]byte(expected)}

	reader, err := New("", s.rep).LogReader([]string{"foo"}, "24", false)
	c.Check(err, IsNil)
	logs, err := ioutil.ReadAll(reader)
	c.Assert(err, IsNil)
	c.Check(string(logs), Equals, expected)
	c.Check(s.jns, DeepEquals, []string{"24"})
	c.Check(s.jsvcs, DeepEquals, [][]string{{"foo"}})
	c.Check(s.jfollows, DeepEquals, []bool{false})
	c.Check(s.j, Equals, 1)
}

func (s *SystemdTestSuite) TestLogPID(c *C) {
	c.Check(Log{}.PID(), Equals, "-")
	c.Check(Log{"_PID": "99"}.PID(), Equals, "99")
	c.Check(Log{"SYSLOG_PID": "99"}.PID(), Equals, "99")
	// things starting with underscore are "trusted", so we trust
	// them more than the user-settable ones:
	c.Check(Log{"_PID": "42", "SYSLOG_PID": "99"}.PID(), Equals, "42")
}

func (s *SystemdTestSuite) TestTime(c *C) {
	t, err := Log{}.Time()
	c.Check(t.IsZero(), Equals, true)
	c.Check(err, ErrorMatches, "no timestamp")

	t, err = Log{"__REALTIME_TIMESTAMP": "what"}.Time()
	c.Check(t.IsZero(), Equals, true)
	c.Check(err, ErrorMatches, `timestamp not a decimal number: "what"`)

	t, err = Log{"__REALTIME_TIMESTAMP": "0"}.Time()
	c.Check(err, IsNil)
	c.Check(t.String(), Equals, "1970-01-01 00:00:00 +0000 UTC")

	t, err = Log{"__REALTIME_TIMESTAMP": "42"}.Time()
	c.Check(err, IsNil)
	c.Check(t.String(), Equals, "1970-01-01 00:00:00.000042 +0000 UTC")
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
Options=nodev,ro

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
Options=nodev,ro,bind

[Install]
WantedBy=multi-user.target
`, snapDir))
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
Options=nodev,ro

[Install]
WantedBy=multi-user.target
`, mockSnapPath))
}

func (s *SystemdTestSuite) TestJctl(c *C) {
	var args []string
	var err error
	MockOsutilStreamCommand(func(name string, myargs ...string) (io.ReadCloser, error) {
		c.Check(cap(myargs) <= len(myargs)+1, Equals, true, Commentf("cap:%d, len:%d", cap(myargs), len(myargs)))
		args = myargs
		return nil, nil
	})

	_, err = Jctl([]string{"foo", "bar"}, "10", false)
	c.Assert(err, IsNil)
	c.Check(args, DeepEquals, []string{"-o", "json", "-n", "10", "--no-pager", "-u", "foo", "-u", "bar"})
	_, err = Jctl([]string{"foo", "bar", "baz"}, "99", true)
	c.Assert(err, IsNil)
	c.Check(args, DeepEquals, []string{"-o", "json", "-n", "99", "--no-pager", "-f", "-u", "foo", "-u", "bar", "-u", "baz"})
}
