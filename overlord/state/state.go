// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2023 Canonical Ltd
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

// Package state implements the representation of system state.
package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/snapcore/snapd/logger"
)

// A Backend is used by State to checkpoint on every unlock operation
// and to mediate requests to ensure the state sooner or request restarts.
type Backend interface {
	Checkpoint(data []byte) error
	EnsureBefore(d time.Duration)
}

type customData map[string]*json.RawMessage

func (data customData) get(key string, value interface{}) error {
	entryJSON := data[key]
	if entryJSON == nil {
		return &NoStateError{Key: key}
	}
	err := json.Unmarshal(*entryJSON, value)
	if err != nil {
		return fmt.Errorf("internal error: could not unmarshal state entry %q: %v", key, err)
	}
	return nil
}

func (data customData) has(key string) bool {
	return data[key] != nil
}

func (data customData) set(key string, value interface{}) {
	if value == nil {
		delete(data, key)
		return
	}
	serialized, err := json.Marshal(value)
	if err != nil {
		logger.Panicf("internal error: could not marshal value for state entry %q: %v", key, err)
	}
	entryJSON := json.RawMessage(serialized)
	data[key] = &entryJSON
}

// State represents an evolving system state that persists across restarts.
//
// The State is concurrency-safe, and all reads and writes to it must be
// performed with the state locked. It's a runtime error (panic) to perform
// operations without it.
//
// The state is persisted on every unlock operation via the StateBackend
// it was initialized with.
type State struct {
	mu  sync.Mutex
	muC int32

	lastTaskId   int
	lastChangeId int
	lastLaneId   int
	lastNoticeId int
	// lastHandlerId is not serialized, it's only used during runtime
	// for registering runtime callbacks
	lastHandlerId int

	backend  Backend
	data     customData
	changes  map[string]*Change
	tasks    map[string]*Task
	warnings map[string]*Warning
	notices  map[noticeKey]*Notice

	noticeCond *sync.Cond

	modified bool

	cache map[interface{}]interface{}

	pendingChangeByAttr map[string]func(*Change) bool

	// task/changes observing
	taskHandlers   map[int]func(t *Task, old, new Status)
	changeHandlers map[int]func(chg *Change, old, new Status)
}

// New returns a new empty state.
func New(backend Backend) *State {
	st := &State{
		backend:             backend,
		data:                make(customData),
		changes:             make(map[string]*Change),
		tasks:               make(map[string]*Task),
		warnings:            make(map[string]*Warning),
		notices:             make(map[noticeKey]*Notice),
		modified:            true,
		cache:               make(map[interface{}]interface{}),
		pendingChangeByAttr: make(map[string]func(*Change) bool),
		taskHandlers:        make(map[int]func(t *Task, old Status, new Status)),
		changeHandlers:      make(map[int]func(chg *Change, old Status, new Status)),
	}
	st.noticeCond = sync.NewCond(st) // use State.Lock and State.Unlock
	return st
}

// Modified returns whether the state was modified since the last checkpoint.
func (s *State) Modified() bool {
	return s.modified
}

// Lock acquires the state lock.
func (s *State) Lock() {
	s.mu.Lock()
	atomic.AddInt32(&s.muC, 1)
}

func (s *State) reading() {
	if atomic.LoadInt32(&s.muC) != 1 {
		panic("internal error: accessing state without lock")
	}
}

func (s *State) writing() {
	s.modified = true
	if atomic.LoadInt32(&s.muC) != 1 {
		panic("internal error: accessing state without lock")
	}
}

func (s *State) unlock() {
	atomic.AddInt32(&s.muC, -1)
	s.mu.Unlock()
}

type marshalledState struct {
	Data     map[string]*json.RawMessage `json:"data"`
	Changes  map[string]*Change          `json:"changes"`
	Tasks    map[string]*Task            `json:"tasks"`
	Warnings []*Warning                  `json:"warnings,omitempty"`
	Notices  []*Notice                   `json:"notices,omitempty"`

	LastChangeId int `json:"last-change-id"`
	LastTaskId   int `json:"last-task-id"`
	LastLaneId   int `json:"last-lane-id"`
	LastNoticeId int `json:"last-notice-id"`
}

