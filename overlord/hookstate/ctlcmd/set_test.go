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
	"encoding/json"
	"fmt"
	"strings"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/hookstate"
	"github.com/snapcore/snapd/overlord/hookstate/ctlcmd"
	"github.com/snapcore/snapd/overlord/hookstate/hooktest"
	"github.com/snapcore/snapd/overlord/registrystate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
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
	s.mockContext, err = hookstate.NewContext(task, task.State(), setup, s.mockHandler, "")
	c.Assert(err, IsNil)
}

func (s *setSuite) TestInvalidArguments(c *C) {
	_, _, err := ctlcmd.Run(s.mockContext, []string{"set"}, 0)
	c.Check(err, ErrorMatches, "set which option.*")
	_, _, err = ctlcmd.Run(s.mockContext, []string{"set", "foo", "bar"}, 0)
	c.Check(err, ErrorMatches, ".*invalid configuration.*want key=value.*")
	_, _, err = ctlcmd.Run(s.mockContext, []string{"set", ":foo", "bar=baz"}, 0)
	c.Check(err, ErrorMatches, ".*interface attributes can only be set during the execution of prepare hooks.*")
}

func (s *setSuite) TestCommand(c *C) {
	stdout, stderr, err := ctlcmd.Run(s.mockContext, []string{"set", "foo=bar", "baz=qux"}, 0)
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

func (s *setSuite) TestSetRegularUserForbidden(c *C) {
	_, _, err := ctlcmd.Run(s.mockContext, []string{"set", "test-key1"}, 1000)
	c.Assert(err, ErrorMatches, `cannot use "set" with uid 1000, try with sudo`)
	forbidden, _ := err.(*ctlcmd.ForbiddenCommandError)
	c.Assert(forbidden, NotNil)
}

func (s *setSuite) TestSetHelpRegularUserAllowed(c *C) {
	_, _, err := ctlcmd.Run(s.mockContext, []string{"set", "-h"}, 1000)
	c.Assert(err, NotNil)
	c.Assert(strings.HasPrefix(err.Error(), "Usage:"), Equals, true)
}

func (s *setSuite) TestSetConfigOptionWithColon(c *C) {
	stdout, stderr, err := ctlcmd.Run(s.mockContext, []string{"set", "device-service.url=192.168.0.1:5555"}, 0)
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

func (s *setSuite) TestUnsetConfigOptionWithInitialConfiguration(c *C) {
	// Setup an initial configuration
	s.mockContext.State().Lock()
	tr := config.NewTransaction(s.mockContext.State())
	tr.Set("test-snap", "test-key1", "test-value1")
	tr.Set("test-snap", "test-key2", "test-value2")
	tr.Set("test-snap", "test-key3.foo", "foo-value")
	tr.Set("test-snap", "test-key3.bar", "bar-value")
	tr.Commit()
	s.mockContext.State().Unlock()

	stdout, stderr, err := ctlcmd.Run(s.mockContext, []string{"set", "test-key1!", "test-key3.foo!"}, 0)
	c.Check(err, IsNil)
	c.Check(string(stdout), Equals, "")
	c.Check(string(stderr), Equals, "")

	// Notify the context that we're done. This should save the config.
	s.mockContext.Lock()
	defer s.mockContext.Unlock()
	c.Check(s.mockContext.Done(), IsNil)

	// Verify that the global config has been updated.
	var value string
	tr = config.NewTransaction(s.mockContext.State())
	c.Check(tr.Get("test-snap", "test-key2", &value), IsNil)
	c.Check(value, Equals, "test-value2")
	c.Check(tr.Get("test-snap", "test-key1", &value), ErrorMatches, `snap "test-snap" has no "test-key1" configuration option`)
	var value2 interface{}
	c.Check(tr.Get("test-snap", "test-key3", &value2), IsNil)
	c.Check(value2, DeepEquals, map[string]interface{}{"bar": "bar-value"})
}

func (s *setSuite) TestUnsetConfigOptionWithNoInitialConfiguration(c *C) {
	stdout, stderr, err := ctlcmd.Run(s.mockContext, []string{"set", "test-key.key1=value1", "test-key.key2=value2", "test-key.key1!"}, 0)
	c.Check(err, IsNil)
	c.Check(string(stdout), Equals, "")
	c.Check(string(stderr), Equals, "")

	// Notify the context that we're done. This should save the config.
	s.mockContext.Lock()
	defer s.mockContext.Unlock()
	c.Check(s.mockContext.Done(), IsNil)

	// Verify that the global config has been updated.
	var value interface{}
	tr := config.NewTransaction(s.mockContext.State())
	c.Check(tr.Get("test-snap", "test-key.key2", &value), IsNil)
	c.Check(value, DeepEquals, "value2")
	c.Check(tr.Get("test-snap", "test-key.key1", &value), ErrorMatches, `snap "test-snap" has no "test-key.key1" configuration option`)
	c.Check(value, DeepEquals, "value2")
}

func (s *setSuite) TestSetNumbers(c *C) {
	stdout, stderr, err := ctlcmd.Run(s.mockContext, []string{"set", "foo=1234567890", "bar=123456.7890"}, 0)
	c.Check(err, IsNil)
	c.Check(string(stdout), Equals, "")
	c.Check(string(stderr), Equals, "")

	// Notify the context that we're done. This should save the config.
	s.mockContext.Lock()
	defer s.mockContext.Unlock()
	c.Check(s.mockContext.Done(), IsNil)

	// Verify that the global config has been updated.
	var value interface{}
	tr := config.NewTransaction(s.mockContext.State())
	c.Check(tr.Get("test-snap", "foo", &value), IsNil)
	c.Check(value, Equals, json.Number("1234567890"))

	c.Check(tr.Get("test-snap", "bar", &value), IsNil)
	c.Check(value, Equals, json.Number("123456.7890"))
}

func (s *setSuite) TestSetStrictJSON(c *C) {
	stdout, stderr, err := ctlcmd.Run(s.mockContext, []string{"set", "-t", `key={"a":"b", "c": 1, "d": {"e":"f"}}`}, 0)
	c.Assert(err, IsNil)
	c.Check(string(stdout), Equals, "")
	c.Check(string(stderr), Equals, "")

	// Notify the context that we're done. This should save the config.
	s.mockContext.Lock()
	defer s.mockContext.Unlock()
	c.Check(s.mockContext.Done(), IsNil)

	// Verify that the global config has been updated.
	var value interface{}
	tr := config.NewTransaction(s.mockContext.State())
	c.Assert(tr.Get("test-snap", "key", &value), IsNil)
	c.Check(value, DeepEquals, map[string]interface{}{"a": "b", "c": json.Number("1"), "d": map[string]interface{}{"e": "f"}})
}

func (s *setSuite) TestSetFailWithStrictJSON(c *C) {
	_, _, err := ctlcmd.Run(s.mockContext, []string{"set", "-t", `key=a`}, 0)
	c.Assert(err, ErrorMatches, "failed to parse JSON:.*")
}

func (s *setSuite) TestSetAsString(c *C) {
	expected := `{"a":"b", "c": 1, "d": {"e": "f"}}`
	stdout, stderr, err := ctlcmd.Run(s.mockContext, []string{"set", "-s", fmt.Sprintf("key=%s", expected)}, 0)
	c.Assert(err, IsNil)
	c.Check(string(stdout), Equals, "")
	c.Check(string(stderr), Equals, "")

	// Notify the context that we're done. This should save the config.
	s.mockContext.Lock()
	defer s.mockContext.Unlock()
	c.Check(s.mockContext.Done(), IsNil)

	// Verify that the global config has been updated.
	var value interface{}
	tr := config.NewTransaction(s.mockContext.State())
	c.Assert(tr.Get("test-snap", "key", &value), IsNil)
	c.Check(value, Equals, expected)
}

func (s *setSuite) TestSetErrorOnStrictJSONAndString(c *C) {
	stdout, stderr, err := ctlcmd.Run(s.mockContext, []string{"set", "-s", "-t", `{"a":"b"}`}, 0)
	c.Assert(err, ErrorMatches, "cannot use -t and -s together")
	c.Check(string(stdout), Equals, "")
	c.Check(string(stderr), Equals, "")
}

func (s *setSuite) TestCommandSavesDeltasOnly(c *C) {
	// Setup an initial configuration
	s.mockContext.State().Lock()
	tr := config.NewTransaction(s.mockContext.State())
	tr.Set("test-snap", "test-key1", "test-value1")
	tr.Set("test-snap", "test-key2", "test-value2")
	tr.Commit()
	s.mockContext.State().Unlock()

	stdout, stderr, err := ctlcmd.Run(s.mockContext, []string{"set", "test-key2=test-value3"}, 0)
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
	_, _, err := ctlcmd.Run(nil, []string{"set", "foo=bar"}, 0)
	c.Check(err, ErrorMatches, `cannot invoke snapctl operation commands \(here "set"\) from outside of a snap`)
}

func (s *setAttrSuite) SetUpTest(c *C) {
	s.mockHandler = hooktest.NewMockHandler()
	state := state.New(nil)
	state.Lock()
	ch := state.NewChange("mychange", "mychange")

	attrsTask := state.NewTask("connect-task", "my connect task")
	attrsTask.Set("plug", &interfaces.PlugRef{Snap: "a", Name: "aplug"})
	attrsTask.Set("slot", &interfaces.SlotRef{Snap: "b", Name: "bslot"})
	staticAttrs := map[string]interface{}{
		"lorem": "ipsum",
		"nested": map[string]interface{}{
			"x": "y",
		},
	}
	dynamicAttrs := make(map[string]interface{})
	attrsTask.Set("plug-static", staticAttrs)
	attrsTask.Set("plug-dynamic", dynamicAttrs)
	attrsTask.Set("slot-static", staticAttrs)
	attrsTask.Set("slot-dynamic", dynamicAttrs)
	ch.AddTask(attrsTask)
	state.Unlock()

	var err error

	// setup plug hook task
	state.Lock()
	plugHookTask := state.NewTask("run-hook", "my test task")
	state.Unlock()
	plugTaskSetup := &hookstate.HookSetup{Snap: "test-snap", Revision: snap.R(1), Hook: "prepare-plug-aplug"}
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
	slotTaskSetup := &hookstate.HookSetup{Snap: "test-snap", Revision: snap.R(1), Hook: "prepare-slot-aplug"}
	s.mockSlotHookContext, err = hookstate.NewContext(slotHookTask, slotHookTask.State(), slotTaskSetup, s.mockHandler, "")
	c.Assert(err, IsNil)

	s.mockSlotHookContext.Lock()
	s.mockSlotHookContext.Set("attrs-task", attrsTask.ID())
	s.mockSlotHookContext.Unlock()

	state.Lock()
	defer state.Unlock()
	ch.AddTask(slotHookTask)
}

func (s *setAttrSuite) TestSetPlugAttributesInPlugHook(c *C) {
	stdout, stderr, err := ctlcmd.Run(s.mockPlugHookContext, []string{"set", ":aplug", "foo=bar"}, 0)
	c.Check(err, IsNil)
	c.Check(string(stdout), Equals, "")
	c.Check(string(stderr), Equals, "")

	attrsTask, err := ctlcmd.AttributesTask(s.mockPlugHookContext)
	c.Assert(err, IsNil)
	st := s.mockPlugHookContext.State()
	st.Lock()
	defer st.Unlock()
	dynattrs := make(map[string]interface{})
	err = attrsTask.Get("plug-dynamic", &dynattrs)
	c.Assert(err, IsNil)
	c.Check(dynattrs["foo"], Equals, "bar")
}

func (s *setAttrSuite) TestSetPlugAttributesSupportsDottedSyntax(c *C) {
	stdout, stderr, err := ctlcmd.Run(s.mockPlugHookContext, []string{"set", ":aplug", "my.attr1=foo", "my.attr2=bar"}, 0)
	c.Check(err, IsNil)
	c.Check(string(stdout), Equals, "")
	c.Check(string(stderr), Equals, "")

	attrsTask, err := ctlcmd.AttributesTask(s.mockPlugHookContext)
	c.Assert(err, IsNil)
	st := s.mockPlugHookContext.State()
	st.Lock()
	defer st.Unlock()
	dynattrs := make(map[string]interface{})
	err = attrsTask.Get("plug-dynamic", &dynattrs)
	c.Assert(err, IsNil)
	c.Check(dynattrs["my"], DeepEquals, map[string]interface{}{"attr1": "foo", "attr2": "bar"})
}

func (s *setAttrSuite) TestPlugOrSlotEmpty(c *C) {
	stdout, stderr, err := ctlcmd.Run(s.mockPlugHookContext, []string{"set", ":", "foo=bar"}, 0)
	c.Check(err, ErrorMatches, "plug or slot name not provided")
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
	mockContext, err = hookstate.NewContext(task, task.State(), setup, s.mockHandler, "")
	c.Assert(err, IsNil)

	stdout, stderr, err := ctlcmd.Run(mockContext, []string{"set", ":aplug", "foo=bar"}, 0)
	c.Check(err, ErrorMatches, `interface attributes can only be set during the execution of prepare hooks`)
	c.Check(string(stdout), Equals, "")
	c.Check(string(stderr), Equals, "")
}

func (s *registrySuite) TestRegistrySetSingleView(c *C) {
	stdout, stderr, err := ctlcmd.Run(s.mockContext, []string{"set", "--view", ":write-wifi", "ssid=other-ssid"}, 0)
	c.Assert(err, IsNil)
	c.Check(stdout, IsNil)
	c.Check(stderr, IsNil)
	s.mockContext.Lock()
	c.Assert(s.mockContext.Done(), IsNil)
	s.mockContext.Unlock()

	s.state.Lock()
	val, err := registrystate.GetViaView(s.state, s.devAccID, "network", "read-wifi", []string{"ssid"})
	s.state.Unlock()
	c.Assert(err, IsNil)
	c.Assert(val, DeepEquals, map[string]interface{}{"ssid": "other-ssid"})
}

func (s *registrySuite) TestRegistrySetManyViews(c *C) {
	stdout, stderr, err := ctlcmd.Run(s.mockContext, []string{"set", "--view", ":write-wifi", "ssid=other-ssid", "password=other-secret"}, 0)
	c.Assert(err, IsNil)
	c.Check(stdout, IsNil)
	c.Check(stderr, IsNil)
	s.mockContext.Lock()
	c.Assert(s.mockContext.Done(), IsNil)
	s.mockContext.Unlock()

	s.state.Lock()
	val, err := registrystate.GetViaView(s.state, s.devAccID, "network", "read-wifi", []string{"ssid", "password"})
	s.state.Unlock()
	c.Assert(err, IsNil)
	c.Assert(val, DeepEquals, map[string]interface{}{
		"ssid":     "other-ssid",
		"password": "other-secret",
	})
}

func (s *registrySuite) TestRegistrySetHappensTransactionally(c *C) {
	// sets values in a transaction
	stdout, stderr, err := ctlcmd.Run(s.mockContext, []string{"set", "--view", ":write-wifi", "ssid=my-ssid"}, 0)
	c.Assert(err, IsNil)
	c.Check(stdout, IsNil)
	c.Check(stderr, IsNil)

	s.state.Lock()
	_, err = registrystate.GetViaView(s.state, s.devAccID, "network", "read-wifi", []string{"ssid"})
	s.state.Unlock()
	c.Assert(err, ErrorMatches, ".*matching rules don't map to any values")

	// commit transaction
	s.mockContext.Lock()
	c.Assert(s.mockContext.Done(), IsNil)
	s.mockContext.Unlock()

	s.state.Lock()
	val, err := registrystate.GetViaView(s.state, s.devAccID, "network", "read-wifi", []string{"ssid"})
	s.state.Unlock()
	c.Assert(err, IsNil)
	c.Assert(val, DeepEquals, map[string]interface{}{
		"ssid": "my-ssid",
	})
}

func (s *registrySuite) TestRegistrySetInvalid(c *C) {
	type testcase struct {
		args []string
		err  string
	}

	tcs := []testcase{
		{
			args: []string{":non-existent", "ssid=my-ssid"},
			err:  `cannot set registry: cannot find plug :non-existent for snap "test-snap"`,
		},
		{
			args: []string{":non-existent", "ssid"},
			err:  `cannot set :non-existent plug: invalid configuration: "ssid" \(want key=value\)`,
		},
	}

	for _, tc := range tcs {
		stdout, stderr, err := ctlcmd.Run(s.mockContext, append([]string{"set", "--view"}, tc.args...), 0)
		c.Assert(err, ErrorMatches, tc.err)
		c.Check(stdout, IsNil)
		c.Check(stderr, IsNil)
	}
}

func (s *registrySuite) TestRegistrySetExclamationMark(c *C) {
	stdout, stderr, err := ctlcmd.Run(s.mockContext, []string{"set", "--view", ":write-wifi", "ssid=other-ssid", "password=other-secret"}, 0)
	c.Assert(err, IsNil)
	c.Check(stdout, IsNil)
	c.Check(stderr, IsNil)

	stdout, stderr, err = ctlcmd.Run(s.mockContext, []string{"set", "--view", ":write-wifi", "password!"}, 0)
	c.Assert(err, IsNil)
	c.Check(stdout, IsNil)
	c.Check(stderr, IsNil)

	stdout, stderr, err = ctlcmd.Run(s.mockContext, []string{"get", "--view", ":read-wifi"}, 0)
	c.Assert(err, IsNil)
	c.Check(string(stdout), Equals, `{
	"ssid": "other-ssid"
}
`)
	c.Check(stderr, IsNil)
}
