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
	"strings"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/hookstate"
	"github.com/snapcore/snapd/overlord/hookstate/ctlcmd"
	"github.com/snapcore/snapd/overlord/hookstate/hooktest"
	"github.com/snapcore/snapd/overlord/registrystate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
)

type unsetSuite struct {
	mockContext *hookstate.Context
	mockHandler *hooktest.MockHandler
}

var _ = Suite(&unsetSuite{})

func (s *unsetSuite) SetUpTest(c *C) {
	s.mockHandler = hooktest.NewMockHandler()

	state := state.New(nil)
	state.Lock()
	defer state.Unlock()

	task := state.NewTask("test-task", "my test task")
	setup := &hookstate.HookSetup{Snap: "test-snap", Revision: snap.R(1), Hook: "hook"}

	var err error
	s.mockContext, err = hookstate.NewContext(task, task.State(), setup, s.mockHandler, "")
	c.Assert(err, IsNil)
}

func (s *unsetSuite) TestInvalidArguments(c *C) {
	_, _, err := ctlcmd.Run(s.mockContext, []string{"unset"}, 0)
	c.Check(err, ErrorMatches, "unset which option.*")
}

func (s *unsetSuite) TestUnsetOne(c *C) {
	// Setup an initial configuration
	s.mockContext.State().Lock()
	tr := config.NewTransaction(s.mockContext.State())
	tr.Set("test-snap", "foo", "a")
	tr.Commit()
	s.mockContext.State().Unlock()

	// Validity check
	var value interface{}
	s.mockContext.State().Lock()
	tr = config.NewTransaction(s.mockContext.State())
	c.Check(tr.Get("test-snap", "foo", &value), IsNil)
	s.mockContext.State().Unlock()
	c.Check(value, Equals, "a")

	stdout, stderr, err := ctlcmd.Run(s.mockContext, []string{"unset", "foo"}, 0)
	c.Check(err, IsNil)
	c.Check(string(stdout), Equals, "")
	c.Check(string(stderr), Equals, "")

	// Notify the context that we're done. This should save the config.
	s.mockContext.Lock()
	defer s.mockContext.Unlock()
	c.Check(s.mockContext.Done(), IsNil)

	// Verify that the global config has been updated.
	tr = config.NewTransaction(s.mockContext.State())
	c.Check(tr.Get("test-snap", "foo", &value), ErrorMatches, `snap "test-snap" has no "foo" configuration option`)
}

func (s *unsetSuite) TestUnsetMany(c *C) {
	// Setup an initial configuration
	s.mockContext.State().Lock()
	tr := config.NewTransaction(s.mockContext.State())
	tr.Set("test-snap", "foo", "a")
	tr.Set("test-snap", "bar", "b")
	tr.Set("test-snap", "baz", "c")
	tr.Commit()
	s.mockContext.State().Unlock()

	stdout, stderr, err := ctlcmd.Run(s.mockContext, []string{"unset", "foo", "bar"}, 0)
	c.Check(err, IsNil)
	c.Check(string(stdout), Equals, "")
	c.Check(string(stderr), Equals, "")

	// Notify the context that we're done. This should save the config.
	s.mockContext.Lock()
	defer s.mockContext.Unlock()
	c.Check(s.mockContext.Done(), IsNil)

	// Verify that the global config has been updated.
	var value interface{}
	tr = config.NewTransaction(s.mockContext.State())
	c.Check(tr.Get("test-snap", "foo", &value), ErrorMatches, `snap "test-snap" has no "foo" configuration option`)
	c.Check(tr.Get("test-snap", "bar", &value), ErrorMatches, `snap "test-snap" has no "bar" configuration option`)
	c.Check(tr.Get("test-snap", "baz", &value), IsNil)
	c.Check(value, Equals, "c")
}

func (s *unsetSuite) TestSetThenUnset(c *C) {
	// Setup an initial configuration
	s.mockContext.State().Lock()
	tr := config.NewTransaction(s.mockContext.State())
	tr.Set("test-snap", "agent.x.a", "1")
	tr.Set("test-snap", "agent.x.b", "2")
	tr.Commit()
	s.mockContext.State().Unlock()

	stdout, stderr, err := ctlcmd.Run(s.mockContext, []string{"set", "agent.x!", "agent.x.a!", "agent.x.b!"}, 0)
	c.Check(err, IsNil)
	c.Check(string(stdout), Equals, "")
	c.Check(string(stderr), Equals, "")

	// Notify the context that we're done. This should save the config.
	s.mockContext.Lock()
	defer s.mockContext.Unlock()
	c.Check(s.mockContext.Done(), IsNil)

	// Verify that the global config has been updated.
	var value interface{}
	tr = config.NewTransaction(s.mockContext.State())
	c.Check(tr.Get("test-snap", "agent.x.a", &value), ErrorMatches, `snap "test-snap" has no "agent.x.a" configuration option`)
}

