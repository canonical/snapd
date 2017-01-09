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

import "sync"

// Contexts maintains a map of contexts
type Contexts struct {
	contextsMutex sync.RWMutex
	contexts      map[string]*Context
}

// ContextsMap keeps tracks of hooks and snap contexts.
type ContextsMap struct {
	hookContexts Contexts
	snapContexts Contexts
}

// AddContext adds a new context mapping.
func (m *ContextMap) AddHookContext(c *Context) {
	contextID := c.ID()
	m.hookContexts.contextsMutex.Lock()
	m.hookContexts.contexts[contextID] = c
	m.hookContexts.contextsMutex.Unlock()
}

// Delete removes a context mapping.
func (m *ContextMap) DeleteHookContext(id string) {
	m.hookContexts.contextsMutex.Lock()
	delete(m.hookContexts.contexts, id)
	m.hookContexts.contextsMutex.Unlock()
}
