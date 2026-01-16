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

package snapstate

import (
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/overlord/state"
)

type taskChainBuilderTestSuite struct{}

var _ = Suite(&taskChainBuilderTestSuite{})

func (s *taskChainBuilderTestSuite) TestAppendWithTaskData(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	b := newTaskChainBuilder()
	span := b.NewSpan()

	// this task data will be applied to all tasks added via this taskChainBuilder or any
	// child taskChainSpans
	span.SetTaskData(map[string]any{"snap-setup": "snapsup-task"})

	// Append applies the taskChainBuilder's task data and chains the task to the tail
	t1 := st.NewTask("task-1", "test")
	span.Append(t1)

	var snapsup string
	c.Assert(t1.Get("snap-setup", &snapsup), IsNil)
	c.Check(snapsup, Equals, "snapsup-task")

	c.Check(span.Tasks(), DeepEquals, []*state.Task{t1})
	c.Check(b.TaskSet().Tasks(), DeepEquals, []*state.Task{t1})

	// Append applies the taskChainBuilder's task data and chains the task to the tail. note,
	// this is added directly on the taskChainBuilder, so this task should not be a part
	// of the taskChainSpan.
	t2 := st.NewTask("task-2", "test")
	b.Append(t2)

	snapsup = ""
	c.Assert(t2.Get("snap-setup", &snapsup), IsNil)
	c.Check(snapsup, Equals, "snapsup-task")

	c.Check(t2.WaitTasks(), DeepEquals, []*state.Task{t1})
	c.Check(span.Tasks(), DeepEquals, []*state.Task{t1})
	c.Check(b.TaskSet().Tasks(), DeepEquals, []*state.Task{t1, t2})
}

func (s *taskChainBuilderTestSuite) TestSpanAppendWithoutData(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	b := newTaskChainBuilder()

	span := b.NewSpan()
	span.SetTaskData(map[string]any{"snap-setup": "snapsup-task"})

	task := st.NewTask("task-1", "test")

	// skips adding task data but still chains the task
	span.AppendWithoutData(task)

	var snapsup string
	c.Check(task.Get("snap-setup", &snapsup), Not(IsNil))
	c.Check(snapsup, Equals, "")

	c.Check(b.TaskSet().Tasks(), DeepEquals, []*state.Task{task})
	c.Check(span.Tasks(), DeepEquals, []*state.Task{task})
}

func (s *taskChainBuilderTestSuite) TestSpanAppendChaining(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	b := newTaskChainBuilder()
	span := b.NewSpan()

	first := st.NewTask("task-1", "first")
	span.Append(first)

	second := st.NewTask("task-2", "second")

	// each task waits for the previous task in the chain
	span.Append(second)

	c.Check(first.WaitTasks(), HasLen, 0)
	c.Check(second.WaitTasks(), DeepEquals, []*state.Task{first})

	// taskChainSpan.tasks tracks all tasks added to this taskChainSpan, in order
	c.Check(span.Tasks(), DeepEquals, []*state.Task{first, second})
}

func (s *taskChainBuilderTestSuite) TestSpanChainWithoutAppending(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	b := newTaskChainBuilder()
	span := b.NewSpan()

	first := st.NewTask("task-1", "first")
	span.Append(first)
	second := st.NewTask("task-2", "second")

	// ChainWithoutAppending chains the task but does not add it to the taskChainBuilder or the taskChainSpan
	b.JoinOn(second)

	// second waits for first but is not kept around in the taskChainBuilder or the taskChainSpan
	c.Check(second.WaitTasks(), DeepEquals, []*state.Task{first})
	c.Check(b.TaskSet().Tasks(), DeepEquals, []*state.Task{first})
	c.Check(span.Tasks(), DeepEquals, []*state.Task{first})
}

