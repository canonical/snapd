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
	s.mockContext, err = hookstate.NewContext(task, task.State(), setup, s.mockHandler, "")
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
		mockContext, err := hookstate.NewContext(task, task.State(), setup, mockHandler, "")
		c.Check(err, IsNil)

		// Initialize configuration
		tr := config.NewTransaction(state)
		tr.Set("test-snap", "test-key1", "test-value1")
		tr.Set("test-snap", "test-key2", 2)
		tr.Commit()

		state.Unlock()

		stdout, stderr, err := ctlcmd.Run(mockContext, strings.Fields(test.args), 0)
		if test.error != "" {
			c.Check(err, ErrorMatches, test.error)
		} else {
			c.Check(err, IsNil)
			c.Check(string(stderr), Equals, "")
			c.Check(string(stdout), Equals, test.stdout)
		}
	}
}

func (s *getSuite) TestGetRegularUser(c *C) {
	state := state.New(nil)
	state.Lock()

	task := state.NewTask("test-task", "my test task")
	setup := &hookstate.HookSetup{Snap: "test-snap", Revision: snap.R(1), Hook: "test-hook"}

	// Initialize configuration
	tr := config.NewTransaction(state)
	tr.Set("test-snap", "test-key1", "test-value1")
	tr.Commit()

	state.Unlock()

	mockHandler := hooktest.NewMockHandler()
	mockContext, err := hookstate.NewContext(task, task.State(), setup, mockHandler, "")
	c.Assert(err, IsNil)
	stdout, stderr, err := ctlcmd.Run(mockContext, []string{"get", "test-key1"}, 1000)
	c.Assert(err, IsNil)
	c.Assert(string(stdout), Equals, "test-value1\n")
	c.Assert(string(stderr), Equals, "")
}

func (s *getSuite) TestCommandWithoutContext(c *C) {
	_, _, err := ctlcmd.Run(nil, []string{"get", "foo"}, 0)
	c.Check(err, ErrorMatches, ".*cannot get without a context.*")
}

func (s *setSuite) TestNull(c *C) {
	_, _, err := ctlcmd.Run(s.mockContext, []string{"set", "foo=null"}, 0)
	c.Check(err, IsNil)

	_, _, err = ctlcmd.Run(s.mockContext, []string{"set", `bar=[null]`}, 0)
	c.Check(err, IsNil)

	// Notify the context that we're done. This should save the config.
	s.mockContext.Lock()
	defer s.mockContext.Unlock()
	c.Check(s.mockContext.Done(), IsNil)

	// Verify config value
	var value interface{}
	tr := config.NewTransaction(s.mockContext.State())
	c.Assert(tr.Get("test-snap", "foo", &value), IsNil)
	c.Assert(value, IsNil)
	c.Assert(tr.Get("test-snap", "bar", &value), IsNil)
	c.Assert(value, DeepEquals, []interface{}{nil})
}
func (s *getAttrSuite) SetUpTest(c *C) {
	s.mockHandler = hooktest.NewMockHandler()

	state := state.New(nil)
	state.Lock()
	ch := state.NewChange("mychange", "mychange")

	attrsTask := state.NewTask("connect-task", "my connect task")
	attrsTask.Set("plug", &interfaces.PlugRef{Snap: "a", Name: "aplug"})
	attrsTask.Set("slot", &interfaces.SlotRef{Snap: "b", Name: "bslot"})
	staticPlugAttrs := map[string]interface{}{
		"aattr":   "foo",
		"baz":     []string{"a", "b"},
		"mapattr": map[string]interface{}{"mapattr1": "mapval1", "mapattr2": "mapval2"},
	}
	dynamicPlugAttrs := map[string]interface{}{
		"dyn-plug-attr": "c",
	}
	dynamicSlotAttrs := map[string]interface{}{
		"dyn-slot-attr": "d",
	}

	staticSlotAttrs := map[string]interface{}{
		"battr": "bar",
	}
	attrsTask.Set("plug-static", staticPlugAttrs)
	attrsTask.Set("plug-dynamic", dynamicPlugAttrs)
	attrsTask.Set("slot-static", staticSlotAttrs)
	attrsTask.Set("slot-dynamic", dynamicSlotAttrs)

	ch.AddTask(attrsTask)
	state.Unlock()

	var err error

	// setup plug hook task
	state.Lock()
	plugHookTask := state.NewTask("run-hook", "my test task")
	state.Unlock()
	plugTaskSetup := &hookstate.HookSetup{Snap: "test-snap", Revision: snap.R(1), Hook: "connect-plug-aplug"}
	s.mockPlugHookContext, err = hookstate.NewContext(plugHookTask, plugHookTask.State(), plugTaskSetup, s.mockHandler, "")
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
	s.mockSlotHookContext, err = hookstate.NewContext(slotHookTask, slotHookTask.State(), slotTaskSetup, s.mockHandler, "")
	c.Assert(err, IsNil)

	s.mockSlotHookContext.Lock()
	s.mockSlotHookContext.Set("attrs-task", attrsTask.ID())
	s.mockSlotHookContext.Unlock()

	state.Lock()
	defer state.Unlock()
	ch.AddTask(slotHookTask)
}

