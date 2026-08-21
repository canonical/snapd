// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2026 Canonical Ltd
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

package state_test

import (
	"fmt"
	"sync"

	. "gopkg.in/check.v1"
	"gopkg.in/tomb.v2"

	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/testutil"
)

type changeAllowlistSuite struct {
	testutil.BaseTest

	state  *state.State
	runner *state.TaskRunner

	mu     sync.Mutex
	events []string
}

var _ = Suite(&changeAllowlistSuite{})

func (s *changeAllowlistSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)

	s.state = state.New(nil)
	s.runner = state.NewTaskRunner(s.state)
	s.AddCleanup(func() { s.runner.Stop() })

	s.events = nil
	s.addHandlers(s.runner)
}

func (s *changeAllowlistSuite) addHandlers(r *state.TaskRunner) {
	r.AddHandler("probe", s.doProbe, s.undoProbe)
	r.AddHandler("probe-fail", s.doProbeFail, s.undoProbe)
	// Second kind stands in for another manager (interfaces, hooks, …).
	r.AddHandler("other-kind", s.doProbe, s.undoProbe)
}

func (s *changeAllowlistSuite) restrict(c *C, chg *state.Change) {
	c.Assert(s.runner.RestrictToChange(chg), IsNil)
}

func (s *changeAllowlistSuite) doProbe(t *state.Task, _ *tomb.Tomb) error {
	st := t.State()
	st.Lock()
	chgID, taskID := t.Change().ID(), t.ID()
	st.Unlock()
	s.record("do:" + chgID + ":" + taskID)
	return nil
}

func (s *changeAllowlistSuite) undoProbe(t *state.Task, _ *tomb.Tomb) error {
	st := t.State()
	st.Lock()
	chgID, taskID := t.Change().ID(), t.ID()
	st.Unlock()
	s.record("undo:" + chgID + ":" + taskID)
	return nil
}

func (s *changeAllowlistSuite) doProbeFail(t *state.Task, _ *tomb.Tomb) error {
	st := t.State()
	st.Lock()
	chgID, taskID := t.Change().ID(), t.ID()
	st.Unlock()
	s.record("do-fail:" + chgID + ":" + taskID)
	return fmt.Errorf("boom")
}

func (s *changeAllowlistSuite) record(ev string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, ev)
}

func (s *changeAllowlistSuite) recorded() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, len(s.events))
	copy(out, s.events)
	return out
}

func (s *changeAllowlistSuite) ensureTimes(n int) {
	for i := 0; i < n; i++ {
		s.runner.Ensure()
		s.runner.Wait()
	}
}

func (s *changeAllowlistSuite) waitReady(c *C, chg *state.Change) {
	for i := 0; i < 30; i++ {
		s.runner.Ensure()
		s.runner.Wait()
		s.state.Lock()
		ready := chg.Status().Ready()
		s.state.Unlock()
		if ready {
			return
		}
	}
	s.state.Lock()
	defer s.state.Unlock()
	c.Fatalf("change %s did not become ready (status %s)", chg.ID(), chg.Status())
}

func (s *changeAllowlistSuite) newChangeWithTask(kind, summary string) (*state.Change, *state.Task) {
	chg := s.state.NewChange("test", summary)
	t := s.state.NewTask(kind, summary)
	chg.AddTask(t)
	return chg, t
}

func (s *changeAllowlistSuite) TestDefaultDoesNotRestrict(c *C) {
	s.state.Lock()
	chgA, tA := s.newChangeWithTask("probe", "a")
	chgB, tB := s.newChangeWithTask("other-kind", "b")
	s.state.Unlock()

	s.waitReady(c, chgA)
	s.waitReady(c, chgB)

	s.state.Lock()
	defer s.state.Unlock()
	c.Check(tA.Status(), Equals, state.DoneStatus)
	c.Check(tB.Status(), Equals, state.DoneStatus)
}

func (s *changeAllowlistSuite) TestOtherChangeTasksDoNotStart(c *C) {
	s.state.Lock()
	chgA, tA := s.newChangeWithTask("probe", "allowed")
	chgB, tB := s.newChangeWithTask("other-kind", "other manager")
	s.restrict(c, chgA)
	s.state.Unlock()

	s.waitReady(c, chgA)
	s.ensureTimes(5)

	s.state.Lock()
	defer s.state.Unlock()
	c.Check(tA.Status(), Equals, state.DoneStatus)
	c.Check(tB.Status(), Equals, state.DoStatus)
	c.Check(chgB.Status(), Equals, state.DoStatus)
	c.Check(s.recorded(), DeepEquals, []string{"do:" + chgA.ID() + ":" + tA.ID()})
}

