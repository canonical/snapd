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
	"github.com/snapcore/snapd/overlord/configstate"
	"github.com/snapcore/snapd/overlord/hookstate"
	"github.com/snapcore/snapd/overlord/hookstate/ctlcmd"
	"github.com/snapcore/snapd/overlord/hookstate/hooktest"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"

	. "gopkg.in/check.v1"
)

type getSuite struct {
	mockContext *hookstate.Context
	mockHandler *hooktest.MockHandler
}

var _ = Suite(&getSuite{})

func (s *getSuite) SetUpTest(c *C) {
	s.mockHandler = hooktest.NewMockHandler()

	state := state.New(nil)
	state.Lock()
	defer state.Unlock()

	task := state.NewTask("test-task", "my test task")
	setup := &hookstate.HookSetup{Snap: "test-snap", Revision: snap.R(1), Hook: "test-hook"}

	var err error
	s.mockContext, err = hookstate.NewContext(task, setup, s.mockHandler)
	c.Assert(err, IsNil)

	// Initialize configuration
	transaction := configstate.NewTransaction(state)
	transaction.Set("test-snap", "initial-key", "initial-value")
	transaction.Commit()
}

func (s *getSuite) TestCommand(c *C) {
	stdout, stderr, err := ctlcmd.Run(s.mockContext, []string{"get", "initial-key"})
	c.Check(err, IsNil)
	c.Check(string(stderr), Equals, "")
	c.Check(string(stdout), Equals, "\"initial-value\"")
}

func (s *getSuite) TestCommandGetsSettedValues(c *C) {
	// Set a value via the `snapctl set` command
	_, _, err := ctlcmd.Run(s.mockContext, []string{"set", "foo=bar"})
	c.Check(err, IsNil)

	stdout, stderr, err := ctlcmd.Run(s.mockContext, []string{"get", "foo"})
	c.Check(err, IsNil)
	c.Check(string(stderr), Equals, "")
	c.Check(string(stdout), Equals, "\"bar\"")
}

func (s *getSuite) TestCommandMultipleKeys(c *C) {
	// Set a value via the `snapctl set` command
	_, _, err := ctlcmd.Run(s.mockContext, []string{"set", "test-key=test-value"})
	c.Check(err, IsNil)

	stdout, stderr, err := ctlcmd.Run(s.mockContext, []string{"get", "initial-key", "test-key"})
	c.Check(err, IsNil)
	c.Check(string(stderr), Equals, "")
	c.Check(string(stdout), Equals, `{
	"initial-key": "initial-value",
	"test-key": "test-value"
}`)
}

func (s *getSuite) TestCommandWithNoConfig(c *C) {
	stdout, stderr, err := ctlcmd.Run(s.mockContext, []string{"get", "foo"})
	c.Check(err, ErrorMatches, ".*snap.*has no.*configuration option.*")
	c.Check(string(stderr), Equals, "")
	c.Check(string(stdout), Equals, "")
}

func (s *getSuite) TestCommandWithoutContext(c *C) {
	_, _, err := ctlcmd.Run(nil, []string{"get", "foo"})
	c.Check(err, ErrorMatches, ".*cannot get without a context.*")
}
