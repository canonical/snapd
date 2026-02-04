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
	"errors"

	"github.com/snapcore/snapd/overlord/state"
)

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

// OpenSpan creates a new taskChainSpan that shares this taskChainBuilder's task
// set and tail.
func (b *taskChainBuilder) OpenSpan() *taskChainSpan {
	return &taskChainSpan{b: b}
}

// JoinOn makes the given task wait for the current tail and updates the tail to
// this task, but does NOT add the task to the builder's TaskSet. This is useful
// for shared tasks that belong to another TaskSet but need to be part of this
// chain's dependency graph.
func (b *taskChainBuilder) JoinOn(t *state.Task) {
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

// Close returns the tasks owned by this taskChainSpan. It also validates that
// the span has a clear start and end task so the returned slice can be organized
// with other slices of tasks.
func (s *taskChainSpan) Close() ([]*state.Task, error) {
	if len(s.tasks) > 0 {
		head, tails, _ := findHeadAndTailTasks(s.tasks)
		if len(head) > 1 {
			return s.tasks, errors.New("internal error: cannot start task chain span with multiple heads")
		}
		if len(tails) > 1 {
			return s.tasks, errors.New("internal error: cannot end task chain span with multiple tails")
		}
	}

	return s.tasks, nil
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

	heads, tails, remainder := findHeadAndTailTasks(tasks)

	// only head tasks need to wait on the existing tails
	for _, head := range heads {
		for _, tail := range s.b.tails {
			head.WaitFor(tail)
		}
	}

	// the ordering here is important. we want users of the span's output to be
	// able to assume that the first and last task of the slice are the head and
	// tail of the chain, respectively. to ensure this, we manually order the
	// tasks from the task set when adding them to our internal record of the
	// tasks in this span.
	order := heads
	order = append(order, remainder...)
	order = append(order, tails...)

	// note, the set of tasks in heads could equal tails. in that case,
	// remainder would be empty. but we still must make sure not to add heads
	// and tails twice.
	added := make(map[*state.Task]bool, len(tasks))
	for _, t := range order {
		if added[t] {
			continue
		}

		s.tasks = append(s.tasks, t)
		added[t] = true
	}

	s.b.ts.AddAll(ts)
	s.b.tails = tails
}

// findHeadAndTailTasks identifies entry and exit points within a task set based
// on internal dependencies. Head tasks have no predecessors within the set, and
// tail tasks have no successors within the set.
//
// The returned remainder contains tasks that are neither heads nor tails (i.e.,
// they have both predecessors and successors within the set).
//
// Special case: when a task is both a head and a tail (e.g., a single task with
// no internal dependencies, or disconnected tasks), it appears in both heads and
// tails but remainder will be empty. Callers must handle this case to avoid
// double-counting tasks.
func findHeadAndTailTasks(tasks []*state.Task) (heads, tails, remainder []*state.Task) {
	if len(tasks) == 0 {
		return nil, nil, nil
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

		if !head && !tail {
			remainder = append(remainder, t)
		}
	}

	return heads, tails, remainder
}
