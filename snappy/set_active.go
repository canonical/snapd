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

	"launchpad.net/snappy/progress"
)

// SetActive sets the active state of the given package
func SetActive(fullName string, active bool, meter progress.Meter) error {
	// TODO: switch this to using lightweights
	m := NewMetaLocalRepository()
	installed, err := m.Installed()
	if err != nil {
		return err
	}

	parts := FindSnapsByName(fullName, installed)
	if len(parts) == 0 {
		return ErrPackageNotFound
	}

	sort.Sort(sort.Reverse(BySnapVersion(parts)))

	part := parts[0]
	for i := range parts {
		if parts[i].IsActive() {
			part = parts[i]
			break
		}
	}

	return part.SetActive(active, meter)
}