func (s *taskChainBuilderTestSuite) TestChainWithoutAppendingSharedTask(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	b1 := newTaskChainBuilder()
	span1 := b1.NewSpan()

	t1 := st.NewTask("task-1", "in-taskChainBuilder-1")
	span1.Append(t1)

	b2 := newTaskChainBuilder()
	span2 := b2.NewSpan()

	t2 := st.NewTask("task-2", "in-taskChainBuilder-2")
	span2.Append(t2)

	// ChainWithoutAppending adds the same task to both chains
	chained := st.NewTask("chained", "in-both")
	b1.JoinOn(chained)
	b2.JoinOn(chained)

	// chained now waits for both task1 and task3, belonging to multiple chains
	c.Check(chained.WaitTasks(), HasLen, 2)
	c.Check(chained.WaitTasks()[0], Equals, t1)
	c.Check(chained.WaitTasks()[1], Equals, t2)

	// but it doesn't belong to either taskChainBuilder task sets. this lets callers
	// safely add the generated task sets to the same change, since a change
	// cannot contain a task more than once.
	c.Check(b1.TaskSet().Tasks(), DeepEquals, []*state.Task{t1})
	c.Check(b2.TaskSet().Tasks(), DeepEquals, []*state.Task{t2})
}

func (s *taskChainBuilderTestSuite) TestSpanUpdateEdge(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	b := newTaskChainBuilder()
	span := b.NewSpan()

	first := st.NewTask("task-1", "first")
	span.Append(first)

	edge := state.TaskSetEdge("begin-edge")
	span.UpdateEdge(first, edge)

	edgeTask := b.TaskSet().MaybeEdge(edge)
	c.Check(edgeTask, Equals, first)

	second := st.NewTask("task-2", "second")
	span.Append(second)

	// edges can be overwritten with a different task
	span.UpdateEdge(second, edge)

	edgeTask = b.TaskSet().MaybeEdge(edge)
	c.Check(edgeTask, Equals, second)
}

func (s *taskChainBuilderTestSuite) TestSpanAppendTSWithoutData(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	b := newTaskChainBuilder()
	span := b.NewSpan()

	// add an empty task set, just to make sure we don't panic
	span.AppendTSWithoutData(state.NewTaskSet())

	first := st.NewTask("first", "first")
	span.Append(first)

	// create a diamond-shaped task set:
	//     t2
	//    /  \
	// t1      t4
	//    \  /
	//     t3
	t1 := st.NewTask("t1", "head of diamond")
	t2 := st.NewTask("t2", "left branch")
	t3 := st.NewTask("t3", "right branch")
	t4 := st.NewTask("t4", "tail of diamond")
	t2.WaitFor(t1)
	t3.WaitFor(t1)
	t4.WaitFor(t2)
	t4.WaitFor(t3)

	// AppendTSWithoutData adds an entire TaskSet, preserving its internal
	// dependencies. only head tasks wait for the current tail, and only tail
	// tasks become the new tail.
	otherTS := state.NewTaskSet(t1, t2, t3, t4)
	span.AppendTSWithoutData(otherTS)

	last := st.NewTask("last", "last")
	span.Append(last)

	// t1 (head of otherTS) waits for first
	c.Check(t1.WaitTasks(), DeepEquals, []*state.Task{first})

	// t2 and t3 only wait for t1 (their original dependencies within the task set)
	c.Check(t2.WaitTasks(), DeepEquals, []*state.Task{t1})
	c.Check(t3.WaitTasks(), DeepEquals, []*state.Task{t1})

	// t4 waits for t2 and t3 (its original dependencies within the task set)
	c.Check(t4.WaitTasks(), DeepEquals, []*state.Task{t2, t3})

	// last waits only on t4 (the tail of otherTS), not all tasks in the task
	// set
	c.Check(last.WaitTasks(), DeepEquals, []*state.Task{t4})

	// all tasks are contained within the taskChainBuilder and taskChainSpan
	c.Check(b.TaskSet().Tasks(), DeepEquals, []*state.Task{first, t1, t2, t3, t4, last})
	c.Check(span.Tasks(), DeepEquals, []*state.Task{first, t1, t2, t3, t4, last})
}

