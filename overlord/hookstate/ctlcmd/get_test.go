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
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/hookstate"
	"github.com/snapcore/snapd/overlord/hookstate/ctlcmd"
	"github.com/snapcore/snapd/overlord/hookstate/hooktest"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"

	"strings"

	. "gopkg.in/check.v1"
)

type getSuite struct {
	mockContext *hookstate.Context
	mockHandler *hooktest.MockHandler
}

type getAttrSuite struct {
	mockPlugHookContext *hookstate.Context
	mockSlotHookContext *hookstate.Context
	mockHandler         *hooktest.MockHandler
}

var _ = Suite(&getSuite{})

var _ = Suite(&getAttrSuite{})

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
	tr := config.NewTransaction(state)
	tr.Set("test-snap", "initial-key", "initial-value")
	tr.Commit()
}

var getTests = []struct {
	args, stdout, error string
}{{
	args:  "get",
	error: ".*get which option.*",
}, {
	args:  "get --plug key",
	error: "cannot use --plug or --slot without <snap>:<plug|slot> argument",
}, {
	args:  "get --slot key",
	error: "cannot use --plug or --slot without <snap>:<plug|slot> argument",
}, {
	args:  "get --foo",
	error: ".*unknown flag.*foo.*",
}, {
	args:  "get :foo bar",
	error: ".*interface attributes can only be read during the execution of interface hooks.*",
}, {
	args:   "get test-key1",
	stdout: "test-value1\n",
}, {
	args:   "get test-key2",
	stdout: "2\n",
}, {
	args:   "get missing-key",
	stdout: "\n",
}, {
	args:   "get -t test-key1",
	stdout: "\"test-value1\"\n",
}, {
	args:   "get -t test-key2",
	stdout: "2\n",
}, {
	args:   "get -t missing-key",
	stdout: "null\n",
}, {
	args:   "get -d test-key1",
	stdout: "{\n\t\"test-key1\": \"test-value1\"\n}\n",
}, {
	args:   "get test-key1 test-key2",
	stdout: "{\n\t\"test-key1\": \"test-value1\",\n\t\"test-key2\": 2\n}\n",
}}

func (s *getSuite) TestGetTests(c *C) {
	for _, test := range getTests {
		c.Logf("Test: %s", test.args)

		mockHandler := hooktest.NewMockHandler()

		state := state.New(nil)
		state.Lock()

		task := state.NewTask("test-task", "my test task")
		setup := &hookstate.HookSetup{Snap: "test-snap", Revision: snap.R(1), Hook: "test-hook"}

		var err error
		mockContext, err := hookstate.NewContext(task, setup, mockHandler)
		c.Check(err, IsNil)

		// Initialize configuration
		tr := config.NewTransaction(state)
		tr.Set("test-snap", "test-key1", "test-value1")
		tr.Set("test-snap", "test-key2", 2)
		tr.Commit()

		state.Unlock()

		stdout, stderr, err := ctlcmd.Run(mockContext, strings.Fields(test.args))
		if test.error != "" {
			c.Check(err, ErrorMatches, test.error)
		} else {
			c.Check(err, IsNil)
			c.Check(string(stderr), Equals, "")
			c.Check(string(stdout), Equals, test.stdout)
		}
	}
}

func (s *getSuite) TestCommandWithoutContext(c *C) {
	_, _, err := ctlcmd.Run(nil, []string{"get", "foo"})
	c.Check(err, ErrorMatches, ".*cannot get without a context.*")
}

func (s *getAttrSuite) SetUpTest(c *C) {
	s.mockHandler = hooktest.NewMockHandler()

	state := state.New(nil)
	state.Lock()
	ch := state.NewChange("mychange", "mychange")

	attrsTask := state.NewTask("connect-task", "my connect task")
	attrsTask.Set("plug", &interfaces.PlugRef{Snap: "a", Name: "aplug"})
	attrsTask.Set("slot", &interfaces.SlotRef{Snap: "b", Name: "bslot"})
	plugAttrs := make(map[string]interface{})
	slotAttrs := make(map[string]interface{})
	plugAttrs["aattr"] = "foo"
	plugAttrs["baz"] = []string{"a", "b"}
	slotAttrs["battr"] = "bar"
	attrsTask.Set("plug-attrs", plugAttrs)
	attrsTask.Set("slot-attrs", slotAttrs)
	ch.AddTask(attrsTask)
	state.Unlock()

	var err error

	// setup plug hook task
	state.Lock()
	plugHookTask := state.NewTask("run-hook", "my test task")
	state.Unlock()
	plugTaskSetup := &hookstate.HookSetup{Snap: "test-snap", Revision: snap.R(1), Hook: "connect-plug-aplug"}
	s.mockPlugHookContext, err = hookstate.NewContext(plugHookTask, plugTaskSetup, s.mockHandler)
	c.Assert(err, IsNil)

	s.mockPlugHookContext.Lock()
	s.mockPlugHookContext.Set("attrs-task", attrsTask.ID())
	s.mockPlugHookContext.Unlock()
	state.Lock()
	ch.AddTask(plugHookTask)
	state.Unlock()

	// setup slot hook task
	state.Lock()
	slotHookTask := state.NewTask("run-hook", "my test task")
	state.Unlock()
	slotTaskSetup := &hookstate.HookSetup{Snap: "test-snap", Revision: snap.R(1), Hook: "connect-slot-aplug"}
	s.mockSlotHookContext, err = hookstate.NewContext(slotHookTask, slotTaskSetup, s.mockHandler)
	c.Assert(err, IsNil)

	s.mockSlotHookContext.Lock()
	s.mockSlotHookContext.Set("attrs-task", attrsTask.ID())
	s.mockSlotHookContext.Unlock()

	state.Lock()
	defer state.Unlock()
	ch.AddTask(slotHookTask)
}

