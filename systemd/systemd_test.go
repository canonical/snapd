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

package systemd

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	. "launchpad.net/gocheck"
)

type testreporter struct {
	msgs []string
}

func (tr *testreporter) Notify(msg string) {
	tr.msgs = append(tr.msgs, msg)
}

// Hook up gocheck into the "go test" runner
func Test(t *testing.T) { TestingT(t) }

// systemd's testsuite
type SystemdTestSuite struct {
	i      int
	argses [][]string
	errors []error
	outs   [][]byte
	rep    *testreporter
}

var _ = Suite(&SystemdTestSuite{})

func (s *SystemdTestSuite) SetUpTest(c *C) {
	SystemctlCmd = s.myRun
	s.i = 0
	s.argses = nil
	s.errors = nil
	s.outs = nil
	s.rep = new(testreporter)
}

func (s *SystemdTestSuite) TearDownTest(c *C) {
	SystemctlCmd = run
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

func (s *SystemdTestSuite) errorRun(args ...string) (out []byte, err error) {
	return nil, &Error{cmd: args, exitCode: 1, msg: []byte("error on error")}
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
	s.outs = [][]byte{
		nil, // for the "stop" itself
		[]byte("ActiveState=whatever\n"),
		[]byte("ActiveState=active\n"),
		[]byte("ActiveState=inactive\n"),
	}
	s.errors = []error{nil, nil, nil, nil, &Timeout{}}
	err := New("", s.rep).Stop("foo", time.Millisecond)
	c.Assert(err, IsNil)
	c.Assert(s.argses, HasLen, 4)
	c.Check(s.argses[0], DeepEquals, []string{"stop", "foo"})
	c.Check(s.argses[1], DeepEquals, []string{"show", "--property=ActiveState", "foo"})
	c.Check(s.argses[1], DeepEquals, s.argses[2])
	c.Check(s.argses[1], DeepEquals, s.argses[3])
}

func (s *SystemdTestSuite) TestStopTimeout(c *C) {
	oldSteps := stopSteps
	oldDelay := stopDelay
	stopSteps = 2
	stopDelay = time.Millisecond
	defer func() {
		stopSteps = oldSteps
		stopDelay = oldDelay
	}()

	err := New("", s.rep).Stop("foo", 10*time.Millisecond)
	c.Assert(err, FitsTypeOf, &Timeout{})
	c.Check(s.rep.msgs[0], Equals, "Waiting for foo to stop.")
}

func (s *SystemdTestSuite) TestDisable(c *C) {
	err := New("xyzzy", s.rep).Disable("foo")
	c.Assert(err, IsNil)
	c.Check(s.argses, DeepEquals, [][]string{{"--root", "xyzzy", "disable", "foo"}})
}

func (s *SystemdTestSuite) TestEnable(c *C) {
	sysd := New("xyzzy", s.rep)
	sysd.(*systemd).rootDir = c.MkDir()
	err := os.MkdirAll(filepath.Join(sysd.(*systemd).rootDir, "/etc/systemd/system/multi-user.target.wants"), 0755)
	c.Assert(err, IsNil)

	err = sysd.Enable("foo")
	c.Assert(err, IsNil)

	// check symlink
	enableLink := filepath.Join(sysd.(*systemd).rootDir, "/etc/systemd/system/multi-user.target.wants/foo")
	target, err := os.Readlink(enableLink)
	c.Assert(err, IsNil)
	c.Assert(target, Equals, "/etc/systemd/system/foo")
}

const expectedServiceFmt = `[Unit]
Description=descr
%s
X-Snappy=yes

[Service]
ExecStart=/usr/bin/ubuntu-core-launcher app_service aa-profile /apps/app/1.0/bin/start
WorkingDirectory=/apps/app/1.0/
Environment="SNAPP_APP_PATH=/apps/app/1.0/" "SNAPP_APP_DATA_PATH=/var/lib/apps/app/1.0/" "SNAPP_APP_USER_DATA_PATH=%%h/apps/app/1.0/" "SNAP_APP_PATH=/apps/app/1.0/" "SNAP_APP_DATA_PATH=/var/lib/apps/app/1.0/" "SNAP_APP_USER_DATA_PATH=%%h/apps/app/1.0/" "SNAP_APP=app_service_1.0" "TMPDIR=/tmp/snaps/app/1.0/tmp" "SNAP_APP_TMPDIR=/tmp/snaps/app/1.0/tmp"
ExecStop=/apps/app/1.0/bin/stop
ExecStopPost=/apps/app/1.0/bin/stop --post
TimeoutStopSec=10
%s

[Install]
WantedBy=multi-user.target
`

var (
	expectedAppService  = fmt.Sprintf(expectedServiceFmt, "After=ubuntu-snappy.frameworks.target\nRequires=ubuntu-snappy.frameworks.target", "\n")
	expectedFmkService  = fmt.Sprintf(expectedServiceFmt, "Before=ubuntu-snappy.frameworks.target\nAfter=ubuntu-snappy.frameworks-pre.target\nRequires=ubuntu-snappy.frameworks-pre.target", "\n")
	expectedDbusService = fmt.Sprintf(expectedServiceFmt, "After=ubuntu-snappy.frameworks.target\nRequires=ubuntu-snappy.frameworks.target", "BusName=foo.bar.baz\nType=dbus")
)

func (s *SystemdTestSuite) TestGenAppServiceFile(c *C) {

	desc := &ServiceDescription{
		AppName:     "app",
		ServiceName: "service",
		Version:     "1.0",
		Description: "descr",
		AppPath:     "/apps/app/1.0/",
		Start:       "bin/start",
		Stop:        "bin/stop",
		PostStop:    "bin/stop --post",
		StopTimeout: time.Duration(10 * time.Second),
		AaProfile:   "aa-profile",
	}

	c.Check(New("", nil).GenServiceFile(desc), Equals, expectedAppService)
}

func (s *SystemdTestSuite) TestGenFmkServiceFile(c *C) {

	desc := &ServiceDescription{
		AppName:     "app",
		ServiceName: "service",
		Version:     "1.0",
		Description: "descr",
		AppPath:     "/apps/app/1.0/",
		Start:       "bin/start",
		Stop:        "bin/stop",
		PostStop:    "bin/stop --post",
		StopTimeout: time.Duration(10 * time.Second),
		AaProfile:   "aa-profile",
		IsFramework: true,
	}

	c.Check(New("", nil).GenServiceFile(desc), Equals, expectedFmkService)
}

func (s *SystemdTestSuite) TestGenServiceFileWithBusName(c *C) {

	desc := &ServiceDescription{
		AppName:     "app",
		ServiceName: "service",
		Version:     "1.0",
		Description: "descr",
		AppPath:     "/apps/app/1.0/",
		Start:       "bin/start",
		Stop:        "bin/stop",
		PostStop:    "bin/stop --post",
		StopTimeout: time.Duration(10 * time.Second),
		AaProfile:   "aa-profile",
		BusName:     "foo.bar.baz",
	}

	generated := New("", nil).GenServiceFile(desc)
	c.Assert(generated, Equals, expectedDbusService)
}

func (s *SystemdTestSuite) TestRestart(c *C) {
	s.outs = [][]byte{
		nil, // for the "stop" itself
		[]byte("ActiveState=inactive\n"),
		nil, // for the "start"
	}
	s.errors = []error{nil, nil, nil, nil, &Timeout{}}
	err := New("", s.rep).Restart("foo", time.Millisecond)
	c.Assert(err, IsNil)
	c.Check(s.argses, HasLen, 3)
	c.Check(s.argses[0], DeepEquals, []string{"stop", "foo"})
	c.Check(s.argses[1], DeepEquals, []string{"show", "--property=ActiveState", "foo"})
	c.Check(s.argses[2], DeepEquals, []string{"start", "foo"})
}
