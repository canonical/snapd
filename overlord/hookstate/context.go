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
	"encoding/json"
	"fmt"

	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
)

// Context represents the context under which a given hook is running.
type Context struct {
	task  *state.Task
	setup hookSetup
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
	var data map[string]*json.RawMessage
	if err := c.task.Get("hook-context", &data); err != nil {
		data = make(map[string]*json.RawMessage)
	}

	marshalledValue, err := json.Marshal(value)
	if err != nil {
		panic(fmt.Sprintf("internal error: cannot marshal context value for %q: %s", key, err))
	}
	raw := json.RawMessage(marshalledValue)
	data[key] = &raw

	c.task.Set("hook-context", data)
}

// Get unmarshals the stored value associated with the provided key into the
// value parameter.
func (c *Context) Get(key string, value interface{}) error {
	var data map[string]*json.RawMessage
	if err := c.task.Get("hook-context", &data); err != nil {
		return err
	}

	raw, ok := data[key]
	if !ok {
		return state.ErrNoState
	}

	err := json.Unmarshal([]byte(*raw), &value)
	if err != nil {
		return fmt.Errorf("cannot unmarshal context value for %q: %s", key, err)
	}

	return nil
}
