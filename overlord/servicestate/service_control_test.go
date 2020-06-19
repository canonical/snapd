// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020 Canonical Ltd
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

package servicestate_test

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/overlord"
	"github.com/snapcore/snapd/overlord/servicestate"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/systemd"
	"github.com/snapcore/snapd/testutil"
)

func TestServiceControl(t *testing.T) { TestingT(t) }

type serviceControlSuite struct {
	testutil.BaseTest
	state      *state.State
	o          *overlord.Overlord
	se         *overlord.StateEngine
	serviceMgr *servicestate.ServiceManager
	sysctlArgs [][]string
}

var _ = Suite(&serviceControlSuite{})

const servicesSnapYaml1 = `name: test-snap
version: 1.0
apps:
  someapp:
    command: cmd
  foo:
    daemon: simple
  bar:
    daemon: simple
    after: [foo]
`

func (s *serviceControlSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)

	dirs.SetRootDir(c.MkDir())
	s.AddCleanup(func() { dirs.SetRootDir("") })

	s.o = overlord.Mock()
	s.state = s.o.State()

	s.sysctlArgs = nil
	systemctlRestorer := systemd.MockSystemctl(func(cmd ...string) (buf []byte, err error) {
		s.sysctlArgs = append(s.sysctlArgs, cmd)
		if cmd[0] == "show" {
			return []byte("ActiveState=inactive\n"), nil
		}
		return nil, nil
	})
	s.AddCleanup(systemctlRestorer)

	s.serviceMgr = servicestate.Manager(s.state, s.o.TaskRunner())
	s.o.AddManager(s.serviceMgr)
	s.o.AddManager(s.o.TaskRunner())
	s.se = s.o.StateEngine()
	c.Assert(s.o.StartUp(), IsNil)
}

func (s *serviceControlSuite) mockTestSnap(c *C) {
	si := snap.SideInfo{
		RealName: "test-snap",
		Revision: snap.R(7),
	}
	snaptest.MockSnap(c, servicesSnapYaml1, &si)
	snapstate.Set(s.state, "test-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{&si},
		Current:  snap.R(7),
		SnapType: "app",
	})

	// mock systemd service units, this is required when testing "stop"
	err := os.MkdirAll(filepath.Join(dirs.GlobalRootDir, "etc/systemd/system/"), 0775)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(filepath.Join(dirs.GlobalRootDir, "etc/systemd/system/snap.test-snap.foo.service"), nil, 0644)
	c.Assert(err, IsNil)
}

func (s *serviceControlSuite) TestNoServiceCommandError(c *C) {
	st := s.state
	st.Lock()

	chg := st.NewChange("service-control", "...")
	t := st.NewTask("service-control", "...")
	chg.AddTask(t)

	st.Unlock()
	defer s.se.Stop()
	c.Assert(s.o.Settle(5*time.Second), IsNil)
	st.Lock()

	c.Assert(t.Status(), Equals, state.ErrorStatus)
	c.Check(chg.Err(), ErrorMatches, `cannot perform the following tasks:\n.*internal error: cannot get service-action: no state entry for key.*`)
}

func (s *serviceControlSuite) TestNoopWhenNoServices(c *C) {
	st := s.state
	st.Lock()

	si := snap.SideInfo{RealName: "test-snap", Revision: snap.R(7)}
	snaptest.MockSnap(c, `name: test-snap`, &si)
	snapstate.Set(st, "test-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{&si},
		Current:  snap.R(7),
		SnapType: "app",
	})

	chg := st.NewChange("service-control", "...")
	t := st.NewTask("service-control", "...")
	cmd := &servicestate.ServiceAction{SnapName: "test-snap"}
	t.Set("service-action", cmd)
	chg.AddTask(t)

	st.Unlock()
	defer s.se.Stop()
	c.Assert(s.o.Settle(5*time.Second), IsNil)
	st.Lock()

	c.Check(t.Status(), Equals, state.DoneStatus)
}

func (s *serviceControlSuite) TestUnhandledServiceAction(c *C) {
	st := s.state
	st.Lock()

	s.mockTestSnap(c)

	chg := st.NewChange("service-control", "...")
	t := st.NewTask("service-control", "...")
	cmd := &servicestate.ServiceAction{SnapName: "test-snap", Action: "foo"}
	t.Set("service-action", cmd)
	chg.AddTask(t)

	st.Unlock()
	defer s.se.Stop()
	c.Assert(s.o.Settle(5*time.Second), IsNil)
	st.Lock()

	c.Assert(t.Status(), Equals, state.ErrorStatus)
	c.Check(chg.Err(), ErrorMatches, `cannot perform the following tasks:\n.*unhandled service action: "foo".*`)
}

