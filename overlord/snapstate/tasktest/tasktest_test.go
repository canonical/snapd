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
	"testing"
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/overlord/snapstate/tasktest"
	"github.com/snapcore/snapd/overlord/state"
)

type selectionSuite struct{}

var _ = Suite(&selectionSuite{})

func Test(t *testing.T) { TestingT(t) }

func (s *selectionSuite) TestSelectionTasks(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	first := st.NewTask("first", "...")
	second := st.NewTask("second", "...")
	input := []*state.Task{first, second}

	selection := tasktest.NewSelection(input)

	c.Check(selection.Tasks(), DeepEquals, input)
}

func (s *selectionSuite) TestSelectionFilter(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	first := st.NewTask("keep", "...")
	second := st.NewTask("drop", "...")
	third := st.NewTask("keep", "...")
	selection := tasktest.NewSelection([]*state.Task{first, second, third})

	filtered, err := selection.Filter(func(task *state.Task) (bool, error) {
		return task.Kind() == "keep", nil
	})
	c.Assert(err, IsNil)
	c.Check(filtered.Tasks(), DeepEquals, []*state.Task{first, third})
}

func (s *selectionSuite) TestSelectionHeadsAndTails(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	first := st.NewTask("first", "...")
	second := st.NewTask("second", "...")
	third := st.NewTask("third", "...")
	unrelated := st.NewTask("unrelated", "...")
	second.WaitFor(first)
	third.WaitFor(second)

	selection := tasktest.NewSelection([]*state.Task{third, unrelated, first, second})
	c.Check(selection.Heads().Tasks(), DeepEquals, []*state.Task{unrelated, first})
	c.Check(selection.Tails().Tasks(), DeepEquals, []*state.Task{third, unrelated})

	// dependencies through tasks outside the selection still count.
	endpoints, err := selection.Filter(func(task *state.Task) (bool, error) {
		return task == first || task == third, nil
	})
	c.Assert(err, IsNil)
	c.Check(endpoints.Heads().Tasks(), DeepEquals, []*state.Task{first})
	c.Check(endpoints.Tails().Tasks(), DeepEquals, []*state.Task{third})

	// a selected task can be both a head and a tail despite external dependencies.
	middle := selection.Select(tasktest.Kind("second"))
	c.Check(middle.Heads().Tasks(), DeepEquals, []*state.Task{second})
	c.Check(middle.Tails().Tasks(), DeepEquals, []*state.Task{second})
}

func (s *selectionSuite) TestSelectionCycle(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	first := st.NewTask("first", "...")
	second := st.NewTask("second", "...")
	first.WaitFor(second)
	second.WaitFor(first)

	c.Check(func() {
		tasktest.NewSelection([]*state.Task{first, second})
	}, PanicMatches, `dependency cycle involving task 1 \(first\)`)
}

func (s *selectionSuite) TestSelectionPredecessors(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	first := st.NewTask("first", "...")
	second := st.NewTask("second", "...")
	third := st.NewTask("third", "...")
	fourth := st.NewTask("fourth", "...")
	unrelated := st.NewTask("unrelated", "...")
	second.WaitFor(first)
	third.WaitFor(second)
	third.WaitFor(first)
	fourth.WaitFor(third)

	selection := tasktest.NewSelection([]*state.Task{fourth, second, unrelated, third, first})
	target := tasktest.NewSelection([]*state.Task{third})

	c.Check(selection.Predecessors(target).Tasks(), DeepEquals, []*state.Task{second, first})
}

