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
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/gadget/quantity"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/osutil/squashfs"
	"github.com/snapcore/snapd/sandbox/selinux"
	"github.com/snapcore/snapd/systemd"
	. "github.com/snapcore/snapd/systemd"
	"github.com/snapcore/snapd/testutil"
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
	// 'systemctl' command except for stop. These all
	// run synchronously so they can share attributes.
	i      int
	argses [][]string
	errors []error
	outs   [][]byte
	// 'systemctl stop' command requires some special
	// mocking because it runs in its own thread. We
	// have to track its own set of attributes otherwise
	// the unit test arguments checks will intermittendly
	// fail due to the random completion ordering. We
	// do not care about output for 'systemctl stop'.
	stopDelays []time.Duration
	stopArgses [][]string
	stopErrors []error
	stopIter   int

	j           int
	jns         []string
	jsvcs       [][]string
	jouts       [][]byte
	jerrs       []error
	jfollows    []bool
	jnamespaces []bool

	rep *testreporter

	restoreSystemctl  func()
	restoreJournalctl func()
	restoreSELinux    func()
}

var _ = Suite(&SystemdTestSuite{})

func (s *SystemdTestSuite) SetUpTest(c *C) {
	dirs.SetRootDir(c.MkDir())
	err := os.MkdirAll(dirs.SnapServicesDir, 0755)
	c.Assert(err, IsNil)
	err = os.MkdirAll(dirs.SnapRuntimeServicesDir, 0755)
	c.Assert(err, IsNil)
	c.Assert(os.MkdirAll(filepath.Join(dirs.SnapServicesDir, "snapd.mounts.target.wants"), 0755), IsNil)
	// This is only to handle older version of snapd
	c.Assert(os.MkdirAll(filepath.Join(dirs.SnapServicesDir, "multi-user.target.wants"), 0755), IsNil)

	// force UTC timezone, for reproducible timestamps
	os.Setenv("TZ", "")

	s.restoreSystemctl = MockSystemctlWithDelay(s.myRun)
	s.i = 0
	s.argses = nil
	s.errors = nil
	s.outs = nil
	s.stopDelays = nil
	s.stopArgses = nil
	s.stopErrors = nil
	s.stopIter = 0

	s.restoreJournalctl = MockJournalctl(s.myJctl)
	s.j = 0
	s.jns = nil
	s.jsvcs = nil
	s.jouts = nil
	s.jerrs = nil
	s.jfollows = nil
	s.jnamespaces = nil

	s.rep = new(testreporter)

	s.restoreSELinux = selinux.MockIsEnabled(func() (bool, error) { return false, nil })
}

func (s *SystemdTestSuite) TearDownTest(c *C) {
	s.restoreSystemctl()
	s.restoreJournalctl()
	s.restoreSELinux()
}

func (s *SystemdTestSuite) myRun(args ...string) (out []byte, delay time.Duration, err error) {
	// Default completion time for 'systemctl' operations
	delayReq := time.Duration(0)

	// We deal with 'systemctl stop' separately because this is a
	// blocking call running inside a go routine (separate thread)
	if args[0] == "stop" {
		// 'systemctl stop' cases
		s.stopArgses = append(s.stopArgses, args)
		if s.stopIter < len(s.stopErrors) {
			err = s.stopErrors[s.stopIter]
		}
		if s.stopIter < len(s.stopDelays) {
			delayReq = s.stopDelays[s.stopIter]
		}
		s.stopIter++
	} else {
		// Other 'systemctl' cases
		s.argses = append(s.argses, args)
		if s.i < len(s.outs) {
			out = s.outs[s.i]
		}
		if s.i < len(s.errors) {
			err = s.errors[s.i]
		}
		s.i++
	}

	return out, delayReq, err
}

