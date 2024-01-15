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

// Package backend implements the low-level primitives to manage the snaps and their installation on disk.
package backend

import (
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snapfile"
)

// Backend exposes all the low-level primitives to manage snaps and their installation on disk.
type Backend struct {
	preseed bool
}

// Candidate is a test hook.
func (b Backend) Candidate(*snap.SideInfo) {}

// CurrentInfo is a test hook.
func (b Backend) CurrentInfo(*snap.Info) {}

// OpenSnapFile opens a snap blob returning both a snap.Info completed
// with sideInfo (if not nil) and a corresponding snap.Container.
// Assumes the file was verified beforehand or the user asked for --dangerous.
func OpenSnapFile(snapPath string, sideInfo *snap.SideInfo) (*snap.Info, snap.Container, error) {
	snapf, err := snapfile.Open(snapPath)
	if err != nil {
		return nil, nil, err
	}

	info, err := snap.ReadInfoFromSnapFile(snapf, sideInfo)
	if err != nil {
		return nil, nil, err
	}

	return info, snapf, nil
}

// OpenComponentFile opens a component blob returning a snap.ComponentInfo and
// a corresponding snap.Container. Assumes the file was verified beforehand or
// the user asked for --dangerous.
func OpenComponentFile(compPath string) (*snap.ComponentInfo, snap.Container, error) {
	snapf, err := snapfile.Open(compPath)
	if err != nil {
		return nil, nil, err
	}

	info, err := snap.ReadComponentInfoFromContainer(snapf)
	if err != nil {
		return nil, nil, err
	}

	return info, snapf, nil
}

func NewForPreseedMode() Backend {
	return Backend{preseed: true}
}