func (s *selectionSuite) TestSelectionSuccessors(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	first := st.NewTask("first", "...")
	second := st.NewTask("second", "...")
	third := st.NewTask("third", "...")
	fourth := st.NewTask("fourth", "...")
	unrelated := st.NewTask("unrelated", "...")
	second.WaitFor(first)
	third.WaitFor(second)
	fourth.WaitFor(third)

	selection := tasktest.NewSelection([]*state.Task{fourth, second, unrelated, third, first})
	source := tasktest.NewSelection([]*state.Task{second})
	c.Check(selection.Successors(source).Tasks(), DeepEquals, []*state.Task{fourth, third})

	sources := tasktest.NewSelection([]*state.Task{unrelated, second})
	c.Check(selection.Successors(sources).Tasks(), DeepEquals, []*state.Task{fourth, third})

	limited := tasktest.NewSelection([]*state.Task{fourth, unrelated})
	c.Check(limited.Successors(source).Tasks(), DeepEquals, []*state.Task{fourth})
}

func (s *selectionSuite) TestMissing(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	missing := tasktest.Missing()
	c.Check(missing.Tasks(), HasLen, 0)

	_, err := missing.SelectErr(tasktest.Kind("task"))
	c.Check(err, Equals, tasktest.ErrNoMatches)

	selected, err := missing.SelectErr(tasktest.Kind("task").Optional())
	c.Assert(err, IsNil)
	c.Check(selected.Tasks(), HasLen, 0)

	// missing selections are skipped by assertions
	first := st.NewTask("first", "...")
	second := st.NewTask("second", "...")
	second.WaitFor(first)
	selection := tasktest.NewSelection([]*state.Task{first, second})
	c.Check(tasktest.AssertOrdered(
		selection.Select(tasktest.Kind("first")),
		missing,
		selection.Select(tasktest.Kind("second")),
	), IsNil)
}

func (s *selectionSuite) TestUnion(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	first := st.NewTask("first", "...")
	second := st.NewTask("second", "...")
	third := st.NewTask("third", "...")
	third.WaitFor(first)
	third.WaitFor(second)
	selection := tasktest.NewSelection([]*state.Task{first, second, third})

	firstSel := selection.Select(tasktest.Kind("first"))
	secondSel := selection.Select(tasktest.Kind("second"))
	thirdSel := selection.Select(tasktest.Kind("third"))

	// duplicates are included once, in first-seen order
	union := tasktest.Union(secondSel, firstSel, secondSel)
	c.Check(union.Tasks(), DeepEquals, []*state.Task{second, first})
	c.Check(tasktest.AssertOrdered(union, thirdSel), IsNil)

	// the universe is taken from a selection that is not missing
	union = tasktest.Union(tasktest.Missing(), firstSel, tasktest.Missing())
	c.Check(union.Tasks(), DeepEquals, []*state.Task{first})
	c.Check(tasktest.AssertOrdered(union, thirdSel), IsNil)
	c.Check(union.Select(tasktest.Kind("first")).Tasks(), DeepEquals, []*state.Task{first})

	// the union is missing only if all inputs are missing
	union = tasktest.Union(tasktest.Missing(), tasktest.Missing())
	c.Check(union.Tasks(), HasLen, 0)
	c.Check(tasktest.AssertOrdered(firstSel, union, thirdSel), IsNil)

	union = tasktest.Union(tasktest.Missing(), tasktest.NewSelection(nil))
	c.Check(tasktest.AssertOrdered(firstSel, union, thirdSel), ErrorMatches, "selection 2 is empty")

	c.Check(tasktest.AssertOrdered(firstSel, tasktest.Union(), thirdSel), ErrorMatches, "selection 2 is empty")
}

type taskQuerySuite struct{}

var _ = Suite(&taskQuerySuite{})

func (s *taskQuerySuite) TestID(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	first := st.NewTask("task", "...")
	second := st.NewTask("task", "...")
	selection := tasktest.NewSelection([]*state.Task{first, second})

	selected := selection.Select(tasktest.ID(first.ID()))
	c.Check(selected.Tasks(), DeepEquals, []*state.Task{first})

	_, err := selection.SelectErr(tasktest.ID("missing"))
	c.Check(err, Equals, tasktest.ErrNoMatches)
	_, err = selected.SelectErr(tasktest.ID(second.ID()))
	c.Check(err, Equals, tasktest.ErrNoMatches)
	_, err = selection.SelectErr(tasktest.ID(first.ID()).WithField("missing", true))
	c.Check(err, Equals, tasktest.ErrNoMatches)
}

