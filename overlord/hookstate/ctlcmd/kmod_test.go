// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2022 Canonical Ltd
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
	"errors"

	. "gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/overlord/hookstate"
	"github.com/snapcore/snapd/overlord/hookstate/ctlcmd"
	"github.com/snapcore/snapd/overlord/hookstate/hooktest"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
)

type kmodSuite struct {
	testutil.BaseTest
	state       *state.State
	mockContext *hookstate.Context
	mockHandler *hooktest.MockHandler
	hookTask    *state.Task
	// A connection state for a snap using the kmod interface with the plug
	// properly configured, which we'll be reusing in different test cases
	regularConnState map[string]interface{}
}

var _ = Suite(&kmodSuite{})

func (s *kmodSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
	dirs.SetRootDir(c.MkDir())

	s.mockHandler = hooktest.NewMockHandler()

	s.state = state.New(nil)
	s.state.Lock()
	defer s.state.Unlock()
	task := s.state.NewTask("test-task", "my test task")
	setup := &hookstate.HookSetup{Snap: "snap1", Revision: snap.R(42), Hook: "kmod"}

	ctx := mylog.Check2(hookstate.NewContext(task, s.state, setup, s.mockHandler, ""))

	s.mockContext = ctx

	s.regularConnState = map[string]interface{}{
		"interface": "kernel-module-load",
		"plug-static": map[string]interface{}{
			"modules": []interface{}{
				map[string]interface{}{
					"name": "module1",
					"load": "dynamic",
				},
				map[string]interface{}{
					"name":    "module2",
					"load":    "dynamic",
					"options": "*",
				},
			},
		},
	}
	s.hookTask = task
}

func (s *kmodSuite) injectSnapWithProperPlug(c *C) {
	s.state.Lock()
	mockInstalledSnap(c, s.state, `name: snap1`, "")
	s.state.Set("conns", map[string]interface{}{
		"snap1:plug1 snap2:slot2": s.regularConnState,
	})
	s.state.Unlock()
}

func (s *kmodSuite) TestMissingContext(c *C) {
	_, _ := mylog.Check3(ctlcmd.Run(nil, []string{"kmod", "insert", "module1"}, 0))
	c.Check(err, ErrorMatches, `cannot invoke snapctl operation commands \(here "kmod"\) from outside of a snap`)
	_, _ = mylog.Check3(ctlcmd.Run(nil, []string{"kmod", "remove", "module1"}, 0))
	c.Check(err, ErrorMatches, `cannot invoke snapctl operation commands \(here "kmod"\) from outside of a snap`)
}

func (s *kmodSuite) TestMatchConnection(c *C) {
	for _, td := range []struct {
		attributes    map[string]interface{}
		moduleName    string
		moduleOptions []string
		expectedMatch bool
	}{
		// missing "load" attribute
		{map[string]interface{}{}, "", []string{}, false},
		// empty "load" attribute
		{map[string]interface{}{"load": ""}, "", []string{}, false},
		// "load" attribute must be set to "dynamic"
		{map[string]interface{}{"load": "on-boot"}, "", []string{}, false},
		// different module name
		{map[string]interface{}{"load": "dynamic", "name": "mod1"}, "mod2", []string{}, false},
		// options given but plug does not have "options" attribute
		{map[string]interface{}{"load": "dynamic", "name": "mod1"}, "mod1", []string{"opt1"}, false},
		// options given but plug does not have "options" set to "*"
		{map[string]interface{}{"load": "dynamic", "name": "mod1", "options": "opt1"}, "mod1", []string{"opt1"}, false},
		// happy with no options
		{map[string]interface{}{"load": "dynamic", "name": "mod1"}, "mod1", []string{}, true},
		// happy with options and "*" on plug
		{map[string]interface{}{"load": "dynamic", "name": "mod1", "options": "*"}, "mod1", []string{"opt1"}, true},
	} {
		testLabel := Commentf("Attrs: %v, name: %q, opts: %q", td.attributes, td.moduleName, td.moduleOptions)
		matches := ctlcmd.KmodMatchConnection(td.attributes, td.moduleName, td.moduleOptions)
		c.Check(matches, Equals, td.expectedMatch, testLabel)
	}
}

func (s *kmodSuite) TestFindConnectionBadConnection(c *C) {
	setup := &hookstate.HookSetup{}

	// Inject some invalid connection data into the state, so that
	// ifacestate.ConnectionStates() will return an error.
	state := state.New(nil)
	state.Lock()
	task := state.NewTask("test-task", "my test task")
	state.Set("conns", "I wish I was JSON")
	state.Unlock()
	ctx := mylog.Check2(hookstate.NewContext(task, state, setup, s.mockHandler, ""))

	mylog.Check(ctlcmd.KmodCheckConnection(ctx, "module1", []string{"one", "two"}))
	c.Assert(err, ErrorMatches, `.*internal error: cannot get connections: .*`)
}