func (s *taskChainBuilderTestSuite) TestMultipleSpansShareTaskChainBuilder(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	b := newTaskChainBuilder()

	span1 := b.NewSpan()

	first := st.NewTask("task-1", "first")
	span1.Append(first)

	span2 := b.NewSpan()

	second := st.NewTask("task-2", "second")
	span2.Append(second)

	third := st.NewTask("task-3", "third")
	span2.Append(third)

	c.Check(first.WaitTasks(), HasLen, 0)

	// both taskChainSpans share the same task set and tail, forming a single chain
	c.Check(second.WaitTasks(), DeepEquals, []*state.Task{first})
	c.Check(third.WaitTasks(), DeepEquals, []*state.Task{second})
	c.Check(b.TaskSet().Tasks(), DeepEquals, []*state.Task{first, second, third})

	// each taskChainSpan tracks only the tasks it added, enabling callers to keep track
	// of ranges
	c.Check(span1.Tasks(), DeepEquals, []*state.Task{first})
	c.Check(span2.Tasks(), DeepEquals, []*state.Task{second, third})
}

func (s *taskChainBuilderTestSuite) TestFindHeadAndTailTasksEmpty(c *C) {
	ts := state.NewTaskSet()
	heads, tails := findHeadAndTailTasks(ts)
	c.Check(heads, IsNil)
	c.Check(tails, IsNil)
}

func (s *taskChainBuilderTestSuite) TestFindHeadAndTailTasksSingle(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	t1 := st.NewTask("task-1", "only task")
	ts := state.NewTaskSet(t1)

	heads, tails := findHeadAndTailTasks(ts)
	c.Check(heads, DeepEquals, []*state.Task{t1})
	c.Check(tails, DeepEquals, []*state.Task{t1})
}

func (s *taskChainBuilderTestSuite) TestFindHeadAndTailTasksLinearChain(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	// T1 -> T2 -> T3
	t1 := st.NewTask("task-1", "first")
	t2 := st.NewTask("task-2", "second")
	t3 := st.NewTask("task-3", "third")
	t2.WaitFor(t1)
	t3.WaitFor(t2)

	ts := state.NewTaskSet(t1, t2, t3)

	heads, tails := findHeadAndTailTasks(ts)
	c.Check(heads, DeepEquals, []*state.Task{t1})
	c.Check(tails, DeepEquals, []*state.Task{t3})
}

func (s *taskChainBuilderTestSuite) TestFindHeadAndTailTasksDiamond(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	// T1 -> T2 -> T4
	//  \ -> T3 -> /
	t1 := st.NewTask("task-1", "first")
	t2 := st.NewTask("task-2", "second")
	t3 := st.NewTask("task-3", "third")
	t4 := st.NewTask("task-4", "fourth")
	t2.WaitFor(t1)
	t3.WaitFor(t1)
	t4.WaitFor(t2)
	t4.WaitFor(t3)

	ts := state.NewTaskSet(t1, t2, t3, t4)

	heads, tails := findHeadAndTailTasks(ts)
	c.Check(heads, DeepEquals, []*state.Task{t1})
	c.Check(tails, DeepEquals, []*state.Task{t4})
}

func (s *taskChainBuilderTestSuite) TestFindHeadAndTailTasksDisconnected(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	// T1 and T2 have no dependencies between them
	t1 := st.NewTask("task-1", "first")
	t2 := st.NewTask("task-2", "second")

	ts := state.NewTaskSet(t1, t2)

	heads, tails := findHeadAndTailTasks(ts)
	c.Check(heads, DeepEquals, []*state.Task{t1, t2})
	c.Check(tails, DeepEquals, []*state.Task{t1, t2})
}