func (s *SystemdTestSuite) myJctl(svcs []string, n int, follow, namespaces bool) (io.ReadCloser, error) {
	var err error
	var out []byte

	s.jns = append(s.jns, strconv.Itoa(n))
	s.jsvcs = append(s.jsvcs, svcs)
	s.jfollows = append(s.jfollows, follow)
	s.jnamespaces = append(s.jnamespaces, namespaces)

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

func (s *SystemdTestSuite) TestMockVersion(c *C) {
	for _, tc := range []struct {
		version int
		err     error
	}{
		{0, errors.New("some error")},
		{123, nil},
	} {
		restore := MockSystemdVersion(tc.version, tc.err)
		defer restore()

		version, err := Version()
		c.Check(version, Equals, tc.version)
		c.Check(err, Equals, tc.err)
	}
}

func (s *SystemdTestSuite) TestEnsureAtLeastFail(c *C) {
	for _, tc := range []struct {
		requiredVersion int
		mockedVersion   int
		mockedErr       error
		expectedErr     string
	}{
		{123, 0, errors.New("some error"), "some error"},
		{140, 139, nil, `systemd version 139 is too old \(expected at least 140\)`},
		{149, 150, nil, ""},
	} {
		restore := MockSystemdVersion(tc.mockedVersion, tc.mockedErr)
		defer restore()

		err := EnsureAtLeast(tc.requiredVersion)
		if tc.expectedErr != "" {
			c.Check(err, ErrorMatches, tc.expectedErr)
		} else {
			c.Check(err, IsNil)
		}
	}
}

func (s *SystemdTestSuite) TestBackend(c *C) {
	c.Check(New(SystemMode, s.rep).Backend(), Equals, RunningSystemdBackend)
}

func (s *SystemdTestSuite) TestDaemonReload(c *C) {
	err := New(SystemMode, s.rep).DaemonReload()
	c.Assert(err, IsNil)
	c.Assert(s.argses, DeepEquals, [][]string{{"daemon-reload"}})
}

func (s *SystemdTestSuite) TestDaemonReexec(c *C) {
	err := New(SystemMode, s.rep).DaemonReexec()
	c.Assert(err, IsNil)
	c.Assert(s.argses, DeepEquals, [][]string{{"daemon-reexec"}})
}

func (s *SystemdTestSuite) TestStart(c *C) {
	sysd := New(SystemMode, s.rep)
	err := sysd.Start([]string{"foo"})
	c.Assert(err, IsNil)
	c.Check(s.argses, DeepEquals, [][]string{{"start", "foo"}})

	s.argses = nil
	err = sysd.Start([]string{"foo", "bar", "baz"})
	c.Assert(err, IsNil)
	c.Check(s.argses, DeepEquals, [][]string{{"start", "foo", "bar", "baz"}})
}

func (s *SystemdTestSuite) TestStartNoBlock(c *C) {
	sysd := New(SystemMode, s.rep)
	err := sysd.StartNoBlock([]string{"foo"})
	c.Assert(err, IsNil)
	c.Check(s.argses, DeepEquals, [][]string{{"start", "--no-block", "foo"}})

	s.argses = nil
	err = sysd.StartNoBlock([]string{"foo", "bar"})
	c.Assert(err, IsNil)
	c.Check(s.argses, DeepEquals, [][]string{{"start", "--no-block", "foo", "bar"}})
}

// Stop failed because systemctl stop returned an error
func (s *SystemdTestSuite) TestStopSystemCtlStopError(c *C) {
	// Poll rate = 2ms, Silence timeout = 4ms
	restore := MockStopDelays(2*time.Millisecond, 4*time.Millisecond)
	defer restore()
	s.stopErrors = []error{errors.New("mock error")}
	err := New(SystemMode, s.rep).Stop([]string{"foo"})
	c.Assert(err, ErrorMatches, "mock error")
	c.Assert(s.stopArgses, HasLen, 1)
	c.Check(s.stopArgses[0], DeepEquals, []string{"stop", "foo"})
	// No notifications
	c.Assert(len(s.rep.msgs), Equals, 0)
}

// Stop failed because systemctl show returned an error
func (s *SystemdTestSuite) TestSystemCtlShowError(c *C) {
	// Poll rate = 2ms, Silence timeout = 4ms
	restore := MockStopDelays(2*time.Millisecond, 4*time.Millisecond)
	defer restore()

	// 'systemctl stop'
	s.stopDelays = []time.Duration{
		// The delay we set here applies to the systemctl stop
		// command. The purpose is to simulate how long systemd
		// tries to stop the supplied services before returning.
		50 * time.Millisecond,
	}

	// 'systemctl show'
	s.errors = []error{errors.New("mock error")}

	err := New(SystemMode, s.rep).Stop([]string{"foo"})
	c.Assert(err, ErrorMatches, "mock error")
	c.Assert(s.stopArgses, HasLen, 1)
	c.Check(s.stopArgses[0], DeepEquals, []string{"stop", "foo"})
	c.Assert(s.argses, HasLen, 1)
	c.Check(s.argses[0], DeepEquals, []string{"show", "--property=ActiveState", "foo"})
	// No notifications
	c.Assert(len(s.rep.msgs), Equals, 0)
}

// The stop completed before the notification silence period expired so
// no output (notification) is expected
func (s *SystemdTestSuite) TestStopBeforeNotify(c *C) {
	// Poll rate = 2ms, Silence timeout = 4ms
	restore := MockStopDelays(2*time.Millisecond, 4*time.Millisecond)
	defer restore()

	// 'systemctl stop'
	s.stopDelays = []time.Duration{
		// The delay we set here applies to the systemctl stop
		// command. The purpose is to simulate how long systemd
		// tries to stop the supplied services before returning.
		50 * time.Millisecond,
	}
	s.stopErrors = []error{nil, errors.New("mock error")}

	// 'systemctl show'
	s.outs = [][]byte{
		[]byte("ActiveState=inactive\n"),
	}
	s.errors = []error{nil, errors.New("mock error")}

	err := New(SystemMode, s.rep).Stop([]string{"foo"})
	c.Assert(err, IsNil)
	// 'systemctl stop'
	c.Assert(s.stopArgses, HasLen, 1)
	c.Check(s.stopArgses[0], DeepEquals, []string{"stop", "foo"})
	// 'systemctl show'
	c.Assert(s.argses, HasLen, 1)
	c.Check(s.argses[0], DeepEquals, []string{"show", "--property=ActiveState", "foo"})

	// No notifications
	c.Assert(len(s.rep.msgs), Equals, 0)
}

// The stop completed after the notification silence period expired
func (s *SystemdTestSuite) TestStopAfterNotify(c *C) {
	// Poll rate = 2ms, Silence timeout = 4ms
	restore := MockStopDelays(2*time.Millisecond, 4*time.Millisecond)
	defer restore()

	// 'systemctl stop'
	s.stopDelays = []time.Duration{
		// The delay we set here applies to the systemctl stop
		// command. The purpose is to simulate how long systemd
		// tries to stop the supplied services before returning.
		50 * time.Millisecond,
	}
	s.stopErrors = []error{nil, errors.New("mock error")}

	// 'systemctl show'
	s.outs = [][]byte{
		[]byte("ActiveState=whatever\n"),
		[]byte("ActiveState=whatever\n"),
		[]byte("ActiveState=whatever\n"),
		[]byte("ActiveState=active\n"),
		[]byte("ActiveState=inactive\n"),
	}
	// The timeout error output should never by reached because the 's.outs'
	// entry before this slot indicates the unit has stopped
	s.errors = []error{nil, nil, nil, nil, nil, errors.New("mock error")}

	err := New(SystemMode, s.rep).Stop([]string{"foo"})
	c.Assert(err, IsNil)
	// 'systemctl stop'
	c.Assert(s.stopArgses, HasLen, 1)
	c.Check(s.stopArgses[0], DeepEquals, []string{"stop", "foo"})
	// 'systemctl show'
	c.Assert(s.argses, HasLen, 5)
	c.Check(s.argses[0], DeepEquals, []string{"show", "--property=ActiveState", "foo"})
	c.Check(s.argses[0], DeepEquals, s.argses[1])
	c.Check(s.argses[0], DeepEquals, s.argses[2])
	c.Check(s.argses[0], DeepEquals, s.argses[3])
	c.Check(s.argses[0], DeepEquals, s.argses[4])

	// Only the initial notification will be shown because no
	// list update are possible with only a single service unit
	c.Assert(len(s.rep.msgs), Equals, 1)
	c.Check(s.rep.msgs[0], Equals, `Waiting for "foo" to stop.`)
}

func (s *SystemdTestSuite) TestStopMany(c *C) {
	// Poll rate = 2ms, Silence timeout = 4ms
	restore := MockStopDelays(2*time.Millisecond, 4*time.Millisecond)
	defer restore()

	// 'systemctl stop'
	s.stopDelays = []time.Duration{
		// The delay we set here applies to the systemctl stop
		// command. The purpose is to simulate how long systemd
		// tries to stop the supplied services before returning.
		50 * time.Millisecond,
	}
	s.stopErrors = []error{nil, errors.New("mock error")}

	// 'systemctl show'
	s.outs = [][]byte{
		[]byte("ActiveState=whatever\n"), // foo
		[]byte("ActiveState=whatever\n"), // bar
		[]byte("ActiveState=whatever\n"), // foo
		[]byte("ActiveState=whatever\n"), // bar
		[]byte("ActiveState=active\n"),   // foo
		[]byte("ActiveState=whatever\n"), // bar
		[]byte("ActiveState=inactive\n"), // foo
		[]byte("ActiveState=active\n"),   // bar
		[]byte("ActiveState=inactive\n"), // bar
	}
	// The timeout error output should never by reached because the 's.outs'
	// entry before this slot indicates the unit has stopped
	s.errors = []error{nil, nil, nil, nil, nil, nil, nil, nil, nil, errors.New("mock error")}
	err := New(SystemMode, s.rep).Stop([]string{"foo", "bar"})
	c.Assert(err, IsNil)
	// 'systemctl stop'
	c.Assert(s.stopArgses, HasLen, 1)
	c.Check(s.stopArgses[0], DeepEquals, []string{"stop", "foo", "bar"})
	// 'systemctl show'
	c.Assert(s.argses, HasLen, 9)
	c.Check(s.argses[0], DeepEquals, []string{"show", "--property=ActiveState", "foo"})
	c.Check(s.argses[1], DeepEquals, []string{"show", "--property=ActiveState", "bar"})
	c.Check(s.argses[2], DeepEquals, []string{"show", "--property=ActiveState", "foo"})
	c.Check(s.argses[3], DeepEquals, []string{"show", "--property=ActiveState", "bar"})
	c.Check(s.argses[4], DeepEquals, []string{"show", "--property=ActiveState", "foo"})
	c.Check(s.argses[5], DeepEquals, []string{"show", "--property=ActiveState", "bar"})
	c.Check(s.argses[6], DeepEquals, []string{"show", "--property=ActiveState", "foo"})
	c.Check(s.argses[7], DeepEquals, []string{"show", "--property=ActiveState", "bar"})
	c.Check(s.argses[8], DeepEquals, []string{"show", "--property=ActiveState", "bar"})

	c.Assert(len(s.rep.msgs), Equals, 2)
	c.Check(s.rep.msgs[0], Equals, `Waiting for "foo", "bar" to stop.`)
	c.Check(s.rep.msgs[1], Equals, `Waiting for "bar" to stop.`)
}

func (s *SystemdTestSuite) TestStatus(c *C) {
	s.outs = [][]byte{
		[]byte(`
Type=simple
Id=foo.service
Names=foo.service
ActiveState=active
UnitFileState=enabled
NeedDaemonReload=no

Type=simple
Id=bar.service
Names=bar.service
ActiveState=reloading
UnitFileState=static
NeedDaemonReload=no

Type=potato
Id=baz.service
Names=baz.service
ActiveState=inactive
UnitFileState=disabled
NeedDaemonReload=yes

Type=
Id=missing.service
Names=missing.service
ActiveState=inactive
UnitFileState=
NeedDaemonReload=no
`[1:]),
		[]byte(`
Id=some.timer
Names=some.timer
ActiveState=active
UnitFileState=enabled
NeedDaemonReload=yes

Id=other.socket
Names=other.socket
ActiveState=active
UnitFileState=disabled
NeedDaemonReload=yes

Id=reboot.target
Names=reboot.target ctrl-alt-del.target
ActiveState=inactive
UnitFileState=enabled
NeedDaemonReload=yes

Id=reboot.target
Names=reboot.target ctrl-alt-del.target
ActiveState=inactive
UnitFileState=enabled
NeedDaemonReload=yes
`[1:]),
	}
	s.errors = []error{nil}
	units := []string{
		"foo.service", "bar.service", "baz.service",
		"missing.service", "some.timer", "other.socket",
		"reboot.target", "ctrl-alt-del.target",
	}
	out, err := New(SystemMode, s.rep).Status(units)
	c.Assert(err, IsNil)
	c.Check(out, DeepEquals, []*UnitStatus{
		{
			Daemon:           "simple",
			Name:             "foo.service",
			Names:            []string{"foo.service"},
			Active:           true,
			Enabled:          true,
			Installed:        true,
			Id:               "foo.service",
			NeedDaemonReload: false,
		}, {
			Daemon:           "simple",
			Name:             "bar.service",
			Names:            []string{"bar.service"},
			Active:           true,
			Enabled:          true,
			Installed:        true,
			Id:               "bar.service",
			NeedDaemonReload: false,
		}, {
			Daemon:           "potato",
			Name:             "baz.service",
			Names:            []string{"baz.service"},
			Active:           false,
			Enabled:          false,
			Installed:        true,
			Id:               "baz.service",
			NeedDaemonReload: true,
		}, {
			Daemon:           "",
			Name:             "missing.service",
			Names:            []string{"missing.service"},
			Active:           false,
			Enabled:          false,
			Installed:        false,
			Id:               "missing.service",
			NeedDaemonReload: false,
		}, {
			Name:             "some.timer",
			Names:            []string{"some.timer"},
			Active:           true,
			Enabled:          true,
			Installed:        true,
			Id:               "some.timer",
			NeedDaemonReload: true,
		}, {
			Name:             "other.socket",
			Names:            []string{"other.socket"},
			Active:           true,
			Enabled:          false,
			Installed:        true,
			Id:               "other.socket",
			NeedDaemonReload: true,
		}, {
			Name:             "reboot.target",
			Names:            []string{"reboot.target", "ctrl-alt-del.target"},
			Active:           false,
			Enabled:          true,
			Installed:        true,
			Id:               "reboot.target",
			NeedDaemonReload: true,
		}, {
			Name:             "ctrl-alt-del.target",
			Names:            []string{"reboot.target", "ctrl-alt-del.target"},
			Active:           false,
			Enabled:          true,
			Installed:        true,
			Id:               "reboot.target",
			NeedDaemonReload: true,
		},
	})
	c.Check(s.rep.msgs, IsNil)
	c.Assert(s.argses, DeepEquals, [][]string{
		{"show", "--property=Id,ActiveState,UnitFileState,Type,Names,NeedDaemonReload", "foo.service", "bar.service", "baz.service", "missing.service"},
		{"show", "--property=Id,ActiveState,UnitFileState,Names", "some.timer", "other.socket", "reboot.target", "ctrl-alt-del.target"},
	})
}

func (s *SystemdTestSuite) TestStatusTooManyNumberOfValues(c *C) {
	s.outs = [][]byte{
		[]byte(`
Type=simple
Id=foo.service
Names=foo.service
ActiveState=active
UnitFileState=enabled
NeedDaemonReload=no

Type=simple
Id=foo.service
Names=foo.service
ActiveState=active
UnitFileState=enabled
NeedDaemonReload=no
`[1:]),
	}
	s.errors = []error{nil}
	out, err := New(SystemMode, s.rep).Status([]string{"foo.service"})
	c.Check(err, ErrorMatches, "cannot get unit status: got more results than expected")
	c.Check(out, IsNil)
	c.Check(s.rep.msgs, IsNil)
}

func (s *SystemdTestSuite) TestStatusTooFewNumberOfValues(c *C) {
	s.outs = [][]byte{
		[]byte(`
Type=simple
Id=foo.service
Names=foo.service
ActiveState=active
UnitFileState=enabled
NeedDaemonReload=no

Type=simple
Id=bar.service
Names=foo.service
ActiveState=active
UnitFileState=enabled
NeedDaemonReload=no
`[1:]),
	}
	s.errors = []error{nil}
	units := []string{"foo.service", "bar.service", "test.service"}
	out, err := New(SystemMode, s.rep).Status(units)
	c.Check(err, ErrorMatches, "cannot get unit status: expected 3 results, got 2")
	c.Check(out, IsNil)
	c.Check(s.rep.msgs, IsNil)
}

func (s *SystemdTestSuite) TestStatusBadLine(c *C) {
	s.outs = [][]byte{
		[]byte(`
Type=simple
Id=foo.service
Names=foo.service
ActiveState=active
UnitFileState=enabled
Potatoes
`[1:]),
	}
	s.errors = []error{nil}
	out, err := New(SystemMode, s.rep).Status([]string{"foo.service"})
	c.Assert(err, ErrorMatches, `.* bad line "Potatoes" .*`)
	c.Check(out, IsNil)
}

func (s *SystemdTestSuite) TestStatusBadId(c *C) {
	s.outs = [][]byte{
		[]byte(`
Type=simple
Id=bar.service
Names=bar.service
ActiveState=active
UnitFileState=enabled
NeedDaemonReload=no
`[1:]),
	}
	s.errors = []error{nil}
	out, err := New(SystemMode, s.rep).Status([]string{"foo.service"})
	c.Assert(err, ErrorMatches, `.* queried status of "foo.service" but got status of "bar.service"`)
	c.Check(out, IsNil)
}

func (s *SystemdTestSuite) TestStatusBadField(c *C) {
	s.outs = [][]byte{
		[]byte(`
Type=simple
Id=foo.service
Names=foo.service
ActiveState=active
UnitFileState=enabled
Potatoes=false
NeedDaemonReload=no
`[1:]),
	}
	s.errors = []error{nil}
	out, err := New(SystemMode, s.rep).Status([]string{"foo.service"})
	c.Assert(err, ErrorMatches, `.* unexpected field "Potatoes" .*`)
	c.Check(out, IsNil)
}

func (s *SystemdTestSuite) TestStatusMissingRequiredFieldService(c *C) {
	s.outs = [][]byte{
		[]byte(`
Id=foo.service
ActiveState=active
NeedDaemonReload=no
`[1:]),
	}
	s.errors = []error{nil}
	out, err := New(SystemMode, s.rep).Status([]string{"foo.service"})
	c.Assert(err, ErrorMatches, `.* missing UnitFileState, Type.*`)
	c.Check(out, IsNil)
}

func (s *SystemdTestSuite) TestStatusMissingRequiredFieldTimer(c *C) {
	s.outs = [][]byte{
		[]byte(`
Id=foo.timer
ActiveState=active
`[1:]),
	}
	s.errors = []error{nil}
	out, err := New(SystemMode, s.rep).Status([]string{"foo.timer"})
	c.Assert(err, ErrorMatches, `.* missing UnitFileState, Names.*`)
	c.Check(out, IsNil)
}

func (s *SystemdTestSuite) TestStatusMissingRequiredFieldTarget(c *C) {
	s.outs = [][]byte{
		[]byte(`
Id=reboot.target
ActiveState=active
`[1:]),
	}
	s.errors = []error{nil}
	out, err := New(SystemMode, s.rep).Status([]string{"reboot.target"})
	c.Assert(err, ErrorMatches, `.* missing UnitFileState, Names.*`)
	c.Check(out, IsNil)
}

func (s *SystemdTestSuite) TestStatusDupeField(c *C) {
	s.outs = [][]byte{
		[]byte(`
Type=simple
Id=foo.service
Names=foo.service
ActiveState=active
ActiveState=active
UnitFileState=enabled
NeedDaemonReload=no
`[1:]),
	}
	s.errors = []error{nil}
	out, err := New(SystemMode, s.rep).Status([]string{"foo.service"})
	c.Assert(err, ErrorMatches, `.* duplicate field "ActiveState" .*`)
	c.Check(out, IsNil)
}

func (s *SystemdTestSuite) TestStatusEmptyField(c *C) {
	s.outs = [][]byte{
		[]byte(`
Type=simple
Id=
Names=foo.service
ActiveState=active
UnitFileState=enabled
NeedDaemonReload=no
`[1:]),
	}
	s.errors = []error{nil}
	out, err := New(SystemMode, s.rep).Status([]string{"foo.service"})
	c.Assert(err, ErrorMatches, `.* empty field "Id" .*`)
	c.Check(out, IsNil)
}

func (s *SystemdTestSuite) TestDisable(c *C) {
	sysd := New(SystemMode, s.rep)
	err := sysd.DisableNoReload([]string{"foo"})
	c.Assert(err, IsNil)
	c.Check(s.argses, DeepEquals, [][]string{{"--no-reload", "disable", "foo"}})

	s.argses = nil
	err = sysd.DisableNoReload([]string{"foo", "bar"})
	c.Assert(err, IsNil)
	c.Check(s.argses, DeepEquals, [][]string{{"--no-reload", "disable", "foo", "bar"}})
}

func (s *SystemdTestSuite) TestUnderRootDisable(c *C) {
	err := NewUnderRoot("xyzzy", SystemMode, s.rep).DisableNoReload([]string{"foo"})
	c.Assert(err, IsNil)
	c.Check(s.argses, DeepEquals, [][]string{{"--root", "xyzzy", "disable", "foo"}})
}

func (s *SystemdTestSuite) TestAvailable(c *C) {
	err := Available()
	c.Assert(err, IsNil)
	c.Check(s.argses, DeepEquals, [][]string{{"--version"}})
}

func (s *SystemdTestSuite) TestVersion(c *C) {
	s.outs = [][]byte{
		[]byte("systemd 223\n+PAM\n"),
		[]byte("systemd 245 (245.4-4ubuntu3)\n+PAM +AUDIT +SELINUX +IMA\n"),
		// error cases
		[]byte("foo 223\n+PAM\n"),
		[]byte(""),
		[]byte("systemd abc\n+PAM\n"),
	}

	v, err := Version()
	c.Assert(err, IsNil)
	c.Check(v, Equals, 223)

	v, err = Version()
	c.Assert(err, IsNil)
	c.Check(v, Equals, 245)

	_, err = Version()
	c.Assert(err, ErrorMatches, `cannot parse systemd version: expected "systemd", got "foo"`)

	_, err = Version()
	c.Assert(err, ErrorMatches, `cannot read systemd version: <nil>`)

	_, err = Version()
	c.Assert(err, ErrorMatches, `cannot convert systemd version to number: abc`)

	c.Check(s.argses, DeepEquals, [][]string{
		{"--version"},
		{"--version"},
		{"--version"},
		{"--version"},
		{"--version"},
	})
}

func (s *SystemdTestSuite) TestEnable(c *C) {
	sysd := New(SystemMode, s.rep)
	err := sysd.EnableNoReload([]string{"foo"})
	c.Assert(err, IsNil)
	c.Check(s.argses, DeepEquals, [][]string{{"--no-reload", "enable", "foo"}})

	s.argses = nil
	err = sysd.EnableNoReload([]string{"foo", "bar"})
	c.Assert(err, IsNil)
	c.Check(s.argses, DeepEquals, [][]string{{"--no-reload", "enable", "foo", "bar"}})
}

func (s *SystemdTestSuite) TestEnableUnderRoot(c *C) {
	err := NewUnderRoot("xyzzy", SystemMode, s.rep).EnableNoReload([]string{"foo"})
	c.Assert(err, IsNil)
	c.Check(s.argses, DeepEquals, [][]string{{"--root", "xyzzy", "enable", "foo"}})
}

func (s *SystemdTestSuite) TestMask(c *C) {
	err := New(SystemMode, s.rep).Mask("foo")
	c.Assert(err, IsNil)
	c.Check(s.argses, DeepEquals, [][]string{{"mask", "foo"}})
}

func (s *SystemdTestSuite) TestMaskUnderRoot(c *C) {
	err := NewUnderRoot("xyzzy", SystemMode, s.rep).Mask("foo")
	c.Assert(err, IsNil)
	c.Check(s.argses, DeepEquals, [][]string{{"--root", "xyzzy", "mask", "foo"}})
}

func (s *SystemdTestSuite) TestUnmask(c *C) {
	err := New(SystemMode, s.rep).Unmask("foo")
	c.Assert(err, IsNil)
	c.Check(s.argses, DeepEquals, [][]string{{"unmask", "foo"}})
}

func (s *SystemdTestSuite) TestUnmaskUnderRoot(c *C) {
	err := NewUnderRoot("xyzzy", SystemMode, s.rep).Unmask("foo")
	c.Assert(err, IsNil)
	c.Check(s.argses, DeepEquals, [][]string{{"--root", "xyzzy", "unmask", "foo"}})
}

func (s *SystemdTestSuite) TestRestart(c *C) {
	restore := MockStopDelays(2*time.Millisecond, 4*time.Millisecond)
	defer restore()

	// 'systemctl stop'
	s.stopDelays = []time.Duration{
		// The delay we set here applies to the systemctl stop
		// command. The purpose is to simulate how long systemd
		// tries to stop the supplied services before returning.
		50 * time.Millisecond,
	}

	s.outs = [][]byte{
		[]byte("ActiveState=inactive\n"),
		nil, // for the "start"
	}
	s.errors = []error{nil, nil, errors.New("mock error")}
	err := New(SystemMode, s.rep).Restart([]string{"foo"})
	c.Assert(err, IsNil)
	c.Check(s.stopArgses, HasLen, 1)
	c.Check(s.stopArgses[0], DeepEquals, []string{"stop", "foo"})
	c.Check(s.argses, HasLen, 2)
	c.Check(s.argses[0], DeepEquals, []string{"show", "--property=ActiveState", "foo"})
	c.Check(s.argses[1], DeepEquals, []string{"start", "foo"})
}

func (s *SystemdTestSuite) TestRestartMany(c *C) {
	restore := MockStopDelays(2*time.Millisecond, 4*time.Millisecond)
	defer restore()

	// 'systemctl stop'
	s.stopDelays = []time.Duration{
		// The delay we set here applies to the systemctl stop
		// command. The purpose is to simulate how long systemd
		// tries to stop the supplied services before returning.
		50 * time.Millisecond,
	}

	s.outs = [][]byte{
		[]byte("ActiveState=inactive\n"), // foo
		[]byte("ActiveState=inactive\n"), // bar
		nil,                              // for the "start"
	}
	s.errors = []error{nil, nil, nil, errors.New("mock error")}
	err := New(SystemMode, s.rep).Restart([]string{"foo", "bar"})
	c.Assert(err, IsNil)
	c.Check(s.stopArgses, HasLen, 1)
	c.Check(s.stopArgses[0], DeepEquals, []string{"stop", "foo", "bar"})
	c.Check(s.argses, HasLen, 3)
	c.Check(s.argses[0], DeepEquals, []string{"show", "--property=ActiveState", "foo"})
	c.Check(s.argses[1], DeepEquals, []string{"show", "--property=ActiveState", "bar"})
	c.Check(s.argses[2], DeepEquals, []string{"start", "foo", "bar"})
}

func (s *SystemdTestSuite) TestKill(c *C) {
	c.Assert(New(SystemMode, s.rep).Kill("foo", "HUP", ""), IsNil)
	c.Check(s.argses, DeepEquals, [][]string{{"kill", "foo", "-s", "HUP", "--kill-who=all"}})
}

func (s *SystemdTestSuite) TestLogErrJctl(c *C) {
	s.jerrs = []error{errors.New("mock journalctl error")}

	reader, err := New(SystemMode, s.rep).LogReader([]string{"foo"}, 24, false, false)
	c.Check(err, NotNil)
	c.Check(reader, IsNil)
	c.Check(s.jns, DeepEquals, []string{"24"})
	c.Check(s.jsvcs, DeepEquals, [][]string{{"foo"}})
	c.Check(s.jfollows, DeepEquals, []bool{false})
	c.Check(s.jnamespaces, DeepEquals, []bool{false})
	c.Check(s.j, Equals, 1)
}

func (s *SystemdTestSuite) TestLogs(c *C) {
	expected := `{"a": 1}
{"a": 2}
`
	s.jouts = [][]byte{[]byte(expected)}

	reader, err := New(SystemMode, s.rep).LogReader([]string{"foo"}, 24, false, false)
	c.Check(err, IsNil)
	logs, err := ioutil.ReadAll(reader)
	c.Assert(err, IsNil)
	c.Check(string(logs), Equals, expected)
	c.Check(s.jns, DeepEquals, []string{"24"})
	c.Check(s.jsvcs, DeepEquals, [][]string{{"foo"}})
	c.Check(s.jfollows, DeepEquals, []bool{false})
	c.Check(s.jnamespaces, DeepEquals, []bool{false})
	c.Check(s.j, Equals, 1)
}

// mustJSONMarshal panic's if the value cannot be marshaled
func mustJSONMarshal(v interface{}) *json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic(fmt.Sprintf("couldn't marshal json in test fixture: %v", err))
	}
	msg := json.RawMessage(b)
	return &msg
}