var getPlugAttributesTests = []struct {
	args, stdout, error string
}{{
	args:   "get :aplug aattr",
	stdout: "foo\n",
}, {
	args:   "get -d :aplug baz",
	stdout: "{\n\t\"baz\": [\n\t\t\"a\",\n\t\t\"b\"\n\t]\n}\n",
}, {
	args:   "get :aplug mapattr.mapattr1",
	stdout: "mapval1\n",
}, {
	args:   "get -d :aplug mapattr.mapattr1",
	stdout: "{\n\t\"mapattr.mapattr1\": \"mapval1\"\n}\n",
}, {
	args:   "get :aplug dyn-plug-attr",
	stdout: "c\n",
}, {
	// The --plug parameter doesn't do anything if used on plug side
	args:   "get --plug :aplug aattr",
	stdout: "foo\n",
}, {
	args:   "get --slot :aplug battr",
	stdout: "bar\n",
}, {
	args:  "get :aplug x",
	error: `unknown attribute "x"`,
}, {
	args:  "get :bslot x",
	error: `unknown plug or slot "bslot"`,
}, {
	args:  "get : foo",
	error: "plug or slot name not provided",
}}

func (s *getAttrSuite) TestPlugHookTests(c *C) {
	for _, test := range getPlugAttributesTests {
		c.Logf("Test: %s", test.args)

		stdout, stderr, err := ctlcmd.Run(s.mockPlugHookContext, strings.Fields(test.args), 0)
		if test.error != "" {
			c.Check(err, ErrorMatches, test.error)
		} else {
			c.Check(err, IsNil)
			c.Check(string(stderr), Equals, "")
			c.Check(string(stdout), Equals, test.stdout)
		}
	}
}

var getSlotAttributesTests = []struct {
	args, stdout, error string
}{{
	args:   "get :bslot battr",
	stdout: "bar\n",
}, {
	args:   "get :bslot dyn-slot-attr",
	stdout: "d\n",
}, {
	// The --slot parameter doesn't do anything if used on slot side
	args:   "get --slot :bslot battr",
	stdout: "bar\n",
}, {
	args:   "get --plug :bslot aattr",
	stdout: "foo\n",
}, {
	args:  "get :bslot x",
	error: `unknown attribute "x"`,
}, {
	args:  "get :aplug x",
	error: `unknown plug or slot "aplug"`,
}, {
	args:  "get --slot --plug :aplug x",
	error: `cannot use --plug and --slot together`,
}}

func (s *getAttrSuite) TestSlotHookTests(c *C) {
	for _, test := range getSlotAttributesTests {
		c.Logf("Test: %s", test.args)

		stdout, stderr, err := ctlcmd.Run(s.mockSlotHookContext, strings.Fields(test.args), 0)
		if test.error != "" {
			c.Check(err, ErrorMatches, test.error)
		} else {
			c.Check(err, IsNil)
			c.Check(string(stderr), Equals, "")
			c.Check(string(stdout), Equals, test.stdout)
		}
	}
}