// MarshalJSON makes State a json.Marshaller
func (s *State) MarshalJSON() ([]byte, error) {
	s.reading()
	return json.Marshal(marshalledState{
		Data:     s.data,
		Changes:  s.changes,
		Tasks:    s.tasks,
		Warnings: s.flattenWarnings(),
		Notices:  s.flattenNotices(nil),

		LastTaskId:   s.lastTaskId,
		LastChangeId: s.lastChangeId,
		LastLaneId:   s.lastLaneId,
		LastNoticeId: s.lastNoticeId,
	})
}

// UnmarshalJSON makes State a json.Unmarshaller
func (s *State) UnmarshalJSON(data []byte) error {
	s.writing()
	var unmarshalled marshalledState
	err := json.Unmarshal(data, &unmarshalled)
	if err != nil {
		return err
	}
	s.data = unmarshalled.Data
	s.changes = unmarshalled.Changes
	s.tasks = unmarshalled.Tasks
	s.unflattenWarnings(unmarshalled.Warnings)
	s.unflattenNotices(unmarshalled.Notices)
	s.lastChangeId = unmarshalled.LastChangeId
	s.lastTaskId = unmarshalled.LastTaskId
	s.lastLaneId = unmarshalled.LastLaneId
	s.lastNoticeId = unmarshalled.LastNoticeId
	// backlink state again
	for _, t := range s.tasks {
		t.state = s
	}
	for _, chg := range s.changes {
		chg.state = s
		chg.finishUnmarshal()
	}
	return nil
}

func (s *State) checkpointData() []byte {
	data, err := json.Marshal(s)
	if err != nil {
		// this shouldn't happen, because the actual delicate serializing happens at various Set()s
		logger.Panicf("internal error: could not marshal state for checkpointing: %v", err)
	}
	return data
}

// unlock checkpoint retry parameters (5 mins of retries by default)
var (
	unlockCheckpointRetryMaxTime  = 5 * time.Minute
	unlockCheckpointRetryInterval = 3 * time.Second
)

// Unlocker returns a closure that will unlock and checkpoint the state and
// in turn return a function to relock it.
func (s *State) Unlocker() (unlock func() (relock func())) {
	return func() func() {
		s.Unlock()
		return s.Lock
	}
}

// Unlock releases the state lock and checkpoints the state.
// It does not return until the state is correctly checkpointed.
// After too many unsuccessful checkpoint attempts, it panics.
func (s *State) Unlock() {
	defer s.unlock()

	if !s.modified || s.backend == nil {
		return
	}

	data := s.checkpointData()
	var err error
	start := time.Now()
	for time.Since(start) <= unlockCheckpointRetryMaxTime {
		if err = s.backend.Checkpoint(data); err == nil {
			s.modified = false
			return
		}
		time.Sleep(unlockCheckpointRetryInterval)
	}
	logger.Panicf("cannot checkpoint even after %v of retries every %v: %v", unlockCheckpointRetryMaxTime, unlockCheckpointRetryInterval, err)
}

// EnsureBefore asks for an ensure pass to happen sooner within duration from now.
func (s *State) EnsureBefore(d time.Duration) {
	if s.backend != nil {
		s.backend.EnsureBefore(d)
	}
}

// ErrNoState represents the case of no state entry for a given key.
var ErrNoState = errors.New("no state entry for key")

// NoStateError represents the case where no state could be found for a given key.
type NoStateError struct {
	// Key is the key for which no state could be found.
	Key string
}

func (e *NoStateError) Error() string {
	var keyMsg string
	if e.Key != "" {
		keyMsg = fmt.Sprintf(" %q", e.Key)
	}

	return fmt.Sprintf("no state entry for key%s", keyMsg)
}

// Is returns true if the error is of type *NoStateError or equal to ErrNoState.
// NoStateError's key isn't compared between errors.
func (e *NoStateError) Is(err error) bool {
	_, ok := err.(*NoStateError)
	return ok || errors.Is(err, ErrNoState)
}

// Get unmarshals the stored value associated with the provided key
// into the value parameter.
// It returns ErrNoState if there is no entry for key.
func (s *State) Get(key string, value interface{}) error {
	s.reading()
	return s.data.get(key, value)
}

// Has returns whether the provided key has an associated value.
func (s *State) Has(key string) bool {
	s.reading()
	return s.data.has(key)
}

// Set associates value with key for future consulting by managers.
// The provided value must properly marshal and unmarshal with encoding/json.
func (s *State) Set(key string, value interface{}) {
	s.writing()
	s.data.set(key, value)
}

