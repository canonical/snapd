// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2015 Canonical Ltd
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
	"sort"

	"github.com/ubuntu-core/snappy/progress"
)

// SetActive sets the active state of the given package
func SetActive(fullName string, active bool, meter progress.Meter) error {
	installed, err := (&Overlord{}).Installed()
	if err != nil {
		return err
	}

	snaps := FindSnapsByName(fullName, installed)
	if len(snaps) == 0 {
		return ErrPackageNotFound
	}

	// XXX: why do we do this?
	sort.Sort(sort.Reverse(BySnapVersion(snaps)))

	snap := snaps[0]
	for i := range snaps {
		if snaps[i].IsActive() {
			snap = snaps[i]
			break
		}
	}

	overlord := &Overlord{}
	return overlord.SetActive(snap, active, meter)
}
