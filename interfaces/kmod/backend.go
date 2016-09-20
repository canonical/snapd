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
	"bytes"
	"fmt"
	"os"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/snap"
)

// Backend is responsible for maintaining kernel modules
type Backend struct {
	kmoddb *KModDb
}

func NewKModBackend(statefile string) (b *Backend) {
	b = &Backend{
		kmoddb: &KModDb{},
	}
	if osutil.FileExists(dirs.SnapKModStateFile) {
		var err error
		b.kmoddb, err = b.loadState(dirs.SnapKModStateFile)
		if err != nil {
			panic(fmt.Errorf("Failed to load KMod state: %v", err))
		}
	}
	return b
}

// Name returns the name of the backend.
func (b *Backend) Name() string {
	return "kmod"
}

func (b *Backend) Setup(snapInfo *snap.Info, devMode bool, repo *interfaces.Repository) error {
	snapName := snapInfo.Name()
	// Get the snippets that apply to this snap
	snippets, err := repo.SecuritySnippetsForSnap(snapInfo.Name(), interfaces.SecurityKMod)
	if err != nil {
		return fmt.Errorf("cannot obtain kmod security snippets for snap %q: %s", snapName, err)
	}
	if len(snippets) == 0 {
		return nil
	}

	var candidateModules [][]byte
	candidateModules, err = b.processSnipets(snapInfo, snippets)

	b.kmoddb.Lock()
	defer b.kmoddb.Unlock()

	b.kmoddb.AddModules(snapName, candidateModules)
	err = b.kmoddb.WriteDb(dirs.SnapKModStateFile)
	if err != nil {
		return err
	}

	modules := b.kmoddb.GetUniqueModulesList()
	err = writeModulesFile(modules, dirs.SnapKModModulesFile)
	if err != nil {
		return err
	}
	return nil
}

func (b *Backend) Remove(snapName string) error {
	b.kmoddb.Lock()
	defer b.kmoddb.Unlock()

	if removed := b.kmoddb.Remove(snapName); removed {
		if err := b.kmoddb.WriteDb(dirs.SnapKModStateFile); err != nil {
			return err
		}
		modules := b.kmoddb.GetUniqueModulesList()
		return writeModulesFile(modules, dirs.SnapKModModulesFile)
	}
	return nil
}

func (b *Backend) processSnipets(snapInfo *snap.Info, snippets map[string][][]byte) (candidateModules [][]byte, err error) {
	for _, appInfo := range snapInfo.Apps {
		for _, snippet := range snippets[appInfo.SecurityTag()] {
			// split snippet by newline to get the list of modules
			individualLines := bytes.Split(snippet, []byte{'\n'})
			for _, line := range individualLines {
				l := bytes.Trim(line, " \r")
				// ignore empty lines and comments
				if len(l) > 0 && l[0] != '#' {
					candidateModules = append(candidateModules, line)
				}
			}
		}
	}
	return candidateModules, nil
}

func (b *Backend) loadState(statefile string) (s *KModDb, err error) {
	r, err := os.Open(statefile)
	if err != nil {
		return nil, fmt.Errorf("cannot read the state file: %s", err)
	}
	defer r.Close()

	s, err = ReadDb(r)
	return s, err
}
