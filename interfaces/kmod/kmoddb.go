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

package kmod

import (
	"encoding/json"
	"fmt"
	"io"
	"sync"

	"github.com/snapcore/snapd/osutil"
)

type persistentState struct {
	modules map[string][][]byte
}

type KModDb struct {
	persistentState
	mu             sync.Mutex
	moduleUseCount map[string]int
}

func (s *KModDb) Lock() error {
	s.mu.Lock()
	return nil
}

func (s *KModDb) Unlock() error {
	s.mu.Unlock()
	return nil
}

func (db *KModDb) AddModules(snapName string, modules [][]byte) {
	db.modules[snapName] = modules
	for _, mod := range modules {
		db.moduleUseCount[string(mod)]++
	}
}

func (db *KModDb) Remove(snapName string) error {
	if mods, ok := db.modules[snapName]; ok {
		for _, mod := range mods {
			if db.moduleUseCount[string(mod)] <= 1 {
				delete(db.moduleUseCount, string(mod))
			}
		}
		delete(db.modules, snapName)
		return nil
	}
	return fmt.Errorf("Unknown snap: %s", snapName)
}

// This method returns the list of kernel modules needed by all snaps, deduplicated.
func (db *KModDb) GetUniqueModulesList() (mods [][]byte) {
	for k := range db.moduleUseCount {
		mods = append(mods, []byte(k))
	}
	return mods
}

// NewKModDb returns a new empty state.
func NewKModDb() *KModDb {
	return &KModDb{
		persistentState: persistentState{make(map[string][][]byte)},
	}
}

// ReadDb creates KModDb instance from json data.
func ReadDb(r io.Reader) (*KModDb, error) {
	db := new(KModDb)
	d := json.NewDecoder(r)
	err := d.Decode(&db.persistentState)
	if err != nil {
		return nil, err
	}

	// build moduleUseCount map
	for _, modules := range db.modules {
		for _, mod := range modules {
			db.moduleUseCount[string(mod)]++
		}
	}
	return db, nil
}

func (db *KModDb) WriteDb(path string) error {
	content, err := json.Marshal(db.modules)
	if err != nil {
		return fmt.Errorf("Failed to marshall kmod database: %v", err)
	}

	modulesFile := &osutil.FileState{
		Content: content,
		Mode:    0644,
	}

	if err := osutil.EnsureFileState(path, modulesFile); err == osutil.ErrSameState {
		return nil
	} else if err != nil {
		return err
	}
	return nil
}

func (db *KModDb) writeModulesFile(modulesFile string) error {
	return writeModulesFile(db.GetUniqueModulesList(), modulesFile)
}