func (s *serviceControlSuite) TestUnknownService(c *C) {
	st := s.state
	st.Lock()

	s.mockTestSnap(c)

	chg := st.NewChange("service-control", "...")
	t := st.NewTask("service-control", "...")
	cmd := &servicestate.ServiceAction{
		SnapName: "test-snap",
		Action:   "start",
		Services: []string{"baz"},
	}
	t.Set("service-action", cmd)
	chg.AddTask(t)

	st.Unlock()
	defer s.se.Stop()
	c.Assert(s.o.Settle(5*time.Second), IsNil)
	st.Lock()

	c.Assert(t.Status(), Equals, state.ErrorStatus)
	c.Check(chg.Err(), ErrorMatches, `cannot perform the following tasks:\n.*no such service: baz.*`)
}

func (s *serviceControlSuite) TestNotAService(c *C) {
	st := s.state
	st.Lock()

	s.mockTestSnap(c)

	chg := st.NewChange("service-control", "...")
	t := st.NewTask("service-control", "...")
	cmd := &servicestate.ServiceAction{
		SnapName: "test-snap",
		Action:   "start",
		Services: []string{"someapp"},
	}
	t.Set("service-action", cmd)
	chg.AddTask(t)

	st.Unlock()
	defer s.se.Stop()
	c.Assert(s.o.Settle(5*time.Second), IsNil)
	st.Lock()

	c.Assert(t.Status(), Equals, state.ErrorStatus)
	c.Check(chg.Err(), ErrorMatches, `cannot perform the following tasks:\n.*someapp is not a service.*`)
}

func (s *serviceControlSuite) TestStartAllServices(c *C) {
	st := s.state
	st.Lock()

	s.mockTestSnap(c)

	chg := st.NewChange("service-control", "...")
	t := st.NewTask("service-control", "...")
	cmd := &servicestate.ServiceAction{
		SnapName: "test-snap",
		Action:   "start",
	}
	t.Set("service-action", cmd)
	chg.AddTask(t)

	st.Unlock()
	defer s.se.Stop()
	c.Assert(s.o.Settle(5*time.Second), IsNil)
	st.Lock()

	c.Assert(t.Status(), Equals, state.DoneStatus)

	c.Check(s.sysctlArgs, DeepEquals, [][]string{
		{"--root", dirs.GlobalRootDir, "is-enabled", "snap.test-snap.foo.service"},
		{"--root", dirs.GlobalRootDir, "is-enabled", "snap.test-snap.bar.service"},
		{"start", "snap.test-snap.foo.service"},
		{"start", "snap.test-snap.bar.service"},
	})
}

func (s *serviceControlSuite) TestStartListedServices(c *C) {
	st := s.state
	st.Lock()

	s.mockTestSnap(c)

	chg := st.NewChange("service-control", "...")
	t := st.NewTask("service-control", "...")
	cmd := &servicestate.ServiceAction{
		SnapName: "test-snap",
		Action:   "start",
		Services: []string{"foo"},
	}
	t.Set("service-action", cmd)
	chg.AddTask(t)

	st.Unlock()
	defer s.se.Stop()
	c.Assert(s.o.Settle(5*time.Second), IsNil)
	st.Lock()

	c.Assert(t.Status(), Equals, state.DoneStatus)
	c.Check(s.sysctlArgs, DeepEquals, [][]string{
		{"--root", dirs.GlobalRootDir, "is-enabled", "snap.test-snap.foo.service"},
		{"start", "snap.test-snap.foo.service"},
	})
}

func (s *serviceControlSuite) TestStartEnableServices(c *C) {
	st := s.state
	st.Lock()

	s.mockTestSnap(c)

	chg := st.NewChange("service-control", "...")
	t := st.NewTask("service-control", "...")
	cmd := &servicestate.ServiceAction{
		SnapName:       "test-snap",
		Action:         "start",
		ActionModifier: "enable",
		Services:       []string{"foo"},
	}
	t.Set("service-action", cmd)
	chg.AddTask(t)

	st.Unlock()
	defer s.se.Stop()
	c.Assert(s.o.Settle(5*time.Second), IsNil)
	st.Lock()

	c.Assert(t.Status(), Equals, state.DoneStatus)
	c.Check(s.sysctlArgs, DeepEquals, [][]string{
		{"--root", dirs.GlobalRootDir, "is-enabled", "snap.test-snap.foo.service"},
		{"start", "snap.test-snap.foo.service"},
	})
}

