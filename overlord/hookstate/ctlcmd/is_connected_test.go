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
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/overlord/hookstate"
	"github.com/snapcore/snapd/overlord/hookstate/ctlcmd"
	"github.com/snapcore/snapd/overlord/hookstate/hooktest"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/testutil"

	. "gopkg.in/check.v1"
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
	err:  "the required argument `<plug|slot>` was not provided",
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
}}

func mockInstalledSnap(c *C, st *state.State, snapYaml string) {
	info := snaptest.MockSnapCurrent(c, snapYaml, &snap.SideInfo{Revision: snap.R(1)})
	snapstate.Set(st, info.InstanceName(), &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{
			{
				RealName: info.SnapName(),
				Revision: info.Revision,
				SnapID:   info.InstanceName() + "-id",
			},
		},
		Current: info.Revision,
	})
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
    interface: x11`)

	s.st.Set("conns", map[string]interface{}{
		"snap1:plug1 snap2:slot2": map[string]interface{}{},
		"snap1:plug2 snap3:slot3": map[string]interface{}{"undesired": true},
		"snap1:plug3 snap4:slot4": map[string]interface{}{"hotplug-gone": true},
		"snap3:plug4 snap1:slot1": map[string]interface{}{},
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

	// sanity
	c.Assert(mockContext.IsEphemeral(), Equals, true)

	s.testIsConnected(c, mockContext)
}

func (s *isConnectedSuite) TestNoContextError(c *C) {
	stdout, stderr, err := ctlcmd.Run(nil, []string{"is-connected", "foo"}, 0)
	c.Check(err, ErrorMatches, `cannot check connection status without a context`)
	c.Check(string(stdout), Equals, "")
	c.Check(string(stderr), Equals, "")
}

func (s *isConnectedSuite) TestGetRegularUser(c *C) {
	s.st.Lock()

	mockInstalledSnap(c, s.st, `name: snap1
plugs:
  plug1:
    interface: x11`)

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
