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

package hookstate

import (
	"regexp"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/overlord/hookstate/hooktest"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
)

type repositorySuite struct{}

var _ = Suite(&repositorySuite{})

func (s *repositorySuite) TestAddHandlerGenerator(c *C) {
	repository := NewRepository()

	var calledContext *Context
	mockHandlerGenerator := func(context *Context) Handler {
		calledContext = context
		return hooktest.NewMockHandler()
	}

	// Verify that a handler generator can be added to the repository
	repository.AddHandlerGenerator(regexp.MustCompile("test-hook"), mockHandlerGenerator)

	state := state.New(nil)
	state.Lock()
	task := state.NewTask("test-task", "my test task")
	setup := &HookSetup{Snap: "test-snap", Revision: snap.R(1), Hook: "test-hook"}
	context := &Context{task: task, setup: setup}
	state.Unlock()

	c.Assert(context, NotNil)

	// Verify that the handler can be generated
	handlers := repository.GenerateHandlers(context)
	c.Check(handlers, HasLen, 1)
	c.Check(calledContext, DeepEquals, context)

	// Add another handler
	repository.AddHandlerGenerator(regexp.MustCompile(".*-hook"), mockHandlerGenerator)

	// Verify that two handlers are generated for the test-hook, now
	handlers = repository.GenerateHandlers(context)
	c.Check(handlers, HasLen, 2)
	c.Check(calledContext, DeepEquals, context)
}
