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

package policy

import (
	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
)

type basePolicy struct{}

func (basePolicy) CanRemove(st *state.State, snapst *snapstate.SnapState, all bool) bool {
	name := snapst.InstanceName()
	if name == "" {
		// not installed, or something. What are you even trying to do.
		return false
	}

	if boot.InUse(name, snapst.Current) {
		return false
	}

	if !all {
		return true
	}

	if snapst.Required {
		return false
	}

	// here we use that bases can't be instantiated (InstanceName == SnapName always)
	return !baseInUse(st, name)
}

func baseInUse(st *state.State, baseName string) bool {
	snapStates, err := snapstate.All(st)
	if err != nil {
		return false
	}
	for name, snapst := range snapStates {
		for _, si := range snapst.Sequence {
			if snapInfo, err := snap.ReadInfo(name, si); err == nil {
				if snapInfo.GetType() != snap.TypeApp {
					continue
				}
				if snapInfo.Base == baseName {
					return true
				}
			}
		}
	}
	return false
}