func (s *changeAllowlistSuite) TestNewChangeAfterRestrictStaysDo(c *C) {
	s.state.Lock()
	chgA, _ := s.newChangeWithTask("probe", "allowed")
	s.restrict(c, chgA)
	s.state.Unlock()

	s.waitReady(c, chgA)

	s.state.Lock()
	chgB, tB := s.newChangeWithTask("other-kind", "created-later")
	s.state.Unlock()

	s.ensureTimes(5)

	s.state.Lock()
	defer s.state.Unlock()
	c.Check(tB.Status(), Equals, state.DoStatus)
	c.Check(chgB.Status(), Equals, state.DoStatus)
}

func (s *changeAllowlistSuite) TestClearRestrictionBeforeStartLetsBothRun(c *C) {
	s.state.Lock()
	chgA, tA := s.newChangeWithTask("probe", "allowed")
	chgB, tB := s.newChangeWithTask("other-kind", "other")
	s.restrict(c, chgA)
	c.Assert(s.runner.RestrictToChange(nil), IsNil)
	s.state.Unlock()

	s.waitReady(c, chgA)
	s.waitReady(c, chgB)

	s.state.Lock()
	defer s.state.Unlock()
	c.Check(tA.Status(), Equals, state.DoneStatus)
	c.Check(tB.Status(), Equals, state.DoneStatus)
}

func (s *changeAllowlistSuite) TestNewRunnerIsUnrestricted(c *C) {
	s.state.Lock()
	chgA, tA := s.newChangeWithTask("probe", "allowed")
	chgB, tB := s.newChangeWithTask("other-kind", "other")
	s.restrict(c, chgA)
	s.state.Unlock()

	s.waitReady(c, chgA)
	s.ensureTimes(3)

	s.state.Lock()
	c.Check(tB.Status(), Equals, state.DoStatus)
	s.state.Unlock()

	s.runner.Stop()
	s.runner = state.NewTaskRunner(s.state)
	s.AddCleanup(func() { s.runner.Stop() })
	s.addHandlers(s.runner)

	s.waitReady(c, chgB)

	s.state.Lock()
	defer s.state.Unlock()
	c.Check(tA.Status(), Equals, state.DoneStatus)
	c.Check(tB.Status(), Equals, state.DoneStatus)
}

func (s *changeAllowlistSuite) TestRestrictToChangeAfterStartFails(c *C) {
	s.runner.Ensure()

	s.state.Lock()
	defer s.state.Unlock()
	chgA, _ := s.newChangeWithTask("probe", "too late")
	err := s.runner.RestrictToChange(chgA)
	c.Check(err, ErrorMatches, `internal error: cannot restrict task runner after it has started`)
	err = s.runner.RestrictToChange(nil)
	c.Check(err, ErrorMatches, `internal error: cannot restrict task runner after it has started`)
}

func (s *changeAllowlistSuite) TestAllowlistedUndoStillRuns(c *C) {
	s.state.Lock()
	chgA := s.state.NewChange("test", "allowed")
	tDo := s.state.NewTask("probe", "will undo")
	tFail := s.state.NewTask("probe-fail", "fail")
	tFail.WaitFor(tDo)
	chgA.AddTask(tDo)
	chgA.AddTask(tFail)

	chgB, tB := s.newChangeWithTask("other-kind", "other")
	s.restrict(c, chgA)
	s.state.Unlock()

	s.waitReady(c, chgA)
	s.ensureTimes(5)

	s.state.Lock()
	defer s.state.Unlock()
	c.Check(tDo.Status(), Equals, state.UndoneStatus)
	c.Check(tFail.Status(), Equals, state.ErrorStatus)
	c.Check(tB.Status(), Equals, state.DoStatus)
	c.Check(chgB.Status(), Equals, state.DoStatus)
	c.Check(s.recorded(), DeepEquals, []string{
		"do:" + chgA.ID() + ":" + tDo.ID(),
		"do-fail:" + chgA.ID() + ":" + tFail.ID(),
		"undo:" + chgA.ID() + ":" + tDo.ID(),
	})
}

