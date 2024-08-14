// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2022 Canonical Ltd
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

package state

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/snapcore/snapd/logger"
)

type progress struct {
	Label string `json:"label"`
	Done  int    `json:"done"`
	Total int    `json:"total"`
}

// Task represents an individual operation to be performed
// for accomplishing one or more state changes.
//
// See Change for more details.
type Task struct {
	state   *State
	id      string
	kind    string
	summary string
	status  Status
	// waitedStatus is the Status that should be used instead of
	// WaitStatus once the wait is complete (i.e post reboot).
	waitedStatus Status
	clean        bool
	progress     *progress
	data         customData
	waitTasks    []string
	haltTasks    []string
	lanes        []int
	log          []string
	change       string

	spawnTime time.Time
	readyTime time.Time

	// TODO: add:
	// {,Un}DoingRetries - number of retries
	// Retry{,Un}DoingTimes - time spend to figure out a retry is needed
	doingTime   time.Duration
	undoingTime time.Duration

	atTime time.Time
}

func newTask(state *State, id, kind, summary string) *Task {
	return &Task{
		state:   state,
		id:      id,
		kind:    kind,
		summary: summary,
		data:    make(customData),

		spawnTime: timeNow(),
	}
}

type marshalledTask struct {
	ID           string                      `json:"id"`
	Kind         string                      `json:"kind"`
	Summary      string                      `json:"summary"`
	Status       Status                      `json:"status"`
	WaitedStatus Status                      `json:"waited-status"`
	Clean        bool                        `json:"clean,omitempty"`
	Progress     *progress                   `json:"progress,omitempty"`
	Data         map[string]*json.RawMessage `json:"data,omitempty"`
	WaitTasks    []string                    `json:"wait-tasks,omitempty"`
	HaltTasks    []string                    `json:"halt-tasks,omitempty"`
	Lanes        []int                       `json:"lanes,omitempty"`
	Log          []string                    `json:"log,omitempty"`
	Change       string                      `json:"change"`

	SpawnTime time.Time  `json:"spawn-time"`
	ReadyTime *time.Time `json:"ready-time,omitempty"`

	DoingTime   time.Duration `json:"doing-time,omitempty"`
	UndoingTime time.Duration `json:"undoing-time,omitempty"`

	AtTime *time.Time `json:"at-time,omitempty"`
}

// MarshalJSON makes Task a json.Marshaller
func (t *Task) MarshalJSON() ([]byte, error) {
	t.state.reading()
	var readyTime *time.Time
	if !t.readyTime.IsZero() {
		readyTime = &t.readyTime
	}
	var atTime *time.Time
	if !t.atTime.IsZero() {
		atTime = &t.atTime
	}
	return json.Marshal(marshalledTask{
		ID:           t.id,
		Kind:         t.kind,
		Summary:      t.summary,
		Status:       t.status,
		WaitedStatus: t.waitedStatus,
		Clean:        t.clean,
		Progress:     t.progress,
		Data:         t.data,
		WaitTasks:    t.waitTasks,
		HaltTasks:    t.haltTasks,
		Lanes:        t.lanes,
		Log:          t.log,
		Change:       t.change,

		SpawnTime: t.spawnTime,
		ReadyTime: readyTime,

		DoingTime:   t.doingTime,
		UndoingTime: t.undoingTime,

		AtTime: atTime,
	})
}

// UnmarshalJSON makes Task a json.Unmarshaller
func (t *Task) UnmarshalJSON(data []byte) error {
	if t.state != nil {
		t.state.writing()
	}
	var unmarshalled marshalledTask
	err := json.Unmarshal(data, &unmarshalled)
	if err != nil {
		return err
	}
	t.id = unmarshalled.ID
	t.kind = unmarshalled.Kind
	t.summary = unmarshalled.Summary
	t.status = unmarshalled.Status
	t.waitedStatus = unmarshalled.WaitedStatus
	if t.waitedStatus == DefaultStatus {
		// For backwards-compatibility, default the waitStatus, which is
		// the result status after a wait, to DoneStatus to keep any previous
		// behaviour before any upgrade.
		t.waitedStatus = DoneStatus
	}
	t.clean = unmarshalled.Clean
	t.progress = unmarshalled.Progress
	custData := unmarshalled.Data
	if custData == nil {
		custData = make(customData)
	}
	t.data = custData
	t.waitTasks = unmarshalled.WaitTasks
	t.haltTasks = unmarshalled.HaltTasks
	t.lanes = unmarshalled.Lanes
	t.log = unmarshalled.Log
	t.change = unmarshalled.Change
	t.spawnTime = unmarshalled.SpawnTime
	if unmarshalled.ReadyTime != nil {
		t.readyTime = *unmarshalled.ReadyTime
	}
	if unmarshalled.AtTime != nil {
		t.atTime = *unmarshalled.AtTime
	}
	t.doingTime = unmarshalled.DoingTime
	t.undoingTime = unmarshalled.UndoingTime
	return nil
}