func (s *SystemdTestSuite) TestLogPIDWithNonTrivialKeyValues(c *C) {
	l1 := Log{
		"_PID": mustJSONMarshal([]string{}),
	}
	l2 := Log{
		"_PID": mustJSONMarshal(6),
	}
	l3 := Log{
		"_PID": mustJSONMarshal([]string{"pid1", "pid2", "pid3"}),
	}
	l4 := Log{
		"SYSLOG_PID": mustJSONMarshal([]string{"pid1", "pid2", "pid3"}),
	}
	l5 := Log{
		"_PID":       mustJSONMarshal("42"),
		"SYSLOG_PID": mustJSONMarshal([]string{"pid1", "pid2", "pid3"}),
	}
	l6 := Log{
		"_PID":       mustJSONMarshal([]string{"42"}),
		"SYSLOG_PID": mustJSONMarshal([]string{"pid1", "pid2", "pid3"}),
	}
	l7 := Log{
		"_PID":       mustJSONMarshal([]string{"42", "42"}),
		"SYSLOG_PID": mustJSONMarshal([]string{"singlepid"}),
	}
	c.Check(Log{}.PID(), Equals, "-")
	c.Check(l1.PID(), Equals, "-")
	c.Check(l2.PID(), Equals, "-")
	c.Check(l3.PID(), Equals, "-")
	c.Check(l4.PID(), Equals, "-")
	// things starting with underscore are "trusted", so we trust
	// them more than the user-settable ones:
	c.Check(l5.PID(), Equals, "42")
	c.Check(l6.PID(), Equals, "42")
	c.Check(l7.PID(), Equals, "singlepid")
}

func (s *SystemdTestSuite) TestLogsMessageWithNonUniqueKeys(c *C) {

	tt := []struct {
		msg     *json.RawMessage
		exp     string
		comment string
	}{
		{
			mustJSONMarshal("m1"),
			"m1",
			"simple string",
		},
		{
			mustJSONMarshal("Я"),
			"Я",
			"simple utf-8 string",
		},
		{
			mustJSONMarshal([]rune{65, 66, 67, 192, 69}),
			"ABC\xc0E",
			"invalid utf-8 bytes",
		},
		{
			mustJSONMarshal(""),
			"",
			"empty string",
		},
		{
			mustJSONMarshal([]string{"m1", "m2", "m3"}),
			"m1\nm2\nm3",
			"slice of strings",
		},
		{
			// this is just "hello" in ascii
			mustJSONMarshal([]rune{104, 101, 108, 108, 111}),
			"hello",
			"rune arrays are converted to strings",
		},
		{
			// this is "hello\r" in ascii, the \r char is unprintable
			mustJSONMarshal([]rune{104, 101, 108, 108, 111, 13}),
			"hello\r",
			"rune arrays are converted to strings",
		},
		{
			// this is "hel" and "lo" in ascii
			mustJSONMarshal([][]rune{
				{104, 101, 108},
				{108, 111},
			}),
			"hel\nlo",
			"arrays of rune arrays are converted to arrays of strings",
		},
		{
			// this is invalid utf-8 string followed by a valid one
			mustJSONMarshal([][]byte{
				{65, 66, 67, 192, 69},
				{104, 101, 108, 108, 111},
			}),
			"ABC\xc0E\nhello",
			"arrays of bytes, some are invalid utf-8 strings",
		},
		{
			mustJSONMarshal(5),
			"- (error decoding original message: unsupported JSON encoding format)",
			"invalid message format of raw scalar number",
		},
		{
			mustJSONMarshal(map[string]int{"hello": 1}),
			"- (error decoding original message: unsupported JSON encoding format)",
			"invalid message format of map object",
		},
	}

	// trivial case
	c.Check(Log{}.Message(), Equals, "-")

	// case where the JSON has a "null" JSON value for the key, which happens if
	// the actual message is too large for journald to send
	// we can't use the mustJSONMarshal helper for this in the test table
	// because that gets decoded by Go differently than a verbatim nil here, it
	// gets interpreted as the empty string rather than nil directly
	c.Check(Log{"MESSAGE": nil}.Message(), Equals, "- (error decoding original message: message key \"MESSAGE\" truncated)")

	for _, t := range tt {
		if t.msg == nil {

		}
		c.Check(Log{
			"MESSAGE": t.msg,
		}.Message(), Equals, t.exp, Commentf(t.comment))
	}
}

func (s *SystemdTestSuite) TestLogSID(c *C) {
	c.Check(Log{}.SID(), Equals, "-")
	c.Check(Log{"SYSLOG_IDENTIFIER": mustJSONMarshal("abcdef")}.SID(), Equals, "abcdef")
	c.Check(Log{"SYSLOG_IDENTIFIER": mustJSONMarshal([]string{"abcdef"})}.SID(), Equals, "abcdef")
	// multiple string values are not supported
	c.Check(Log{"SYSLOG_IDENTIFIER": mustJSONMarshal([]string{"abc", "def"})}.SID(), Equals, "-")

}

