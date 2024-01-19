// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2016 Canonical Ltd
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

package internal_test

import (
	"fmt"
	"os"
	"sort"
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	_ "github.com/snapcore/snapd/interfaces/builtin"
	"github.com/snapcore/snapd/progress"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/systemd"
	"github.com/snapcore/snapd/systemd/systemdtest"
	"github.com/snapcore/snapd/testutil"
	"github.com/snapcore/snapd/usersession/agent"
	"github.com/snapcore/snapd/usersession/client"
	"github.com/snapcore/snapd/wrappers"
	"github.com/snapcore/snapd/wrappers/internal"
)

type serviceStatusSuite struct {
	testutil.DBusTest
	tempdir                           string
	sysdLog                           [][]string
	systemctlRestorer, delaysRestorer func()
	agent                             *agent.SessionAgent
}

var _ = Suite(&serviceStatusSuite{})

func (s *serviceStatusSuite) SetUpTest(c *C) {
	s.DBusTest.SetUpTest(c)
	s.tempdir = c.MkDir()
	s.sysdLog = nil
	dirs.SetRootDir(s.tempdir)

	s.systemctlRestorer = systemd.MockSystemctl(func(cmd ...string) ([]byte, error) {
		s.sysdLog = append(s.sysdLog, cmd)
		return []byte("ActiveState=inactive\n"), nil
	})
	s.delaysRestorer = systemd.MockStopDelays(2*time.Millisecond, 4*time.Millisecond)

	xdgRuntimeDir := fmt.Sprintf("%s/%d", dirs.XdgRuntimeDirBase, os.Getuid())
	err := os.MkdirAll(xdgRuntimeDir, 0700)
	c.Assert(err, IsNil)
	s.agent, err = agent.New()
	c.Assert(err, IsNil)
	s.agent.Start()
}

func (s *serviceStatusSuite) TearDownTest(c *C) {
	if s.agent != nil {
		err := s.agent.Stop()
		c.Check(err, IsNil)
	}
	s.systemctlRestorer()
	s.delaysRestorer()
	dirs.SetRootDir("")
	s.DBusTest.TearDownTest(c)
}

// addSnapServices adds service units for the snap applications which
// are services. The services do not get enabled or started.
func (s *serviceStatusSuite) addSnapServices(snapInfo *snap.Info, preseeding bool) error {
	m := map[*snap.Info]*wrappers.SnapServiceOptions{
		snapInfo: nil,
	}
	ensureOpts := &wrappers.EnsureSnapServicesOptions{
		Preseeding: preseeding,
	}
	return wrappers.EnsureSnapServices(m, ensureOpts, nil, progress.Null)
}

