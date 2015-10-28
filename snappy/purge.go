// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2015 Canonical Ltd
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

// PurgeFlags can be used to pass additional flags to the snap removal request
type PurgeFlags uint

const (
	// DoPurgeActive requests that the data files of an active
	// package be removed. Without this that is disallowed.
	DoPurgeActive PurgeFlags = 1 << iota
)

var remove = removeSnapData

// Purge a part by a partSpec string, name[.origin][=version]
func Purge(partSpec string, flags PurgeFlags, meter progress.Meter) error {
	var e error
	datadirs := DataDirs(partSpec)
	if len(datadirs) == 0 {
		return ErrPackageNotFound
	}

	purgeActive := flags&DoPurgeActive != 0

	var active []*SnapPart

	for _, datadir := range datadirs {
		yamlPath := filepath.Join(dirs.SnapAppsDir, datadir.QualifiedName(), datadir.Version, "meta", "package.yaml")
		part, err := NewInstalledSnapPart(yamlPath, datadir.Origin)
		if err != nil {
			// no such part installed
			continue
		}
		if part.IsActive() {
			if !purgeActive {
				return ErrStillActive
			}
			active = append(active, part)
		}
	}

	for i, pkg := range active {
		err := pkg.deactivate(false, meter)
		if err != nil {
			meter.Notify(fmt.Sprintf("Unable to deactivate %s: %s", pkg.Name(), err))
			meter.Notify("Purge continues.")
			active[i] = nil // don't reactivate
		}
	}

	for _, datadir := range datadirs {
		if err := remove(datadir.QualifiedName(), datadir.Version); err != nil {
			e = err
			meter.Notify(fmt.Sprintf("unable to purge %s version %s: %s", datadir.QualifiedName(), datadir.Version, err.Error()))
		}
	}

	for _, pkg := range active {
		if pkg == nil {
			continue
		}
		if err := pkg.activate(false, meter); err != nil {
			meter.Notify(fmt.Sprintf("Unable to activate %s: %s", pkg.Name(), err))
		}
	}

	return e
}