func (s *taskChainBuilderTestSuite) TestFindHeadAndTailTasksMultipleHeads(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	// T1 -> T3
	// T2 -> /
	t1 := st.NewTask("task-1", "first")
	t2 := st.NewTask("task-2", "second")
	t3 := st.NewTask("task-3", "third")
	t3.WaitFor(t1)
	t3.WaitFor(t2)

	ts := state.NewTaskSet(t1, t2, t3)

	heads, tails := findHeadAndTailTasks(ts)
	c.Check(heads, DeepEquals, []*state.Task{t1, t2})
	c.Check(tails, DeepEquals, []*state.Task{t3})
}

func (s *taskChainBuilderTestSuite) TestFindHeadAndTailTasksMultipleTails(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	// T1 -> T2
	//  \ -> T3
	t1 := st.NewTask("task-1", "first")
	t2 := st.NewTask("task-2", "second")
	t3 := st.NewTask("task-3", "third")
	t2.WaitFor(t1)
	t3.WaitFor(t1)

	ts := state.NewTaskSet(t1, t2, t3)

	heads, tails := findHeadAndTailTasks(ts)
	c.Check(heads, DeepEquals, []*state.Task{t1})
	c.Check(tails, DeepEquals, []*state.Task{t2, t3})
}

func (s *taskChainBuilderTestSuite) TestFindHeadAndTailTasksTwoChains(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	// two independent chains:
	// T1 -> T2 -> T3
	// T4 -> T5 -> T6
	t1 := st.NewTask("task-1", "first")
	t2 := st.NewTask("task-2", "second")
	t3 := st.NewTask("task-3", "third")
	t4 := st.NewTask("task-4", "fourth")
	t5 := st.NewTask("task-5", "fifth")
	t6 := st.NewTask("task-6", "sixth")

	t2.WaitFor(t1)
	t3.WaitFor(t2)
	t5.WaitFor(t4)
	t6.WaitFor(t5)

	ts := state.NewTaskSet(t1, t2, t3, t4, t5, t6)

	heads, tails := findHeadAndTailTasks(ts)
	c.Check(heads, DeepEquals, []*state.Task{t1, t4})
	c.Check(tails, DeepEquals, []*state.Task{t3, t6})
}

func (s *taskChainBuilderTestSuite) TestFindHeadAndTailTasksIgnoresExternalDeps(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	// T1 -> T2 -> T3, but T2 also waits for external task
	t1 := st.NewTask("task-1", "first")
	t2 := st.NewTask("task-2", "second")
	t3 := st.NewTask("task-3", "third")
	external := st.NewTask("external", "not in set")

	t2.WaitFor(t1)
	t2.WaitFor(external)
	t3.WaitFor(t2)

	// only include t1, t2, t3 in the set (not external)
	ts := state.NewTaskSet(t1, t2, t3)

	heads, tails := findHeadAndTailTasks(ts)
	// t1 is still the only head (external dep is ignored)
	c.Check(heads, DeepEquals, []*state.Task{t1})
	c.Check(tails, DeepEquals, []*state.Task{t3})
}

func (s *taskChainBuilderTestSuite) TestFindHeadAndTailTasksWithExternalWaitAndHalt(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	// external-1 -> t1 -> t2 -> external-2
	external1 := st.NewTask("external-1", "external predecessor")
	t1 := st.NewTask("task-1", "first in set")
	t2 := st.NewTask("task-2", "second in set")
	external2 := st.NewTask("external-2", "external successor")

	t1.WaitFor(external1)
	t2.WaitFor(t1)
	external2.WaitFor(t2)

	// only include t1, t2 in the set
	ts := state.NewTaskSet(t1, t2)

	heads, tails := findHeadAndTailTasks(ts)
	// t1 is still the head (external predecessor is ignored)
	c.Check(heads, DeepEquals, []*state.Task{t1})
	// t2 is still the tail (external successor is ignored)
	c.Check(tails, DeepEquals, []*state.Task{t2})
}
