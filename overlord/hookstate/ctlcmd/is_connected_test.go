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
	"github.com/snapcore/snapd/overlord/hookstate"
	"github.com/snapcore/snapd/overlord/hookstate/ctlcmd"
	"github.com/snapcore/snapd/overlord/hookstate/hooktest"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"

	. "gopkg.in/check.v1"
)

type isConnectedSuite struct {
	st          *state.State
	mockHandler *hooktest.MockHandler
}

var _ = Suite(&isConnectedSuite{})

func (s *isConnectedSuite) SetUpTest(c *C) {
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
	args:     []string{"is-connected", "slot2"},
	exitCode: 1,
}, {
	args:     []string{"is-connected", "foo"},
	exitCode: 1,
}}

func (s *isConnectedSuite) testIsConnected(c *C, context *hookstate.Context) {
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
		if test.exitCode > 0 {
			unsuccessfulErr, ok := err.(*ctlcmd.UnsuccessfulError)
			c.Assert(ok, Equals, true)
			c.Check(unsuccessfulErr.ExitCode, Equals, test.exitCode)
		} else {
			if test.err == "" {
				c.Check(err, IsNil)
			} else {
				c.Check(err, ErrorMatches, test.err)
			}
		}

		c.Check(string(stdout), Equals, test.stdout, Commentf("%s\n", test.args))
		c.Check(string(stderr), Equals, "")
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
