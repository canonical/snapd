// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2016 Canonical Ltd
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

package snappy

import (
	"fmt"
	"path/filepath"

	"github.com/ubuntu-core/snappy/dirs"
	"github.com/ubuntu-core/snappy/progress"
)

// Overlord is responsible for the overall system state.
type Overlord struct {
}

// Install installs the given snap file to the system.
//
// It returns the local snap file or an error
func (o *Overlord) Install(snapFilePath string, origin string, flags InstallFlags, meter progress.Meter) (*SnapPart, error) {
	return nil, ErrNotImplemented
}

// Uninstall removes the given local snap from the system.
//
// It returns an error on failure
func (o *Overlord) Uninstall(sp *SnapPart, meter progress.Meter) error {
	return ErrNotImplemented
}

// SetActive sets the active state of the given snap
//
// It returns an error on failure
func (o *Overlord) SetActive(sp *SnapPart, active bool, meter progress.Meter) error {
	return ErrNotImplemented
}

// Installed returns the installed snaps from this repository
func (o *Overlord) Installed() ([]*SnapPart, error) {
	globExpr := filepath.Join(dirs.SnapSnapsDir, "*", "*", "meta", "snap.yaml")
	parts, err := o.partsForGlobExpr(globExpr)
	if err != nil {
		return nil, fmt.Errorf("Can not get the installed snaps: %s", err)

	}

	return parts, nil
}

func (o *Overlord) partsForGlobExpr(globExpr string) (parts []*SnapPart, err error) {
	matches, err := filepath.Glob(globExpr)
	if err != nil {
		return nil, err
	}

	for _, yamlfile := range matches {
		// skip "current" and similar symlinks
		realpath, err := filepath.EvalSymlinks(yamlfile)
		if err != nil {
			return nil, err
		}
		if realpath != yamlfile {
			continue
		}

		origin, _ := originFromYamlPath(realpath)
		snap, err := NewInstalledSnapPart(realpath, origin)
		if err != nil {
			return nil, err
		}
		parts = append(parts, snap)
	}

	return parts, nil
}
