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

package hookstate_test

import (
	"regexp"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/overlord/hookstate"
	"github.com/snapcore/snapd/overlord/state"
)

func TestRepository(t *testing.T) { TestingT(t) }

type repositorySuite struct{}

var _ = Suite(&repositorySuite{})

func (s *repositorySuite) TestAddHandlerGenerator(c *C) {
	repository := hookstate.NewRepository()

	var calledContext *hookstate.Context
	mockHandlerGenerator := func(context *hookstate.Context) hookstate.Handler {
		calledContext = context
		return newMockHandler()
	}

	// Verify that a handler generator can be added to the repository
	repository.AddHandlerGenerator(regexp.MustCompile("test-hook"), mockHandlerGenerator)

	state := state.New(nil)
	state.Lock()
	task := state.NewTask("test-task", "my test task")
	hookSetup := hookstate.HookSetup{Hook: "test-hook", Snap: "test-snap"}
	context := hookstate.NewContext(task, hookSetup)
	state.Unlock()

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