func (s *getAttrSuite) TestGetPlugAttributesInPlugHook(c *C) {
	stdout, stderr, err := ctlcmd.Run(s.mockPlugHookContext, []string{"get", ":aplug", "aattr"})
	c.Check(err, IsNil)
	c.Check(string(stdout), Equals, "foo\n")
	c.Check(string(stderr), Equals, "")

	stdout, stderr, err = ctlcmd.Run(s.mockPlugHookContext, []string{"get", "-d", ":aplug", "baz"})
	c.Check(err, IsNil)
	c.Check(string(stdout), Equals, "{\n\t\"baz\": [\n\t\t\"a\",\n\t\t\"b\"\n\t]\n}\n")
	c.Check(string(stderr), Equals, "")

	// The --plug parameter doesn't do anything if used on plug side
	stdout, stderr, err = ctlcmd.Run(s.mockPlugHookContext, []string{"get", "--plug", ":aplug", "aattr"})
	c.Check(err, IsNil)
	c.Check(string(stdout), Equals, "foo\n")
	c.Check(string(stderr), Equals, "")
}

func (s *getAttrSuite) TestGetSlotAttributesInSlotHook(c *C) {
	stdout, stderr, err := ctlcmd.Run(s.mockSlotHookContext, []string{"get", ":bslot", "battr"})
	c.Check(err, IsNil)
	c.Check(string(stdout), Equals, "bar\n")
	c.Check(string(stderr), Equals, "")

	// The --slot parameter doesn't do anything if used on slot side
	stdout, stderr, err = ctlcmd.Run(s.mockSlotHookContext, []string{"get", "--slot", ":bslot", "battr"})
	c.Check(err, IsNil)
	c.Check(string(stdout), Equals, "bar\n")
	c.Check(string(stderr), Equals, "")
}

func (s *getAttrSuite) TestGetSlotAttributeInPlugHook(c *C) {
	stdout, stderr, err := ctlcmd.Run(s.mockPlugHookContext, []string{"get", "--slot", ":aplug", "battr"})
	c.Check(err, IsNil)
	c.Check(string(stdout), Equals, "bar\n")
	c.Check(string(stderr), Equals, "")
}

func (s *getAttrSuite) TestGetPlugAttributeInSlotHook(c *C) {
	stdout, stderr, err := ctlcmd.Run(s.mockSlotHookContext, []string{"get", "--plug", ":bslot", "aattr"})
	c.Check(err, IsNil)
	c.Check(string(stdout), Equals, "foo\n")
	c.Check(string(stderr), Equals, "")
}

func (s *getAttrSuite) TestUnknownPlugAttribute(c *C) {
	stdout, stderr, err := ctlcmd.Run(s.mockPlugHookContext, []string{"get", ":aplug", "x"})
	c.Check(err, NotNil)
	c.Check(err.Error(), Equals, `unknown attribute "x"`)
	c.Check(string(stdout), Equals, "")
	c.Check(string(stderr), Equals, "")
}

func (s *getAttrSuite) TestUnknownSlotAttribute(c *C) {
	stdout, stderr, err := ctlcmd.Run(s.mockSlotHookContext, []string{"get", ":bslot", "x"})
	c.Check(err, NotNil)
	c.Check(err.Error(), Equals, `unknown attribute "x"`)
	c.Check(string(stdout), Equals, "")
	c.Check(string(stderr), Equals, "")
}

func (s *getAttrSuite) TestUsingPlugNameInSlotHookFails(c *C) {
	stdout, stderr, err := ctlcmd.Run(s.mockSlotHookContext, []string{"get", ":aplug", "x"})
	c.Check(err, NotNil)
	c.Check(err.Error(), Equals, `unknown plug or slot "aplug"`)
	c.Check(string(stdout), Equals, "")
	c.Check(string(stderr), Equals, "")
}

func (s *getAttrSuite) TestUsingSlotNameInPlugHookFails(c *C) {
	stdout, stderr, err := ctlcmd.Run(s.mockPlugHookContext, []string{"get", ":bslot", "x"})
	c.Check(err, NotNil)
	c.Check(err.Error(), Equals, `unknown plug or slot "bslot"`)
	c.Check(string(stdout), Equals, "")
	c.Check(string(stderr), Equals, "")
}

func (s *getAttrSuite) TestForcePlugOrSlotMutuallyExclusive(c *C) {
	stdout, stderr, err := ctlcmd.Run(s.mockSlotHookContext, []string{"get", "--slot", "--plug", ":aplug", "x"})
	c.Check(err, NotNil)
	c.Check(err.Error(), Equals, `cannot use --plug and --slot together`)
	c.Check(string(stdout), Equals, "")
	c.Check(string(stderr), Equals, "")
}

func (s *getAttrSuite) TestPlugOrSlotEmpty(c *C) {
	stdout, stderr, err := ctlcmd.Run(s.mockPlugHookContext, []string{"get", ":", "foo"})
	c.Check(err.Error(), Equals, "plug or slot name not provided")
	c.Check(string(stdout), Equals, "")
	c.Check(string(stderr), Equals, "")
}
