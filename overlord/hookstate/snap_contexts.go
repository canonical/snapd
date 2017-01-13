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
	contextsMutex   sync.RWMutex
	contexts        map[string]*Context
	snapToContextID map[string]string
}

func newSnapContexts(s *state.State) *SnapContexts {
	// TODO: restore from state
	dir := dirs.SnapContextsDir
	if err := os.MkdirAll(dir, 0755); err != nil {
		panic(fmt.Errorf("cannot create directory for snap contexts %q: %s", dir, err))
	}
	//_, removed, errEnsure := osutil.EnsureDirState(dir, glob, content)
	return &SnapContexts{
		contexts:        make(map[string]*Context),
		snapToContextID: make(map[string]string),
	}
}

func (m *SnapContexts) addContext(c *Context) {
	contextID := c.ID()
	m.contextsMutex.Lock()
	m.contexts[contextID] = c
	m.snapToContextID[c.setup.Snap] = contextID
	m.contextsMutex.Unlock()
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
	hooksup := &HookSetup{Snap: snapName}
	context, err := NewContext(nil, hooksup, nil)
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
