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
	"sync"

	"github.com/snapcore/snapd/overlord/state"
)

// SnapContexts maintains a map of snap contexts
type SnapContexts struct {
	contextsMutex sync.RWMutex
	contexts      map[string]*Context
}

func newSnapContexts(s *state.State) *SnapContexts {
	// TODO: restore from state
	return &SnapContexts{}
}

func (m *SnapContexts) addContext(c *Context) {
	contextID := c.ID()
	m.contextsMutex.Lock()
	m.contexts[contextID] = c
	m.contextsMutex.Unlock()
}

// Delete removes context mapping for given snap.
func (m *SnapContexts) DeleteHookContext(name string) {
	m.contextsMutex.Lock()
	delete(m.contexts, name)
	m.contextsMutex.Unlock()
}

// CreateSnapContext creates a new context mapping for given snap name
func (m *SnapContexts) CreateSnapContext(name string) (*Context, error) {
	return nil, nil
}
