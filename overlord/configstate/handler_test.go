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
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/overlord/configstate"
	"github.com/snapcore/snapd/overlord/hookstate"
	"github.com/snapcore/snapd/overlord/hookstate/hooktest"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
)

func TestConfigState(t *testing.T) { TestingT(t) }

type configureHandlerSuite struct {
	context *hookstate.Context
	handler hookstate.Handler
}

var _ = Suite(&configureHandlerSuite{})

func (s *configureHandlerSuite) SetUpTest(c *C) {
	state := state.New(nil)
	state.Lock()
	defer state.Unlock()

	task := state.NewTask("test-task", "my test task")
	setup := &hookstate.HookSetup{Snap: "test-snap", Revision: snap.R(1), Hook: "test-hook"}

	var err error
	s.context, err = hookstate.NewContext(task, setup, hooktest.NewMockHandler())
	c.Assert(err, IsNil)

	s.handler = configstate.NewConfigureHandler(s.context)
}

func (s *configureHandlerSuite) TestBeforeInitializesTransaction(c *C) {
	// Initialize context
	s.context.Lock()
	s.context.Set("patch", map[string]interface{}{
		"foo": "bar",
	})
	s.context.Unlock()

	c.Check(s.handler.Before(), IsNil)

	s.context.Lock()
	tr := configstate.ContextTransaction(s.context)
	s.context.Unlock()

	var value string
	c.Check(tr.Get("test-snap", "foo", &value), IsNil)
	c.Check(value, Equals, "bar")
}
