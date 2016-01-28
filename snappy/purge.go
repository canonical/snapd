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

	// There can be a number of datadirs, such as for multiple versions, the
	// .snap being installed for multiple users, or the .snap using both the
	// snap data path as well as the user data path. They all need to be purged.
	for _, datadir := range datadirs {
		yamlPath := filepath.Join(dirs.SnapSnapsDir, datadir.QualifiedName(), datadir.Version, "meta", "snap.yaml")
		part, err := NewInstalledSnapPart(yamlPath, datadir.Origin)
		if err != nil {
			// no such part installed
			continue
		}
		if part.IsActive() {
			if !purgeActive {
				return ErrStillActive
			}

			// We've been asked to purge a currently-active part. We don't want
			// to blow away data out from under an active part, so we'll
			// temporarily deactivate it here and keep track of it so we can
			// reactivate it later.
			err = part.deactivate(false, meter)
			if err == nil {
				active = append(active, part)
			} else {
				meter.Notify(fmt.Sprintf("Unable to deactivate %s: %s", part.Name(), err))
				meter.Notify("Purge continues.")
			}
		}
	}

	// Conduct the purge.
	for _, datadir := range datadirs {
		if err := remove(datadir.QualifiedName(), datadir.Version); err != nil {
			e = err
			meter.Notify(fmt.Sprintf("unable to purge %s version %s: %s", datadir.QualifiedName(), datadir.Version, err.Error()))
		}
	}

	// Reactivate the temporarily deactivated parts.
	for _, part := range active {
		if err := part.activate(false, meter); err != nil {
			meter.Notify(fmt.Sprintf("Unable to reactivate %s: %s", part.Name(), err))
		}
	}

	return e
}
