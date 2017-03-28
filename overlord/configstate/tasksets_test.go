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

package configstate_test

import (
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/overlord/configstate"
	"github.com/snapcore/snapd/overlord/hookstate"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
)

type tasksetsSuite struct {
	state *state.State
}

var _ = Suite(&tasksetsSuite{})

func (s *tasksetsSuite) SetUpTest(c *C) {
	s.state = state.New(nil)
}

var configureTests = []struct {
	patch      map[string]interface{}
	optional   bool
	ignoreFail bool
}{{
	patch:      nil,
	optional:   true,
	ignoreFail: false,
}, {
	patch:      map[string]interface{}{},
	optional:   true,
	ignoreFail: false,
}, {
	patch:      map[string]interface{}{"foo": "bar"},
	optional:   false,
	ignoreFail: false,
}, {
	patch:      nil,
	optional:   true,
	ignoreFail: true,
}}

func (s *tasksetsSuite) TestConfigure(c *C) {
	for _, test := range configureTests {
		var flags int
		if test.ignoreFail {
			flags |= snapstate.IgnoreHookFailure
		}

		s.state.Lock()
		taskset := configstate.Configure(s.state, "test-snap", test.patch, flags)
		s.state.Unlock()

		tasks := taskset.Tasks()
		c.Assert(tasks, HasLen, 1)
		task := tasks[0]

		c.Assert(task.Kind(), Equals, "run-hook")

		summary := `Run configure hook of "test-snap" snap`
		if test.optional {
			summary += " if present"
		}
		c.Assert(task.Summary(), Equals, summary)

		var hooksup hookstate.HookSetup
		s.state.Lock()
		err := task.Get("hook-setup", &hooksup)
		s.state.Unlock()
		c.Check(err, IsNil)

		c.Assert(hooksup.Snap, Equals, "test-snap")
		c.Assert(hooksup.Hook, Equals, "configure")
		c.Assert(hooksup.Optional, Equals, test.optional)
		c.Assert(hooksup.IgnoreFail, Equals, test.ignoreFail)

		context, err := hookstate.NewContext(task, &hooksup, nil)
		c.Check(err, IsNil)
		c.Check(context.SnapName(), Equals, "test-snap")
		c.Check(context.SnapRevision(), Equals, snap.Revision{})
		c.Check(context.HookName(), Equals, "configure")

		var patch map[string]interface{}
		context.Lock()
		err = context.Get("patch", &patch)
		context.Unlock()
		if len(test.patch) > 0 {
			c.Check(err, IsNil)
			c.Check(patch, DeepEquals, test.patch)
		} else {
			c.Check(err, Equals, state.ErrNoState)
			c.Check(patch, IsNil)
		}
	}
}
