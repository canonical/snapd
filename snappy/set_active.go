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
	"github.com/ubuntu-core/snappy/progress"
)

// SetActive sets the active state of the given package
func SetActive(fullName string, active bool, meter progress.Meter) error {
	name, developer := SplitDeveloper(fullName)
	if developer == "" {
		developer = "*"
	}

	// TODO: switch this to using lightweights
	snaps, err := NewLocalSnapRepository().Snaps(name, developer)
	if err != nil {
		return err
	}

	if len(snaps) != 1 {
		return ErrPackageNotFound
	}

	snap := snaps[0]
	overlord := &Overlord{}
	return overlord.SetActive(snap, active, meter)
}
