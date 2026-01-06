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

type taskBuilderTestSuite struct{}

var _ = Suite(&taskBuilderTestSuite{})

func (s *taskBuilderTestSuite) TestAppendWithTaskData(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	b := newTaskSetBuilder()
	seq := b.NewTaskSequence()

	// this task data will be applied to all tasks added via this taskSetBuilder or any
	// child taskSequences
	seq.SetTaskData(map[string]any{"snap-setup": "snapsup-task"})

	// Append applies the taskSetBuilder's task data and chains the task to the tail
	t1 := st.NewTask("task-1", "test")
	seq.Append(t1)

	var snapsup string
	c.Assert(t1.Get("snap-setup", &snapsup), IsNil)
	c.Check(snapsup, Equals, "snapsup-task")

	c.Check(seq.Tasks(), DeepEquals, []*state.Task{t1})
	c.Check(b.TaskSet().Tasks(), DeepEquals, []*state.Task{t1})

	// Append applies the taskSetBuilder's task data and chains the task to the tail. note,
	// this is added directly on the taskSetBuilder, so this task should not be a part
	// of the taskSequence.
	t2 := st.NewTask("task-2", "test")
	b.Append(t2)

	snapsup = ""
	c.Assert(t2.Get("snap-setup", &snapsup), IsNil)
	c.Check(snapsup, Equals, "snapsup-task")

	c.Check(t2.WaitTasks(), DeepEquals, []*state.Task{t1})
	c.Check(seq.Tasks(), DeepEquals, []*state.Task{t1})
	c.Check(b.TaskSet().Tasks(), DeepEquals, []*state.Task{t1, t2})
}

func (s *taskBuilderTestSuite) TestSequenceAppendWithoutData(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	b := newTaskSetBuilder()

	seq := b.NewTaskSequence()
	seq.SetTaskData(map[string]any{"snap-setup": "snapsup-task"})

	task := st.NewTask("task-1", "test")

	// skips adding task data but still chains the task
	seq.AppendWithoutData(task)

	var snapsup string
	c.Check(task.Get("snap-setup", &snapsup), Not(IsNil))
	c.Check(snapsup, Equals, "")

	c.Check(b.TaskSet().Tasks(), DeepEquals, []*state.Task{task})
	c.Check(seq.Tasks(), DeepEquals, []*state.Task{task})
}

func (s *taskBuilderTestSuite) TestSequenceAppendChaining(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	b := newTaskSetBuilder()
	seq := b.NewTaskSequence()

	first := st.NewTask("task-1", "first")
	seq.Append(first)

	second := st.NewTask("task-2", "second")

	// each task waits for the previous task in the chain
	seq.Append(second)

	c.Check(first.WaitTasks(), HasLen, 0)
	c.Check(second.WaitTasks(), DeepEquals, []*state.Task{first})

	// taskSequence.tasks tracks all tasks added to this taskSequence, in order
	c.Check(seq.Tasks(), DeepEquals, []*state.Task{first, second})
}

func (s *taskBuilderTestSuite) TestSequenceChainWithoutAppending(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	b := newTaskSetBuilder()
	seq := b.NewTaskSequence()

	first := st.NewTask("task-1", "first")
	seq.Append(first)
	second := st.NewTask("task-2", "second")

	// chainWithoutAppending chains the task but does not add it to the taskSetBuilder or the taskSequence
	b.ChainWithoutAppending(second)

	// second waits for first but is not kept around in the taskSetBuilder or the taskSequence
	c.Check(second.WaitTasks(), DeepEquals, []*state.Task{first})
	c.Check(b.TaskSet().Tasks(), DeepEquals, []*state.Task{first})
	c.Check(seq.Tasks(), DeepEquals, []*state.Task{first})
}