func (s *serviceStatusSuite) TestQueryServiceStatusMany(c *C) {
	const snapYaml = `name: test-snap
version: 1.0
apps:
  foo:
    command: bin/foo
    daemon: simple
    daemon-scope: user
  bar:
    command: bin/bar
    daemon: simple
`
	info := snaptest.MockSnap(c, snapYaml, &snap.SideInfo{Revision: snap.R(1)})
	fooSrvFile := "snap.test-snap.foo.service"
	barSrvFile := "snap.test-snap.bar.service"

	r := systemd.MockSystemctl(func(cmd ...string) ([]byte, error) {
		s.sysdLog = append(s.sysdLog, cmd)
		if out := systemdtest.HandleMockAllUnitsActiveOutput(cmd, nil); out != nil {
			return out, nil
		}
		if cmd[0] == "--user" && cmd[1] == "show" {
			return []byte(`Type=simple
Id=snap.test-snap.foo.service
Names=snap.test-snap.foo.service
ActiveState=inactive
UnitFileState=enabled
NeedDaemonReload=no
`), nil
		}
		return []byte(`ActiveState=inactive`), nil
	})
	defer r()

	err := s.addSnapServices(info, false)
	c.Assert(err, IsNil)

	sorted := info.Services()
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Name < sorted[j].Name
	})

	sysd := systemd.New(systemd.SystemMode, progress.Null)
	svcs, usrSvcs, err := internal.QueryServiceStatusMany(sorted, sysd)
	c.Assert(err, IsNil)
	c.Assert(svcs, HasLen, 1)
	c.Check(svcs[0].Name(), Equals, "bar")
	c.Check(svcs[0].IsUserService(), Equals, false)
	c.Check(svcs[0].ServiceUnitStatus(), DeepEquals, &systemd.UnitStatus{
		Daemon:           "simple",
		Id:               barSrvFile,
		Name:             barSrvFile,
		Names:            []string{barSrvFile},
		Enabled:          true,
		Active:           true,
		Installed:        true,
		NeedDaemonReload: false,
	})
	c.Assert(usrSvcs, HasLen, 1)

	// To avoid referring directly to the uid (which may be different on different hosts)
	var hostUid int
	for hostUid = range usrSvcs {
		// we expect just one
	}

	c.Assert(usrSvcs[hostUid], HasLen, 1)
	c.Check(usrSvcs[hostUid][0].Name(), Equals, "foo")
	c.Check(usrSvcs[hostUid][0].IsUserService(), Equals, true)
	c.Check(usrSvcs[hostUid][0].ServiceUnitStatus(), DeepEquals, &systemd.UnitStatus{
		Daemon:           "simple",
		Id:               fooSrvFile,
		Name:             fooSrvFile,
		Names:            []string{fooSrvFile},
		Enabled:          true,
		Active:           false, // ActiveState=inactive
		Installed:        true,
		NeedDaemonReload: false,
	})

	c.Check(s.sysdLog, DeepEquals, [][]string{
		{"daemon-reload"},
		{"--user", "daemon-reload"},
		{"show", "--property=Id,ActiveState,UnitFileState,Type,Names,NeedDaemonReload", barSrvFile},
		{"--user", "show", "--property=Id,ActiveState,UnitFileState,Type,Names,NeedDaemonReload", fooSrvFile},
	})
}

func (s *serviceStatusSuite) TestQueryServiceStatusManyWithSockets(c *C) {
	const snapYaml = `name: test-snap
version: 1.0
apps:
  foo:
    command: bin/foo
    daemon: simple
    daemon-scope: user
    plugs: [network-bind]
    sockets:
      sock1:
        listen-stream: $SNAP_USER_COMMON/sock1.socket
        socket-mode: 0666
  bar:
    command: bin/bar
    daemon: simple
    plugs: [network-bind]
    sockets:
      sock1:
        listen-stream: $SNAP_COMMON/sock1.socket
        socket-mode: 0666
`
	info := snaptest.MockSnap(c, snapYaml, &snap.SideInfo{Revision: snap.R(1)})
	fooSrvFile := "snap.test-snap.foo.service"
	fooSockSrvFile := "snap.test-snap.foo.sock1.socket"
	barSrvFile := "snap.test-snap.bar.service"
	barSockSrvFile := "snap.test-snap.bar.sock1.socket"

	r := systemd.MockSystemctl(func(cmd ...string) ([]byte, error) {
		s.sysdLog = append(s.sysdLog, cmd)
		if out := systemdtest.HandleMockAllUnitsActiveOutput(cmd, nil); out != nil {
			return out, nil
		}
		if cmd[len(cmd)-1] == "daemon-reload" {
			return []byte(`ActiveState=inactive`), nil
		}

		return []byte(fmt.Sprintf(`Type=simple
Id=%[1]s
Names=%[1]s
ActiveState=active
UnitFileState=enabled
NeedDaemonReload=no
`, cmd[len(cmd)-1])), nil
	})
	defer r()

	err := s.addSnapServices(info, false)
	c.Assert(err, IsNil)

	sorted := info.Services()
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Name < sorted[j].Name
	})

	sysd := systemd.New(systemd.SystemMode, progress.Null)
	svcs, usrSvcs, err := internal.QueryServiceStatusMany(sorted, sysd)
	c.Assert(err, IsNil)
	c.Assert(svcs, HasLen, 1)
	c.Check(svcs[0].Name(), Equals, "bar")
	c.Check(svcs[0].IsUserService(), Equals, false)
	c.Check(svcs[0].ServiceUnitStatus(), DeepEquals, &systemd.UnitStatus{
		Daemon:           "simple",
		Id:               barSrvFile,
		Name:             barSrvFile,
		Names:            []string{barSrvFile},
		Enabled:          true,
		Active:           true,
		Installed:        true,
		NeedDaemonReload: false,
	})
	c.Check(svcs[0].ActivatorUnitStatuses(), DeepEquals, []*systemd.UnitStatus{
		{
			Daemon:           "simple",
			Id:               barSockSrvFile,
			Name:             barSockSrvFile,
			Names:            []string{barSockSrvFile},
			Enabled:          true,
			Active:           true,
			Installed:        true,
			NeedDaemonReload: false,
		},
	})
	c.Assert(usrSvcs, HasLen, 1)

	// To avoid referring directly to the uid (which may be different on different hosts)
	var hostUid int
	for hostUid = range usrSvcs {
		// we expect just one
	}

	c.Assert(usrSvcs[hostUid], HasLen, 1)
	c.Check(usrSvcs[hostUid][0].Name(), Equals, "foo")
	c.Check(usrSvcs[hostUid][0].IsUserService(), Equals, true)
	c.Check(usrSvcs[hostUid][0].ServiceUnitStatus(), DeepEquals, &systemd.UnitStatus{
		Daemon:           "simple",
		Id:               fooSrvFile,
		Name:             fooSrvFile,
		Names:            []string{fooSrvFile},
		Enabled:          true,
		Active:           true, // ActiveState=active
		Installed:        true,
		NeedDaemonReload: false,
	})
	c.Check(usrSvcs[hostUid][0].ActivatorUnitStatuses(), DeepEquals, []*systemd.UnitStatus{
		{
			Daemon:           "simple",
			Id:               fooSockSrvFile,
			Name:             fooSockSrvFile,
			Names:            []string{fooSockSrvFile},
			Enabled:          true,
			Active:           true,
			Installed:        true,
			NeedDaemonReload: false,
		},
	})

	c.Check(s.sysdLog, DeepEquals, [][]string{
		{"daemon-reload"},
		{"--user", "daemon-reload"},
		{"show", "--property=Id,ActiveState,UnitFileState,Type,Names,NeedDaemonReload", barSrvFile},
		{"show", "--property=Id,ActiveState,UnitFileState,Names", barSockSrvFile},
		{"--user", "show", "--property=Id,ActiveState,UnitFileState,Type,Names,NeedDaemonReload", fooSrvFile},
		{"--user", "show", "--property=Id,ActiveState,UnitFileState,Names", fooSockSrvFile},
	})
}

