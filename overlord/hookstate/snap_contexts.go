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
	"os"
	"path/filepath"
	"sync"

	"fmt"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/state"
)

// SnapContexts maintains a map of snap contexts
type SnapContexts struct {
	state           *state.State
	contextsMutex   sync.RWMutex
	contexts        map[string]*Context
	snapToContextID map[string]string
}

func newSnapContexts(s *state.State) *SnapContexts {
	dir := dirs.SnapContextsDir
	if err := os.MkdirAll(dir, 0700); err != nil {
		panic(fmt.Errorf("cannot create directory for snap contexts %q: %s", dir, err))
	}
	sc := &SnapContexts{
		state:           s,
		contexts:        make(map[string]*Context),
		snapToContextID: make(map[string]string),
	}
	if err := sc.ensureState(); err != nil {
		panic(fmt.Errorf("Failed to restore state: %q", err))
	}
	return sc
}

func (m *SnapContexts) ensureState() error {
	m.state.Lock()
	if err := m.state.Get("contexts", &m.snapToContextID); err != nil && err != state.ErrNoState {
		return fmt.Errorf("Failed to get contexts: %q", err)
	}
	m.state.Unlock()

	// Iterate over contexts retrieved from the state and populate
	// contexts map of SnapContexts struct.
	// Ensure the filesystem (/var/lib/snapd/contexts/) is in sync.
	content := make(map[string]*osutil.FileState)
	for snapName, contextID := range m.snapToContextID {
		fstate := osutil.FileState{
			Content: []byte(contextID),
			Mode:    0600,
		}
		content[fmt.Sprintf("snap.%s", snapName)] = &fstate
		m.contexts[contextID] = NewContextWithID(nil, &HookSetup{Snap: snapName}, nil, contextID)
	}
	dir := dirs.SnapContextsDir
	_, _, err := osutil.EnsureDirState(dir, "snap.*", content)
	return err
}

func (m *SnapContexts) addContext(c *Context) {
	contextID := c.ID()
	m.contextsMutex.Lock()
	m.contexts[contextID] = c
	m.snapToContextID[c.setup.Snap] = contextID
	m.contextsMutex.Unlock()
	m.state.Lock()
	m.state.Set("contexts", &m.snapToContextID)
	m.state.Unlock()
}

// DeleteSnapContext removes context mapping for given snap.
func (m *SnapContexts) DeleteSnapContext(snapName string) {
	m.contextsMutex.Lock()
	if contextID, ok := m.snapToContextID[snapName]; ok {
		delete(m.contexts, contextID)
		delete(m.snapToContextID, snapName)
	}
	m.contextsMutex.Unlock()
}

// CreateSnapContext creates a new context mapping for given snap name
func (m *SnapContexts) CreateSnapContext(snapName string) (*Context, error) {
	context, err := NewContext(nil, &HookSetup{Snap: snapName}, nil)
	if err != nil {
		return nil, err
	}
	path := filepath.Join(dirs.SnapContextsDir, fmt.Sprintf("snap.%s", snapName))
	fstate := osutil.FileState{
		Content: []byte(context.ID()),
		Mode:    0600,
	}
	if err = osutil.EnsureFileState(path, &fstate); err != nil {
		return nil, err
	}
	m.addContext(context)
	return context, nil
}