func (s *taskBuilderTestSuite) TestChainWithoutAppendingSharedTask(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	b1 := newTaskSetBuilder()
	seq1 := b1.NewTaskSequence()

	t1 := st.NewTask("task-1", "in-taskSetBuilder-1")
	seq1.Append(t1)

	b2 := newTaskSetBuilder()
	seq2 := b2.NewTaskSequence()

	t2 := st.NewTask("task-2", "in-taskSetBuilder-2")
	seq2.Append(t2)

	// ChainWithoutAppending adds the same task to both chains
	chained := st.NewTask("chained", "in-both")
	b1.ChainWithoutAppending(chained)
	b2.ChainWithoutAppending(chained)

	// chained now waits for both task1 and task3, belonging to multiple chains
	c.Check(chained.WaitTasks(), HasLen, 2)
	c.Check(chained.WaitTasks()[0], Equals, t1)
	c.Check(chained.WaitTasks()[1], Equals, t2)

	// but it doesn't belong to either taskSetBuilder task sets. this lets callers
	// safely add the generated task sets to the same change, since a change
	// cannot contain a task more than once.
	c.Check(b1.TaskSet().Tasks(), DeepEquals, []*state.Task{t1})
	c.Check(b2.TaskSet().Tasks(), DeepEquals, []*state.Task{t2})
}

func (s *taskBuilderTestSuite) TestSequenceUpdateEdge(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	b := newTaskSetBuilder()
	seq := b.NewTaskSequence()

	first := st.NewTask("task-1", "first")
	seq.Append(first)

	edge := state.TaskSetEdge("begin-edge")
	seq.UpdateEdge(first, edge)

	edgeTask := b.TaskSet().MaybeEdge(edge)
	c.Check(edgeTask, Equals, first)

	second := st.NewTask("task-2", "second")
	seq.Append(second)

	// edges can be overwritten with a different task
	seq.UpdateEdge(second, edge)

	edgeTask = b.TaskSet().MaybeEdge(edge)
	c.Check(edgeTask, Equals, second)
}

func (s *taskBuilderTestSuite) TestSequenceAppendTSWithoutData(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	b := newTaskSetBuilder()
	seq := b.NewTaskSequence()

	// add an empty task set, just to make sure we don't panic
	seq.AppendTSWithoutData(state.NewTaskSet())

	first := st.NewTask("task-1", "first")
	seq.Append(first)

	second := st.NewTask("task-2", "second")
	third := st.NewTask("task-3", "third")
	third.WaitFor(second)

	// AppendTSWithoutData adds an entire TaskSet, preserving its internal
	// dependencies
	otherTS := state.NewTaskSet(second, third)
	seq.AppendTSWithoutData(otherTS)

	fourth := st.NewTask("task-4", "fourth")
	seq.Append(fourth)

	// third and second both wait for first, and third waits for second (the
	// original chain within the task set)
	c.Check(second.WaitTasks(), DeepEquals, []*state.Task{first})
	c.Check(third.WaitTasks(), DeepEquals, []*state.Task{second, first})

	// fourth waits on the tail of the added task set, third
	c.Check(fourth.WaitTasks(), DeepEquals, []*state.Task{third})

	// all tasks are contained within the taskSetBuilder and taskSequence
	c.Check(b.TaskSet().Tasks(), DeepEquals, []*state.Task{first, second, third, fourth})
	c.Check(seq.Tasks(), DeepEquals, []*state.Task{first, second, third, fourth})
}

func (s *taskBuilderTestSuite) TestMultipleSequencesShareTaskSetBuilder(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	b := newTaskSetBuilder()

	seq1 := b.NewTaskSequence()

	first := st.NewTask("task-1", "first")
	seq1.Append(first)

	seq2 := b.NewTaskSequence()

	second := st.NewTask("task-2", "second")
	seq2.Append(second)

	third := st.NewTask("task-3", "third")
	seq2.Append(third)

	c.Check(first.WaitTasks(), HasLen, 0)

	// both taskSequences share the same task set and tail, forming a single chain
	c.Check(second.WaitTasks(), DeepEquals, []*state.Task{first})
	c.Check(third.WaitTasks(), DeepEquals, []*state.Task{second})
	c.Check(b.TaskSet().Tasks(), DeepEquals, []*state.Task{first, second, third})

	// each taskSequence tracks only the tasks it added, enabling callers to keep track
	// of ranges
	c.Check(seq1.Tasks(), DeepEquals, []*state.Task{first})
	c.Check(seq2.Tasks(), DeepEquals, []*state.Task{second, third})
}
