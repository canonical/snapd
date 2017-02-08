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

	"strings"

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
	tr := config.NewTransaction(state)
	tr.Set("test-snap", "initial-key", "initial-value")
	tr.Commit()
}

var getTests = []struct {
	args, stdout, error string
}{
	{
		args:  "get",
		error: ".*get which option.*",
	},
	{
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
