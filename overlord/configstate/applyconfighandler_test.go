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
	"github.com/snapcore/snapd/overlord/hookstate/hooktest"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
)

type applyConfigHandlerSuite struct {
	context *hookstate.Context
	handler hookstate.Handler
}

var _ = Suite(&applyConfigHandlerSuite{})

func (s *applyConfigHandlerSuite) SetUpTest(c *C) {
	state := state.New(nil)
	state.Lock()
	defer state.Unlock()

	task := state.NewTask("test-task", "my test task")
	setup := &hookstate.HookSetup{Snap: "test-snap", Revision: snap.R(1), Hook: "test-hook"}

	var err error
	s.context, err = hookstate.NewContext(task, setup, hooktest.NewMockHandler())
	c.Assert(err, IsNil)

	s.handler = configstate.NewApplyConfigHandler(s.context)
}

func (s *applyConfigHandlerSuite) TestBeforeInitializesTransaction(c *C) {
	// Initialize context
	s.context.Lock()
	s.context.Set("patch", map[string]interface{}{
		"foo": "bar",
	})
	s.context.Unlock()

	c.Check(s.handler.Before(), IsNil)

	s.context.Lock()
	transaction := configstate.ContextTransaction(s.context)
	s.context.Unlock()

	var value string
	c.Check(transaction.Get("test-snap", "foo", &value), IsNil)
	c.Check(value, Equals, "bar")
}
