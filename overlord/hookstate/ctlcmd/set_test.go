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
	"reflect"

	"github.com/snapcore/snapd/interfaces"
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

type setAttrSuite struct {
	mockPlugHookContext *hookstate.Context
	mockSlotHookContext *hookstate.Context
	mockHandler         *hooktest.MockHandler
}

var _ = Suite(&setSuite{})
var _ = Suite(&setAttrSuite{})

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
	c.Check(err, ErrorMatches, "set which option.*")
	_, _, err = ctlcmd.Run(s.mockContext, []string{"set", "foo", "bar"})
	c.Check(err, ErrorMatches, ".*invalid parameter.*want key=value.*")
	_, _, err = ctlcmd.Run(s.mockContext, []string{"set", ":foo", "bar=baz"})
	c.Check(err, ErrorMatches, ".*interface attributes can only be set during the execution of prepare hooks.*")
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

func (s *setSuite) TestSetConfigOptionWithColon(c *C) {
	stdout, stderr, err := ctlcmd.Run(s.mockContext, []string{"set", "device-service.url=192.168.0.1:5555"})
	c.Check(err, IsNil)
	c.Check(string(stdout), Equals, "")
	c.Check(string(stderr), Equals, "")

	// Notify the context that we're done. This should save the config.
	s.mockContext.Lock()
	defer s.mockContext.Unlock()
	c.Check(s.mockContext.Done(), IsNil)

	// Verify that the global config has been updated.
	var value string
	tr := config.NewTransaction(s.mockContext.State())
	c.Check(tr.Get("test-snap", "device-service.url", &value), IsNil)
	c.Check(value, Equals, "192.168.0.1:5555")
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

func (s *setAttrSuite) SetUpTest(c *C) {
	s.mockHandler = hooktest.NewMockHandler()
	state := state.New(nil)
	state.Lock()
	ch := state.NewChange("mychange", "mychange")

	attrsTask := state.NewTask("connect-task", "my connect task")
	attrsTask.Set("plug", &interfaces.PlugRef{Snap: "a", Name: "aplug"})
	attrsTask.Set("slot", &interfaces.SlotRef{Snap: "b", Name: "bslot"})
	attrs := map[string]interface{}{
		"lorem": "ipsum",
		"nested": map[string]interface{}{
			"x": "y",
		},
	}
	attrsTask.Set("plug-attrs", attrs)
	attrsTask.Set("slot-attrs", attrs)
	ch.AddTask(attrsTask)
	state.Unlock()

	var err error

	// setup plug hook task
	state.Lock()
	plugHookTask := state.NewTask("run-hook", "my test task")
	state.Unlock()
	plugTaskSetup := &hookstate.HookSetup{Snap: "test-snap", Revision: snap.R(1), Hook: "prepare-plug-aplug"}
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
	slotTaskSetup := &hookstate.HookSetup{Snap: "test-snap", Revision: snap.R(1), Hook: "prepare-slot-aplug"}
	s.mockSlotHookContext, err = hookstate.NewContext(slotHookTask, slotTaskSetup, s.mockHandler)
	c.Assert(err, IsNil)

	s.mockSlotHookContext.Lock()
	s.mockSlotHookContext.Set("attrs-task", attrsTask.ID())
	s.mockSlotHookContext.Unlock()

	state.Lock()
	defer state.Unlock()
	ch.AddTask(slotHookTask)
}

func (s *setAttrSuite) TestSetPlugAttributesInPlugHook(c *C) {
	stdout, stderr, err := ctlcmd.Run(s.mockPlugHookContext, []string{"set", ":aplug", "foo=bar"})
	c.Check(err, IsNil)
	c.Check(string(stdout), Equals, "")
	c.Check(string(stderr), Equals, "")

	attrsTask, err := ctlcmd.AttributesTask(s.mockPlugHookContext)
	c.Assert(err, IsNil)
	st := s.mockPlugHookContext.State()
	st.Lock()
	defer st.Unlock()
	attrs := make(map[string]interface{})
	err = attrsTask.Get("plug-attrs", &attrs)
	c.Assert(err, IsNil)
	c.Check(attrs["foo"], Equals, "bar")
}

func (s *setAttrSuite) TestSetPlugAttributesSupportsDottedSyntax(c *C) {
	stdout, stderr, err := ctlcmd.Run(s.mockPlugHookContext, []string{"set", ":aplug", "my.attr1=foo", "my.attr2=bar"})
	c.Check(err, IsNil)
	c.Check(string(stdout), Equals, "")
	c.Check(string(stderr), Equals, "")

	attrsTask, err := ctlcmd.AttributesTask(s.mockPlugHookContext)
	c.Assert(err, IsNil)
	st := s.mockPlugHookContext.State()
	st.Lock()
	defer st.Unlock()
	attrs := make(map[string]interface{})
	err = attrsTask.Get("plug-attrs", &attrs)
	c.Assert(err, IsNil)
	c.Check(attrs["my"], DeepEquals, map[string]interface{}{"attr1": "foo", "attr2": "bar"})
}

func (s *setAttrSuite) TestSetProtectedAttributesCannotBeSet(c *C) {
	_, _, err := ctlcmd.Run(s.mockPlugHookContext, []string{"set", ":aplug", "lorem=z"})
	c.Assert(err, NotNil)
	c.Check(err, ErrorMatches, `cannot set attribute: attribute "lorem" cannot be overwritten`)

	_, _, err = ctlcmd.Run(s.mockPlugHookContext, []string{"set", ":aplug", "nested=z"})
	c.Assert(err, NotNil)
	c.Check(err, ErrorMatches, `cannot set attribute: attribute "nested" cannot be overwritten`)

	_, _, err = ctlcmd.Run(s.mockPlugHookContext, []string{"set", ":aplug", "nested.x=123"})
	c.Assert(err, NotNil)
	c.Check(err, ErrorMatches, `cannot set attribute: attribute "nested.x" cannot be overwritten`)
}

func (s *setAttrSuite) TestSetNewAttributeInProtectedMapCanBeSet(c *C) {
	_, _, err := ctlcmd.Run(s.mockPlugHookContext, []string{"set", ":aplug", "nested.new=w"})
	c.Assert(err, IsNil)

	// doing it again should work too
	_, _, err = ctlcmd.Run(s.mockPlugHookContext, []string{"set", ":aplug", "nested.new=w"})
	c.Assert(err, IsNil)

	attrsTask, err := ctlcmd.AttributesTask(s.mockPlugHookContext)
	c.Assert(err, IsNil)

	st := s.mockPlugHookContext.State()
	st.Lock()
	defer st.Unlock()

	attrs := make(map[string]interface{})
	err = attrsTask.Get("plug-attrs", &attrs)
	c.Assert(err, IsNil)
	c.Check(attrs["nested"], DeepEquals, map[string]interface{}{"x": "y", "new": "w"})
}

func (s *setAttrSuite) TestSetSlotAttributesInSlotHook(c *C) {
	stdout, stderr, err := ctlcmd.Run(s.mockSlotHookContext, []string{"set", ":bslot", "foo=bar"})
	c.Check(err, IsNil)
	c.Check(string(stdout), Equals, "")
	c.Check(string(stderr), Equals, "")

	attrsTask, err := ctlcmd.AttributesTask(s.mockSlotHookContext)
	c.Assert(err, IsNil)
	st := s.mockSlotHookContext.State()
	st.Lock()
	defer st.Unlock()
	attrs := make(map[string]interface{})
	err = attrsTask.Get("slot-attrs", &attrs)
	c.Assert(err, IsNil)
	c.Check(attrs["foo"], Equals, "bar")
}

func (s *setAttrSuite) TestPlugOrSlotEmpty(c *C) {
	stdout, stderr, err := ctlcmd.Run(s.mockPlugHookContext, []string{"set", ":", "foo=bar"})
	c.Check(err.Error(), Equals, "plug or slot name not provided")
	c.Check(string(stdout), Equals, "")
	c.Check(string(stderr), Equals, "")
}

func (s *setAttrSuite) TestSetCommandFailsOutsideOfValidContext(c *C) {
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
	c.Check(err.Error(), Equals, `interface attributes can only be set during the execution of prepare hooks`)
	c.Check(string(stdout), Equals, "")
	c.Check(string(stderr), Equals, "")
}

func (s *setAttrSuite) TestCopyAttributes(c *C) {
	orig := map[string]interface{}{
		"a": "A",
		"b": true,
		"c": int(100),
		"d": []interface{}{"x", "y", true},
		"e": map[string]interface{}{
			"e1": "E1",
		},
	}

	cpy, err := ctlcmd.CopyAttributes(orig)
	c.Assert(err, IsNil)
	// verify that int is converted into int64
	c.Check(reflect.TypeOf(cpy["c"]).Kind(), Equals, reflect.Int64)
	c.Check(reflect.TypeOf(orig["c"]).Kind(), Equals, reflect.Int)
	// change the type of orig's value to int64 to make DeepEquals happy in the test
	orig["c"] = int64(100)
	c.Check(cpy, DeepEquals, orig)

	cpy["d"].([]interface{})[0] = 999
	c.Check(orig["d"].([]interface{})[0], Equals, "x")
	cpy["e"].(map[string]interface{})["e1"] = "x"
	c.Check(orig["e"].(map[string]interface{})["e1"], Equals, "E1")

	type unsupported struct{}
	var x unsupported
	_, err = ctlcmd.CopyAttributes(map[string]interface{}{"x": x})
	c.Assert(err, NotNil)
	c.Check(err, ErrorMatches, "unsupported attribute type 'ctlcmd_test.unsupported', value '{}'")
}
