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

package tasktest_test

import (
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/overlord/snapstate/tasktest"
	"github.com/snapcore/snapd/overlord/state"
)

type assertionsSuite struct{}

var _ = Suite(&assertionsSuite{})

func (s *assertionsSuite) TestAssertSequenced(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	root := st.NewTask("root", "...")
	left := st.NewTask("middle", "...")
	right := st.NewTask("middle", "...")
	tail := st.NewTask("tail", "...")
	left.WaitFor(root)
	right.WaitFor(root)
	tail.WaitFor(left)
	tail.WaitFor(right)
	selection := tasktest.NewSelection([]*state.Task{root, left, right, tail})

	c.Check(tasktest.AssertOrdered(
		selection.Select(tasktest.Kind("root")),
		selection.Select(tasktest.Kind("middle").All()),
		selection.Select(tasktest.Kind("tail")),
	), IsNil)
}

func (s *assertionsSuite) TestAssertSequencedMissingDependency(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	first := st.NewTask("first", "...")
	second := st.NewTask("second", "...")
	third := st.NewTask("third", "...")
	fourth := st.NewTask("fourth", "...")
	third.WaitFor(first)
	third.WaitFor(second)
	fourth.WaitFor(first)
	before := tasktest.NewSelection([]*state.Task{first, second})
	after := tasktest.NewSelection([]*state.Task{third, fourth})

	c.Check(tasktest.AssertOrdered(before, after), ErrorMatches, `task 2 \(second\) is not sequenced before task 4 \(fourth\)`)
}

func (s *assertionsSuite) TestAssertNotSequenced(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	first := st.NewTask("first", "...")
	second := st.NewTask("second", "...")
	unrelated := st.NewTask("unrelated", "...")
	second.WaitFor(first)
	later := tasktest.NewSelection([]*state.Task{second})
	others := tasktest.NewSelection([]*state.Task{first, unrelated})

	c.Check(tasktest.AssertNotOrdered(later, others), IsNil)
}

func (s *assertionsSuite) TestAssertNotSequencedHasDependency(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	first := st.NewTask("first", "...")
	second := st.NewTask("second", "...")
	third := st.NewTask("third", "...")
	fourth := st.NewTask("fourth", "...")
	middle := st.NewTask("middle", "...")
	middle.WaitFor(second)
	fourth.WaitFor(middle)
	before := tasktest.NewSelection([]*state.Task{first, second})
	after := tasktest.NewSelection([]*state.Task{third, fourth})

	c.Check(tasktest.AssertNotOrdered(before, after), ErrorMatches, `task 2 \(second\) is sequenced before task 4 \(fourth\)`)
}

func (s *assertionsSuite) TestAssertLaneSuperset(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	first := st.NewTask("first", "...")
	first.JoinLane(1)
	first.JoinLane(2)
	second := st.NewTask("second", "...")
	second.JoinLane(2)
	second.JoinLane(1)
	second.JoinLane(1)
	third := st.NewTask("third", "...")
	third.JoinLane(1)
	fourth := st.NewTask("fourth", "...")
	fourth.JoinLane(2)
	fourth.JoinLane(1)
	superset := tasktest.NewSelection([]*state.Task{first, second})
	subset := tasktest.NewSelection([]*state.Task{third, fourth})

	c.Check(tasktest.AssertLaneSuperset(superset, subset), IsNil)
}

func (s *assertionsSuite) TestAssertLaneSupersetMissingLane(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	first := st.NewTask("first", "...")
	first.JoinLane(1)
	first.JoinLane(2)
	second := st.NewTask("second", "...")
	second.JoinLane(1)
	third := st.NewTask("third", "...")
	third.JoinLane(1)
	fourth := st.NewTask("fourth", "...")
	fourth.JoinLane(1)
	fourth.JoinLane(2)
	superset := tasktest.NewSelection([]*state.Task{first, second})
	subset := tasktest.NewSelection([]*state.Task{third, fourth})

	c.Check(tasktest.AssertLaneSuperset(superset, subset), ErrorMatches, `task 2 \(second\) with lanes \[1\] is not a lane superset of task 4 \(fourth\) with lanes \[1 2\]`)
}