func (s *changeAllowlistSuite) TestOtherChangeUndoIsBlocked(c *C) {
	s.state.Lock()
	chgOther := s.state.NewChange("test", "other")
	t1 := s.state.NewTask("other-kind", "done then undo")
	t2 := s.state.NewTask("other-kind", "never starts")
	t2.WaitFor(t1)
	chgOther.AddTask(t1)
	chgOther.AddTask(t2)
	// Previous process left t1 Done; this process has not Ensure'd yet.
	t1.SetStatus(state.DoneStatus)

	chgA, tA := s.newChangeWithTask("probe", "allowed")
	s.restrict(c, chgA)
	chgOther.Abort()
	c.Assert(t1.Status(), Equals, state.UndoStatus)
	c.Assert(t2.Status(), Equals, state.HoldStatus)
	s.state.Unlock()

	s.waitReady(c, chgA)
	s.ensureTimes(5)

	s.state.Lock()
	c.Check(t1.Status(), Equals, state.UndoStatus)
	s.state.Unlock()

	s.runner.Stop()
	s.runner = state.NewTaskRunner(s.state)
	s.AddCleanup(func() { s.runner.Stop() })
	s.addHandlers(s.runner)

	s.waitReady(c, chgOther)

	s.state.Lock()
	defer s.state.Unlock()
	c.Check(t1.Status(), Equals, state.UndoneStatus)
	c.Check(t2.Status(), Equals, state.HoldStatus)
	c.Check(s.recorded(), DeepEquals, []string{
		"do:" + chgA.ID() + ":" + tA.ID(),
		"undo:" + chgOther.ID() + ":" + t1.ID(),
	})
}

func (s *changeAllowlistSuite) TestIntraChangeWaitsStillApply(c *C) {
	s.state.Lock()
	chgA := s.state.NewChange("test", "allowed")
	t1 := s.state.NewTask("probe", "first")
	t2 := s.state.NewTask("other-kind", "second, other manager")
	t2.WaitFor(t1)
	chgA.AddTask(t1)
	chgA.AddTask(t2)

	_, tB := s.newChangeWithTask("other-kind", "other change")
	s.restrict(c, chgA)
	s.state.Unlock()

	s.waitReady(c, chgA)
	s.ensureTimes(5)

	s.state.Lock()
	defer s.state.Unlock()
	c.Check(t1.Status(), Equals, state.DoneStatus)
	c.Check(t2.Status(), Equals, state.DoneStatus)
	c.Check(tB.Status(), Equals, state.DoStatus)
	c.Check(s.recorded(), DeepEquals, []string{
		"do:" + chgA.ID() + ":" + t1.ID(),
		"do:" + chgA.ID() + ":" + t2.ID(),
	})
}

func (s *changeAllowlistSuite) TestOrphanTaskIsBlocked(c *C) {
	s.state.Lock()
	chgA, _ := s.newChangeWithTask("probe", "allowed")
	orphan := s.state.NewTask("other-kind", "no change")
	s.restrict(c, chgA)
	s.state.Unlock()

	s.waitReady(c, chgA)
	s.ensureTimes(5)

	s.state.Lock()
	defer s.state.Unlock()
	c.Check(orphan.Status(), Equals, state.DoStatus)
	c.Check(orphan.Change(), IsNil)
}

func (s *changeAllowlistSuite) TestIndependentTasksInAllowedChangeBothRun(c *C) {
	s.state.Lock()
	chgA := s.state.NewChange("test", "allowed")
	t1 := s.state.NewTask("probe", "one")
	t2 := s.state.NewTask("other-kind", "two")
	chgA.AddTask(t1)
	chgA.AddTask(t2)
	_, tB := s.newChangeWithTask("other-kind", "other")
	s.restrict(c, chgA)
	s.state.Unlock()

	s.waitReady(c, chgA)
	s.ensureTimes(5)

	s.state.Lock()
	defer s.state.Unlock()
	c.Check(t1.Status(), Equals, state.DoneStatus)
	c.Check(t2.Status(), Equals, state.DoneStatus)
	c.Check(tB.Status(), Equals, state.DoStatus)

	got := s.recorded()
	c.Check(got, HasLen, 2)
	c.Check(got, testutil.Contains, "do:"+chgA.ID()+":"+t1.ID())
	c.Check(got, testutil.Contains, "do:"+chgA.ID()+":"+t2.ID())
}

func (s *changeAllowlistSuite) TestSetBlockedDoesNotClearRestriction(c *C) {
	s.state.Lock()
	chgA, tA := s.newChangeWithTask("probe", "allowed")
	chgB, tB := s.newChangeWithTask("other-kind", "other")
	s.restrict(c, chgA)
	s.state.Unlock()

	s.runner.SetBlocked(func(t *state.Task, running []*state.Task) bool {
		return false
	})

	s.waitReady(c, chgA)
	s.ensureTimes(5)

	s.state.Lock()
	defer s.state.Unlock()
	c.Check(tA.Status(), Equals, state.DoneStatus)
	c.Check(tB.Status(), Equals, state.DoStatus)
	c.Check(chgB.Status(), Equals, state.DoStatus)
}
