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
	"sort"

	"github.com/ubuntu-core/snappy/progress"
)

// Rollback will roll the given pkg back to the given ver. If the version
// is empty the previous installed version will be used.
//
// The version needs to be installed on disk
func Rollback(pkg, ver string, inter progress.Meter) (version string, err error) {

	// no version specified, find the previous one
	if ver == "" {
		installed, err := (&Overlord{}).Installed()
		if err != nil {
			return "", err
		}
		snaps := FindSnapsByName(pkg, installed)
		if len(snaps) < 2 {
			return "", fmt.Errorf("no version to rollback to")
		}
		// FIXME: sort by revision sequence
		sort.Sort(BySnapVersion(snaps))
		// -1 is the most recent, -2 the previous one
		ver = snaps[len(snaps)-2].Version()
	}

	if err := makeSnapActiveByNameAndVersion(pkg, ver, inter); err != nil {
		return "", err
	}

	return ver, nil
}