func (s *SystemdTestSuite) TestLogPID(c *C) {
	c.Check(Log{}.PID(), Equals, "-")
	c.Check(Log{"_PID": mustJSONMarshal("99")}.PID(), Equals, "99")
	c.Check(Log{"SYSLOG_PID": mustJSONMarshal("99")}.PID(), Equals, "99")
	// things starting with underscore are "trusted", so we trust
	// them more than the user-settable ones:
	c.Check(Log{
		"_PID":       mustJSONMarshal("42"),
		"SYSLOG_PID": mustJSONMarshal("99"),
	}.PID(), Equals, "42")
}

func (s *SystemdTestSuite) TestTime(c *C) {
	t, err := Log{}.Time()
	c.Check(t.IsZero(), Equals, true)
	c.Check(err, ErrorMatches, "key \"__REALTIME_TIMESTAMP\" missing from message")

	// multiple timestampe keys mean we don't have a timestamp
	t, err = Log{"__REALTIME_TIMESTAMP": mustJSONMarshal([]string{"1", "2"})}.Time()
	c.Check(t.IsZero(), Equals, true)
	c.Check(err, ErrorMatches, `no timestamp`)

	t, err = Log{"__REALTIME_TIMESTAMP": mustJSONMarshal("what")}.Time()
	c.Check(t.IsZero(), Equals, true)
	c.Check(err, ErrorMatches, `timestamp not a decimal number: "what"`)

	t, err = Log{"__REALTIME_TIMESTAMP": mustJSONMarshal("0")}.Time()
	c.Check(err, IsNil)
	c.Check(t.String(), Equals, "1970-01-01 00:00:00 +0000 UTC")

	t, err = Log{"__REALTIME_TIMESTAMP": mustJSONMarshal("42")}.Time()
	c.Check(err, IsNil)
	c.Check(t.String(), Equals, "1970-01-01 00:00:00.000042 +0000 UTC")
}

func (s *SystemdTestSuite) TestMountUnitPath(c *C) {
	c.Assert(MountUnitPath("/apps/hello/1.1"), Equals, filepath.Join(dirs.SnapServicesDir, "apps-hello-1.1.mount"))
}

func makeMockFile(c *C, path string) {
	err := os.MkdirAll(filepath.Dir(path), 0755)
	c.Assert(err, IsNil)
	err = os.WriteFile(path, nil, 0644)
	c.Assert(err, IsNil)
}

func (s *SystemdTestSuite) TestAddMountUnit(c *C) {
	rootDir := dirs.GlobalRootDir

	restore := squashfs.MockNeedsFuse(false)
	defer restore()

	mockSnapPath := filepath.Join(c.MkDir(), "/var/lib/snappy/snaps/foo_1.0.snap")
	makeMockFile(c, mockSnapPath)

	mountUnitName, err := NewUnderRoot(rootDir, SystemMode, nil).EnsureMountUnitFile("Mount unit for foo, revision 42", mockSnapPath, "/snap/snapname/123", "squashfs")
	c.Assert(err, IsNil)
	defer os.Remove(mountUnitName)

	c.Assert(filepath.Join(dirs.SnapServicesDir, mountUnitName), testutil.FileEquals, fmt.Sprintf(`
[Unit]
Description=Mount unit for foo, revision 42
After=snapd.mounts-pre.target
Before=snapd.mounts.target
Before=local-fs.target

[Mount]
What=%s
Where=/snap/snapname/123
Type=squashfs
Options=nodev,ro,x-gdu.hide,x-gvfs-hide
LazyUnmount=yes

[Install]
WantedBy=snapd.mounts.target
WantedBy=multi-user.target
`[1:], mockSnapPath))

	c.Assert(s.argses, DeepEquals, [][]string{
		{"daemon-reload"},
		{"--root", rootDir, "enable", "snap-snapname-123.mount"},
		{"reload-or-restart", "snap-snapname-123.mount"},
	})
}

func (s *SystemdTestSuite) TestEnsureMountUnitUnchanged(c *C) {
	rootDir := dirs.GlobalRootDir

	restore := squashfs.MockNeedsFuse(false)
	defer restore()

	mockSnapPath := filepath.Join(c.MkDir(), "/var/lib/snappy/snaps/foo_1.0.snap")
	makeMockFile(c, mockSnapPath)

	// Now we create the mount unit already
	err := os.MkdirAll(dirs.SnapServicesDir, 0755)
	c.Assert(err, IsNil)
	content := fmt.Sprintf(`
[Unit]
Description=Mount unit for foo, revision 42
After=snapd.mounts-pre.target
Before=snapd.mounts.target
Before=local-fs.target

[Mount]
What=%s
Where=/snap/snapname/123
Type=squashfs
Options=nodev,ro,x-gdu.hide,x-gvfs-hide
LazyUnmount=yes

[Install]
WantedBy=snapd.mounts.target
WantedBy=multi-user.target
`[1:], mockSnapPath)
	err = os.WriteFile(filepath.Join(dirs.SnapServicesDir, "snap-snapname-123.mount"), []byte(content), 0644)
	c.Assert(err, IsNil)

	mountUnitName, err := NewUnderRoot(rootDir, SystemMode, nil).EnsureMountUnitFile("Mount unit for foo, revision 42", mockSnapPath, "/snap/snapname/123", "squashfs")
	c.Assert(err, IsNil)

	// Should still be the same file
	c.Assert(filepath.Join(dirs.SnapServicesDir, mountUnitName), testutil.FileEquals, fmt.Sprintf(`
[Unit]
Description=Mount unit for foo, revision 42
After=snapd.mounts-pre.target
Before=snapd.mounts.target
Before=local-fs.target

[Mount]
What=%s
Where=/snap/snapname/123
Type=squashfs
Options=nodev,ro,x-gdu.hide,x-gvfs-hide
LazyUnmount=yes

[Install]
WantedBy=snapd.mounts.target
WantedBy=multi-user.target
`[1:], mockSnapPath))

	// No call systemctl
	c.Assert(s.argses, DeepEquals, [][]string(nil))
}

func (s *SystemdTestSuite) TestEnsureMountUnitChanged(c *C) {
	rootDir := dirs.GlobalRootDir

	restore := squashfs.MockNeedsFuse(false)
	defer restore()

	mockSnapPath := filepath.Join(c.MkDir(), "/var/lib/snappy/snaps/foo_1.0.snap")
	makeMockFile(c, mockSnapPath)

	// Now we create the mount unit already
	err := os.MkdirAll(dirs.SnapServicesDir, 0755)
	c.Assert(err, IsNil)

	// Note the "someotheroption" that should be removed
	content := fmt.Sprintf(`
[Unit]
Description=Mount unit for foo, revision 42
Before=snapd.service

[Mount]
What=%s
Where=/snap/snapname/123
Type=squashfs
Options=nodev,ro,x-gdu.hide,x-gvfs-hide,someotheroption
LazyUnmount=yes

[Install]
WantedBy=snapd.mounts.target
WantedBy=multi-user.target
`[1:], mockSnapPath)
	err = os.WriteFile(filepath.Join(dirs.SnapServicesDir, "snap-snapname-123.mount"), []byte(content), 0644)
	c.Assert(err, IsNil)

	mountUnitName, err := NewUnderRoot(rootDir, SystemMode, nil).EnsureMountUnitFile("Mount unit for foo, revision 42", mockSnapPath, "/snap/snapname/123", "squashfs")
	c.Assert(err, IsNil)

	// Should still be the same file
	c.Assert(filepath.Join(dirs.SnapServicesDir, mountUnitName), testutil.FileEquals, fmt.Sprintf(`
[Unit]
Description=Mount unit for foo, revision 42
After=snapd.mounts-pre.target
Before=snapd.mounts.target
Before=local-fs.target

[Mount]
What=%s
Where=/snap/snapname/123
Type=squashfs
Options=nodev,ro,x-gdu.hide,x-gvfs-hide
LazyUnmount=yes

[Install]
WantedBy=snapd.mounts.target
WantedBy=multi-user.target
`[1:], mockSnapPath))

	// No call systemctl
	c.Assert(s.argses, DeepEquals, [][]string{
		{"daemon-reload"},
		{"--root", rootDir, "enable", "snap-snapname-123.mount"},
		{"reload-or-restart", "snap-snapname-123.mount"},
	})
}

func (s *SystemdTestSuite) TestAddMountUnitForDirs(c *C) {
	restore := squashfs.MockNeedsFuse(false)
	defer restore()

	// a directory instead of a file produces a different output
	snapDir := c.MkDir()
	mountUnitName, err := New(SystemMode, nil).EnsureMountUnitFile("Mount unit for foodir, revision x1", snapDir, "/snap/snapname/x1", "squashfs")
	c.Assert(err, IsNil)
	defer os.Remove(mountUnitName)

	c.Assert(filepath.Join(dirs.SnapServicesDir, mountUnitName), testutil.FileEquals, fmt.Sprintf(`
[Unit]
Description=Mount unit for foodir, revision x1
After=snapd.mounts-pre.target
Before=snapd.mounts.target
Before=local-fs.target

[Mount]
What=%s
Where=/snap/snapname/x1
Type=none
Options=nodev,ro,x-gdu.hide,x-gvfs-hide,bind
LazyUnmount=yes

[Install]
WantedBy=snapd.mounts.target
WantedBy=multi-user.target
`[1:], snapDir))

	c.Assert(s.argses, DeepEquals, [][]string{
		{"daemon-reload"},
		{"--no-reload", "enable", "snap-snapname-x1.mount"},
		{"reload-or-restart", "snap-snapname-x1.mount"},
	})
}

func (s *SystemdTestSuite) TestAddMountUnitTransient(c *C) {
	rootDir := dirs.GlobalRootDir

	restore := squashfs.MockNeedsFuse(false)
	defer restore()

	mockSnapPath := filepath.Join(c.MkDir(), "/var/lib/snappy/snaps/foo_1.0.snap")
	makeMockFile(c, mockSnapPath)

	addMountUnitOptions := &MountUnitOptions{
		Lifetime:    Transient,
		Description: "Mount unit for foo via bar",
		What:        mockSnapPath,
		Where:       "/snap/snapname/345",
		Fstype:      "squashfs",
		Options:     []string{"remount,ro"},
		Origin:      "bar",
	}
	mountUnitName, err := NewUnderRoot(rootDir, SystemMode, nil).EnsureMountUnitFileWithOptions(addMountUnitOptions)
	c.Assert(err, IsNil)
	defer os.Remove(mountUnitName)

	c.Assert(filepath.Join(dirs.SnapRuntimeServicesDir, mountUnitName), testutil.FileEquals, fmt.Sprintf(`
[Unit]
Description=Mount unit for foo via bar
After=snapd.mounts-pre.target
Before=snapd.mounts.target
Before=local-fs.target

[Mount]
What=%s
Where=/snap/snapname/345
Type=squashfs
Options=remount,ro
LazyUnmount=yes

[Install]
WantedBy=snapd.mounts.target
WantedBy=multi-user.target
X-SnapdOrigin=bar
`[1:], mockSnapPath))

	c.Assert(s.argses, DeepEquals, [][]string{
		{"daemon-reload"},
		{"--root", rootDir, "enable", "snap-snapname-345.mount"},
		{"reload-or-restart", "snap-snapname-345.mount"},
	})
}

func (s *SystemdTestSuite) TestAddKernelModulesMountUnit(c *C) {
	rootDir := dirs.GlobalRootDir

	restore := squashfs.MockNeedsFuse(false)
	defer restore()

	mockSnapPath := filepath.Join(c.MkDir(), "/var/lib/snapd/snaps/mykernel+mykmod_11.comp")
	makeMockFile(c, mockSnapPath)

	addMountUnitOptions := &MountUnitOptions{
		MountUnitType: BeforeDriversLoadMountUnit,
		Lifetime:      Persistent,
		Description:   "Mount unit for wifi kernel modules component",
		What:          mockSnapPath,
		Where:         "/run/mnt/kernel-modules/5.15.0-91-generic/mykmod/",
		Fstype:        "squashfs",
		Options:       []string{"nodev,ro,x-gdu.hide,x-gvfs-hide"},
		Origin:        "",
	}
	mountUnitName, err := NewUnderRoot(rootDir, SystemMode, nil).EnsureMountUnitFileWithOptions(addMountUnitOptions)
	c.Assert(err, IsNil)
	defer os.Remove(mountUnitName)

	c.Assert(filepath.Join(dirs.SnapServicesDir, mountUnitName), testutil.FileEquals, fmt.Sprintf(`[Unit]
Description=Mount unit for wifi kernel modules component
DefaultDependencies=no
After=systemd-remount-fs.service
Before=sysinit.target
Before=systemd-udevd.service systemd-modules-load.service
Before=umount.target
Conflicts=umount.target

[Mount]
What=%s
Where=/run/mnt/kernel-modules/5.15.0-91-generic/mykmod/
Type=squashfs
Options=nodev,ro,x-gdu.hide,x-gvfs-hide

[Install]
WantedBy=sysinit.target
`, mockSnapPath))
	escapedUnit := "run-mnt-kernel\\x2dmodules-5.15.0\\x2d91\\x2dgeneric-mykmod.mount"
	c.Assert(s.argses, DeepEquals, [][]string{
		{"daemon-reload"},
		{"--root", rootDir, "enable", escapedUnit},
		{"reload-or-restart", escapedUnit},
	})
}