func (s *taskQuerySuite) TestKind(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	first := st.NewTask("match", "...")
	second := st.NewTask("other", "...")
	selection := tasktest.NewSelection([]*state.Task{first, second})

	selected := selection.Select(tasktest.Kind("match"))
	c.Check(selected.Tasks(), DeepEquals, []*state.Task{first})
}

func (s *taskQuerySuite) TestWithField(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	first := st.NewTask("task", "...")
	first.Set("channel", "stable")
	first.Set("enabled", true)

	second := st.NewTask("task", "...")
	second.Set("channel", "candidate")
	second.Set("enabled", true)

	third := st.NewTask("task", "...")
	third.Set("channel", "stable")

	selection := tasktest.NewSelection([]*state.Task{first, second, third})

	query := tasktest.Kind("task").WithField("channel", "stable")
	c.Check(selection.Select(query.WithField("enabled", true)).Tasks(), DeepEquals, []*state.Task{first})
	c.Check(selection.Select(query.WithField("enabled", nil)).Tasks(), DeepEquals, []*state.Task{third})
	c.Check(query.Fields, DeepEquals, map[string]any{"channel": "stable"})
	c.Check(selection.Select(query.All()).Tasks(), DeepEquals, []*state.Task{first, third})
}

func (s *taskQuerySuite) TestWithTimeField(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	task := st.NewTask("task", "...")

	// storing a time.Time as JSON removes its monotonic clock data. this test
	// ensures that the query properly handles data that has been mutated during
	// JSON serialization.
	lastRefreshTime := time.Now()
	task.Set("old-last-refresh-time", lastRefreshTime)

	selection := tasktest.NewSelection([]*state.Task{task})

	selected := selection.Select(tasktest.Kind("task").WithField("old-last-refresh-time", lastRefreshTime))
	c.Check(selected.Tasks(), DeepEquals, []*state.Task{task})
}

func (s *taskQuerySuite) TestAbsentField(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	withoutField := st.NewTask("task", "...")

	withField := st.NewTask("task", "...")
	withField.Set("marker", true)

	selection := tasktest.NewSelection([]*state.Task{withoutField, withField})

	selected := selection.Select(tasktest.Kind("task").WithField("marker", nil))
	c.Check(selected.Tasks(), DeepEquals, []*state.Task{withoutField})
}

func (s *taskQuerySuite) TestDefaultCardinality(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	first := st.NewTask("match", "...")
	second := st.NewTask("match", "...")
	selection := tasktest.NewSelection([]*state.Task{first, second})

	_, err := selection.SelectErr(tasktest.TaskQuery{})
	c.Check(err, ErrorMatches, "task query matched 2 tasks, expected 1")
}

func (s *taskQuerySuite) TestTaskCount(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	first := st.NewTask("match", "...")
	second := st.NewTask("match", "...")
	third := st.NewTask("other", "...")
	selection := tasktest.NewSelection([]*state.Task{first, second, third})

	selected := selection.Select(tasktest.Kind("match").TaskCount(2))
	c.Check(selected.Tasks(), DeepEquals, []*state.Task{first, second})

	_, err := selection.SelectErr(tasktest.Kind("match").TaskCount(3))
	c.Check(err, ErrorMatches, "task query matched 2 tasks, expected 3")
}

func (s *taskQuerySuite) TestAll(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	first := st.NewTask("match", "...")
	second := st.NewTask("match", "...")
	selection := tasktest.NewSelection([]*state.Task{first, second})

	selected := selection.Select(tasktest.Kind("match").All())
	c.Check(selected.Tasks(), DeepEquals, []*state.Task{first, second})
}

