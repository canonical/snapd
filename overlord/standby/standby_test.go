// -*- Mode: Go; indent-tabs-mode: t -*-
/*
 * Copyright (C) 2018 Canonical Ltd
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
package standby_test

import (
	"testing"
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/overlord/standby"
	"github.com/snapcore/snapd/overlord/state"
)

// Hook up v1 into the "go test" runner
func Test(t *testing.T) { TestingT(t) }

type standbySuite struct {
	state *state.State

	canStandby bool
}

var _ = Suite(&standbySuite{})

func (s *standbySuite) SetUpTest(c *C) {
	s.state = state.New(nil)
}

func (s *standbySuite) TestCanStandbyNoChanges(c *C) {
	m := standby.New(s.state)
	c.Check(m.CanStandby(), Equals, false)

	m.SetStartTime(time.Time{})
	c.Check(m.CanStandby(), Equals, true)
}

func (s *standbySuite) TestCanStandbyPendingChanges(c *C) {
	st := s.state
	st.Lock()
	chg := st.NewChange("foo", "fake change")
	chg.AddTask(st.NewTask("bar", "fake task"))
	c.Assert(chg.Status(), Equals, state.DoStatus)
	st.Unlock()

	m := standby.New(s.state)
	m.SetStartTime(time.Time{})
	c.Check(m.CanStandby(), Equals, false)
}

func (s *standbySuite) TestCanStandbyPendingClean(c *C) {
	st := s.state
	st.Lock()
	t := st.NewTask("bar", "fake task")
	chg := st.NewChange("foo", "fake change")
	chg.AddTask(t)
	t.SetStatus(state.DoneStatus)
	c.Assert(chg.Status(), Equals, state.DoneStatus)
	c.Assert(t.IsClean(), Equals, false)
	st.Unlock()

	m := standby.New(s.state)
	m.SetStartTime(time.Time{})
	c.Check(m.CanStandby(), Equals, false)
}

func (s *standbySuite) TestCanStandbyOnlyDonePendingChanges(c *C) {
	st := s.state
	st.Lock()
	t := st.NewTask("bar", "fake task")
	chg := st.NewChange("foo", "fake change")
	chg.AddTask(t)
	t.SetStatus(state.DoneStatus)
	t.SetClean()
	c.Assert(chg.Status(), Equals, state.DoneStatus)
	c.Assert(t.IsClean(), Equals, true)
	st.Unlock()

	m := standby.New(s.state)
	m.SetStartTime(time.Time{})
	c.Check(m.CanStandby(), Equals, true)
}

func (s *standbySuite) CanStandby() bool {
	return s.canStandby
}

func (s *standbySuite) TestCanStandbyWithOpinion(c *C) {
	m := standby.New(s.state)
	m.AddOpinion(s)
	m.SetStartTime(time.Time{})

	s.canStandby = true
	c.Check(m.CanStandby(), Equals, true)

	s.canStandby = false
	c.Check(m.CanStandby(), Equals, false)
}
