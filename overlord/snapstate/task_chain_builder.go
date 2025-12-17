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

import "github.com/snapcore/snapd/overlord/state"

// taskChainBuilder constructs a chain of tasks with automatic dependency
// chaining and task data management.
type taskChainBuilder struct {
	// ts is the underlying TaskSet that accumulates all tasks added to this chain.
	// It is also used to track edges within the chain.
	ts *state.TaskSet
	// tails holds the tasks that represent the tail of the chain so far.
	// Newly appended tasks will wait for all tasks in the current tail.
	tails []*state.Task
	// taskData contains data that tasks added to the chain get adorned with.
	taskData map[string]any
}

// newTaskChainBuilder returns a taskChainBuilder initialized with an empty task set.
func newTaskChainBuilder() taskChainBuilder {
	return taskChainBuilder{
		ts: state.NewTaskSet(),
	}
}

// TaskSet returns the task set that contains all tasks added to this taskChainBuilder,
// either directly or via child taskChainSpans.
func (b *taskChainBuilder) TaskSet() *state.TaskSet {
	return b.ts
}

// Append appends a task to the end of the existing chain of tasks. Any existing
// task data is attached to the given task.
func (b *taskChainBuilder) Append(t *state.Task) {
	tmp := taskChainSpan{b: b}
	tmp.Append(t)
}

// NewSpan creates a new taskChainSpan that shares this taskChainBuilder's task set and tail.
func (b *taskChainBuilder) NewSpan() taskChainSpan {
	return taskChainSpan{b: b}
}

// ChainWithoutAppending makes the given task wait for the current tail and updates
// the tail to this task, but does NOT add the task to the builder's TaskSet. This
// is useful for shared tasks that belong to another TaskSet but need to be part of
// this chain's dependency graph.
func (b *taskChainBuilder) ChainWithoutAppending(t *state.Task) {
	for _, prev := range b.tails {
		t.WaitFor(prev)
	}
	b.tails = []*state.Task{t}
}

// taskChainSpan represents a contiguous range of tasks within a task chain builder.
// This type is used to construct ranges of tasks for easier grouping, while still
// enabling the marking of edges in the parent taskChainBuilder's task set.
type taskChainSpan struct {
	b     *taskChainBuilder
	tasks []*state.Task
}

// SetTaskData sets the task data applied to all future tasks added to the parent
// taskChainBuilder's task set.
func (s *taskChainSpan) SetTaskData(taskData map[string]any) {
	s.b.taskData = taskData
}

// Append appends a task to the chain of tasks managed by the parent taskChainBuilder.
// Additionally, the task is added to this taskChainSpan's range of tasks. The task has
// the taskChainBuilder's task data applied.
func (s *taskChainSpan) Append(t *state.Task) {
	for k, v := range s.b.taskData {
		t.Set(k, v)
	}
	s.AppendWithoutData(t)
}

// AppendWithoutData behaves the same as Append, but task data is not applied to
// the added task.
func (s *taskChainSpan) AppendWithoutData(t *state.Task) {
	for _, prev := range s.b.tails {
		t.WaitFor(prev)
	}
	s.b.tails = []*state.Task{t}
	s.b.ts.AddTask(t)
	s.tasks = append(s.tasks, t)
}

// UpdateEdge marks the task as an edge. If the task set owned by the parent
// taskChainBuilder already has that edge, it is overwritten.
func (s *taskChainSpan) UpdateEdge(t *state.Task, e state.TaskSetEdge) {
	s.b.ts.MarkEdge(t, e)
}

// AppendTSWithoutData adds all tasks from another task set without applying
// task data.
//
// The tasks in ts are chained after the current tail. Only head tasks (tasks
// with no predecessors within ts) are made to wait on the current tail, and
// only tail tasks (tasks with no successors within ts) become the new tail.
// This preserves the internal dependency structure of ts without creating
// redundant dependencies.
func (s *taskChainSpan) AppendTSWithoutData(ts *state.TaskSet) {
	tasks := ts.Tasks()
	if len(tasks) == 0 {
		return
	}

	heads, tails := findHeadAndTailTasks(ts)

	// only head tasks need to wait on the existing tails
	for _, head := range heads {
		for _, tail := range s.b.tails {
			head.WaitFor(tail)
		}
	}

	s.b.ts.AddAll(ts)
	s.b.tails = tails
	s.tasks = append(s.tasks, tasks...)
}

// Tasks returns the tasks owned by this taskChainSpan.
func (s *taskChainSpan) Tasks() []*state.Task {
	return s.tasks
}

// findHeadAndTailTasks identifies entry and exit points within a task set based
// on internal dependencies. Head tasks have no predecessors within the set, and
// tail tasks have no successors within the set.
func findHeadAndTailTasks(ts *state.TaskSet) (heads, tails []*state.Task) {
	tasks := ts.Tasks()
	if len(tasks) == 0 {
		return nil, nil
	}

	inSet := make(map[string]bool, len(tasks))
	for _, t := range tasks {
		inSet[t.ID()] = true
	}

	for _, t := range tasks {
		head := true
		for _, wt := range t.WaitTasks() {
			if inSet[wt.ID()] {
				head = false
				break
			}
		}

		// t is a head if it doesn't have any wait tasks, or if all of its wait
		// tasks are outside of the task set
		if head {
			heads = append(heads, t)
		}

		tail := true
		for _, ht := range t.HaltTasks() {
			if inSet[ht.ID()] {
				tail = false
				break
			}
		}

		// t is a tail if it doesn't have any halt tasks, or if all of its halt
		// tasks are outside of the task set
		if tail {
			tails = append(tails, t)
		}
	}

	return heads, tails
}
