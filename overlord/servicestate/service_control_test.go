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
	"sort"
	"strings"
	"testing"
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/client"
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
  abc:
    daemon: simple
    after: [bar]
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

func (s *serviceControlSuite) mockTestSnap(c *C) *snap.Info {
	si := snap.SideInfo{
		RealName: "test-snap",
		Revision: snap.R(7),
	}
	info := snaptest.MockSnap(c, servicesSnapYaml1, &si)
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

	return info
}

func verifyControlTasks(c *C, tasks []*state.Task, expectedAction, actionModifier string, expectedServices ...string) {
	// sanity, ensures test checks below are hit
	c.Assert(len(tasks) > 0, Equals, true)

	// group service names by snaps
	bySnap := make(map[string][]string)
	for _, name := range expectedServices {
		// split service name, e.g. snap.test-snap.foo.service
		parts := strings.Split(name, ".")
		c.Assert(parts, HasLen, 4)
		snapName := parts[1]
		serviceName := parts[2]
		bySnap[snapName] = append(bySnap[snapName], serviceName)
	}

	var execCommandTasks int
	var serviceControlTasks int
	// snaps from service-control tasks
	seenSnaps := make(map[string]bool)

	var i int
	for i < len(tasks) {
		var argv []string
		kind := tasks[i].Kind()
		if kind == "exec-command" {
			execCommandTasks++
			var ignore bool
			c.Assert(tasks[i].Get("ignore", &ignore), IsNil)
			c.Check(ignore, Equals, true)
			switch expectedAction {
			case "start":
				if actionModifier != "" {
					c.Assert(tasks[i].Get("argv", &argv), IsNil)
					c.Check(argv, DeepEquals, append([]string{"systemctl", actionModifier}, expectedServices...))
					i++
					wt := tasks[i].WaitTasks()
					c.Assert(wt, HasLen, 1)
					c.Assert(wt[0].ID(), Equals, tasks[i-1].ID())
				}
				c.Assert(tasks[i].Get("argv", &argv), IsNil)
				c.Check(argv, DeepEquals, append([]string{"systemctl", "start"}, expectedServices...))
			case "stop":
				if actionModifier != "" {
					c.Assert(tasks[i].Get("argv", &argv), IsNil)
					c.Check(argv, DeepEquals, append([]string{"systemctl", actionModifier}, expectedServices...))
					i++
					wt := tasks[i].WaitTasks()
					c.Assert(wt, HasLen, 1)
					c.Assert(wt[0].ID(), Equals, tasks[i-1].ID())
				}
				c.Assert(tasks[i].Get("argv", &argv), IsNil)
				c.Check(argv, DeepEquals, append([]string{"systemctl", "stop"}, expectedServices...))
			case "restart":
				if actionModifier != "" {
					c.Assert(tasks[i].Get("argv", &argv), IsNil)
					c.Check(argv, DeepEquals, append([]string{"systemctl", "reload-or-restart"}, expectedServices...))
				} else {
					c.Assert(tasks[i].Get("argv", &argv), IsNil)
					c.Check(argv, DeepEquals, append([]string{"systemctl", "restart"}, expectedServices...))
				}
			default:
				c.Fatalf("unhandled action %s", expectedAction)
			}
		} else if kind == "service-control" {
			serviceControlTasks++
			var sa servicestate.ServiceAction
			c.Assert(tasks[i].Get("service-action", &sa), IsNil)
			switch expectedAction {
			case "start":
				c.Check(sa.Action, Equals, "start")
				if actionModifier != "" {
					c.Check(sa.ActionModifier, Equals, actionModifier)
				}
			case "stop":
				c.Check(sa.Action, Equals, "stop")
				if actionModifier != "" {
					c.Check(sa.ActionModifier, Equals, actionModifier)
				}
			case "restart":
				if actionModifier == "reload" {
					c.Check(sa.Action, Equals, "reload-or-restart")
				} else {
					c.Check(sa.Action, Equals, "restart")
				}
			default:
				c.Fatalf("unhandled action %s", expectedAction)
			}
			seenSnaps[sa.SnapName] = true
			obtainedServices := sa.Services
			sort.Strings(obtainedServices)
			sort.Strings(bySnap[sa.SnapName])
			c.Assert(obtainedServices, DeepEquals, bySnap[sa.SnapName])
		} else {
			c.Fatalf("unexpected task: %s", tasks[i].Kind())
		}
		i++
	}

	c.Check(execCommandTasks > 0, Equals, true)

	// we should have one service-control task for every snap
	c.Assert(serviceControlTasks, Equals, len(bySnap))
	c.Assert(len(bySnap), Equals, len(seenSnaps))
	for snapName := range bySnap {
		c.Assert(seenSnaps[snapName], Equals, true)
	}
}