func (s *serviceStatusSuite) TestQueryServiceStatusManyWithTimers(c *C) {
	const snapYaml = `name: test-snap
version: 1.0
apps:
  foo:
    command: bin/foo
    daemon: simple
    daemon-scope: user
    timer: 10:00-12:00
  bar:
    command: bin/bar
    daemon: simple
    timer: 10:00-12:00
`
	info := snaptest.MockSnap(c, snapYaml, &snap.SideInfo{Revision: snap.R(1)})
	fooSrvFile := "snap.test-snap.foo.service"
	fooTimerSrvFile := "snap.test-snap.foo.timer"
	barSrvFile := "snap.test-snap.bar.service"
	barTimerSrvFile := "snap.test-snap.bar.timer"

	r := systemd.MockSystemctl(func(cmd ...string) ([]byte, error) {
		s.sysdLog = append(s.sysdLog, cmd)
		if out := systemdtest.HandleMockAllUnitsActiveOutput(cmd, nil); out != nil {
			return out, nil
		}
		if cmd[len(cmd)-1] == "daemon-reload" {
			return []byte(`ActiveState=inactive`), nil
		}

		return []byte(fmt.Sprintf(`Type=simple
Id=%[1]s
Names=%[1]s
ActiveState=active
UnitFileState=enabled
NeedDaemonReload=no
`, cmd[len(cmd)-1])), nil
	})
	defer r()

	err := s.addSnapServices(info, false)
	c.Assert(err, IsNil)

	sorted := info.Services()
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Name < sorted[j].Name
	})

	sysd := systemd.New(systemd.SystemMode, progress.Null)
	svcs, usrSvcs, err := internal.QueryServiceStatusMany(sorted, sysd)
	c.Assert(err, IsNil)
	c.Assert(svcs, HasLen, 1)
	c.Check(svcs[0].Name(), Equals, "bar")
	c.Check(svcs[0].IsUserService(), Equals, false)
	c.Check(svcs[0].ServiceUnitStatus(), DeepEquals, &systemd.UnitStatus{
		Daemon:           "simple",
		Id:               barSrvFile,
		Name:             barSrvFile,
		Names:            []string{barSrvFile},
		Enabled:          true,
		Active:           true,
		Installed:        true,
		NeedDaemonReload: false,
	})
	c.Check(svcs[0].ActivatorUnitStatuses(), DeepEquals, []*systemd.UnitStatus{
		{
			Daemon:           "simple",
			Id:               barTimerSrvFile,
			Name:             barTimerSrvFile,
			Names:            []string{barTimerSrvFile},
			Enabled:          true,
			Active:           true,
			Installed:        true,
			NeedDaemonReload: false,
		},
	})
	c.Assert(usrSvcs, HasLen, 1)

	// To avoid referring directly to the uid (which may be different on different hosts)
	var hostUid int
	for hostUid = range usrSvcs {
		// we expect just one
	}

	c.Assert(usrSvcs[hostUid], HasLen, 1)
	c.Check(usrSvcs[hostUid][0].Name(), Equals, "foo")
	c.Check(usrSvcs[hostUid][0].IsUserService(), Equals, true)
	c.Check(usrSvcs[hostUid][0].ServiceUnitStatus(), DeepEquals, &systemd.UnitStatus{
		Daemon:           "simple",
		Id:               fooSrvFile,
		Name:             fooSrvFile,
		Names:            []string{fooSrvFile},
		Enabled:          true,
		Active:           true, // ActiveState=active
		Installed:        true,
		NeedDaemonReload: false,
	})
	c.Check(usrSvcs[hostUid][0].ActivatorUnitStatuses(), DeepEquals, []*systemd.UnitStatus{
		{
			Daemon:           "simple",
			Id:               fooTimerSrvFile,
			Name:             fooTimerSrvFile,
			Names:            []string{fooTimerSrvFile},
			Enabled:          true,
			Active:           true,
			Installed:        true,
			NeedDaemonReload: false,
		},
	})

	c.Check(s.sysdLog, DeepEquals, [][]string{
		{"daemon-reload"},
		{"--user", "daemon-reload"},
		{"show", "--property=Id,ActiveState,UnitFileState,Type,Names,NeedDaemonReload", barSrvFile},
		{"show", "--property=Id,ActiveState,UnitFileState,Names", barTimerSrvFile},
		{"--user", "show", "--property=Id,ActiveState,UnitFileState,Type,Names,NeedDaemonReload", fooSrvFile},
		{"--user", "show", "--property=Id,ActiveState,UnitFileState,Names", fooTimerSrvFile},
	})
}

