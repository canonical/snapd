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
	"strings"

	"github.com/ubuntu-core/snappy/progress"
)

// RemoveFlags can be used to pass additional flags to the snap removal request
type RemoveFlags uint

const (
	// DoRemoveGC will ensure that garbage collection is done, unless a
	// version is specified.
	DoRemoveGC RemoveFlags = 1 << iota
)

// Remove a snap by a snapSpec string, name[.developer][=version]
func Remove(snapSpec string, flags RemoveFlags, meter progress.Meter) error {
	var snaps BySnapVersion

	installed, err := (&Overlord{}).Installed()
	if err != nil {
		return err
	}
	// Note that "=" is not legal in a snap name or a snap version
	l := strings.Split(snapSpec, "=")
	if len(l) == 2 {
		name := l[0]
		version := l[1]
		snaps = FindSnapsByNameAndVersion(name, version, installed)
	} else {
		if (flags & DoRemoveGC) == 0 {
			if snap := ActiveSnapByName(snapSpec); snap != nil {
				snaps = append(snaps, snap)
			}
		} else {
			snaps = FindSnapsByName(snapSpec, installed)
		}
	}

	if len(snaps) == 0 {
		return ErrPackageNotFound
	}

	overlord := &Overlord{}
	for _, snap := range snaps {
		if err := overlord.Uninstall(snap, meter); err != nil {
			return err
		}
	}

	return nil
}