func (s *SystemdTestSuite) TestAddKernelTreeMountUnit(c *C) {
	rootDir := dirs.GlobalRootDir

	restore := squashfs.MockNeedsFuse(false)
	defer restore()

	// systemd would automatically add a dependency for this unit on the mount unit for
	// /run/mnt/kernel-modules/5.15.0-91-generic/mykmod
	addMountUnitOptions := &MountUnitOptions{
		MountUnitType: BeforeDriversLoadMountUnit,
		Lifetime:      Persistent,
		Description:   "Mount unit for kernel modules in kernel tree",
		What:          "/run/mnt/kernel-modules/5.15.0-91-generic/mykmod/modules/5.15.0-91-generic",
		Where:         "/usr/lib/modules/5.15.0-91-generic/updates/mykmod/",
		Fstype:        "none",
		Options:       []string{"bind"},
		Origin:        "",
	}
	mountUnitName, err := NewUnderRoot(rootDir, SystemMode, nil).EnsureMountUnitFileWithOptions(addMountUnitOptions)
	c.Assert(err, IsNil)
	defer os.Remove(mountUnitName)

	c.Assert(filepath.Join(dirs.SnapServicesDir, mountUnitName), testutil.FileEquals, fmt.Sprintf(`[Unit]
Description=Mount unit for kernel modules in kernel tree
DefaultDependencies=no
After=systemd-remount-fs.service
Before=sysinit.target
Before=systemd-udevd.service systemd-modules-load.service
Before=umount.target
Conflicts=umount.target

[Mount]
What=/run/mnt/kernel-modules/5.15.0-91-generic/mykmod/modules/5.15.0-91-generic
Where=/usr/lib/modules/5.15.0-91-generic/updates/mykmod/
Type=none
Options=bind

[Install]
WantedBy=sysinit.target
`))
	escapedUnit := "usr-lib-modules-5.15.0\\x2d91\\x2dgeneric-updates-mykmod.mount"
	c.Assert(s.argses, DeepEquals, [][]string{
		{"daemon-reload"},
		{"--root", rootDir, "enable", escapedUnit},
		{"reload-or-restart", escapedUnit},
	})
}

func (s *SystemdTestSuite) TestWriteSELinuxMountUnit(c *C) {
	restore := selinux.MockIsEnabled(func() (bool, error) { return true, nil })
	defer restore()
	restore = selinux.MockIsEnforcing(func() (bool, error) { return true, nil })
	defer restore()
	restore = squashfs.MockNeedsFuse(false)
	defer restore()

	mockSnapPath := filepath.Join(c.MkDir(), "/var/lib/snappy/snaps/foo_1.0.snap")
	err := os.MkdirAll(filepath.Dir(mockSnapPath), 0755)
	c.Assert(err, IsNil)
	err = os.WriteFile(mockSnapPath, nil, 0644)
	c.Assert(err, IsNil)

	mountUnitName, err := New(SystemMode, nil).EnsureMountUnitFile("Mount unit for foo, revision 42", mockSnapPath, "/snap/snapname/123", "squashfs")
	c.Assert(err, IsNil)
	defer os.Remove(mountUnitName)

	c.Assert(filepath.Join(dirs.SnapServicesDir, mountUnitName), testutil.FileEquals, fmt.Sprintf(`
[Unit]
Description=Mount unit for foo, revision 42
After=snapd.mounts-pre.target
Before=snapd.mounts.target
Before=local-fs.target

[Mount]
What=%s
Where=/snap/snapname/123
Type=squashfs
Options=nodev,context=system_u:object_r:snappy_snap_t:s0,ro,x-gdu.hide,x-gvfs-hide
LazyUnmount=yes

[Install]
WantedBy=snapd.mounts.target
WantedBy=multi-user.target
`[1:], mockSnapPath))
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
	err = os.WriteFile(mockSnapPath, nil, 0644)
	c.Assert(err, IsNil)

	mountUnitName, err := New(SystemMode, nil).EnsureMountUnitFile("Mount unit for foo, revision x1", mockSnapPath, "/snap/snapname/123", "squashfs")
	c.Assert(err, IsNil)
	defer os.Remove(mountUnitName)

	c.Check(filepath.Join(dirs.SnapServicesDir, mountUnitName), testutil.FileEquals, fmt.Sprintf(`
[Unit]
Description=Mount unit for foo, revision x1
After=snapd.mounts-pre.target
Before=snapd.mounts.target
Before=local-fs.target

[Mount]
What=%s
Where=/snap/snapname/123
Type=fuse.squashfuse
Options=nodev,ro,x-gdu.hide,x-gvfs-hide,allow_other
LazyUnmount=yes

[Install]
WantedBy=snapd.mounts.target
WantedBy=multi-user.target
`[1:], mockSnapPath))
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
	err = os.WriteFile(mockSnapPath, nil, 0644)
	c.Assert(err, IsNil)

	mountUnitName, err := New(SystemMode, nil).EnsureMountUnitFile("Mount unit for foo, revision x1", mockSnapPath, "/snap/snapname/123", "squashfs")
	c.Assert(err, IsNil)
	defer os.Remove(mountUnitName)

	c.Assert(filepath.Join(dirs.SnapServicesDir, mountUnitName), testutil.FileEquals, fmt.Sprintf(`
[Unit]
Description=Mount unit for foo, revision x1
After=snapd.mounts-pre.target
Before=snapd.mounts.target
Before=local-fs.target

[Mount]
What=%s
Where=/snap/snapname/123
Type=squashfs
Options=nodev,ro,x-gdu.hide,x-gvfs-hide
LazyUnmount=yes

[Install]
WantedBy=snapd.mounts.target
WantedBy=multi-user.target
`[1:], mockSnapPath))
}

func (s *SystemdTestSuite) TestJctl(c *C) {
	var args []string
	var err error
	MockOsutilStreamCommand(func(name string, myargs ...string) (io.ReadCloser, error) {
		c.Check(cap(myargs) <= len(myargs)+3, Equals, true, Commentf("cap:%d, len:%d", cap(myargs), len(myargs)))
		args = myargs
		return nil, nil
	})

	_, err = Jctl([]string{"foo", "bar"}, 10, false, false)
	c.Assert(err, IsNil)
	c.Check(args, DeepEquals, []string{"-o", "json", "--no-pager", "-n", "10", "-u", "foo", "-u", "bar"})
	_, err = Jctl([]string{"foo", "bar", "baz"}, 99, true, false)
	c.Assert(err, IsNil)
	c.Check(args, DeepEquals, []string{"-o", "json", "--no-pager", "-n", "99", "-f", "-u", "foo", "-u", "bar", "-u", "baz"})
	_, err = Jctl([]string{"foo", "bar"}, -1, false, false)
	c.Assert(err, IsNil)
	c.Check(args, DeepEquals, []string{"-o", "json", "--no-pager", "--no-tail", "-u", "foo", "-u", "bar"})
	_, err = Jctl([]string{"foo", "bar"}, -1, false, true)
	c.Assert(err, IsNil)
	c.Check(args, DeepEquals, []string{"-o", "json", "--no-pager", "--no-tail", "--namespace=*", "-u", "foo", "-u", "bar"})
}

func (s *SystemdTestSuite) TestIsActiveUnderRoot(c *C) {
	sysErr := &Error{}
	// manpage states that systemctl returns exit code 3 for inactive
	// services, however we should check any non-0 exit status
	sysErr.SetExitCode(1)
	sysErr.SetMsg([]byte("inactive\n"))
	s.errors = []error{sysErr}

	_, err := NewUnderRoot("xyzzy", SystemMode, s.rep).IsActive("foo")
	c.Assert(err, IsNil)
	c.Check(s.argses, DeepEquals, [][]string{{"--root", "xyzzy", "is-active", "foo"}})
}

func (s *SystemdTestSuite) TestIsActiveIsInactive(c *C) {
	sysErr := &Error{}
	// manpage states that systemctl returns exit code 3 for inactive
	// services, however we should check any non-0 exit status
	sysErr.SetExitCode(1)
	sysErr.SetMsg([]byte("inactive\n"))
	s.errors = []error{sysErr}

	active, err := New(SystemMode, s.rep).IsActive("foo")
	c.Assert(active, Equals, false)
	c.Assert(err, IsNil)
	c.Check(s.argses, DeepEquals, [][]string{{"is-active", "foo"}})
}

func (s *SystemdTestSuite) TestIsActiveIsInactiveAlternativeMessage(c *C) {
	sysErr := &Error{}
	// on Centos 7, with systemd 219 we see "unknown" returned when querying the
	// active state for a slice unit which does not exist, check that we handle
	// this case properly as well
	sysErr.SetExitCode(3)
	sysErr.SetMsg([]byte("unknown\n"))
	s.errors = []error{sysErr}

	active, err := New(SystemMode, s.rep).IsActive("foo")
	c.Assert(active, Equals, false)
	c.Assert(err, IsNil)
	c.Check(s.argses, DeepEquals, [][]string{{"is-active", "foo"}})
}

func (s *SystemdTestSuite) TestIsActiveIsFailed(c *C) {
	sysErr := &Error{}
	// seen in the wild to be reported for a 'failed' service
	sysErr.SetExitCode(3)
	sysErr.SetMsg([]byte("failed\n"))
	s.errors = []error{sysErr}

	active, err := New(SystemMode, s.rep).IsActive("foo")
	c.Assert(active, Equals, false)
	c.Assert(err, IsNil)
	c.Check(s.argses, DeepEquals, [][]string{{"is-active", "foo"}})
}

func (s *SystemdTestSuite) TestIsActiveIsActive(c *C) {
	s.errors = []error{nil}

	active, err := New(SystemMode, s.rep).IsActive("foo")
	c.Assert(active, Equals, true)
	c.Assert(err, IsNil)
	c.Check(s.argses, DeepEquals, [][]string{{"is-active", "foo"}})
}

func (s *SystemdTestSuite) TestIsActiveUnexpectedErr(c *C) {
	sysErr := &Error{}
	sysErr.SetExitCode(1)
	sysErr.SetMsg([]byte("random-failure\n"))
	s.errors = []error{sysErr}

	active, err := NewUnderRoot("xyzzy", SystemMode, s.rep).IsActive("foo")
	c.Assert(active, Equals, false)
	c.Assert(err, ErrorMatches, ".* failed with exit status 1: random-failure\n")
}

func makeMockMountUnit(c *C, mountDir string) string {
	mountUnit := MountUnitPath(dirs.StripRootDir(mountDir))
	err := os.WriteFile(mountUnit, nil, 0644)
	c.Assert(err, IsNil)
	return mountUnit
}

// FIXME: also test for the "IsMounted" case
func (s *SystemdTestSuite) TestRemoveMountUnit(c *C) {
	rootDir := dirs.GlobalRootDir

	restore := osutil.MockMountInfo("")
	defer restore()

	mountDir := rootDir + "/snap/foo/42"
	mountUnit := makeMockMountUnit(c, mountDir)
	err := NewUnderRoot(rootDir, SystemMode, nil).RemoveMountUnitFile(mountDir)

	c.Assert(err, IsNil)
	// the file is gone
	c.Check(osutil.FileExists(mountUnit), Equals, false)
	// and the unit is disabled and the daemon reloaded
	c.Check(s.argses, DeepEquals, [][]string{
		{"--root", rootDir, "disable", "snap-foo-42.mount"},
		{"daemon-reload"},
	})
}

func (s *SystemdTestSuite) TestDaemonReloadMutex(c *C) {
	s.testDaemonOpWithMutex(c, Systemd.DaemonReload)
}

