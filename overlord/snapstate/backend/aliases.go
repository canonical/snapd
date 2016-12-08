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

package backend

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
)

// Alias represents a command alias with a name and its application target.
type Alias struct {
	Name   string `json:"name"`
	Target string `json:"target"`
}

// MissingAliases returns the subset of aliases that are missing on disk.
func (b Backend) MissingAliases(aliases []*Alias) ([]*Alias, error) {
	var res []*Alias
	for _, cand := range aliases {
		_, err := os.Lstat(filepath.Join(dirs.SnapBinariesDir, cand.Name))
		if err != nil {
			if !os.IsNotExist(err) {
				return nil, err
			}
			res = append(res, cand)
		}
	}
	return res, nil
}

// MatchingAliases returns the subset of aliases that exist on disk and have the expected targets.
func (b Backend) MatchingAliases(aliases []*Alias) ([]*Alias, error) {
	var res []*Alias
	for _, cand := range aliases {
		fn := filepath.Join(dirs.SnapBinariesDir, cand.Name)
		fileInfo, err := os.Lstat(fn)
		if err != nil {
			if !os.IsNotExist(err) {
				return nil, err
			}
			continue
		}
		if (fileInfo.Mode() & os.ModeSymlink) != 0 {
			target, err := os.Readlink(fn)
			if err != nil {
				return nil, err
			}
			if target == cand.Target {
				res = append(res, cand)
			}
		}
	}
	return res, nil
}

// UpdateAliases adds and removes the given aliases.
func (b Backend) UpdateAliases(add []*Alias, remove []*Alias) error {
	for _, alias := range add {
		err := os.Symlink(alias.Target, filepath.Join(dirs.SnapBinariesDir, alias.Name))
		if err != nil {
			return fmt.Errorf("cannot create alias symlink: %v", err)
		}
	}

	for _, alias := range remove {
		err := os.Remove(filepath.Join(dirs.SnapBinariesDir, alias.Name))
		if err != nil {
			return fmt.Errorf("cannot remove alias symlink: %v", err)
		}
	}
	return nil
}

// RemoveSnapAliases removes all the aliases targeting the given snap.
func (b Backend) RemoveSnapAliases(snapName string) error {
	cands, err := filepath.Glob(filepath.Join(dirs.SnapBinariesDir, "*"))
	if err != nil {
		return err
	}
	prefix := fmt.Sprintf("%s.", snapName)
	var firstErr error
	// best effort
	for _, cand := range cands {
		if osutil.IsSymlink(cand) {
			target, err := os.Readlink(cand)
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
	}
	return firstErr
}