func makeControlChange(c *C, st *state.State, inst *servicestate.Instruction, info *snap.Info) *state.Change {
	apps := []*snap.AppInfo{}
	for _, name := range inst.Names {
		c.Assert(info.Apps[name], NotNil)
		apps = append(apps, info.Apps[name])
	}

	flags := &servicestate.Flags{CreateExecCommandTasks: true}
	tss, err := servicestate.Control(st, apps, inst, flags, nil)
	c.Assert(err, IsNil)

	chg := st.NewChange("service-control", "...")
	for _, ts := range tss {
		chg.AddAll(ts)
	}
	return chg
}

func (s *serviceControlSuite) TestControlDoesntCreateExecCommandTasksIfNoFlags(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	info := s.mockTestSnap(c)
	inst := &servicestate.Instruction{
		Action: "start",
		Names:  []string{"foo"},
	}

	flags := &servicestate.Flags{}
	tss, err := servicestate.Control(st, []*snap.AppInfo{info.Apps["foo"]}, inst, flags, nil)
	c.Assert(err, IsNil)
	// service-control is the only task
	c.Assert(tss, HasLen, 1)
	c.Assert(tss[0].Tasks(), HasLen, 1)
	c.Check(tss[0].Tasks()[0].Kind(), Equals, "service-control")
}

func (s *serviceControlSuite) TestControlConflict(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	inf := s.mockTestSnap(c)

	// create conflicting change
	t := st.NewTask("link-snap", "...")
	snapsup := &snapstate.SnapSetup{SideInfo: &snap.SideInfo{RealName: "test-snap"}}
	t.Set("snap-setup", snapsup)
	chg := st.NewChange("manip", "...")
	chg.AddTask(t)

	inst := &servicestate.Instruction{Action: "start", Names: []string{"foo"}}
	_, err := servicestate.Control(st, []*snap.AppInfo{inf.Apps["foo"]}, inst, nil, nil)
	c.Check(err, ErrorMatches, `snap "test-snap" has "manip" change in progress`)
}

func (s *serviceControlSuite) TestControlStartInstruction(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	inf := s.mockTestSnap(c)

	inst := &servicestate.Instruction{
		Action: "start",
		Names:  []string{"foo"},
	}

	chg := makeControlChange(c, st, inst, inf)
	verifyControlTasks(c, chg.Tasks(), "start", "", "snap.test-snap.foo.service")
}

func (s *serviceControlSuite) TestControlStartEnableMultipleInstruction(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	inf := s.mockTestSnap(c)

	inst := &servicestate.Instruction{
		Action:       "start",
		Names:        []string{"foo", "bar"},
		StartOptions: client.StartOptions{Enable: true},
	}

	chg := makeControlChange(c, st, inst, inf)
	verifyControlTasks(c, chg.Tasks(), "start", "enable", "snap.test-snap.foo.service", "snap.test-snap.bar.service")
}

func (s *serviceControlSuite) TestControlStopInstruction(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	inf := s.mockTestSnap(c)

	inst := &servicestate.Instruction{
		Action: "stop",
		Names:  []string{"foo"},
	}

	chg := makeControlChange(c, st, inst, inf)
	verifyControlTasks(c, chg.Tasks(), "stop", "", "snap.test-snap.foo.service")
}

func (s *serviceControlSuite) TestControlStopDisableInstruction(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	inf := s.mockTestSnap(c)

	inst := &servicestate.Instruction{
		Action:      "stop",
		Names:       []string{"bar"},
		StopOptions: client.StopOptions{Disable: true},
	}

	chg := makeControlChange(c, st, inst, inf)
	verifyControlTasks(c, chg.Tasks(), "stop", "disable", "snap.test-snap.bar.service")
}

func (s *serviceControlSuite) TestControlRestartInstruction(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	inf := s.mockTestSnap(c)

	inst := &servicestate.Instruction{
		Action: "restart",
		Names:  []string{"bar"},
	}

	chg := makeControlChange(c, st, inst, inf)
	verifyControlTasks(c, chg.Tasks(), "restart", "", "snap.test-snap.bar.service")
}

func (s *serviceControlSuite) TestControlRestartReloadMultipleInstruction(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	inf := s.mockTestSnap(c)

	inst := &servicestate.Instruction{
		Action:         "restart",
		Names:          []string{"foo", "bar"},
		RestartOptions: client.RestartOptions{Reload: true},
	}

	chg := makeControlChange(c, st, inst, inf)
	verifyControlTasks(c, chg.Tasks(), "restart", "reload", "snap.test-snap.foo.service", "snap.test-snap.bar.service")
}

func (s *serviceControlSuite) TestControlUnknownInstruction(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	info := s.mockTestSnap(c)
	inst := &servicestate.Instruction{
		Action:         "boo",
		Names:          []string{"foo"},
		RestartOptions: client.RestartOptions{Reload: true},
	}

	_, err := servicestate.Control(st, []*snap.AppInfo{info.Apps["foo"]}, inst, nil, nil)
	c.Assert(err, ErrorMatches, `unknown action "boo"`)
}

