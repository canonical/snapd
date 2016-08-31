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
	_, _, err := ctlcmd.Run(s.mockContext, []string{"set", "foo", "bar"})
	c.Check(err, ErrorMatches, ".*invalid configuration.*want key=value.*")
}

func (s *setSuite) TestCommand(c *C) {
	stdout, stderr, err := ctlcmd.Run(s.mockContext, []string{"set", "foo=bar"})
	c.Check(err, IsNil)
	c.Check(string(stdout), Equals, "")
	c.Check(string(stderr), Equals, "")

	// Verify that the handler's SetConf function was called appropriately
	c.Assert(s.mockHandler.SetConfCalls, HasLen, 1)
	c.Check(s.mockHandler.SetConfCalls[0].Key, Equals, "foo")
	c.Check(s.mockHandler.SetConfCalls[0].Value, Equals, "bar")
}

func (s *setSuite) TestCommandMultipleValues(c *C) {
	stdout, stderr, err := ctlcmd.Run(s.mockContext, []string{"set", "foo=bar", "baz=qux"})
	c.Check(err, IsNil)
	c.Check(string(stdout), Equals, "")
	c.Check(string(stderr), Equals, "")

	// Verify that the handler's SetConf function was called appropriately
	c.Assert(s.mockHandler.SetConfCalls, HasLen, 2)
	c.Check(s.mockHandler.SetConfCalls[0].Key, Equals, "foo")
	c.Check(s.mockHandler.SetConfCalls[0].Value, Equals, "bar")
	c.Check(s.mockHandler.SetConfCalls[1].Key, Equals, "baz")
	c.Check(s.mockHandler.SetConfCalls[1].Value, Equals, "qux")
}

func (s *setSuite) TestCommandWithoutContext(c *C) {
	_, _, err := ctlcmd.Run(nil, []string{"set", "foo=bar"})
	c.Check(err, ErrorMatches, ".*cannot set without a context.*")
}
