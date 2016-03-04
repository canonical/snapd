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

package overlord_test

import (
	"errors"

	. "gopkg.in/check.v1"

	"github.com/ubuntu-core/snappy/overlord"
	"github.com/ubuntu-core/snappy/overlord/state"
)

type stateEngineSuite struct{}

var _ = Suite(&stateEngineSuite{})

func (ses *stateEngineSuite) TestNewAndState(c *C) {
	s := state.New(nil)
	se := overlord.NewStateEngine(s)

	c.Check(se.State(), Equals, s)
}

type fakeManager struct {
	name                              string
	state                             *state.State
	calls                             *[]string
	initError, ensureError, stopError error
}

func (fm *fakeManager) Init(s *state.State) error {
	fm.state = s
	*fm.calls = append(*fm.calls, "init:"+fm.name)
	return fm.initError
}

func (fm *fakeManager) Ensure() error {
	*fm.calls = append(*fm.calls, "ensure:"+fm.name)
	return fm.ensureError
}

func (fm *fakeManager) Stop() error {
	*fm.calls = append(*fm.calls, "stop:"+fm.name)
	return fm.stopError
}

var _ overlord.StateManager = (*fakeManager)(nil)

func (ses *stateEngineSuite) TestEnsure(c *C) {
	s := state.New(nil)
	se := overlord.NewStateEngine(s)

	calls := []string{}

	mgr1 := &fakeManager{name: "mgr1", calls: &calls}
	mgr2 := &fakeManager{name: "mgr2", calls: &calls}

	se.AddManager(mgr1)
	se.AddManager(mgr2)

	err := se.Ensure()
	c.Assert(err, IsNil)
	c.Check(calls, DeepEquals, []string{"init:mgr1", "init:mgr2", "ensure:mgr1", "ensure:mgr2"})
	c.Check(mgr1.state, Equals, s)
	c.Check(mgr2.state, Equals, s)

	err = se.Ensure()
	c.Assert(err, IsNil)
	c.Check(calls, DeepEquals, []string{"init:mgr1", "init:mgr2", "ensure:mgr1", "ensure:mgr2", "ensure:mgr1", "ensure:mgr2"})
}

func (ses *stateEngineSuite) TestEnsureInitError(c *C) {
	s := state.New(nil)
	se := overlord.NewStateEngine(s)

	calls := []string{}

	err1 := errors.New("boom1")
	err2 := errors.New("boom2")

	mgr1 := &fakeManager{name: "mgr1", calls: &calls, initError: err1}
	mgr2 := &fakeManager{name: "mgr2", calls: &calls, initError: err2}

	se.AddManager(mgr1)
	se.AddManager(mgr2)

	err := se.Ensure()
	c.Check(err, Equals, err1)
	c.Check(calls, DeepEquals, []string{"init:mgr1"})
}

func (ses *stateEngineSuite) TestEnsureError(c *C) {
	s := state.New(nil)
	se := overlord.NewStateEngine(s)

	calls := []string{}

	err1 := errors.New("boom1")
	err2 := errors.New("boom2")

	mgr1 := &fakeManager{name: "mgr1", calls: &calls, ensureError: err1}
	mgr2 := &fakeManager{name: "mgr2", calls: &calls, ensureError: err2}

	se.AddManager(mgr1)
	se.AddManager(mgr2)

	err := se.Ensure()
	c.Check(err, Equals, err1)
	c.Check(calls, DeepEquals, []string{"init:mgr1", "init:mgr2", "ensure:mgr1"})
}

func (ses *stateEngineSuite) TestStop(c *C) {
	s := state.New(nil)
	se := overlord.NewStateEngine(s)

	calls := []string{}

	mgr1 := &fakeManager{name: "mgr1", calls: &calls}
	mgr2 := &fakeManager{name: "mgr2", calls: &calls}

	se.AddManager(mgr1)
	se.AddManager(mgr2)

	err := se.Ensure()
	c.Assert(err, IsNil)
	c.Check(calls, HasLen, 4)

	err = se.Stop()
	c.Assert(err, IsNil)
	c.Check(calls, DeepEquals, []string{"init:mgr1", "init:mgr2", "ensure:mgr1", "ensure:mgr2", "stop:mgr1", "stop:mgr2"})

	err = se.Ensure()
	c.Assert(err, IsNil)
	c.Check(calls, DeepEquals, []string{"init:mgr1", "init:mgr2", "ensure:mgr1", "ensure:mgr2", "stop:mgr1", "stop:mgr2", "init:mgr1", "init:mgr2", "ensure:mgr1", "ensure:mgr2"})
}

func (ses *stateEngineSuite) TestStopError(c *C) {
	s := state.New(nil)
	se := overlord.NewStateEngine(s)

	calls := []string{}

	err1 := errors.New("boom1")
	err2 := errors.New("boom2")

	mgr1 := &fakeManager{name: "mgr1", calls: &calls, stopError: err1}
	mgr2 := &fakeManager{name: "mgr2", calls: &calls, stopError: err2}

	se.AddManager(mgr1)
	se.AddManager(mgr2)

	err := se.Ensure()
	c.Assert(err, IsNil)
	c.Check(calls, HasLen, 4)

	err = se.Stop()
	c.Check(err, Equals, err1)
	c.Check(calls, DeepEquals, []string{"init:mgr1", "init:mgr2", "ensure:mgr1", "ensure:mgr2", "stop:mgr1"})

}