// ID returns the individual random key for this task.
func (t *Task) ID() string {
	return t.id
}

// Kind returns the nature of this task for managers to know how to handle it.
func (t *Task) Kind() string {
	return t.kind
}

// Summary returns a summary describing what the task is about.
func (t *Task) Summary() string {
	return t.summary
}

// Status returns the current task status.
//
// Possible state transitions:
//
//	   /----aborting lane--Do
//	   |                   |
//	   V                   V
//	  Hold               Doing-->Wait
//	   ^                /  |  \
//	   |         abort /   V   V
//	 no undo          /  Done  Error
//	   |             V     |
//	   \----------Abort   aborting lane
//	   /          |        |
//	   |       finished or |
//	running    not running |
//	   V          \------->|
//	kill goroutine         |
//	   |                   V
//	  / \           ----->Undo
//	 /   no error  /       |
//	 |   from goroutine    |
//	error                  |
//	from goroutine         |
//	 |                     V
//	 |                  Undoing-->Wait
//	 V                     |   \
//	Error                  V    V
//	                     Undone Error
//
// Do -> Doing -> Done is the direct success scenario.
//
// Wait can transition to its waited status,
// usually Done|Undone or back to Doing.
// See Wait struct, SetToWait and WaitedStatus.
func (t *Task) Status() Status {
	t.state.reading()
	if t.status == DefaultStatus {
		return DoStatus
	}
	return t.status
}

func (t *Task) changeStatus(old, new Status) {
	if old == new {
		return
	}
	t.status = new
	if !old.Ready() && new.Ready() {
		t.readyTime = timeNow()
	}
	chg := t.Change()
	if chg != nil {
		chg.taskStatusChanged(t, old, new)
	}
	t.state.notifyTaskStatusChangedHandlers(t, old, new)
}

// SetStatus sets the task status, overriding the default behavior (see Status method).
func (t *Task) SetStatus(new Status) {
	if new == WaitStatus {
		panic("Task.SetStatus() called with WaitStatus, which is not allowed. Use SetToWait() instead")
	}

	t.state.writing()
	old := t.status
	if new == DoneStatus && old == AbortStatus {
		// if the task is in AbortStatus (because some other task ran
		// in parallel and had an error so the change is aborted) and
		// DoneStatus was requested (which can happen if the
		// task handler sets its status explicitly) then keep it at
		// aborted so it can transition to Undo.
		return
	}
	t.changeStatus(old, new)
}

// SetToWait puts the task into WaitStatus, and sets the status the task should be restored
// to after the SetToWait.
func (t *Task) SetToWait(resultStatus Status) {
	switch resultStatus {
	case DefaultStatus, WaitStatus:
		panic("Task.SetToWait() cannot be invoked with either of DefaultStatus or WaitStatus")
	}

	t.state.writing()
	old := t.status
	if old == AbortStatus {
		// if the task is in AbortStatus (because some other task ran
		// in parallel and had an error so the change is aborted) and
		// WaitStatus was requested (which can happen if the
		// task handler sets its status explicitly) then keep it at
		// aborted so it can transition to Undo.
		return
	}
	t.waitedStatus = resultStatus
	t.changeStatus(old, WaitStatus)
}

// WaitedStatus returns the status the Task should return to once the current WaitStatus
// has been resolved.
func (t *Task) WaitedStatus() Status {
	t.state.reading()
	return t.waitedStatus
}

// IsClean returns whether the task has been cleaned. See SetClean.
func (t *Task) IsClean() bool {
	t.state.reading()
	return t.clean
}

// SetClean flags the task as clean after any left over data was removed.
//
// Cleaning a task must only be done after the change is ready.
func (t *Task) SetClean() {
	t.state.writing()
	if t.clean {
		return
	}
	t.clean = true
	chg := t.Change()
	if chg != nil {
		chg.taskCleanChanged()
	}
}

// State returns the system State
func (t *Task) State() *State {
	return t.state
}

// Change returns the change the task is registered with.
func (t *Task) Change() *Change {
	t.state.reading()
	return t.state.changes[t.change]
}

// Progress returns the current progress for the task.
// If progress is not explicitly set, it returns
// (0, 1) if the status is DoStatus and (1, 1) otherwise.
func (t *Task) Progress() (label string, done, total int) {
	t.state.reading()
	if t.progress == nil {
		if t.Status() == DoStatus {
			return "", 0, 1
		}
		return "", 1, 1
	}
	return t.progress.Label, t.progress.Done, t.progress.Total
}