func (s *SystemdTestSuite) testDaemonOpWithMutex(c *C, testFunc func(Systemd) error) {
	rootDir := dirs.GlobalRootDir
	sysd := NewUnderRoot(rootDir, SystemMode, nil)

	mockSnapPath := filepath.Join(c.MkDir(), "/var/lib/snappy/snaps/foo_1.0.snap")
	makeMockFile(c, mockSnapPath)

	// create a go-routine that will try to daemon-reload like crazy
	stopCh := make(chan bool, 1)
	stoppedCh := make(chan bool, 1)
	go func() {
		for {
			testFunc(sysd)
			select {
			case <-stopCh:
				close(stoppedCh)
				return
			default:
				//pass
			}
		}
	}()

	// And now add a mount unit file while the go-routine tries to
	// daemon-reload. This will be serialized, if not this would
	// panic because systemd.daemonReloadNoLock ensures the lock is
	// taken when this happens.
	_, err := sysd.EnsureMountUnitFile("42", mockSnapPath, "/snap/foo/42", "squashfs")
	c.Assert(err, IsNil)
	close(stopCh)
	<-stoppedCh
}

func (s *SystemdTestSuite) TestDaemonReexecMutex(c *C) {
	s.testDaemonOpWithMutex(c, Systemd.DaemonReexec)
}

func (s *SystemdTestSuite) TestUserMode(c *C) {
	rootDir := dirs.GlobalRootDir
	sysd := NewUnderRoot(rootDir, UserMode, nil)

	c.Assert(sysd.EnableNoReload([]string{"foo"}), IsNil)
	c.Check(s.argses[0], DeepEquals, []string{"--user", "--root", rootDir, "enable", "foo"})
	c.Assert(sysd.Start([]string{"foo"}), IsNil)
	c.Check(s.argses[1], DeepEquals, []string{"--user", "start", "foo"})
}

func (s *SystemdTestSuite) TestGlobalUserMode(c *C) {
	rootDir := dirs.GlobalRootDir
	sysd := NewUnderRoot(rootDir, GlobalUserMode, nil)

	c.Assert(sysd.EnableNoReload([]string{"foo"}), IsNil)
	c.Check(s.argses[0], DeepEquals, []string{"--user", "--global", "--root", rootDir, "enable", "foo"})
	c.Assert(sysd.DisableNoReload([]string{"foo"}), IsNil)
	c.Check(s.argses[1], DeepEquals, []string{"--user", "--global", "--root", rootDir, "disable", "foo"})
	c.Assert(sysd.Mask("foo"), IsNil)
	c.Check(s.argses[2], DeepEquals, []string{"--user", "--global", "--root", rootDir, "mask", "foo"})
	c.Assert(sysd.Unmask("foo"), IsNil)
	c.Check(s.argses[3], DeepEquals, []string{"--user", "--global", "--root", rootDir, "unmask", "foo"})
	_, err := sysd.IsEnabled("foo")
	c.Check(err, IsNil)
	c.Check(s.argses[4], DeepEquals, []string{"--user", "--global", "--root", rootDir, "is-enabled", "foo"})

	// Commands that don't make sense for GlobalUserMode panic
	c.Check(sysd.DaemonReload, Panics, "cannot call daemon-reload with GlobalUserMode")
	c.Check(sysd.DaemonReexec, Panics, "cannot call daemon-reexec with GlobalUserMode")
	c.Check(func() { sysd.Start([]string{"foo"}) }, Panics, "cannot call start with GlobalUserMode")
	c.Check(func() { sysd.StartNoBlock([]string{"foo"}) }, Panics, "cannot call start with GlobalUserMode")
	c.Check(func() { sysd.Stop([]string{"foo"}) }, Panics, "cannot call stop with GlobalUserMode")
	c.Check(func() { sysd.Restart([]string{"foo"}) }, Panics, "cannot call restart with GlobalUserMode")
	c.Check(func() { sysd.Kill("foo", "HUP", "") }, Panics, "cannot call kill with GlobalUserMode")
	c.Check(func() { sysd.IsActive("foo") }, Panics, "cannot call is-active with GlobalUserMode")
}

func (s *SystemdTestSuite) TestStatusGlobalUserMode(c *C) {
	output := []byte("enabled\ndisabled\nstatic\n")
	sysdErr := &Error{}
	sysdErr.SetExitCode(1)
	sysdErr.SetMsg(output)

	s.outs = [][]byte{output, nil, output}
	s.errors = []error{nil, sysdErr, nil}

	rootDir := dirs.GlobalRootDir
	sysd := NewUnderRoot(rootDir, GlobalUserMode, nil)
	sts, err := sysd.Status([]string{"foo", "bar", "baz"})
	c.Check(err, IsNil)
	c.Check(sts, DeepEquals, []*UnitStatus{
		{Name: "foo", Enabled: true},
		{Name: "bar", Enabled: false},
		{Name: "baz", Enabled: true},
	})
	c.Check(s.argses[0], DeepEquals, []string{"--user", "--global", "--root", rootDir, "is-enabled", "foo", "bar", "baz"})

	// Output is collected if systemctl has a non-zero exit status
	sts, err = sysd.Status([]string{"one", "two", "three"})
	c.Check(err, IsNil)
	c.Check(sts, DeepEquals, []*UnitStatus{
		{Name: "one", Enabled: true},
		{Name: "two", Enabled: false},
		{Name: "three", Enabled: true},
	})
	c.Check(s.argses[1], DeepEquals, []string{"--user", "--global", "--root", rootDir, "is-enabled", "one", "two", "three"})

	// An error is returned if the wrong number of statuses are returned
	sts, err = sysd.Status([]string{"one"})
	c.Check(err, ErrorMatches, "cannot get enabled status of services: expected 1 results, got 3")
	c.Check(sts, IsNil)
	c.Check(s.argses[2], DeepEquals, []string{"--user", "--global", "--root", rootDir, "is-enabled", "one"})
}

func (s *SystemdTestSuite) TestEmulationModeBackend(c *C) {
	sysd := NewEmulationMode(dirs.GlobalRootDir)
	c.Check(sysd.Backend(), Equals, EmulationModeBackend)
}

const unitTemplate = `
[Unit]
Description=Mount unit for foo, revision 42
After=snapd.mounts-pre.target
Before=snapd.mounts.target
Before=local-fs.target

[Mount]
What=%s
Where=/snap/snapname/123
Type=%s
Options=%s
LazyUnmount=yes

[Install]
WantedBy=snapd.mounts.target
WantedBy=multi-user.target
`

func (s *SystemdTestSuite) TestPreseedModeAddMountUnit(c *C) {
	sysd := NewEmulationMode(dirs.GlobalRootDir)

	restore := squashfs.MockNeedsFuse(false)
	defer restore()

	mockMountCmd := testutil.MockCommand(c, "mount", "")
	defer mockMountCmd.Restore()

	mockSnapPath := filepath.Join(c.MkDir(), "/var/lib/snappy/snaps/foo_1.0.snap")
	makeMockFile(c, mockSnapPath)

	mountUnitName, err := sysd.EnsureMountUnitFile("Mount unit for foo, revision 42", mockSnapPath, "/snap/snapname/123", "squashfs")
	c.Assert(err, IsNil)
	defer os.Remove(mountUnitName)

	c.Check(s.argses, DeepEquals, [][]string{{"--root", dirs.GlobalRootDir, "enable", "snap-snapname-123.mount"}})
	// mount was called
	c.Check(mockMountCmd.Calls()[0], DeepEquals, []string{"mount", "-t", "squashfs", mockSnapPath, "/snap/snapname/123", "-o", "nodev,ro,x-gdu.hide,x-gvfs-hide"})
	c.Check(filepath.Join(dirs.SnapServicesDir, mountUnitName), testutil.FileEquals, fmt.Sprintf(unitTemplate[1:], mockSnapPath, "squashfs", "nodev,ro,x-gdu.hide,x-gvfs-hide"))
}

func (s *SystemdTestSuite) TestPreseedModeAddMountUnitUnchanged(c *C) {
	sysd := NewEmulationMode(dirs.GlobalRootDir)

	restore := squashfs.MockNeedsFuse(false)
	defer restore()

	mockMountCmd := testutil.MockCommand(c, "mount", "")
	defer mockMountCmd.Restore()

	mockSnapPath := filepath.Join(c.MkDir(), "/var/lib/snappy/snaps/foo_1.0.snap")
	makeMockFile(c, mockSnapPath)

	err := os.MkdirAll(dirs.SnapServicesDir, 0755)
	c.Assert(err, IsNil)
	content := fmt.Sprintf(`
[Unit]
Description=Mount unit for foo, revision 42
After=snapd.mounts-pre.target
Before=snapd.mounts.target
Before=local-fs.target

[Mount]
What=%s
Where=/snap/snapname/123
Type=squashfs
Options=nodev,ro,x-gdu.hide,x-gvfs-hide
LazyUnmount=yes

[Install]
WantedBy=snapd.mounts.target
WantedBy=multi-user.target
`[1:], mockSnapPath)
	err = os.WriteFile(filepath.Join(dirs.SnapServicesDir, "snap-snapname-123.mount"), []byte(content), 0644)
	c.Assert(err, IsNil)

	_, err = sysd.EnsureMountUnitFile("Mount unit for foo, revision 42", mockSnapPath, "/snap/snapname/123", "squashfs")
	c.Assert(err, IsNil)

	// systemd was not called
	c.Check(s.argses, HasLen, 0)
	// no mount was called
	c.Check(mockMountCmd.Calls(), HasLen, 0)
}

func (s *SystemdTestSuite) TestPreseedModeAddMountUniModified(c *C) {
	sysd := NewEmulationMode(dirs.GlobalRootDir)

	restore := squashfs.MockNeedsFuse(false)
	defer restore()

	mockMountCmd := testutil.MockCommand(c, "mount", "")
	defer mockMountCmd.Restore()

	mockSnapPath := filepath.Join(c.MkDir(), "/var/lib/snappy/snaps/foo_1.0.snap")
	makeMockFile(c, mockSnapPath)

	err := os.MkdirAll(dirs.SnapServicesDir, 0755)
	c.Assert(err, IsNil)
	// Note the "anotheroption"
	content := fmt.Sprintf(`
[Unit]
Description=Mount unit for foo, revision 42
After=snapd.mounts-pre.target
Before=snapd.mounts.target
Before=local-fs.target

[Mount]
What=%s
Where=/snap/snapname/123
Type=squashfs
Options=nodev,ro,x-gdu.hide,x-gvfs-hide,anotheroption
LazyUnmount=yes

[Install]
WantedBy=snapd.mounts.target
WantedBy=multi-user.target
`[1:], mockSnapPath)
	err = os.WriteFile(filepath.Join(dirs.SnapServicesDir, "snap-snapname-123.mount"), []byte(content), 0644)
	c.Assert(err, IsNil)

	mountUnitName, err := sysd.EnsureMountUnitFile("Mount unit for foo, revision 42", mockSnapPath, "/snap/snapname/123", "squashfs")
	c.Assert(err, IsNil)

	c.Check(s.argses, DeepEquals, [][]string{{"--root", dirs.GlobalRootDir, "enable", "snap-snapname-123.mount"}})
	// mount was called with remount option
	c.Check(mockMountCmd.Calls()[0], DeepEquals, []string{"mount", "-t", "squashfs", mockSnapPath, "/snap/snapname/123", "-o", "nodev,ro,x-gdu.hide,x-gvfs-hide,remount"})
	c.Check(filepath.Join(dirs.SnapServicesDir, mountUnitName), testutil.FileEquals, fmt.Sprintf(unitTemplate[1:], mockSnapPath, "squashfs", "nodev,ro,x-gdu.hide,x-gvfs-hide"))
}

func (s *SystemdTestSuite) TestPreseedModeAddMountUnitWithFuse(c *C) {
	sysd := NewEmulationMode(dirs.GlobalRootDir)

	restore := MockSquashFsType(func() (string, []string) { return "fuse.squashfuse", []string{"a,b,c"} })
	defer restore()

	mockMountCmd := testutil.MockCommand(c, "mount", "")
	defer mockMountCmd.Restore()

	mockSnapPath := filepath.Join(c.MkDir(), "/var/lib/snappy/snaps/foo_1.0.snap")
	makeMockFile(c, mockSnapPath)

	mountUnitName, err := sysd.EnsureMountUnitFile("Mount unit for foo, revision 42", mockSnapPath, "/snap/snapname/123", "squashfs")
	c.Assert(err, IsNil)
	defer os.Remove(mountUnitName)

	c.Check(mockMountCmd.Calls()[0], DeepEquals, []string{"mount", "-t", "fuse.squashfuse", mockSnapPath, "/snap/snapname/123", "-o", "nodev,a,b,c"})
	c.Check(filepath.Join(dirs.SnapServicesDir, mountUnitName), testutil.FileEquals, fmt.Sprintf(unitTemplate[1:], mockSnapPath, "squashfs", "nodev,ro,x-gdu.hide,x-gvfs-hide"))
}