func (s *assertionsSuite) TestAssertDoesNotShareLane(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	first := st.NewTask("first", "...")
	first.JoinLane(1)
	first.JoinLane(2)
	second := st.NewTask("second", "...")
	second.JoinLane(2)
	third := st.NewTask("third", "...")
	third.JoinLane(3)
	fourth := st.NewTask("fourth", "...")
	fourth.JoinLane(3)
	fourth.JoinLane(4)
	left := tasktest.NewSelection([]*state.Task{first, second})
	right := tasktest.NewSelection([]*state.Task{third, fourth})

	c.Check(tasktest.AssertDoesNotShareLane(left, right), IsNil)
}

func (s *assertionsSuite) TestAssertDoesNotShareLaneSharedLane(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	first := st.NewTask("first", "...")
	first.JoinLane(1)
	second := st.NewTask("second", "...")
	second.JoinLane(2)
	third := st.NewTask("third", "...")
	third.JoinLane(3)
	fourth := st.NewTask("fourth", "...")
	fourth.JoinLane(4)
	fourth.JoinLane(2)
	left := tasktest.NewSelection([]*state.Task{first, second})
	right := tasktest.NewSelection([]*state.Task{third, fourth})

	c.Check(tasktest.AssertDoesNotShareLane(left, right), ErrorMatches, `task 2 \(second\) and task 4 \(fourth\) share lane 2`)
}

func (s *assertionsSuite) TestAssertSameLanes(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	first := st.NewTask("first", "...")
	first.JoinLane(1)
	first.JoinLane(2)
	second := st.NewTask("second", "...")
	second.JoinLane(2)
	second.JoinLane(1)
	second.JoinLane(1)
	third := st.NewTask("third", "...")
	third.JoinLane(1)
	third.JoinLane(2)
	selection := tasktest.NewSelection([]*state.Task{first, second, third})
	begin := selection.Select(tasktest.Kind("first"))

	c.Check(tasktest.AssertSameLanes(begin, selection.Select(tasktest.Kind("second")), selection.Select(tasktest.Kind("third"))), IsNil)
}

func (s *assertionsSuite) TestAssertSameLanesDifferentSize(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	first := st.NewTask("first", "...")
	first.JoinLane(1)
	second := st.NewTask("second", "...")
	second.JoinLane(1)
	second.JoinLane(2)
	selection := tasktest.NewSelection([]*state.Task{first, second})

	c.Check(tasktest.AssertSameLanes(selection), ErrorMatches, `task 2 \(second\) has lanes \[1 2\], expected the same lanes as task 1 \(first\) with lanes \[1\]`)
}

func (s *assertionsSuite) TestAssertSameLanesDifferentMembership(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	first := st.NewTask("first", "...")
	first.JoinLane(1)
	first.JoinLane(2)
	second := st.NewTask("second", "...")
	second.JoinLane(1)
	second.JoinLane(2)
	third := st.NewTask("third", "...")
	third.JoinLane(1)
	third.JoinLane(3)
	selection := tasktest.NewSelection([]*state.Task{first, second, third})

	c.Check(tasktest.AssertSameLanes(
		selection.Select(tasktest.Kind("first")),
		selection.Select(tasktest.Kind("second")),
		selection.Select(tasktest.Kind("third")),
	), ErrorMatches, `task 3 \(third\) has lanes \[1 3\], expected the same lanes as task 1 \(first\) with lanes \[1 2\]`)
}

func (s *assertionsSuite) TestAssertionsCheckEmptySets(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	task := st.NewTask("task", "...")
	selection := tasktest.NewSelection([]*state.Task{task})
	empty := tasktest.NewSelection(nil)

	c.Check(tasktest.AssertOrdered(selection, empty), ErrorMatches, "selection 2 is empty")
	c.Check(tasktest.AssertNotOrdered(selection, empty), ErrorMatches, "selection 2 is empty")
	c.Check(tasktest.AssertLaneSuperset(selection, empty), ErrorMatches, "selection 2 is empty")
	c.Check(tasktest.AssertDoesNotShareLane(selection, empty), ErrorMatches, "selection 2 is empty")
	c.Check(tasktest.AssertSameLanes(selection, empty), ErrorMatches, "selection 2 is empty")
}
