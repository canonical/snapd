// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019 Canonical Ltd
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

package internal

import (
	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/snap/naming"
)

func MakeSystemSnap(snapName string, defaultChannel string, modes []string) *asserts.ModelSnap {
	return &asserts.ModelSnap{
		Name:           snapName,
		SnapID:         naming.WellKnownSnapID(snapName),
		SnapType:       snapName, // same as snapName for core, snapd
		Modes:          modes,
		DefaultChannel: defaultChannel,
		Presence:       "required",
	}
}
