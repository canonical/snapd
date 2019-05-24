// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019 Canonical Ltd
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

package devicestate_test

import (
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/overlord/assertstate"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/devicestate/devicestatetest"
)

// TODO: should we move this into a new handlers suite?
func (s *deviceMgrSuite) TestSetModelHandlerNewRevision(c *C) {
	s.state.Lock()
	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand: "canonical",
		Model: "pc-model",
	})
	s.makeModelAssertionInState(c, "canonical", "pc-model", map[string]interface{}{
		"architecture": "amd64",
		"kernel":       "pc-kernel",
		"gadget":       "pc",
		"revision":     "1",
	})
	s.state.Unlock()

	newModel := s.brands.Model("canonical", "pc-model", map[string]interface{}{
		"architecture": "amd64",
		"kernel":       "pc-kernel",
		"gadget":       "pc",
		"revision":     "2",
	})

	s.state.Lock()
	t := s.state.NewTask("set-model", "set-model test")
	chg := s.state.NewChange("dummy", "...")
	chg.AddTask(t)
	chg.Set("new-model", string(asserts.Encode(newModel)))

	s.state.Unlock()

	s.se.Ensure()
	s.se.Wait()

	s.state.Lock()
	defer s.state.Unlock()
	m, err := s.mgr.Model()
	c.Assert(err, IsNil)
	c.Assert(m, DeepEquals, newModel)

	c.Assert(chg.Err(), IsNil)
}

func (s *deviceMgrSuite) TestSetModelHandlerSameRevisionNoError(c *C) {
	model := s.brands.Model("canonical", "pc-model", map[string]interface{}{
		"architecture": "amd64",
		"kernel":       "pc-kernel",
		"gadget":       "pc",
		"revision":     "1",
	})

	s.state.Lock()

	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand: "canonical",
		Model: "pc-model",
	})
	err := assertstate.Add(s.state, model)
	c.Assert(err, IsNil)

	t := s.state.NewTask("set-model", "set-model test")
	chg := s.state.NewChange("dummy", "...")
	chg.AddTask(t)
	chg.Set("new-model", string(asserts.Encode(model)))

	s.state.Unlock()

	s.se.Ensure()
	s.se.Wait()

	s.state.Lock()
	defer s.state.Unlock()
	c.Assert(chg.Err(), IsNil)
}