func (s *SystemdTestSuite) TestPreseedModeAddMountUnitWithOptions(c *C) {
	sysd := NewEmulationMode(dirs.GlobalRootDir)

	restore := MockSquashFsType(func() (string, []string) { return "fuse.squashfuse", []string{"a,b,c"} })
	defer restore()

	mockMountCmd := testutil.MockCommand(c, "mount", "")
	defer mockMountCmd.Restore()

	mockSnapPath := filepath.Join(c.MkDir(), "/var/lib/snappy/snaps/pc-kernel_1.0.snap")
	makeMockFile(c, mockSnapPath)

	mountUnitOptions := &systemd.MountUnitOptions{
		MountUnitType: systemd.BeforeDriversLoadMountUnit,
		Lifetime:      systemd.Persistent,
		Description:   "Early mount unit for kernel snap",
		What:          mockSnapPath,
		Where:         "/run/mnt/kernel-snaps/pc-kernel/1",
		Fstype:        "squashfs",
		Options:       []string{"nodev,ro,x-gdu.hide,x-gvfs-hide"},
	}
	mountUnitName, err := sysd.EnsureMountUnitFileWithOptions(mountUnitOptions)

	c.Assert(err, IsNil)
	defer os.Remove(mountUnitName)

	c.Check(mockMountCmd.Calls()[0], DeepEquals, []string{"mount", "-t", "fuse.squashfuse", mockSnapPath, "/run/mnt/kernel-snaps/pc-kernel/1", "-o", "nodev,a,b,c"})
	where := filepath.Join(dirs.RunDir, "mnt/kernel-snaps/pc-kernel/1")
	c.Check(osutil.IsDirectory(where), Equals, true)
	c.Check(filepath.Join(dirs.SnapServicesDir, mountUnitName), testutil.FileEquals,
		fmt.Sprintf(`[Unit]
Description=Early mount unit for kernel snap
DefaultDependencies=no
After=systemd-remount-fs.service
Before=sysinit.target
Before=systemd-udevd.service systemd-modules-load.service
Before=umount.target
Conflicts=umount.target

[Mount]
What=%s
Where=/run/mnt/kernel-snaps/pc-kernel/1
Type=squashfs
Options=nodev,ro,x-gdu.hide,x-gvfs-hide

[Install]
WantedBy=sysinit.target
`, mockSnapPath))
}

func (s *SystemdTestSuite) TestPreseedModeMountError(c *C) {
	sysd := NewEmulationMode(dirs.GlobalRootDir)

	restore := squashfs.MockNeedsFuse(false)
	defer restore()

	mockMountCmd := testutil.MockCommand(c, "mount", `echo "some failure"; exit 1`)
	defer mockMountCmd.Restore()

	mockSnapPath := filepath.Join(c.MkDir(), "/var/lib/snappy/snaps/foo_1.0.snap")
	makeMockFile(c, mockSnapPath)

	_, err := sysd.EnsureMountUnitFile("42", mockSnapPath, "/snap/snapname/123", "squashfs")
	c.Assert(err, ErrorMatches, `cannot mount .*/var/lib/snappy/snaps/foo_1.0.snap \(squashfs\) at /snap/snapname/123 in preseed mode: exit status 1; some failure\n`)
}

func (s *SystemdTestSuite) TestPreseedModeRemoveMountUnit(c *C) {
	mountDir := dirs.GlobalRootDir + "/snap/foo/42"

	restore := MockOsutilIsMounted(func(path string) (bool, error) {
		c.Check(path, Equals, mountDir)
		return true, nil
	})
	defer restore()

	mockUmountCmd := testutil.MockCommand(c, "umount", "")
	defer mockUmountCmd.Restore()

	sysd := NewEmulationMode(dirs.GlobalRootDir)

	mountUnit := makeMockMountUnit(c, mountDir)

	c.Assert(sysd.RemoveMountUnitFile(mountDir), IsNil)

	c.Check(s.argses, DeepEquals, [][]string{{"--root", dirs.GlobalRootDir, "disable", "snap-foo-42.mount"}})

	// the file is gone
	c.Check(osutil.FileExists(mountUnit), Equals, false)

	// umount was called
	c.Check(mockUmountCmd.Calls(), DeepEquals, [][]string{{"umount", "-d", "-l", mountDir}})
}

func (s *SystemdTestSuite) TestPreseedModeRemoveMountUnitUnmounted(c *C) {
	mountDir := dirs.GlobalRootDir + "/snap/foo/42"

	restore := MockOsutilIsMounted(func(path string) (bool, error) {
		c.Check(path, Equals, mountDir)
		return false, nil
	})
	defer restore()

	mockUmountCmd := testutil.MockCommand(c, "umount", "")
	defer mockUmountCmd.Restore()

	sysd := NewEmulationMode(dirs.GlobalRootDir)
	mountUnit := makeMockMountUnit(c, mountDir)
	symlinkPath := filepath.Join(dirs.SnapServicesDir, "snapd.mounts.target.wants", filepath.Base(mountUnit))
	c.Assert(os.Symlink(mountUnit, symlinkPath), IsNil)
	// This is only to handle older version of snapd
	oldSymlinkPath := filepath.Join(dirs.SnapServicesDir, "multi-user.target.wants", filepath.Base(mountUnit))
	c.Assert(os.Symlink(mountUnit, oldSymlinkPath), IsNil)

	c.Assert(sysd.RemoveMountUnitFile(mountDir), IsNil)

	c.Check(s.argses, DeepEquals, [][]string{{"--root", dirs.GlobalRootDir, "disable", "snap-foo-42.mount"}})
	// umount was not called
	c.Check(mockUmountCmd.Calls(), HasLen, 0)
}

func (s *SystemdTestSuite) TestPreseedModeBindmountNotSupported(c *C) {
	sysd := NewEmulationMode(dirs.GlobalRootDir)

	restore := squashfs.MockNeedsFuse(false)
	defer restore()

	mockSnapPath := c.MkDir()

	_, err := sysd.EnsureMountUnitFile("42", mockSnapPath, "/snap/snapname/123", "")
	c.Assert(err, ErrorMatches, `bind-mounted directory is not supported in emulation mode`)
}

func (s *SystemdTestSuite) TestEnableInEmulationMode(c *C) {
	sysd := NewEmulationMode("/path")
	c.Assert(sysd.EnableNoReload([]string{"foo"}), IsNil)

	sysd = NewEmulationMode("")
	c.Assert(sysd.EnableNoReload([]string{"bar"}), IsNil)
	c.Check(s.argses, DeepEquals, [][]string{
		{"--root", "/path", "enable", "foo"},
		{"--root", dirs.GlobalRootDir, "enable", "bar"}})
}

func (s *SystemdTestSuite) TestDisableInEmulationMode(c *C) {
	sysd := NewEmulationMode("/path")
	c.Assert(sysd.DisableNoReload([]string{"foo"}), IsNil)

	c.Check(s.argses, DeepEquals, [][]string{
		{"--root", "/path", "disable", "foo"}})
}

func (s *SystemdTestSuite) TestMaskInEmulationMode(c *C) {
	sysd := NewEmulationMode("/path")
	c.Assert(sysd.Mask("foo"), IsNil)

	c.Check(s.argses, DeepEquals, [][]string{
		{"--root", "/path", "mask", "foo"}})
}

func (s *SystemdTestSuite) TestUnmaskInEmulationMode(c *C) {
	sysd := NewEmulationMode("/path")
	c.Assert(sysd.Unmask("foo"), IsNil)

	c.Check(s.argses, DeepEquals, [][]string{
		{"--root", "/path", "unmask", "foo"}})
}

func (s *SystemdTestSuite) TestListMountUnitsEmpty(c *C) {
	s.outs = [][]byte{
		[]byte("\n"),
	}

	sysd := New(SystemMode, nil)
	units, err := sysd.ListMountUnits("some-snap", "")
	c.Check(units, HasLen, 0)
	c.Check(err, IsNil)
}

func (s *SystemdTestSuite) TestListMountUnitsMalformed(c *C) {
	s.outs = [][]byte{
		[]byte(`Description=Mount unit for some-snap, revision x1
Where=/somewhere/here
FragmentPath=/etc/systemd/system/somewhere-here.mount
HereIsOneLineWithoutAnEqualSign
`),
	}

	sysd := New(SystemMode, nil)
	units, err := sysd.ListMountUnits("some-snap", "")
	c.Check(units, HasLen, 0)
	c.Check(err, ErrorMatches, "cannot parse systemctl output:.*")
}

func (s *SystemdTestSuite) TestListMountUnitsHappy(c *C) {
	tmpDir, err := ioutil.TempDir("/tmp", "snapd-systemd-test-list-mounts-*")
	c.Assert(err, IsNil)
	defer os.RemoveAll(tmpDir)

	var systemctlOutput string
	createFakeUnit := func(fileName, snapName, where, origin string) error {
		path := filepath.Join(tmpDir, fileName)
		if len(systemctlOutput) > 0 {
			systemctlOutput += "\n\n"
		}
		systemctlOutput += fmt.Sprintf(`Description=Mount unit for %s, revision x1
Where=%s
FragmentPath=%s
`, snapName, where, path)
		contents := fmt.Sprintf(`[Unit]
Description=Mount unit for %s, revision x1

[Mount]
What=/does/not/matter
Where=%s
Type=doesntmatter
Options=do,not,matter,either

[Install]
WantedBy=doesntmatter.target
X-SnapdOrigin=%s
`, snapName, where, origin)
		return os.WriteFile(path, []byte(contents), 0644)
	}

	// Prepare the unit files
	err = createFakeUnit("somepath-somedir.mount", "some-snap", "/somepath/somedir", "module1")
	c.Assert(err, IsNil)
	err = createFakeUnit("somewhere-here.mount", "some-other-snap", "/somewhere/here", "module2")
	c.Assert(err, IsNil)
	err = createFakeUnit("somewhere-there.mount", "some-snap", "/somewhere/there", "module3")
	c.Assert(err, IsNil)

	s.outs = [][]byte{
		[]byte(systemctlOutput),
	}
	sysd := New(SystemMode, nil)

	// First, get all mount units for some-snap, without filter on the origin module
	units, err := sysd.ListMountUnits("some-snap", "")
	c.Check(units, DeepEquals, []string{"/somepath/somedir", "/somewhere/there"})
	c.Check(err, IsNil)

	// Now repeat the same, filtering on the origin module
	s.i = 0 // this resets the systemctl output iterator back to the beginning
	units, err = sysd.ListMountUnits("some-snap", "module3")
	c.Check(units, DeepEquals, []string{"/somewhere/there"})
	c.Check(err, IsNil)
}

func (s *SystemdTestSuite) TestMountHappy(c *C) {
	sysd := New(SystemMode, nil)

	cmd := testutil.MockCommand(c, "systemd-mount", "")
	defer cmd.Restore()

	c.Assert(sysd.Mount("foo", "bar"), IsNil)
	c.Check(cmd.Calls(), DeepEquals, [][]string{
		{"systemd-mount", "foo", "bar"},
	})
	cmd.ForgetCalls()
	c.Assert(sysd.Mount("foo", "bar", "-o", "bind"), IsNil)
	c.Check(cmd.Calls(), DeepEquals, [][]string{
		{"systemd-mount", "-o", "bind", "foo", "bar"},
	})
}

func (s *SystemdTestSuite) TestMountErr(c *C) {
	sysd := New(SystemMode, nil)

	cmd := testutil.MockCommand(c, "systemd-mount", `echo "failed"; exit 111`)
	defer cmd.Restore()

	err := sysd.Mount("foo", "bar")
	c.Assert(err, ErrorMatches, "failed")
	c.Check(cmd.Calls(), DeepEquals, [][]string{
		{"systemd-mount", "foo", "bar"},
	})
}

func (s *SystemdTestSuite) TestUmountHappy(c *C) {
	sysd := New(SystemMode, nil)

	cmd := testutil.MockCommand(c, "systemd-mount", "")
	defer cmd.Restore()

	c.Assert(sysd.Umount("bar"), IsNil)
	c.Check(cmd.Calls(), DeepEquals, [][]string{
		{"systemd-mount", "--umount", "bar"},
	})
}