func (s *taskQuerySuite) TestNoMatches(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	task := st.NewTask("other", "...")
	selection := tasktest.NewSelection([]*state.Task{task})

	for _, query := range []tasktest.TaskQuery{
		tasktest.Kind("match"),
		tasktest.Kind("match").All(),
		tasktest.Kind("match").TaskCount(2),
	} {
		_, err := selection.SelectErr(query)
		c.Check(err, Equals, tasktest.ErrNoMatches)
	}
}

func (s *taskQuerySuite) TestOptional(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	first := st.NewTask("match", "...")
	second := st.NewTask("match", "...")
	third := st.NewTask("single", "...")
	third.WaitFor(first)
	third.WaitFor(second)
	selection := tasktest.NewSelection([]*state.Task{first, second, third})

	// no matches result in a missing selection
	missing, err := selection.SelectErr(tasktest.Kind("absent").Optional())
	c.Assert(err, IsNil)
	c.Check(missing.Tasks(), HasLen, 0)
	c.Check(tasktest.AssertOrdered(missing, selection.Select(tasktest.Kind("single"))), IsNil)

	// a single match is selected as usual
	selected := selection.Select(tasktest.Kind("single").Optional())
	c.Check(selected.Tasks(), DeepEquals, []*state.Task{third})

	// a selection with tasks is not missing, so the order is checked
	c.Check(tasktest.AssertOrdered(selected, selection.Select(tasktest.Kind("match").All())),
		ErrorMatches, `task 3 \(single\) is not sequenced before task 1 \(match\)`)

	// the default cardinality is still checked
	_, err = selection.SelectErr(tasktest.Kind("match").Optional())
	c.Check(err, ErrorMatches, "task query matched 2 tasks, expected 1")
}

func (s *taskQuerySuite) TestOptionalAll(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	first := st.NewTask("match", "...")
	second := st.NewTask("match", "...")
	selection := tasktest.NewSelection([]*state.Task{first, second})

	selected, err := selection.SelectErr(tasktest.Kind("absent").All().Optional())
	c.Assert(err, IsNil)
	c.Check(selected.Tasks(), HasLen, 0)

	selected = selection.Select(tasktest.Kind("match").All().Optional())
	c.Check(selected.Tasks(), DeepEquals, []*state.Task{first, second})
}

func (s *taskQuerySuite) TestOptionalTaskCount(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	first := st.NewTask("match", "...")
	second := st.NewTask("match", "...")
	selection := tasktest.NewSelection([]*state.Task{first, second})

	selected, err := selection.SelectErr(tasktest.Kind("absent").TaskCount(2).Optional())
	c.Assert(err, IsNil)
	c.Check(selected.Tasks(), HasLen, 0)

	selected = selection.Select(tasktest.Kind("match").TaskCount(2).Optional())
	c.Check(selected.Tasks(), DeepEquals, []*state.Task{first, second})

	_, err = selection.SelectErr(tasktest.Kind("match").TaskCount(3).Optional())
	c.Check(err, ErrorMatches, "task query matched 2 tasks, expected 3")
}

func (s *taskQuerySuite) TestTaskCountInvalid(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	task := st.NewTask("match", "...")
	selection := tasktest.NewSelection([]*state.Task{task})

	_, err := selection.SelectErr(tasktest.Kind("match").TaskCount(-2))
	c.Check(err, ErrorMatches, "invalid task query cardinality -2")
}

func (s *taskQuerySuite) TestFieldDecodeError(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	task := st.NewTask("task", "...")
	task.Set("field", "value")
	selection := tasktest.NewSelection([]*state.Task{task})

	_, err := selection.SelectErr(tasktest.TaskQuery{
		Fields: map[string]any{"field": 1},
	})
	c.Check(err, ErrorMatches, `cannot read field "field" of task 1 \(task\) as int`)
}