func (s *serviceStatusSuite) TestQueryServiceStatusManySystemServicesFail(c *C) {
	const snapYaml = `name: test-snap
version: 1.0
apps:
  foo:
    command: bin/foo
    daemon: simple
  bar:
    command: bin/bar
    daemon: simple
`
	info := snaptest.MockSnap(c, snapYaml, &snap.SideInfo{Revision: snap.R(1)})
	fooSrvFile := "snap.test-snap.foo.service"
	barSrvFile := "snap.test-snap.bar.service"

	r := systemd.MockSystemctl(func(cmd ...string) ([]byte, error) {
		s.sysdLog = append(s.sysdLog, cmd)
		if cmd[0] == "daemon-reload" {
			return []byte(`okay`), nil
		}
		return nil, fmt.Errorf("oh noes")
	})
	defer r()

	err := s.addSnapServices(info, false)
	c.Assert(err, IsNil)

	sorted := info.Services()
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Name < sorted[j].Name
	})

	sysd := systemd.New(systemd.SystemMode, progress.Null)
	svcs, usrSvcs, err := internal.QueryServiceStatusMany(sorted, sysd)
	c.Assert(err, ErrorMatches, `oh noes`)
	c.Assert(svcs, HasLen, 0)
	c.Assert(usrSvcs, HasLen, 0)

	c.Check(s.sysdLog, DeepEquals, [][]string{
		{"daemon-reload"},
		{"show", "--property=Id,ActiveState,UnitFileState,Type,Names,NeedDaemonReload", barSrvFile, fooSrvFile},
	})
}