func (s *serviceControlSuite) TestStartEnableMultipleServices(c *C) {
	st := s.state
	st.Lock()

	s.mockTestSnap(c)

	chg := st.NewChange("service-control", "...")
	t := st.NewTask("service-control", "...")
	cmd := &servicestate.ServiceAction{
		SnapName:       "test-snap",
		Action:         "start",
		ActionModifier: "enable",
	}
	t.Set("service-action", cmd)
	chg.AddTask(t)

	st.Unlock()
	defer s.se.Stop()
	c.Assert(s.o.Settle(5*time.Second), IsNil)
	st.Lock()

	c.Assert(t.Status(), Equals, state.DoneStatus)
	c.Check(s.sysctlArgs, DeepEquals, [][]string{
		{"--root", dirs.GlobalRootDir, "is-enabled", "snap.test-snap.foo.service"},
		{"--root", dirs.GlobalRootDir, "is-enabled", "snap.test-snap.bar.service"},
		{"start", "snap.test-snap.foo.service"},
		{"start", "snap.test-snap.bar.service"},
	})
}

func (s *serviceControlSuite) TestStopServices(c *C) {
	st := s.state
	st.Lock()

	s.mockTestSnap(c)

	chg := st.NewChange("service-control", "...")
	t := st.NewTask("service-control", "...")
	cmd := &servicestate.ServiceAction{
		SnapName: "test-snap",
		Action:   "stop",
		Services: []string{"foo"},
	}
	t.Set("service-action", cmd)
	chg.AddTask(t)

	st.Unlock()
	defer s.se.Stop()
	c.Assert(s.o.Settle(5*time.Second), IsNil)
	st.Lock()

	c.Assert(t.Status(), Equals, state.DoneStatus)
	c.Check(s.sysctlArgs, DeepEquals, [][]string{
		{"stop", "snap.test-snap.foo.service"},
		{"show", "--property=ActiveState", "snap.test-snap.foo.service"},
	})
}

func (s *serviceControlSuite) TestStopDisableServices(c *C) {
	st := s.state
	st.Lock()

	s.mockTestSnap(c)

	chg := st.NewChange("service-control", "...")
	t := st.NewTask("service-control", "...")
	cmd := &servicestate.ServiceAction{
		SnapName:       "test-snap",
		Action:         "stop",
		ActionModifier: "disable",
		Services:       []string{"foo"},
	}
	t.Set("service-action", cmd)
	chg.AddTask(t)

	st.Unlock()
	defer s.se.Stop()
	c.Assert(s.o.Settle(5*time.Second), IsNil)
	st.Lock()

	c.Assert(t.Status(), Equals, state.DoneStatus)
	c.Check(s.sysctlArgs, DeepEquals, [][]string{
		{"stop", "snap.test-snap.foo.service"},
		{"show", "--property=ActiveState", "snap.test-snap.foo.service"},
		{"--root", dirs.GlobalRootDir, "disable", "snap.test-snap.foo.service"},
	})
}

func (s *serviceControlSuite) TestRestartServices(c *C) {
	st := s.state
	st.Lock()

	s.mockTestSnap(c)

	chg := st.NewChange("service-control", "...")
	t := st.NewTask("service-control", "...")
	cmd := &servicestate.ServiceAction{
		SnapName: "test-snap",
		Action:   "restart",
		Services: []string{"foo"},
	}
	t.Set("service-action", cmd)
	chg.AddTask(t)

	st.Unlock()
	defer s.se.Stop()
	c.Assert(s.o.Settle(5*time.Second), IsNil)
	st.Lock()

	c.Assert(t.Status(), Equals, state.DoneStatus)
	c.Check(s.sysctlArgs, DeepEquals, [][]string{
		{"stop", "snap.test-snap.foo.service"},
		{"show", "--property=ActiveState", "snap.test-snap.foo.service"},
		{"start", "snap.test-snap.foo.service"},
	})
}

func (s *serviceControlSuite) TestReloadServices(c *C) {
	st := s.state
	st.Lock()

	s.mockTestSnap(c)

	chg := st.NewChange("service-control", "...")
	t := st.NewTask("service-control", "...")
	cmd := &servicestate.ServiceAction{
		SnapName: "test-snap",
		Action:   "reload-or-restart",
		Services: []string{"foo"},
	}
	t.Set("service-action", cmd)
	chg.AddTask(t)

	st.Unlock()
	defer s.se.Stop()
	c.Assert(s.o.Settle(5*time.Second), IsNil)
	st.Lock()

	c.Assert(t.Status(), Equals, state.DoneStatus)
	c.Check(s.sysctlArgs, DeepEquals, [][]string{
		{"reload-or-restart", "snap.test-snap.foo.service"},
	})
}

func (s *serviceControlSuite) TestConflict(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	s.mockTestSnap(c)

	chg := st.NewChange("service-control", "...")
	t := st.NewTask("service-control", "...")
	cmd := &servicestate.ServiceAction{
		SnapName: "test-snap",
		Action:   "reload-or-restart",
		Services: []string{"foo"},
	}
	t.Set("service-action", cmd)
	chg.AddTask(t)

	_, err := snapstate.Remove(st, "test-snap", snap.Revision{}, nil)
	c.Assert(err, ErrorMatches, `snap "test-snap" has "service-control" change in progress`)
}