func (s *SystemdTestSuite) TestUmountErr(c *C) {
	sysd := New(SystemMode, nil)

	cmd := testutil.MockCommand(c, "systemd-mount", `echo "failed"; exit 111`)
	defer cmd.Restore()

	err := sysd.Umount("bar")
	c.Assert(err, ErrorMatches, "failed")
	c.Check(cmd.Calls(), DeepEquals, [][]string{
		{"systemd-mount", "--umount", "bar"},
	})
}

func (s *SystemdTestSuite) TestCurrentUsageFamilyReallyInvalid(c *C) {
	s.outs = [][]byte{
		[]byte(`gahstringsarehard`),
		[]byte(`gahstringsarehard`),
	}
	sysd := New(SystemMode, s.rep)
	_, err := sysd.CurrentMemoryUsage("bar.service")
	c.Assert(err, ErrorMatches, `invalid property format from systemd for MemoryCurrent \(got gahstringsarehard\)`)
	_, err = sysd.CurrentTasksCount("bar.service")
	c.Assert(err, ErrorMatches, `invalid property format from systemd for TasksCurrent \(got gahstringsarehard\)`)
	c.Check(s.argses, DeepEquals, [][]string{
		{"show", "--property", "MemoryCurrent", "bar.service"},
		{"show", "--property", "TasksCurrent", "bar.service"},
	})
}

func (s *SystemdTestSuite) TestCurrentUsageFamilyInactive(c *C) {
	s.outs = [][]byte{
		[]byte(`MemoryCurrent=[not set]`),
		[]byte(`TasksCurrent=[not set]`),
	}
	sysd := New(SystemMode, s.rep)
	_, err := sysd.CurrentMemoryUsage("bar.service")
	c.Assert(err, ErrorMatches, "memory usage unavailable")
	_, err = sysd.CurrentTasksCount("bar.service")
	c.Assert(err, ErrorMatches, "tasks count unavailable")
	c.Check(s.argses, DeepEquals, [][]string{
		{"show", "--property", "MemoryCurrent", "bar.service"},
		{"show", "--property", "TasksCurrent", "bar.service"},
	})
}

func (s *SystemdTestSuite) TestCurrentUsageFamilyInvalid(c *C) {
	s.outs = [][]byte{
		[]byte(`MemoryCurrent=blahhhhhhhhhhhhhh`),
		[]byte(`TasksCurrent=blahhhhhhhhhhhhhh`),
	}
	sysd := New(SystemMode, s.rep)
	_, err := sysd.CurrentMemoryUsage("bar.service")
	c.Assert(err, ErrorMatches, `invalid property value from systemd for MemoryCurrent: cannot parse "blahhhhhhhhhhhhhh" as an integer`)
	_, err = sysd.CurrentTasksCount("bar.service")
	c.Assert(err, ErrorMatches, `invalid property value from systemd for TasksCurrent: cannot parse "blahhhhhhhhhhhhhh" as an integer`)
	c.Check(s.argses, DeepEquals, [][]string{
		{"show", "--property", "MemoryCurrent", "bar.service"},
		{"show", "--property", "TasksCurrent", "bar.service"},
	})
}

func (s *SystemdTestSuite) TestCurrentUsageFamilyHappy(c *C) {
	s.outs = [][]byte{
		[]byte(`MemoryCurrent=1024`),
		[]byte(`MemoryCurrent=18446744073709551615`), // special value from systemd bug
		[]byte(`TasksCurrent=10`),
	}
	sysd := New(SystemMode, s.rep)
	memUsage, err := sysd.CurrentMemoryUsage("bar.service")
	c.Assert(err, IsNil)
	c.Assert(memUsage, Equals, quantity.SizeKiB)
	memUsage, err = sysd.CurrentMemoryUsage("bar.service")
	c.Assert(err, IsNil)
	const sixteenExb = quantity.Size(1<<64 - 1)
	c.Assert(memUsage, Equals, sixteenExb)
	tasksUsage, err := sysd.CurrentTasksCount("bar.service")
	c.Assert(tasksUsage, Equals, uint64(10))
	c.Assert(err, IsNil)
	c.Check(s.argses, DeepEquals, [][]string{
		{"show", "--property", "MemoryCurrent", "bar.service"},
		{"show", "--property", "MemoryCurrent", "bar.service"},
		{"show", "--property", "TasksCurrent", "bar.service"},
	})
}

func (s *SystemdTestSuite) TestInactiveEnterTimestampZero(c *C) {
	s.outs = [][]byte{
		[]byte(`InactiveEnterTimestamp=`),
	}
	sysd := New(SystemMode, s.rep)
	stamp, err := sysd.InactiveEnterTimestamp("bar.service")
	c.Assert(err, IsNil)
	c.Check(s.argses, DeepEquals, [][]string{
		{"show", "--property", "InactiveEnterTimestamp", "bar.service"},
	})
	c.Check(stamp.IsZero(), Equals, true)
}

func (s *SystemdTestSuite) TestInactiveEnterTimestampValidWhitespace(c *C) {
	s.outs = [][]byte{
		[]byte(`InactiveEnterTimestamp=Fri 2021-04-16 15:32:21 UTC
`),
	}

	stamp, err := New(SystemMode, s.rep).InactiveEnterTimestamp("bar.service")
	c.Assert(err, IsNil)
	c.Check(s.argses, DeepEquals, [][]string{
		{"show", "--property", "InactiveEnterTimestamp", "bar.service"},
	})
	c.Check(stamp.Equal(time.Date(2021, time.April, 16, 15, 32, 21, 0, time.UTC)), Equals, true)
}

func (s *SystemdTestSuite) TestInactiveEnterTimestampValid(c *C) {
	s.outs = [][]byte{
		[]byte(`InactiveEnterTimestamp=Fri 2021-04-16 15:32:21 UTC`),
	}

	stamp, err := New(SystemMode, s.rep).InactiveEnterTimestamp("bar.service")
	c.Assert(err, IsNil)
	c.Check(s.argses, DeepEquals, [][]string{
		{"show", "--property", "InactiveEnterTimestamp", "bar.service"},
	})
	c.Check(stamp.Equal(time.Date(2021, time.April, 16, 15, 32, 21, 0, time.UTC)), Equals, true)
}

func (s *SystemdTestSuite) TestInactiveEnterTimestampFailure(c *C) {
	s.outs = [][]byte{
		[]byte(`mocked failure`),
	}
	s.errors = []error{
		fmt.Errorf("mocked failure"),
	}
	stamp, err := New(SystemMode, s.rep).InactiveEnterTimestamp("bar.service")
	c.Assert(err, ErrorMatches, "mocked failure")
	c.Check(stamp.IsZero(), Equals, true)
}

func (s *SystemdTestSuite) TestInactiveEnterTimestampMalformed(c *C) {
	s.outs = [][]byte{
		[]byte(`InactiveEnterTimestamp`),
		[]byte(``),
		[]byte(`some random garbage
with newlines`),
	}
	sysd := New(SystemMode, s.rep)
	for i := 0; i < len(s.outs); i++ {
		s.argses = nil
		stamp, err := sysd.InactiveEnterTimestamp("bar.service")
		c.Assert(err.Error(), testutil.Contains, `invalid property format from systemd for InactiveEnterTimestamp (got`)
		c.Check(s.argses, DeepEquals, [][]string{
			{"show", "--property", "InactiveEnterTimestamp", "bar.service"},
		})
		c.Check(stamp.IsZero(), Equals, true)
	}
}

func (s *SystemdTestSuite) TestInactiveEnterTimestampMalformedMore(c *C) {
	s.outs = [][]byte{
		[]byte(`InactiveEnterTimestamp=0`), // 0 is valid for InactiveEnterTimestampMonotonic
	}
	sysd := New(SystemMode, s.rep)

	stamp, err := sysd.InactiveEnterTimestamp("bar.service")

	c.Assert(err, ErrorMatches, `internal error: systemctl time output \(0\) is malformed`)
	c.Check(s.argses, DeepEquals, [][]string{
		{"show", "--property", "InactiveEnterTimestamp", "bar.service"},
	})
	c.Check(stamp.IsZero(), Equals, true)
}

func (s *SystemdTestSuite) TestSystemdRunError(c *C) {
	sr := testutil.MockCommand(c, "systemd-run", `echo "fail"; exit 11`)
	defer sr.Restore()

	sysd := New(SystemMode, s.rep)
	output, err := sysd.Run([]string{"bad-cmd", "arg1"}, nil)
	c.Check(output, IsNil)
	c.Assert(err, ErrorMatches, `cannot run \["bad-cmd" "arg1"\]: fail`)
}

func (s *SystemdTestSuite) TestSystemdRunHappyNoStderr(c *C) {
	sr := testutil.MockCommand(c, "systemd-run", `echo "happy output" && >&2 echo "to stderr"`)
	defer sr.Restore()

	sysd := New(SystemMode, s.rep)
	output, err := sysd.Run([]string{"happy-cmd", "arg1"}, nil)
	c.Check(string(output), Equals, "happy output\n")
	c.Check(err, IsNil)
	c.Check(sr.Calls(), DeepEquals, [][]string{
		{"systemd-run", "--wait", "--pipe", "--collect", "--service-type=exec", "--quiet", "--", "happy-cmd", "arg1"},
	})
}

func (s *SystemdTestSuite) TestSystemdRunHappyWithStdin(c *C) {
	sr := testutil.MockCommand(c, "systemd-run", `echo "some output" && cat - `)
	defer sr.Restore()

	sysd := New(SystemMode, s.rep)
	opts := &RunOptions{Stdin: bytes.NewBufferString("stdin input\n")}
	output, err := sysd.Run([]string{"cmd-with-stdin", "arg1"}, opts)
	c.Check(string(output), Equals, "some output\nstdin input\n")
	c.Check(err, IsNil)
	c.Check(sr.Calls(), DeepEquals, [][]string{
		{"systemd-run", "--wait", "--pipe", "--collect", "--service-type=exec", "--quiet", "--", "cmd-with-stdin", "arg1"},
	})
}

func (s *SystemdTestSuite) TestSystemdRunKeyringMode(c *C) {
	sr := testutil.MockCommand(c, "systemd-run", `echo "happy output"`)
	defer sr.Restore()

	sysd := New(SystemMode, s.rep)
	opts := &RunOptions{KeyringMode: KeyringModePrivate}
	output, err := sysd.Run([]string{"happy-cmd", "arg1"}, opts)
	c.Check(string(output), Equals, "happy output\n")
	c.Check(err, IsNil)
	c.Check(sr.Calls(), DeepEquals, [][]string{
		{"systemd-run", "--wait", "--pipe", "--collect", "--service-type=exec", "--quiet", "--property=KeyringMode=private", "--", "happy-cmd", "arg1"},
	})
}

type systemdErrorSuite struct{}

var _ = Suite(&systemdErrorSuite{})

func (s *systemdErrorSuite) TestErrorStringNormalError(c *C) {
	systemctl := testutil.MockCommand(c, "systemctl", `echo "I fail"; exit 11`)
	defer systemctl.Restore()

	_, err := Version()
	c.Check(err, ErrorMatches, `systemctl command \[--version\] failed with exit status 11: I fail`)
}

func (s *systemdErrorSuite) TestErrorStringNoOutput(c *C) {
	systemctl := testutil.MockCommand(c, "systemctl", `exit 22`)
	defer systemctl.Restore()

	_, err := Version()
	c.Check(err, ErrorMatches, `systemctl command \[--version\] failed with exit status 22`)
}

func (s *systemdErrorSuite) TestErrorStringNoSystemctl(c *C) {
	oldPath := os.Getenv("PATH")
	os.Setenv("PATH", "/xxx")
	defer func() { os.Setenv("PATH", oldPath) }()

	_, err := Version()
	c.Check(err, ErrorMatches, `systemctl command \[--version\] failed with: exec: "systemctl": executable file not found in \$PATH`)
}

func (s *systemdErrorSuite) TestEnsureMountUnitFileEnsureFileStateErr(c *C) {
	dirs.SetRootDir(c.MkDir())
	defer dirs.SetRootDir("")

	restore := osutil.MockMountInfo("")
	defer restore()

	mockSnapPath := filepath.Join(dirs.GlobalRootDir, "/var/lib/snappy/snaps/foo_1.0.snap")
	makeMockFile(c, mockSnapPath)

	// trigger an error below by creating a directory with the
	// same name as the mount unit file
	err := os.MkdirAll(filepath.Join(dirs.GlobalRootDir, "/etc/systemd/system/snap-snapname-123.mount"), 0755)
	c.Assert(err, IsNil)

	_, err = New(SystemMode, nil).EnsureMountUnitFile("42", mockSnapPath, "/snap/snapname/123", "squashfs")
	c.Assert(err, ErrorMatches, fmt.Sprintf("internal error: only regular files are supported, got %q instead", os.ModeDir))
}
