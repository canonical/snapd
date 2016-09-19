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
	"io"
	"sync"
)

type PersistentState struct {
	modules map[string]string
}

type KModDb struct {
	PersistentState
	mu            sync.Mutex
	uniqueModules map[string]int
}

func (s *KModDb) Lock() error {
	s.mu.Lock()
	return nil
}

func (s *KModDb) Unlock() error {
	s.mu.Unlock()
	return nil
}

func (s *KModDb) AddModules(snapName string, modules []string) {
}

func (s *KModDb) Remove(snapName string) error {
	return nil
}

// This method returns the list of kernel modules needed by all snaps, deduplicated.
func (s *KModDb) GetUniqueModulesList() []string {
	return []string{}
}

// New returns a new empty state.
func NewKModDb() *KModDb {
	return &KModDb{
		PersistentState: PersistentState{make(map[string]string)},
	}
}

func ReadDb(r io.Reader) (*KModDb, error) {
	s := new(KModDb)
	d := json.NewDecoder(r)
	err := d.Decode(&s)
	if err != nil {
		return nil, err
	}
	return s, nil
}

func (db *KModDb) WriteDb(w io.Writer) error {
	return nil
}

func (db *KModDb) writeModulesFile(modulesFile string) error {
	return writeModulesFile(db.GetUniqueModulesList(), modulesFile)
}
