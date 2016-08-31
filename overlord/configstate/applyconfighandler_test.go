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
	"fmt"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/overlord/configstate"
	"github.com/snapcore/snapd/overlord/hookstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
)

type applyConfigHandlerSuite struct {
	state   *state.State
	manager *configstate.ConfigManager
	handler hookstate.Handler
}

var _ = Suite(&applyConfigHandlerSuite{})

func (s *applyConfigHandlerSuite) SetUpTest(c *C) {
	s.state = state.New(nil)

	hookManager, err := hookstate.Manager(s.state)
	c.Assert(err, IsNil)
	s.manager, err = configstate.Manager(s.state, hookManager)
	c.Assert(err, IsNil)

	s.state.Lock()
	task := s.state.NewTask("test-task", "my test task")
	s.state.Unlock()

	setup := &hookstate.HookSetup{Snap: "test-snap", Revision: snap.R(1), Hook: "test-hook"}
	context, err := hookstate.NewContext(task, setup, nil)
	c.Assert(err, IsNil)

	s.handler = configstate.NewApplyConfigHandler(context, s.manager.NewTransaction())
}

func (s *applyConfigHandlerSuite) TestSetConfDoesntSave(c *C) {
	s.handler.SetConf("test-key", "test-value")

	// Verify that SetConf didn't touch the state
	transaction := s.manager.NewTransaction()
	var value string
	c.Check(transaction.Get("test-snap", "test-key", &value), ErrorMatches, ".*no config available.*")
	c.Check(value, Equals, "")
}

func (s *applyConfigHandlerSuite) TestDoneSavesConfChanges(c *C) {
	s.handler.SetConf("test-key", "test-value")
	c.Check(s.handler.Done(), IsNil)

	// Verify that the conf changes have now been saved.
	transaction := s.manager.NewTransaction()
	var value string
	c.Check(transaction.Get("test-snap", "test-key", &value), IsNil)
	c.Check(value, Equals, "test-value")
}

func (s *applyConfigHandlerSuite) TestDoneSavesConfChangesDeltasOnly(c *C) {
	// Create an initial configuration
	s.handler.SetConf("test-key1", "test-value1")
	s.handler.SetConf("test-key2", "test-value2")
	c.Check(s.handler.Done(), IsNil)

	// Now update the configuration
	s.handler.SetConf("test-key2", "test-value3")
	c.Check(s.handler.Done(), IsNil)

	// Verify that only test-key2 was updated
	transaction := s.manager.NewTransaction()
	var value string
	c.Check(transaction.Get("test-snap", "test-key1", &value), IsNil)
	c.Check(value, Equals, "test-value1")
	c.Check(transaction.Get("test-snap", "test-key2", &value), IsNil)
	c.Check(value, Equals, "test-value3")
}

func (s *applyConfigHandlerSuite) TestErrorDoesntSave(c *C) {
	s.handler.SetConf("test-key", "test-value")
	c.Check(s.handler.Error(fmt.Errorf("failed on purpose")), IsNil)

	// Verify that Error didn't touch the state
	transaction := s.manager.NewTransaction()
	var value string
	c.Check(transaction.Get("test-snap", "test-key", &value), ErrorMatches, ".*no config available.*")
	c.Check(value, Equals, "")
}