func (s *kmodSuite) TestFindConnectionMissingProperPlug(c *C) {
	s.state.Lock()
	mockInstalledSnap(c, s.state, `name: snap1`, "")
	// Inject a lot of connections in the state, but all of them defective for
	// one or another reason
	connections := make(map[string]interface{})
	// wrong interface
	conn := CopyMap(s.regularConnState)
	conn["interface"] = "unrelated"
	connections["snap1:plug1 snap2:slot1"] = conn
	// undesired
	conn = CopyMap(s.regularConnState)
	conn["undesired"] = true
	connections["snap1:plug2 snap2:slot1"] = conn
	// hotplug gone
	conn = CopyMap(s.regularConnState)
	conn["hotplug-gone"] = true
	connections["snap1:plug3 snap2:slot1"] = conn
	// different snap
	conn = CopyMap(s.regularConnState)
	connections["othersnap:plug1 snap2:slot1"] = conn
	// missing plug info
	conn = CopyMap(s.regularConnState)
	delete(conn, "plug-static")
	connections["snap1:plug4 snap2:slot1"] = conn
	// good connection, finally; but our module name won't match
	conn = CopyMap(s.regularConnState)
	connections["snap1:plug5 snap2:slot1"] = conn

	s.state.Set("conns", connections)
	s.state.Unlock()
	mylog.Check(ctlcmd.KmodCheckConnection(s.mockContext, "module3", []string{"opt1=v1"}))
	c.Check(err, ErrorMatches, "required interface not connected")
}

func (s *kmodSuite) TestFindConnectionHappy(c *C) {
	s.injectSnapWithProperPlug(c)
	mylog.Check(ctlcmd.KmodCheckConnection(s.mockContext, "module2", []string{"opt1=v1"}))
	c.Check(err, IsNil)
}

func (s *kmodSuite) TestInsertFailure(c *C) {
	s.injectSnapWithProperPlug(c)

	var loadModuleError error
	var ensureConnectionError error

	r1 := ctlcmd.MockKmodCheckConnection(func(ctx *hookstate.Context, moduleName string, moduleOptions []string) error {
		c.Check(moduleName, Equals, "moderr")
		c.Check(moduleOptions, DeepEquals, []string{"o1=v1", "o2=v2"})
		return ensureConnectionError
	})
	defer r1()

	r2 := ctlcmd.MockKmodLoadModule(func(name string, options []string) error {
		c.Check(name, Equals, "moderr")
		c.Check(options, DeepEquals, []string{"o1=v1", "o2=v2"})
		return loadModuleError
	})
	defer r2()

	for _, td := range []struct {
		ensureConnectionError error
		loadModuleError       error
		expectedError         string
	}{
		{
			// error retrieving the snap connections
			ensureConnectionError: errors.New("state error"),
			expectedError:         `cannot load module "moderr": state error`,
		},
		{
			// error calling modprobe
			loadModuleError: errors.New("modprobe failure"),
			expectedError:   `cannot load module "moderr": modprobe failure`,
		},
	} {
		ensureConnectionError = td.ensureConnectionError
		loadModuleError = td.loadModuleError
		_, _ := mylog.Check3(ctlcmd.Run(s.mockContext, []string{"kmod", "insert", "moderr", "o1=v1", "o2=v2"}, 0))
		c.Check(err, ErrorMatches, td.expectedError)
	}
}

func (s *kmodSuite) TestInsertHappy(c *C) {
	s.injectSnapWithProperPlug(c)

	loadModuleCalls := 0
	restore := ctlcmd.MockKmodLoadModule(func(name string, options []string) error {
		loadModuleCalls++
		c.Check(name, Equals, "module2")
		c.Check(options, DeepEquals, []string{"opt1=v1", "opt2=v2"})
		return nil
	})
	defer restore()

	_, _ := mylog.Check3(ctlcmd.Run(s.mockContext,
		[]string{"kmod", "insert", "module2", "opt1=v1", "opt2=v2"}, 0))
	c.Check(err, IsNil)
	c.Check(loadModuleCalls, Equals, 1)
}

func (s *kmodSuite) TestRemoveFailure(c *C) {
	s.injectSnapWithProperPlug(c)

	var loadModuleError error
	var ensureConnectionError error

	r1 := ctlcmd.MockKmodCheckConnection(func(ctx *hookstate.Context, moduleName string, moduleOptions []string) error {
		c.Check(moduleName, Equals, "moderr")
		c.Check(moduleOptions, HasLen, 0)
		return ensureConnectionError
	})
	defer r1()

	r2 := ctlcmd.MockKmodUnloadModule(func(name string) error {
		c.Check(name, Equals, "moderr")
		return loadModuleError
	})
	defer r2()

	for _, td := range []struct {
		ensureConnectionError error
		loadModuleError       error
		expectedError         string
	}{
		{
			// error retrieving the snap connections
			ensureConnectionError: errors.New("state error"),
			expectedError:         `cannot unload module "moderr": state error`,
		},
		{
			// error calling modprobe
			loadModuleError: errors.New("modprobe failure"),
			expectedError:   `cannot unload module "moderr": modprobe failure`,
		},
	} {
		ensureConnectionError = td.ensureConnectionError
		loadModuleError = td.loadModuleError
		_, _ := mylog.Check3(ctlcmd.Run(s.mockContext, []string{"kmod", "remove", "moderr"}, 0))
		c.Check(err, ErrorMatches, td.expectedError)
	}
}

func (s *kmodSuite) TestRemoveHappy(c *C) {
	s.injectSnapWithProperPlug(c)

	unloadModuleCalls := 0
	restore := ctlcmd.MockKmodUnloadModule(func(name string) error {
		unloadModuleCalls++
		c.Check(name, Equals, "module2")
		return nil
	})
	defer restore()

	_, _ := mylog.Check3(ctlcmd.Run(s.mockContext,
		[]string{"kmod", "remove", "module2"}, 0))
	c.Check(err, IsNil)
	c.Check(unloadModuleCalls, Equals, 1)
}

func (s *kmodSuite) TestkmodCommandExecute(c *C) {
	// This is a useless test just to make test coverage greener. The Execute()
	// method exercised here is never called in real life.
	cmd := &ctlcmd.KmodCommand{}
	mylog.Check(cmd.Execute([]string{}))
	c.Check(err, IsNil)
}