func (s *serviceStatusSuite) TestQueryServiceStatusManyUserFails(c *C) {
	const snapYaml = `name: test-snap
version: 1.0
apps:
  foo:
    command: bin/foo
    daemon: simple
    daemon-scope: user
  bar:
    command: bin/foo
    daemon: simple
    daemon-scope: user
`
	info := snaptest.MockSnap(c, snapYaml, &snap.SideInfo{Revision: snap.R(1)})
	fooSrvFile := "snap.test-snap.foo.service"
	barSrvFile := "snap.test-snap.bar.service"

	r := systemd.MockSystemctl(func(cmd ...string) ([]byte, error) {
		s.sysdLog = append(s.sysdLog, cmd)
		if cmd[0] == "--user" && cmd[1] == "show" {
			if cmd[len(cmd)-1] == fooSrvFile {
				return nil, fmt.Errorf("oh no %s does not exist", fooSrvFile)
			}

			return []byte(fmt.Sprintf(`Type=simple
Id=%[1]s
Names=%[1]s
ActiveState=inactive
UnitFileState=enabled
NeedDaemonReload=no
`, cmd[len(cmd)-1])), nil
		}
		return []byte(`okay`), nil
	})
	defer r()

	err := s.addSnapServices(info, false)
	c.Assert(err, IsNil)

	sorted := info.Services()
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Name < sorted[j].Name
	})

	sysd := systemd.New(systemd.SystemMode, progress.Null)
	svcs, usrSvcs, err := internal.QueryServiceStatusMany(sorted, sysd)
	c.Assert(err, IsNil)
	c.Assert(svcs, HasLen, 0)
	c.Assert(usrSvcs, HasLen, 0)

	c.Check(s.sysdLog, DeepEquals, [][]string{
		{"--user", "daemon-reload"},
		{"--user", "show", "--property=Id,ActiveState,UnitFileState,Type,Names,NeedDaemonReload", barSrvFile},
		{"--user", "show", "--property=Id,ActiveState,UnitFileState,Type,Names,NeedDaemonReload", fooSrvFile},
	})
}

func (s *serviceStatusSuite) TestQueryServiceStatusManyUserInvalidServicesReceived(c *C) {
	const snapYaml = `name: test-snap
version: 1.0
apps:
  foo:
    command: bin/foo
    daemon: simple
    daemon-scope: user
  bar:
    command: bin/foo
    daemon: simple
    daemon-scope: user
`
	info := snaptest.MockSnap(c, snapYaml, &snap.SideInfo{Revision: snap.R(1)})
	fooSrvFile := "snap.test-snap.foo.service"

	err := s.addSnapServices(info, false)
	c.Assert(err, IsNil)

	sorted := info.Services()
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Name < sorted[j].Name
	})

	r := internal.MockUserSessionQueryServiceStatusMany(func(units []string) (map[int][]client.ServiceUnitStatus, map[int][]client.ServiceFailure, error) {
		return map[int][]client.ServiceUnitStatus{
			1000: {
				{Name: fooSrvFile},
			},
		}, nil, nil
	})
	defer r()

	sysd := systemd.New(systemd.SystemMode, progress.Null)
	svcs, usrSvcs, err := internal.QueryServiceStatusMany(sorted, sysd)
	c.Assert(err, ErrorMatches, `internal error: no status received for service snap.test-snap.bar.service`)
	c.Assert(svcs, HasLen, 0)
	c.Assert(usrSvcs, HasLen, 0)
}

func (s *serviceStatusSuite) TestSnapServiceUnits(c *C) {
	const surviveYaml = `name: test-snap
version: 1.0
apps:
  foo:
    command: bin/foo
    daemon: simple
    daemon-scope: user
    timer: 10:00-12:00,20:00-22:00
    sockets:
      sock1:
       listen-stream: $SNAP_DATA/sock1.socket
      sock2:
       listen-stream: $SNAP_DATA/sock2.socket
`
	info := snaptest.MockSnap(c, surviveYaml, &snap.SideInfo{Revision: snap.R(1)})

	svc, activators := internal.SnapServiceUnits(info.Apps["foo"])
	c.Check(svc, Equals, "snap.test-snap.foo.service")

	// The activators must appear the in following order:
	// Sockets, sorted
	// Timer unit
	c.Check(activators, DeepEquals, []string{
		"snap.test-snap.foo.sock1.socket",
		"snap.test-snap.foo.sock2.socket",
		"snap.test-snap.foo.timer",
	})
}