// Cached returns the cached value associated with the provided key.
// It returns nil if there is no entry for key.
func (s *State) Cached(key interface{}) interface{} {
	s.reading()
	return s.cache[key]
}

// Cache associates value with key for future consulting by managers.
// The cached value is not persisted.
func (s *State) Cache(key, value interface{}) {
	s.reading() // Doesn't touch persisted data.
	if value == nil {
		delete(s.cache, key)
	} else {
		s.cache[key] = value
	}
}

// NewChange adds a new change to the state.
func (s *State) NewChange(kind, summary string) *Change {
	s.writing()
	s.lastChangeId++
	id := strconv.Itoa(s.lastChangeId)
	chg := newChange(s, id, kind, summary)
	s.changes[id] = chg
	// Add change-update notice for newly spawned change
	// NOTE: Implies State.writing()
	if err := chg.addNotice(); err != nil {
		logger.Panicf(`internal error: failed to add "change-update" notice for new change: %v`, err)
	}
	return chg
}

// NewLane creates a new lane in the state.
func (s *State) NewLane() int {
	s.writing()
	s.lastLaneId++
	return s.lastLaneId
}

// Changes returns all changes currently known to the state.
func (s *State) Changes() []*Change {
	s.reading()
	res := make([]*Change, 0, len(s.changes))
	for _, chg := range s.changes {
		res = append(res, chg)
	}
	return res
}

// Change returns the change for the given ID.
func (s *State) Change(id string) *Change {
	s.reading()
	return s.changes[id]
}

// NewTask creates a new task.
// It usually will be registered with a Change using AddTask or
// through a TaskSet.
func (s *State) NewTask(kind, summary string) *Task {
	s.writing()
	s.lastTaskId++
	id := strconv.Itoa(s.lastTaskId)
	t := newTask(s, id, kind, summary)
	s.tasks[id] = t
	return t
}

// Tasks returns all tasks currently known to the state and linked to changes.
func (s *State) Tasks() []*Task {
	s.reading()
	res := make([]*Task, 0, len(s.tasks))
	for _, t := range s.tasks {
		if t.Change() == nil { // skip unlinked tasks
			continue
		}
		res = append(res, t)
	}
	return res
}

// Task returns the task for the given ID if the task has been linked to a change.
func (s *State) Task(id string) *Task {
	s.reading()
	t := s.tasks[id]
	if t == nil || t.Change() == nil {
		return nil
	}
	return t
}

// TaskCount returns the number of tasks that currently exist in the state,
// whether linked to a change or not.
func (s *State) TaskCount() int {
	s.reading()
	return len(s.tasks)
}

func (s *State) tasksIn(tids []string) []*Task {
	res := make([]*Task, len(tids))
	for i, tid := range tids {
		res[i] = s.tasks[tid]
	}
	return res
}

// RegisterPendingChangeByAttr registers predicates that will be invoked by
// Prune on changes with the specified attribute set to check whether even if
// they meet the time criteria they must not be aborted yet.
func (s *State) RegisterPendingChangeByAttr(attr string, f func(*Change) bool) {
	s.pendingChangeByAttr[attr] = f
}

// Prune does several cleanup tasks to the in-memory state:
//
//   - it removes changes that became ready for more than pruneWait and aborts
//     tasks spawned for more than abortWait unless prevented by predicates
//     registered with RegisterPendingChangeByAttr.
//
//   - it removes tasks unlinked to changes after pruneWait. When there are more
//     changes than the limit set via "maxReadyChanges" those changes in ready
//     state will also removed even if they are below the pruneWait duration.
//
//   - it removes expired warnings and notices.
func (s *State) Prune(startOfOperation time.Time, pruneWait, abortWait time.Duration, maxReadyChanges int) {
	now := time.Now()
	pruneLimit := now.Add(-pruneWait)
	abortLimit := now.Add(-abortWait)

	// sort from oldest to newest
	changes := s.Changes()
	sort.Sort(byReadyTime(changes))

	readyChangesCount := 0
	for i := range changes {
		// changes are sorted (not-ready sorts first)
		// so we know we can iterate in reverse and break once we
		// find a ready time of "zero"
		chg := changes[len(changes)-i-1]
		if chg.ReadyTime().IsZero() {
			break
		}
		readyChangesCount++
	}

	for k, w := range s.warnings {
		if w.ExpiredBefore(now) {
			delete(s.warnings, k)
		}
	}

	for k, n := range s.notices {
		if n.expired(now) {
			delete(s.notices, k)
		}
	}

NextChange:
	for _, chg := range changes {
		readyTime := chg.ReadyTime()
		spawnTime := chg.SpawnTime()
		if spawnTime.Before(startOfOperation) {
			spawnTime = startOfOperation
		}
		if readyTime.IsZero() {
			if spawnTime.Before(pruneLimit) && len(chg.Tasks()) == 0 {
				chg.Abort()
				delete(s.changes, chg.ID())
			} else if spawnTime.Before(abortLimit) {
				for attr, pending := range s.pendingChangeByAttr {
					if chg.Has(attr) && pending(chg) {
						continue NextChange
					}
				}
				chg.AbortUnreadyLanes()
			}
			continue
		}
		// change old or we have too many changes
		if readyTime.Before(pruneLimit) || readyChangesCount > maxReadyChanges {
			s.writing()
			for _, t := range chg.Tasks() {
				delete(s.tasks, t.ID())
			}
			delete(s.changes, chg.ID())
			readyChangesCount--
		}
	}

	for tid, t := range s.tasks {
		// TODO: this could be done more aggressively
		if t.Change() == nil && t.SpawnTime().Before(pruneLimit) {
			s.writing()
			delete(s.tasks, tid)
		}
	}
}

