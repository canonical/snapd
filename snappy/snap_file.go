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
	"github.com/snapcore/snapd/snap"
)

// openSnapFile opens a snap blob returning both a snap.Info completed
// with sideInfo (if not nil) and a corresponding snap.Container.
func openSnapFile(snapPath string, unsignedOk bool, sideInfo *snap.SideInfo) (*snap.Info, snap.Container, error) {
	// TODO: what precautions to take if unsignedOk == false ?

	snapf, err := snap.Open(snapPath)
	if err != nil {
		return nil, nil, err
	}

	info, err := snap.ReadInfoFromSnapFile(snapf, sideInfo)
	if err != nil {
		return nil, nil, err
	}

	return info, snapf, nil
}
