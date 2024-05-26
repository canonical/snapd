// -*- Mode: Go; indent-tabs-mode: t -*-
/*
 * Copyright (C) 2018-2021 Canonical Ltd
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

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/overlord/restart"
	"github.com/snapcore/snapd/overlord/snapstate/snapstatetest"
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

type opine func() bool

func (f opine) CanStandby() bool {
	return f()
}

func (s *standbySuite) TestStartChecks(c *C) {
	n := 0
	// opinions
	ch1 := make(chan bool, 1)
	// sync with request restart
	ch2 := make(chan struct{})

	defer standby.MockStandbyWait(time.Millisecond)()
	s.state.Lock()
	_ := mylog.Check2(restart.Manager(s.state, "boot-id-0", snapstatetest.MockRestartHandler(func(t restart.RestartType) {
		c.Check(t, Equals, restart.RestartSocket)
		n++
		ch2 <- struct{}{}
	})))
	s.state.Unlock()


	m := standby.New(s.state)
	m.AddOpinion(opine(func() bool {
		opinion := <-ch1
		return opinion
	}))

	m.Start()
	ch1 <- false
	c.Check(n, Equals, 0)
	ch1 <- false
	c.Check(n, Equals, 0)

	ch1 <- true
	<-ch2
	c.Check(n, Equals, 1)
	// no more opinions
	close(ch1)

	m.Stop()
	close(ch2)
}

func (s *standbySuite) TestStopWaits(c *C) {
	defer standby.MockStandbyWait(time.Millisecond)()
	s.state.Lock()
	_ := mylog.Check2(restart.Manager(s.state, "boot-id-0", snapstatetest.MockRestartHandler(func(t restart.RestartType) {
		c.Fatal("request restart should have not been called")
	})))
	s.state.Unlock()


	ch := make(chan struct{})
	opineReady := make(chan struct{})
	done := make(chan struct{})
	m := standby.New(s.state)
	synced := false
	m.AddOpinion(opine(func() bool {
		if !synced {
			// synchronize with the main goroutine only at the
			// beginning
			close(opineReady)
			synced = true
		}
		select {
		case <-time.After(200 * time.Millisecond):
		case <-done:
		}
		return false
	}))

	m.Start()

	// let the opinionator start its delay
	<-opineReady
	go func() {
		// this will block until standby stops
		m.Stop()
		close(ch)
	}()

	select {
	case <-time.After(100 * time.Millisecond):
		// wheee
	case <-ch:
		c.Fatal("stop should have blocked and didn't")
	}

	close(done)

	// wait for Stop to complete now
	select {
	case <-ch:
		// nothing to do here
	case <-time.After(10 * time.Second):
		c.Fatal("stop did not complete")
	}
}
