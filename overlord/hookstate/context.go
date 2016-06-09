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
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
)

type Context struct {
	task  *state.Task
	setup hookSetup
}

// newContext returns a new context with the given task and setup.
func newContext(task *state.Task, setup hookSetup) *Context {
	return &Context{
		task:  task,
		setup: setup,
	}
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
	c.task.State().Lock()
}

// Unlock releases the state lock for this context.
func (c *Context) Unlock() {
	c.task.State().Unlock()
}

// Set associates value with key.
// The provided value must properly marshal and unmarshal with encoding/json.
func (c *Context) Set(key string, value interface{}) {
	c.task.Set(key, value)
}

// Get unmarshals the stored value associated with the provided key into the
// value parameter.
func (c *Context) Get(key string, value interface{}) error {
	return c.task.Get(key, value)
}
