// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
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

package hookstate

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/snapcore/snapd/jsonutil"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/strutil"
)

// Context represents the context under which the snap is calling back into snapd.
// It is associated with a task when the callback is happening from within a hook,
// or otherwise considered an ephemeral context in that its associated data will
// be discarded once that individual call is finished.
type Context struct {
	task    *state.Task
	state   *state.State
	setup   *HookSetup
	id      string
	handler Handler

	cache  map[interface{}]interface{}
	onDone []func() error

	mutex        sync.Mutex
	mutexChecker int32
}

// NewContext returns a new context associated with the provided task or
// an ephemeral context if task is nil.
//
// A random ID is generated if contextID is empty.
func NewContext(task *state.Task, state *state.State, setup *HookSetup, handler Handler, contextID string) (*Context, error) {
	if contextID == "" {
		contextID = strutil.MakeRandomString(44)
	}

	return &Context{
		task:    task,
		state:   state,
		setup:   setup,
		id:      contextID,
		handler: handler,
		cache:   make(map[interface{}]interface{}),
	}, nil
}

// SnapName returns the name of the snap containing the hook.
func (c *Context) SnapName() string {
	return c.setup.Snap
}

// SnapRevision returns the revision of the snap containing the hook.
func (c *Context) SnapRevision() snap.Revision {
	return c.setup.Revision
}

// Task returns the task associated with the hook or (nil, false) if the context is ephemeral
// and task is not available.
func (c *Context) Task() (*state.Task, bool) {
	return c.task, c.task != nil
}

// HookName returns the name of the hook in this context.
func (c *Context) HookName() string {
	return c.setup.Hook
}

// Timeout returns the maximum time this hook can run
func (c *Context) Timeout() time.Duration {
	return c.setup.Timeout
}

// ID returns the ID of the context.
func (c *Context) ID() string {
	return c.id
}

// Handler returns the handler for this context
func (c *Context) Handler() Handler {
	return c.handler
}

// Lock acquires the lock for this context (required for Set/Get, Cache/Cached),
// and OnDone/Done).
func (c *Context) Lock() {
	c.mutex.Lock()
	c.state.Lock()
	atomic.AddInt32(&c.mutexChecker, 1)
}

// Unlock releases the lock for this context.
func (c *Context) Unlock() {
	atomic.AddInt32(&c.mutexChecker, -1)
	c.state.Unlock()
	c.mutex.Unlock()
}

func (c *Context) reading() {
	if atomic.LoadInt32(&c.mutexChecker) != 1 {
		panic("internal error: accessing context without lock")
	}
}

func (c *Context) writing() {
	if atomic.LoadInt32(&c.mutexChecker) != 1 {
		panic("internal error: accessing context without lock")
	}
}

// Set associates value with key. The provided value must properly marshal and
// unmarshal with encoding/json. Note that the context needs to be locked and
// unlocked by the caller.
func (c *Context) Set(key string, value interface{}) {
	c.writing()

	var data map[string]*json.RawMessage
	if c.IsEphemeral() {
		data, _ = c.cache["ephemeral-context"].(map[string]*json.RawMessage)
	} else {
		if err := c.task.Get("hook-context", &data); err != nil && err != state.ErrNoState {
			panic(fmt.Sprintf("internal error: cannot unmarshal context: %v", err))
		}
	}
	if data == nil {
		data = make(map[string]*json.RawMessage)
	}

	marshalledValue, err := json.Marshal(value)
	if err != nil {
		panic(fmt.Sprintf("internal error: cannot marshal context value for %q: %s", key, err))
	}
	raw := json.RawMessage(marshalledValue)
	data[key] = &raw

	if c.IsEphemeral() {
		c.cache["ephemeral-context"] = data
	} else {
		c.task.Set("hook-context", data)
	}
}

// Get unmarshals the stored value associated with the provided key into the
// value parameter. Note that the context needs to be locked/unlocked by the
// caller.
func (c *Context) Get(key string, value interface{}) error {
	c.reading()

	var data map[string]*json.RawMessage
	if c.IsEphemeral() {
		data, _ = c.cache["ephemeral-context"].(map[string]*json.RawMessage)
		if data == nil {
			return state.ErrNoState
		}
	} else {
		if err := c.task.Get("hook-context", &data); err != nil {
			return err
		}
	}

	raw, ok := data[key]
	if !ok {
		return state.ErrNoState
	}

	err := jsonutil.DecodeWithNumber(bytes.NewReader(*raw), &value)
	if err != nil {
		return fmt.Errorf("cannot unmarshal context value for %q: %s", key, err)
	}

	return nil
}

// State returns the state contained within the context
func (c *Context) State() *state.State {
	return c.state
}

// Cached returns the cached value associated with the provided key. It returns
// nil if there is no entry for key. Note that the context needs to be locked
// and unlocked by the caller.
func (c *Context) Cached(key interface{}) interface{} {
	c.reading()

	return c.cache[key]
}

// Cache associates value with key. The cached value is not persisted. Note that
// the context needs to be locked/unlocked by the caller.
func (c *Context) Cache(key, value interface{}) {
	c.writing()

	c.cache[key] = value
}

// OnDone requests the provided function to be run once the context knows it's
// complete. This can be called multiple times; each function will be called in
// the order in which they were added. Note that the context needs to be locked
// and unlocked by the caller.
func (c *Context) OnDone(f func() error) {
	c.writing()

	c.onDone = append(c.onDone, f)
}

// Done is called to notify the context that its hook has exited successfully.
// It will call all of the functions added in OnDone (even if one of them
// returns an error) and will return the first error encountered. Note that the
// context needs to be locked/unlocked by the caller.
func (c *Context) Done() error {
	c.reading()

	var firstErr error
	for _, f := range c.onDone {
		if err := f(); err != nil && firstErr == nil {
			firstErr = err
		}
	}

	return firstErr
}

func (c *Context) IsEphemeral() bool {
	return c.task == nil
}
