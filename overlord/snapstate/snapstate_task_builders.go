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

// taskSetBuilder constructs a graph of tasks with automatic dependency chaining and
// task data management.
type taskSetBuilder struct {
	// ts contains all tasks managed by this taskSetBuilder and any child taskSequence.
	// Primarily, this is used to keep track of edges.
	ts *state.TaskSet
	// tail points to the tip of the current graph. It is updated by the taskSetBuilder
	// or any child taskSequences.
	tail *state.Task
	// taskData contains data that tasks added to the graph get adorned with.
	taskData map[string]any
}

// newTaskSetBuilder returns a taskSetBuilder initialized with an empty task set.
func newTaskSetBuilder() taskSetBuilder {
	return taskSetBuilder{
		ts: state.NewTaskSet(),
	}
}

// TaskSet returns the task set that contains all tasks added to this taskSetBuilder,
// either directly or via child taskSequences.
func (b *taskSetBuilder) TaskSet() *state.TaskSet {
	return b.ts
}

// Append appends a task to the end of the existing chain of tasks. Any existing
// task data is attached to the given task.
func (b *taskSetBuilder) Append(t *state.Task) {
	tmp := taskSequence{b: b}
	tmp.Append(t)
}

// NewTaskSequence creates a new taskSequence that shares this taskSetBuilder's task set and tail.
func (b *taskSetBuilder) NewTaskSequence() taskSequence {
	return taskSequence{b: b}
}

// ChainWithoutAppending chains a task into the dependency sequence without adding it
// to the taskSetBuilder's task set or taskSequence. This is useful when inserting
// a task that might be shared by multiple taskSetBuilders.
func (b *taskSetBuilder) ChainWithoutAppending(t *state.Task) {
	if b.tail != nil {
		t.WaitFor(b.tail)
	}
	b.tail = t
}

// taskSequence represents a logical grouping of tasks within a task builder. This type
// is used to contruct ranges of tasks for easier grouping, while still enabling
// the marking of edges in the parent taskSetBuilder's task set.
type taskSequence struct {
	b     *taskSetBuilder
	tasks []*state.Task
}

// SetTaskData sets the task data applied to all future tasks added to the parent
// taskSetBuilder's task set.
func (s *taskSequence) SetTaskData(taskData map[string]any) {
	s.b.taskData = taskData
}

// Append appends a task to graph of tasks managed by the parent taskSetBuilder.
// Additionally, the task is added to this taskSequence's range of tasks. The task has
// the taskSetBuilder's task data applied.
func (s *taskSequence) Append(t *state.Task) {
	for k, v := range s.b.taskData {
		t.Set(k, v)
	}
	s.AppendWithoutData(t)
}

// AppendWithoutData behaves the same as Append, but task data is not applied to
// the added task.
func (s *taskSequence) AppendWithoutData(t *state.Task) {
	if s.b.tail != nil {
		t.WaitFor(s.b.tail)
	}
	s.b.tail = t
	s.b.ts.AddTask(t)
	s.tasks = append(s.tasks, t)
}

// UpdateEdge marks the task as an edge. If the task set owned by the parent
// taskSetBuilder already has that edge, it is overwritten.
func (s *taskSequence) UpdateEdge(t *state.Task, e state.TaskSetEdge) {
	s.b.ts.MarkEdge(t, e)
}

// AppendTSWithoutData adds all tasks from another task set without applying
// task data. It is assumed that the last task in the task set is the final task
// in it's dependency graph.
func (s *taskSequence) AppendTSWithoutData(ts *state.TaskSet) {
	tasks := ts.Tasks()
	if len(tasks) == 0 {
		return
	}

	if s.b.tail != nil {
		ts.WaitFor(s.b.tail)
	}
	s.b.ts.AddAll(ts)
	s.b.tail = tasks[len(tasks)-1]
	s.tasks = append(s.tasks, ts.Tasks()...)
}

// Tasks returns the tasks owned by this taskSequence
func (s *taskSequence) Tasks() []*state.Task {
	return s.tasks
}
