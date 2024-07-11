// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019 Canonical Ltd
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

package ctlcmd_test

import (
	"fmt"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/overlord/hookstate"
	"github.com/snapcore/snapd/overlord/hookstate/ctlcmd"
	"github.com/snapcore/snapd/overlord/hookstate/hooktest"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/snapstate/snapstatetest"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/testutil"
)

type isConnectedSuite struct {
	testutil.BaseTest
	st          *state.State
	mockHandler *hooktest.MockHandler
}

var _ = Suite(&isConnectedSuite{})

func (s *isConnectedSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
	dirs.SetRootDir(c.MkDir())
	s.AddCleanup(func() { dirs.SetRootDir("/") })
	s.st = state.New(nil)
	s.mockHandler = hooktest.NewMockHandler()
}

var isConnectedTests = []struct {
	args                []string
	stdout, stderr, err string
	exitCode            int
}{{
	args: []string{"is-connected"},
	err:  "must specify either a plug/slot name or --list",
}, {
	args: []string{"is-connected", "plug1"},
}, {
	args: []string{"is-connected", "slot1"},
}, {
	// reported as not connected because of undesired flag
	args:     []string{"is-connected", "plug2"},
	exitCode: 1,
}, {
	// reported as not connected because of hotplug-gone flag
	args:     []string{"is-connected", "plug3"},
	exitCode: 1,
}, {
	args: []string{"is-connected", "slot2"},
	err:  `snap "snap1" has no plug or slot named "slot2"`,
}, {
	args: []string{"is-connected", "foo"},
	err:  `snap "snap1" has no plug or slot named "foo"`,
}, {
	// snap1:plug1 does not use an allowed interface
	args: []string{"is-connected", "--pid", "1002", "plug1"},
	err:  `cannot use --pid check with snap1:plug1`,
}, {
	// snap1:slot1 does not use an allowed interface
	args: []string{"is-connected", "--pid", "1002", "slot1"},
	err:  `cannot use --pid check with snap1:slot1`,
}, {
	// snap1:cc slot is not connected to snap2
	args:     []string{"is-connected", "--pid", "1002", "cc"},
	exitCode: 1,
}, {
	// snap1:cc slot is connected to snap3
	args:     []string{"is-connected", "--pid", "1003", "cc"},
	exitCode: 0,
}, {
	// snap1:cc slot is not connected to a non-snap pid
	args:     []string{"is-connected", "--pid", "42", "cc"},
	exitCode: ctlcmd.NotASnapCode,
}, {
	// snap1:cc slot is connected to a classic snap5
	args:     []string{"is-connected", "--pid", "1005", "cc"},
	exitCode: 0,
}, {
	// snap1:audio-record slot is not connected to classic snap5
	args:     []string{"is-connected", "--pid", "1005", "audio-record"},
	exitCode: ctlcmd.ClassicSnapCode,
}, {
	// snap1:plug1 does not use an allowed interface
	args: []string{"is-connected", "--apparmor-label", "snap.snap2.app", "plug1"},
	err:  `cannot use --apparmor-label check with snap1:plug1`,
}, {
	// snap1:slot1 does not use an allowed interface
	args: []string{"is-connected", "--apparmor-label", "snap.snap2.app", "slot1"},
	err:  `cannot use --apparmor-label check with snap1:slot1`,
}, {
	// snap1:cc slot is not connected to snap2
	args:     []string{"is-connected", "--apparmor-label", "snap.snap2.app", "cc"},
	exitCode: 1,
}, {
	// snap1:cc slot is connected to snap3
	args:     []string{"is-connected", "--apparmor-label", "snap.snap3.app", "cc"},
	exitCode: 0,
}, {
	// snap1:cc slot is not connected to a non-snap pid
	args:     []string{"is-connected", "--apparmor-label", "/usr/bin/evince", "cc"},
	exitCode: ctlcmd.NotASnapCode,
}, {
	// snap1:cc slot is connected to a classic snap5
	args:     []string{"is-connected", "--apparmor-label", "snap.snap5.app", "cc"},
	exitCode: 0,
}, {
	// snap1:audio-record slot is not connected to classic snap5
	args:     []string{"is-connected", "--apparmor-label", "snap.snap5.app", "audio-record"},
	exitCode: ctlcmd.ClassicSnapCode,
}}

func mockInstalledSnap(c *C, st *state.State, snapYaml, cohortKey string) *snap.Info {
	info := snaptest.MockSnapCurrent(c, snapYaml, &snap.SideInfo{Revision: snap.R(1)})
	snapstate.Set(st, info.InstanceName(), &snapstate.SnapState{
		Active: true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{
				RealName: info.SnapName(),
				Revision: info.Revision,
				SnapID:   info.InstanceName() + "-id",
			},
		}),
		Current:         info.Revision,
		TrackingChannel: "stable",
		CohortKey:       cohortKey,
	})
	return info
}