// SetProgress sets the task progress to cur out of total steps.
func (t *Task) SetProgress(label string, done, total int) {
	// Only mark state for checkpointing if progress is final.
	if total > 0 && done == total {
		t.state.writing()
	} else {
		t.state.reading()
	}
	if total <= 0 || done > total {
		// Doing math wrong is easy. Be conservative.
		t.progress = nil
	} else {
		t.progress = &progress{Label: label, Done: done, Total: total}
	}
}

// SpawnTime returns the time when the change was created.
func (t *Task) SpawnTime() time.Time {
	t.state.reading()
	return t.spawnTime
}

// ReadyTime returns the time when the change became ready.
func (t *Task) ReadyTime() time.Time {
	t.state.reading()
	return t.readyTime
}

// AtTime returns the time at which the task is scheduled to run. A zero time means no special schedule, i.e. run as soon as prerequisites are met.
func (t *Task) AtTime() time.Time {
	t.state.reading()
	return t.atTime
}

func (t *Task) accumulateDoingTime(duration time.Duration) {
	t.state.writing()
	t.doingTime += duration
}

func (t *Task) accumulateUndoingTime(duration time.Duration) {
	t.state.writing()
	t.undoingTime += duration
}

func (t *Task) DoingTime() time.Duration {
	t.state.reading()
	return t.doingTime
}

func (t *Task) UndoingTime() time.Duration {
	t.state.reading()
	return t.undoingTime
}

const (
	// Messages logged in tasks are guaranteed to use the time formatted
	// per RFC3339 plus the following strings as a prefix, so these may
	// be handled programmatically and parsed or stripped for presentation.
	LogInfo  = "INFO"
	LogError = "ERROR"
)

var timeNow = time.Now

func MockTime(now time.Time) (restore func()) {
	timeNow = func() time.Time { return now }
	return func() { timeNow = time.Now }
}

func (t *Task) addLog(kind, format string, args []interface{}) {
	if len(t.log) > 9 {
		copy(t.log, t.log[len(t.log)-9:])
		t.log = t.log[:9]
	}

	tstr := timeNow().Format(time.RFC3339)
	msg := tstr + " " + kind + " " + fmt.Sprintf(format, args...)
	t.log = append(t.log, msg)
	logger.Debug(msg)
}

// Log returns the most recent messages logged into the task.
//
// Only the most recent entries logged are returned, potentially with
// different behavior for different task statuses. How many entries
// are returned is an implementation detail and may change over time.
//
// Messages are prefixed with one of the known message kinds.
// See details about LogInfo and LogError.
//
// The returned slice should not be read from without the
// state lock held, and should not be written to.
func (t *Task) Log() []string {
	t.state.reading()
	return t.log
}

// Logf logs information about the progress of the task.
func (t *Task) Logf(format string, args ...interface{}) {
	t.state.writing()
	t.addLog(LogInfo, format, args)
}

// Errorf logs error information about the progress of the task.
func (t *Task) Errorf(format string, args ...interface{}) {
	t.state.writing()
	t.addLog(LogError, format, args)
}

// Set associates value with key for future consulting by managers.
// The provided value must properly marshal and unmarshal with encoding/json.
func (t *Task) Set(key string, value interface{}) {
	t.state.writing()
	t.data.set(key, value)
}

// Get unmarshals the stored value associated with the provided key
// into the value parameter.
func (t *Task) Get(key string, value interface{}) error {
	t.state.reading()
	return t.data.get(key, value)
}

// Has returns whether the provided key has an associated value.
func (t *Task) Has(key string) bool {
	t.state.reading()
	return t.data.has(key)
}

// Clear disassociates the value from key.
func (t *Task) Clear(key string) {
	t.state.writing()
	delete(t.data, key)
}

func addOnce(set []string, s string) []string {
	for _, cur := range set {
		if s == cur {
			return set
		}
	}
	return append(set, s)
}

// WaitFor registers another task as a requirement for t to make progress.
func (t *Task) WaitFor(another *Task) {
	t.state.writing()
	t.waitTasks = addOnce(t.waitTasks, another.id)
	another.haltTasks = addOnce(another.haltTasks, t.id)
}

// WaitAll registers all the tasks in the set as a requirement for t
// to make progress.
func (t *Task) WaitAll(ts *TaskSet) {
	for _, req := range ts.tasks {
		t.WaitFor(req)
	}
}

// WaitTasks returns the list of tasks registered for t to wait for.
func (t *Task) WaitTasks() []*Task {
	t.state.reading()
	return t.state.tasksIn(t.waitTasks)
}