// GetMaybeTimings implements timings.GetSaver
func (s *State) GetMaybeTimings(timings interface{}) error {
	if err := s.Get("timings", timings); err != nil && !errors.Is(err, ErrNoState) {
		return err
	}
	return nil
}

// AddTaskStatusChangedHandler adds a callback function that will be invoked
// whenever tasks change status.
// NOTE: Callbacks registered this way may be invoked in the context
// of the taskrunner, so the callbacks should be as simple as possible, and return
// as quickly as possible, and should avoid the use of i/o code or blocking, as this
// will stop the entire task system.
func (s *State) AddTaskStatusChangedHandler(f func(t *Task, old, new Status)) (id int) {
	// We are reading here as we want to ensure access to the state is serialized,
	// and not writing as we are not changing the part of state that goes on the disk.
	s.reading()
	id = s.lastHandlerId
	s.lastHandlerId++
	s.taskHandlers[id] = f
	return id
}

func (s *State) RemoveTaskStatusChangedHandler(id int) {
	s.reading()
	delete(s.taskHandlers, id)
}

func (s *State) notifyTaskStatusChangedHandlers(t *Task, old, new Status) {
	s.reading()
	for _, f := range s.taskHandlers {
		f(t, old, new)
	}
}

// AddChangeStatusChangedHandler adds a callback function that will be invoked
// whenever a Change changes status.
// NOTE: Callbacks registered this way may be invoked in the context
// of the taskrunner, so the callbacks should be as simple as possible, and return
// as quickly as possible, and should avoid the use of i/o code or blocking, as this
// will stop the entire task system.
func (s *State) AddChangeStatusChangedHandler(f func(chg *Change, old, new Status)) (id int) {
	// We are reading here as we want to ensure access to the state is serialized,
	// and not writing as we are not changing the part of state that goes on the disk.
	s.reading()
	id = s.lastHandlerId
	s.lastHandlerId++
	s.changeHandlers[id] = f
	return id
}

func (s *State) RemoveChangeStatusChangedHandler(id int) {
	s.reading()
	delete(s.changeHandlers, id)
}

func (s *State) notifyChangeStatusChangedHandlers(chg *Change, old, new Status) {
	s.reading()
	for _, f := range s.changeHandlers {
		f(chg, old, new)
	}
}

// SaveTimings implements timings.GetSaver
func (s *State) SaveTimings(timings interface{}) {
	s.Set("timings", timings)
}

// ReadState returns the state deserialized from r.
func ReadState(backend Backend, r io.Reader) (*State, error) {
	s := new(State)
	s.Lock()
	defer s.unlock()
	d := json.NewDecoder(r)
	err := d.Decode(&s)
	if err != nil {
		return nil, fmt.Errorf("cannot read state: %s", err)
	}
	s.backend = backend
	s.noticeCond = sync.NewCond(s)
	s.modified = false
	s.cache = make(map[interface{}]interface{})
	s.pendingChangeByAttr = make(map[string]func(*Change) bool)
	s.changeHandlers = make(map[int]func(chg *Change, old Status, new Status))
	s.taskHandlers = make(map[int]func(t *Task, old Status, new Status))
	return s, err
}