func (s *unsetSuite) TestUnsetRegularUserForbidden(c *C) {
	_, _, err := ctlcmd.Run(s.mockContext, []string{"unset", "key"}, 1000)
	c.Assert(err, ErrorMatches, `cannot use "unset" with uid 1000, try with sudo`)
	forbidden, _ := err.(*ctlcmd.ForbiddenCommandError)
	c.Assert(forbidden, NotNil)
}

func (s *unsetSuite) TestUnsetHelpRegularUserAllowed(c *C) {
	_, _, err := ctlcmd.Run(s.mockContext, []string{"unset", "-h"}, 1000)
	c.Assert(strings.HasPrefix(err.Error(), "Usage:"), Equals, true)
}

func (s *unsetSuite) TestCommandWithoutContext(c *C) {
	_, _, err := ctlcmd.Run(nil, []string{"unset", "foo"}, 0)
	c.Check(err, ErrorMatches, `cannot invoke snapctl operation commands \(here "unset"\) from outside of a snap`)
}

func (s *registrySuite) TestRegistryUnsetManyViews(c *C) {
	s.state.Lock()
	err := registrystate.SetViaView(s.state, s.devAccID, "network", "write-wifi", map[string]interface{}{"ssid": "my-ssid", "password": "my-secret"})
	s.state.Unlock()
	c.Assert(err, IsNil)

	stdout, stderr, err := ctlcmd.Run(s.mockContext, []string{"unset", "--view", ":write-wifi", "ssid", "password"}, 0)
	c.Assert(err, IsNil)
	c.Check(stdout, IsNil)
	c.Check(stderr, IsNil)
	s.mockContext.Lock()
	c.Assert(s.mockContext.Done(), IsNil)
	s.mockContext.Unlock()

	s.state.Lock()
	_, err = registrystate.GetViaView(s.state, s.devAccID, "network", "write-wifi", []string{"ssid", "password"})
	s.state.Unlock()
	c.Assert(err, ErrorMatches, `cannot get "ssid", "password" .*: matching rules don't map to any values`)
}

func (s *registrySuite) TestRegistryUnsetHappensTransactionally(c *C) {
	s.state.Lock()
	err := registrystate.SetViaView(s.state, s.devAccID, "network", "write-wifi", map[string]interface{}{"ssid": "my-ssid"})
	s.state.Unlock()
	c.Assert(err, IsNil)

	stdout, stderr, err := ctlcmd.Run(s.mockContext, []string{"unset", "--view", ":write-wifi", "ssid"}, 0)
	c.Assert(err, IsNil)
	c.Check(stdout, IsNil)
	c.Check(stderr, IsNil)

	s.state.Lock()
	val, err := registrystate.GetViaView(s.state, s.devAccID, "network", "read-wifi", []string{"ssid"})
	s.state.Unlock()
	c.Assert(err, IsNil)
	c.Assert(val, DeepEquals, map[string]interface{}{
		"ssid": "my-ssid",
	})

	// commit transaction
	s.mockContext.Lock()
	c.Assert(s.mockContext.Done(), IsNil)
	s.mockContext.Unlock()

	s.state.Lock()
	_, err = registrystate.GetViaView(s.state, s.devAccID, "network", "read-wifi", []string{"ssid"})
	s.state.Unlock()
	c.Assert(err, ErrorMatches, `cannot get "ssid" .*: matching rules don't map to any values`)
}

func (s *registrySuite) TestRegistryUnsetInvalid(c *C) {
	type testcase struct {
		args []string
		err  string
	}

	tcs := []testcase{
		{
			args: []string{"snap:plug"},
			err:  `cannot unset registry: plug must conform to format ":<plug-name>": snap:plug`,
		},
		{
			args: []string{":"},
			err:  `cannot unset registry: plug name was not provided`,
		},
		{
			args: []string{":plug"},
			err:  `cannot unset registry: no paths provided to unset`,
		},
	}

	for _, tc := range tcs {
		stdout, stderr, err := ctlcmd.Run(s.mockContext, append([]string{"unset", "--view"}, tc.args...), 0)
		c.Assert(err, ErrorMatches, tc.err)
		c.Check(stdout, IsNil)
		c.Check(stderr, IsNil)
	}
}
