// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2017 Canonical Ltd
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

package backend

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/snapcore/snapd/dirs"
)

// Alias represents a command alias with a name and its application target.
type Alias struct {
	Name   string `json:"name"`
	Target string `json:"target"`
}

// MatchingAliases returns the subset of aliases that exist on disk and have the expected targets.

// UpdateAliases adds and removes the given aliases.
func (b Backend) UpdateAliases(add []*Alias, remove []*Alias) error {
	removed := make(map[string]bool, len(remove))
	for _, alias := range remove {
		err := os.Remove(filepath.Join(dirs.SnapBinariesDir, alias.Name))
		if err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("cannot remove alias symlink: %v", err)
		}
		removed[alias.Name] = true
		completer := filepath.Join(dirs.CompletersDir, alias.Name)
		target, err := os.Readlink(completer)
		if err != nil || target != alias.Target {
			continue
		}
		os.Remove(completer)
	}

	for _, alias := range add {
		p := filepath.Join(dirs.SnapBinariesDir, alias.Name)

		if !removed[alias.Name] {
			if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("cannot remove alias symlink: %v", err)
			}
		}

		err := os.Symlink(alias.Target, p)
		if err != nil {
			return fmt.Errorf("cannot create alias symlink: %v", err)
		}

		target, err := os.Readlink(filepath.Join(dirs.CompletersDir, alias.Target))
		if err == nil && target == dirs.CompleteSh {
			os.Symlink(alias.Target, filepath.Join(dirs.CompletersDir, alias.Name))
		}
	}
	return nil
}

// RemoveSnapAliases removes all the aliases targeting the given snap.
func (b Backend) RemoveSnapAliases(snapName string) error {
	removeSymlinksTo(dirs.CompletersDir, snapName)
	return removeSymlinksTo(dirs.SnapBinariesDir, snapName)
}

func removeSymlinksTo(dir, snapName string) error {
	cands, err := filepath.Glob(filepath.Join(dir, "*"))
	if err != nil {
		return err
	}
	prefix := fmt.Sprintf("%s.", snapName)
	var firstErr error
	// best effort
	for _, cand := range cands {
		target, err := os.Readlink(cand)
		if err, ok := err.(*os.PathError); ok && err.Err == syscall.EINVAL {
			continue
		}
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		if target == snapName || strings.HasPrefix(target, prefix) {
			err := os.Remove(cand)
			if err != nil && firstErr == nil {
				firstErr = fmt.Errorf("cannot remove alias symlink: %v", err)
			}
		}
	}
	return firstErr
}