// HaltTasks returns the list of tasks registered to wait for t.
func (t *Task) HaltTasks() []*Task {
	t.state.reading()
	return t.state.tasksIn(t.haltTasks)
}

// NumHaltTasks returns the number of tasks registered to wait for t.
func (t *Task) NumHaltTasks() int {
	return len(t.haltTasks)
}

// Lanes returns the lanes the task is in.
func (t *Task) Lanes() []int {
	t.state.reading()
	if len(t.lanes) == 0 {
		return []int{0}
	}
	return t.lanes
}

// JoinLane registers the task in the provided lane. Tasks in different lanes
// abort independently on errors. See Change.AbortLane for details.
func (t *Task) JoinLane(lane int) {
	t.state.writing()
	t.lanes = append(t.lanes, lane)
}

// At schedules the task, if it's not ready, to happen no earlier than when, if when is the zero time any previous special scheduling is suppressed.
func (t *Task) At(when time.Time) {
	t.state.writing()
	iszero := when.IsZero()
	if t.Status().Ready() && !iszero {
		return
	}
	t.atTime = when
	if !iszero {
		d := when.Sub(timeNow())
		if d < 0 {
			d = 0
		}
		t.state.EnsureBefore(d)
	}
}

// TaskSetEdge designates tasks inside a TaskSet for outside reference.
//
// This is useful to give tasks inside TaskSets a special meaning. It
// is used to mark e.g. the last task used for downloading a snap.
type TaskSetEdge string

// A TaskSet holds a set of tasks.
type TaskSet struct {
	tasks []*Task

	edges map[TaskSetEdge]*Task
}

// NewTaskSet returns a new TaskSet comprising the given tasks.
func NewTaskSet(tasks ...*Task) *TaskSet {
	// we init all members of TaskSet so that `go vet` will not complain
	return &TaskSet{tasks, nil}
}

// MaybeEdge returns the task marked with the given edge name or nil if no such
// task exists.
func (ts TaskSet) MaybeEdge(e TaskSetEdge) *Task {
	return ts.edges[e]
}

// Edge returns the task marked with the given edge name or an error.
func (ts TaskSet) Edge(e TaskSetEdge) (*Task, error) {
	t := ts.MaybeEdge(e)
	if t == nil {
		return nil, fmt.Errorf("internal error: missing %q edge in task set", e)
	}
	return t, nil
}

// WaitFor registers a task as a requirement for the tasks in the set
// to make progress.
func (ts TaskSet) WaitFor(another *Task) {
	for _, t := range ts.tasks {
		t.WaitFor(another)
	}
}

// WaitAll registers all the tasks in the argument set as requirements for ts
// the target set to make progress.
func (ts *TaskSet) WaitAll(anotherTs *TaskSet) {
	for _, req := range anotherTs.tasks {
		ts.WaitFor(req)
	}
}

// AddTask adds the task to the task set.
func (ts *TaskSet) AddTask(task *Task) {
	for _, t := range ts.tasks {
		if t == task {
			return
		}
	}
	ts.tasks = append(ts.tasks, task)
}

// MarkEdge marks the given task as a specific edge. Any pre-existing
// edge mark will be overridden.
func (ts *TaskSet) MarkEdge(task *Task, edge TaskSetEdge) {
	if task == nil {
		panic(fmt.Sprintf("cannot set edge %q with nil task", edge))
	}
	if ts.edges == nil {
		ts.edges = make(map[TaskSetEdge]*Task)
	}
	ts.edges[edge] = task
}

// AddAll adds all the tasks in the argument set to the target set ts.
func (ts *TaskSet) AddAll(anotherTs *TaskSet) {
	for _, t := range anotherTs.tasks {
		ts.AddTask(t)
	}
}

// AddAllWithEdges adds all the tasks in the argument set to the target
// set ts and also adds all TaskSetEdges. Duplicated TaskSetEdges are
// an error.
func (ts *TaskSet) AddAllWithEdges(anotherTs *TaskSet) error {
	ts.AddAll(anotherTs)
	for edge, t := range anotherTs.edges {
		if tex, ok := ts.edges[edge]; ok && t != tex {
			return fmt.Errorf("cannot add taskset: duplicated edge %q", edge)
		}
		ts.MarkEdge(t, edge)
	}
	return nil
}

// JoinLane adds all the tasks in the current taskset to the given lane.
func (ts *TaskSet) JoinLane(lane int) {
	for _, t := range ts.tasks {
		t.JoinLane(lane)
	}
}

// Tasks returns the tasks in the task set.
func (ts TaskSet) Tasks() []*Task {
	// Return something mutable, just like every other Tasks method.
	return append([]*Task(nil), ts.tasks...)
}
