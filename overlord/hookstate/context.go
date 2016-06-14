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
	"fmt"
	"sync"

	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
)

const (
	contextDataKey = "hook-context"
)

// Context represents the context under which a given hook is running.
type Context struct {
	task  *state.Task
	setup hookSetup

	// A local copy of the data which is also saved into the task.
	mutex   sync.Mutex
	data    map[string]interface{}
	dataKey string
}

// newContext returns a new context with the given task and hook setup.
func newContext(task *state.Task, setup hookSetup) *Context {
	context := &Context{
		task:    task,
		setup:   setup,
		dataKey: setup.Hook + "-" + contextDataKey,
	}

	// Initialize hook context data from the task
	task.State().Lock()
	defer task.State().Unlock()
	if err := task.Get(context.dataKey, &context.data); err != nil {
		context.data = make(map[string]interface{})
	}

	return context
}

// SnapName returns the name of the snap containing the hook.
func (c *Context) SnapName() string {
	return c.setup.Snap
}

// SnapRevision returns the revision of the snap containing the hook.
func (c *Context) SnapRevision() snap.Revision {
	return c.setup.Revision
}

// HookName returns the name of the hook in this context.
func (c *Context) HookName() string {
	return c.setup.Hook
}

// Lock acquires the state lock for this context (required for Set/Get).
func (c *Context) Lock() {
	c.mutex.Lock()
}

// Unlock releases the state lock for this context.
func (c *Context) Unlock() {
	c.mutex.Unlock()
}

// Set associates value with key.
// The provided value must properly marshal and unmarshal with encoding/json.
func (c *Context) Set(key string, value interface{}) {
	c.data[key] = value

	// Save hook context data to the task
	c.task.State().Lock()
	defer c.task.State().Unlock()
	c.task.Set(c.dataKey, c.data)
}

// Get returns the stored value associated with the provided key.
func (c *Context) Get(key string) (interface{}, error) {
	value, ok := c.data[key]
	if !ok {
		return nil, fmt.Errorf("no such key: %q", key)
	}

	return value, nil
}