func (s *serviceControlSuite) TestControlStopDisableMultipleInstruction(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	inf := s.mockTestSnap(c)

	inst := &servicestate.Instruction{
		Action:      "stop",
		Names:       []string{"foo", "bar"},
		StopOptions: client.StopOptions{Disable: true},
	}

	chg := makeControlChange(c, st, inst, inf)
	verifyControlTasks(c, chg.Tasks(), "stop", "disable", "snap.test-snap.foo.service", "snap.test-snap.bar.service")
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
		{"start", "snap.test-snap.foo.service"},
		{"start", "snap.test-snap.bar.service"},
		{"start", "snap.test-snap.abc.service"},
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
		{"enable", "snap.test-snap.foo.service"},
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
		{"enable", "snap.test-snap.foo.service"},
		{"enable", "snap.test-snap.bar.service"},
		{"enable", "snap.test-snap.abc.service"},
		{"start", "snap.test-snap.foo.service"},
		{"start", "snap.test-snap.bar.service"},
		{"start", "snap.test-snap.abc.service"},
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
		{"disable", "snap.test-snap.foo.service"},
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

func (s *serviceControlSuite) TestRestartAllServices(c *C) {
	st := s.state
	st.Lock()

	s.mockTestSnap(c)

	chg := st.NewChange("service-control", "...")
	t := st.NewTask("service-control", "...")
	cmd := &servicestate.ServiceAction{
		SnapName: "test-snap",
		Action:   "restart",
		Services: []string{"abc", "foo", "bar"},
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
		{"stop", "snap.test-snap.bar.service"},
		{"show", "--property=ActiveState", "snap.test-snap.bar.service"},
		{"start", "snap.test-snap.bar.service"},
		{"stop", "snap.test-snap.abc.service"},
		{"show", "--property=ActiveState", "snap.test-snap.abc.service"},
		{"start", "snap.test-snap.abc.service"},
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

func (s *serviceControlSuite) TestReloadAllServices(c *C) {
	st := s.state
	st.Lock()

	s.mockTestSnap(c)

	chg := st.NewChange("service-control", "...")
	t := st.NewTask("service-control", "...")
	cmd := &servicestate.ServiceAction{
		SnapName: "test-snap",
		Action:   "reload-or-restart",
		Services: []string{"foo", "abc", "bar"},
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
		{"reload-or-restart", "snap.test-snap.bar.service"},
		{"reload-or-restart", "snap.test-snap.abc.service"},
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

func (s *serviceControlSuite) TestUpdateSnapstateServices(c *C) {
	var tests = []struct {
		enable                    []string
		disable                   []string
		expectedSnapstateEnabled  []string
		expectedSnapstateDisabled []string
		changed                   bool
	}{
		// These test scenarios share a single SnapState instance and accumulate
		// changes to ServicesEnabledByHooks and ServicesDisabledByHooks.
		{
			changed: false,
		},
		{
			enable: []string{"a"},
			expectedSnapstateEnabled: []string{"a"},
			changed:                  true,
		},
		// enable again does nothing
		{
			enable: []string{"a"},
			expectedSnapstateEnabled: []string{"a"},
			changed:                  false,
		},
		{
			disable:                   []string{"a"},
			expectedSnapstateDisabled: []string{"a"},
			changed:                   true,
		},
		{
			enable: []string{"a", "c"},
			expectedSnapstateEnabled: []string{"a", "c"},
			changed:                  true,
		},
		{
			disable:                   []string{"b"},
			expectedSnapstateEnabled:  []string{"a", "c"},
			expectedSnapstateDisabled: []string{"b"},
			changed:                   true,
		},
		{
			disable:                   []string{"b", "c"},
			expectedSnapstateEnabled:  []string{"a"},
			expectedSnapstateDisabled: []string{"b", "c"},
			changed:                   true,
		},
	}

	snapst := snapstate.SnapState{}

	for _, tst := range tests {
		var enable, disable []*snap.AppInfo
		for _, srv := range tst.enable {
			enable = append(enable, &snap.AppInfo{Name: srv})
		}
		for _, srv := range tst.disable {
			disable = append(disable, &snap.AppInfo{Name: srv})
		}
		result, err := servicestate.UpdateSnapstateServices(&snapst, enable, disable)
		c.Assert(err, IsNil)
		c.Check(result, Equals, tst.changed)
		c.Check(snapst.ServicesEnabledByHooks, DeepEquals, tst.expectedSnapstateEnabled)
		c.Check(snapst.ServicesDisabledByHooks, DeepEquals, tst.expectedSnapstateDisabled)
	}

	services := []*snap.AppInfo{{Name: "foo"}}
	_, err := servicestate.UpdateSnapstateServices(nil, services, services)
	c.Assert(err, ErrorMatches, `internal error: cannot handle enabled and disabled services at the same time`)
}
