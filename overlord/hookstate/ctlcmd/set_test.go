// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
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
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/hookstate"
	"github.com/snapcore/snapd/overlord/hookstate/ctlcmd"
	"github.com/snapcore/snapd/overlord/hookstate/hooktest"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"

	. "gopkg.in/check.v1"
)

type setSuite struct {
	mockContext *hookstate.Context
	mockHandler *hooktest.MockHandler
}

var _ = Suite(&setSuite{})

func (s *setSuite) SetUpTest(c *C) {
	s.mockHandler = hooktest.NewMockHandler()

	state := state.New(nil)
	state.Lock()
	defer state.Unlock()

	task := state.NewTask("test-task", "my test task")
	setup := &hookstate.HookSetup{Snap: "test-snap", Revision: snap.R(1), Hook: "test-hook"}

	var err error
	s.mockContext, err = hookstate.NewContext(task, setup, s.mockHandler)
	c.Assert(err, IsNil)
}

func (s *setSuite) TestInvalidArguments(c *C) {
	_, _, err := ctlcmd.Run(s.mockContext, []string{"set"})
	c.Check(err, ErrorMatches, "need option name or plug/slot and attribute name arguments")
	_, _, err = ctlcmd.Run(s.mockContext, []string{"set", "foo", "bar"})
	c.Check(err, ErrorMatches, ".*invalid parameter.*want key=value.*")
	_, _, err = ctlcmd.Run(s.mockContext, []string{"set", ":foo", "bar=baz"})
	c.Check(err, ErrorMatches, ".*interface attributes can only be set during the execution of prepare- hooks.*")
}

func (s *setSuite) TestCommand(c *C) {
	stdout, stderr, err := ctlcmd.Run(s.mockContext, []string{"set", "foo=bar", "baz=qux"})
	c.Check(err, IsNil)
	c.Check(string(stdout), Equals, "")
	c.Check(string(stderr), Equals, "")

	// Verify that the previous set doesn't modify the global state
	s.mockContext.State().Lock()
	tr := config.NewTransaction(s.mockContext.State())
	s.mockContext.State().Unlock()
	var value string
	c.Check(tr.Get("test-snap", "foo", &value), ErrorMatches, ".*snap.*has no.*configuration.*")
	c.Check(tr.Get("test-snap", "baz", &value), ErrorMatches, ".*snap.*has no.*configuration.*")

	// Notify the context that we're done. This should save the config.
	s.mockContext.Lock()
	defer s.mockContext.Unlock()
	c.Check(s.mockContext.Done(), IsNil)

	// Verify that the global config has been updated.
	tr = config.NewTransaction(s.mockContext.State())
	c.Check(tr.Get("test-snap", "foo", &value), IsNil)
	c.Check(value, Equals, "bar")
	c.Check(tr.Get("test-snap", "baz", &value), IsNil)
	c.Check(value, Equals, "qux")
}

func (s *setSuite) TestCommandSavesDeltasOnly(c *C) {
	// Setup an initial configuration
	s.mockContext.State().Lock()
	tr := config.NewTransaction(s.mockContext.State())
	tr.Set("test-snap", "test-key1", "test-value1")
	tr.Set("test-snap", "test-key2", "test-value2")
	tr.Commit()
	s.mockContext.State().Unlock()

	stdout, stderr, err := ctlcmd.Run(s.mockContext, []string{"set", "test-key2=test-value3"})
	c.Check(err, IsNil)
	c.Check(string(stdout), Equals, "")
	c.Check(string(stderr), Equals, "")

	// Notify the context that we're done. This should save the config.
	s.mockContext.Lock()
	defer s.mockContext.Unlock()
	c.Check(s.mockContext.Done(), IsNil)

	// Verify that the global config has been updated, but only test-key2
	tr = config.NewTransaction(s.mockContext.State())
	var value string
	c.Check(tr.Get("test-snap", "test-key1", &value), IsNil)
	c.Check(value, Equals, "test-value1")
	c.Check(tr.Get("test-snap", "test-key2", &value), IsNil)
	c.Check(value, Equals, "test-value3")
}

func (s *setSuite) TestCommandWithoutContext(c *C) {
	_, _, err := ctlcmd.Run(nil, []string{"set", "foo=bar"})
	c.Check(err, ErrorMatches, ".*cannot set without a context.*")
}
