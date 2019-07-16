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

// Package snapstatetest contains helper functions for mocking snaps in the
// snapstate.
package snapstatetest

import (
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
)

// MockSnapCurrent will make the given snapYaml a snap on disk and in the
// state. It will also make the snap "current". The snap revision is
// always snap.R(1). Write MockSnapCurrentWithSideInfo() if different
// side-infos are needed.
func MockSnapCurrent(c *C, st *state.State, snapYaml string) *snap.Info {
	sideInfo := &snap.SideInfo{Revision: snap.R(1)}
	info := snaptest.MockSnapCurrent(c, snapYaml, sideInfo)
	snapstate.Set(st, info.InstanceName(), &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{
			{
				RealName: info.SnapName(),
				Revision: info.Revision,
				SnapID:   info.SnapName() + "-id",
			},
		},
		Current:  info.Revision,
		SnapType: string(info.Type),
	})
	return info
}
