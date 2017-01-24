// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017 Canonical Ltd
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

type setAttrSuite struct {
	mockContext *hookstate.Context
	mockHandler *hooktest.MockHandler
}

var _ = Suite(&setAttrSuite{})

func (s *setAttrSuite) SetUpTest(c *C) {
	s.mockHandler = hooktest.NewMockHandler()

	state := state.New(nil)
	state.Lock()
	defer state.Unlock()

	attributes := make(map[string]map[string]interface{})
	attrs := make(map[string]interface{})
	attributes["test-snap"] = attrs
	contextData := map[string]interface{}{"attributes": attributes, "other-snap": "othersnap", "plug-or-slot": "aplug"}

	task := state.NewTask("test-task", "my test task")
	task.Set("hook-context", contextData)
	setup := &hookstate.HookSetup{Snap: "test-snap", Revision: snap.R(1), Hook: "prepare-plug-a"}

	var err error
	s.mockContext, err = hookstate.NewContext(task, setup, s.mockHandler)
	c.Assert(err, IsNil)
}

func (s *setAttrSuite) TestCommand(c *C) {
	stdout, stderr, err := ctlcmd.Run(s.mockContext, []string{"set", ":aplug", "foo=bar", `baz=["a", "b"]`})
	c.Check(err, IsNil)
	c.Check(string(stdout), Equals, "")
	c.Check(string(stderr), Equals, "")

	s.mockContext.Lock()
	defer s.mockContext.Unlock()
	var attrs map[string]map[string]interface{}
	err = s.mockContext.Get("attributes", &attrs)
	c.Check(err, IsNil)
	c.Check(attrs["test-snap"]["foo"], Equals, "bar")
	c.Check(attrs["test-snap"]["baz"], DeepEquals, []interface{}{"a", "b"})
}

func (s *setAttrSuite) TestCommandFailsOutsideOfValidContext(c *C) {
	var err error
	var mockContext *hookstate.Context

	state := state.New(nil)
	state.Lock()
	defer state.Unlock()

	task := state.NewTask("test-task", "my test task")
	setup := &hookstate.HookSetup{Snap: "test-snap", Revision: snap.R(1), Hook: "not-a-connect-hook"}
	mockContext, err = hookstate.NewContext(task, setup, s.mockHandler)
	c.Assert(err, IsNil)

	stdout, stderr, err := ctlcmd.Run(mockContext, []string{"set", ":aplug", "foo=bar"})
	c.Check(err, NotNil)
	c.Check(err.Error(), Equals, `interface attributes can only be set during the execution of interface hooks`)
	c.Check(string(stdout), Equals, "")
	c.Check(string(stderr), Equals, "")
}