func (s *isConnectedSuite) testIsConnected(c *C, context *hookstate.Context) {
	mockInstalledSnap(c, s.st, `name: snap1
plugs:
  plug1:
    interface: x11
  plug2:
    interface: x11
  plug3:
    interface: x11
slots:
  slot1:
    interface: x11
  cc:
    interface: cups-control
  audio-record:
    interface: audio-record`, "")
	mockInstalledSnap(c, s.st, `name: snap2
slots:
  slot2:
    interface: x11`, "")
	mockInstalledSnap(c, s.st, `name: snap3
plugs:
  plug4:
    interface: x11
  cc:
    interface: cups-control
slots:
  slot3:
    interface: x11`, "")
	mockInstalledSnap(c, s.st, `name: snap4
slots:
  slot4:
    interface: x11`, "")
	mockInstalledSnap(c, s.st, `name: snap5
confinement: classic
plugs:
  cc:
    interface: cups-control`, "")
	restore := ctlcmd.MockCgroupSnapNameFromPid(func(pid int) (string, error) {
		switch {
		case 1000 < pid && pid < 1100:
			return fmt.Sprintf("snap%d", pid-1000), nil
		default:
			return "", fmt.Errorf("Not a snap")
		}
	})
	defer restore()

	s.st.Set("conns", map[string]interface{}{
		"snap1:plug1 snap2:slot2": map[string]interface{}{},
		"snap1:plug2 snap3:slot3": map[string]interface{}{"undesired": true},
		"snap1:plug3 snap4:slot4": map[string]interface{}{"hotplug-gone": true},
		"snap3:plug4 snap1:slot1": map[string]interface{}{},
		"snap3:cc snap1:cc":       map[string]interface{}{},
		"snap5:cc snap1:cc":       map[string]interface{}{},
	})

	s.st.Unlock()
	defer s.st.Lock()

	for _, test := range isConnectedTests {
		stdout, stderr, err := ctlcmd.Run(context, test.args, 0)
		comment := Commentf("%s", test.args)
		if test.exitCode > 0 {
			c.Check(err, DeepEquals, &ctlcmd.UnsuccessfulError{ExitCode: test.exitCode}, comment)
		} else {
			if test.err == "" {
				c.Check(err, IsNil, comment)
			} else {
				c.Check(err, ErrorMatches, test.err, comment)
			}
		}

		c.Check(string(stdout), Equals, test.stdout, comment)
		c.Check(string(stderr), Equals, "", comment)
	}
}

func (s *isConnectedSuite) TestIsConnectedFromHook(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

	task := s.st.NewTask("test-task", "my test task")
	setup := &hookstate.HookSetup{Snap: "snap1", Revision: snap.R(1), Hook: "test-hook"}

	mockContext, err := hookstate.NewContext(task, s.st, setup, s.mockHandler, "")
	c.Check(err, IsNil)

	s.testIsConnected(c, mockContext)
}

func (s *isConnectedSuite) TestIsConnectedFromApp(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

	// ephemeral context
	setup := &hookstate.HookSetup{Snap: "snap1", Revision: snap.R(1)}
	mockContext, err := hookstate.NewContext(nil, s.st, setup, s.mockHandler, "")
	c.Check(err, IsNil)

	// validity
	c.Assert(mockContext.IsEphemeral(), Equals, true)

	s.testIsConnected(c, mockContext)
}

func (s *isConnectedSuite) TestNoContextError(c *C) {
	stdout, stderr, err := ctlcmd.Run(nil, []string{"is-connected", "foo"}, 0)
	c.Check(err, ErrorMatches, `cannot invoke snapctl operation commands \(here "is-connected"\) from outside of a snap`)
	c.Check(string(stdout), Equals, "")
	c.Check(string(stderr), Equals, "")
}

func (s *isConnectedSuite) TestGetRegularUser(c *C) {
	s.st.Lock()

	mockInstalledSnap(c, s.st, `name: snap1
plugs:
  plug1:
    interface: x11`, "")

	s.st.Set("conns", map[string]interface{}{
		"snap1:plug1 snap2:slot2": map[string]interface{}{},
	})

	setup := &hookstate.HookSetup{Snap: "snap1", Revision: snap.R(1)}

	s.st.Unlock()

	mockContext, err := hookstate.NewContext(nil, s.st, setup, s.mockHandler, "")
	c.Assert(err, IsNil)
	stdout, stderr, err := ctlcmd.Run(mockContext, []string{"is-connected", "plug1"}, 1000)
	c.Check(err, IsNil)
	c.Check(string(stdout), Equals, "")
	c.Check(string(stderr), Equals, "")
}

func (s *isConnectedSuite) TestIsConnectedList(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

	task := s.st.NewTask("test-task", "my test task")
	setup := &hookstate.HookSetup{Snap: "snap1", Revision: snap.R(1), Hook: "test-hook"}

	mockContext, err := hookstate.NewContext(task, s.st, setup, s.mockHandler, "")
	c.Check(err, IsNil)

	mockInstalledSnap(c, s.st, `name: snap1
plugs:
  plug1a:
    interface: x11
  plug1b:
    interface: x11
  plug1c:
    interface: x11
  plug1d:
    interface: x11
slots:
  slot1a:
    interface: x11
  slot1b:
    interface: x11`, "")
	mockInstalledSnap(c, s.st, `name: snap2
plugs:
  plug2a:
    interface: x11
  plug2b:
    interface: x11
slots:
  slot2a:
    interface: x11
  slot2b:
    interface: x11`, "")
	mockInstalledSnap(c, s.st, `name: snap3
plugs:
  plug3a:
    interface: x11
slots:
  slot3a:
    interface: x11
  slot3b:
    interface: x11`, "")
	s.st.Set("conns", map[string]interface{}{
		"snap1:plug1a snap2:slot2a": map[string]interface{}{},
		"snap2:plug2a snap1:slot1a": map[string]interface{}{},
		"snap3:plug3a snap1:slot1a": map[string]interface{}{},
		"snap1:plug1c snap3:slot3a": map[string]interface{}{"undesired": true},
		"snap1:plug1d snap3:slot3b": map[string]interface{}{"hotplug-gone": true},
	})

	s.st.Unlock()
	defer s.st.Lock()

	stdout, stderr, err := ctlcmd.Run(mockContext, []string{"is-connected", "--list"}, 0)
	c.Check(err, IsNil)

	c.Check(string(stdout), Equals, "plug1a\nslot1a\n")
	c.Check(string(stderr), Equals, "")
}
