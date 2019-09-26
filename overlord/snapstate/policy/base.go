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
	"github.com/snapcore/snapd/strutil"
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

	// a core system could have core18 required in the model due to dependencies for ex
	if snapst.Required {
		return false
	}

	// here we use that bases can't be instantiated (InstanceName == SnapName always)
	return !baseInUse(st, name, "")
}

func baseInUse(st *state.State, baseName string, altName string) bool {
	snapStates, err := snapstate.All(st)
	if err != nil {
		// on error, assume it's in use
		// (note snapstate.All doesn't currently return ErrNoState)
		return err != state.ErrNoState
	}
	baseNames := []string{baseName}
	if altName != "" {
		if snapst, ok := snapStates[altName]; !ok || !snapst.IsInstalled() {
			// this base is not installed
			baseNames = append(baseNames, altName)
		}
	}

	for name, snapst := range snapStates {
		if typ, err := snapst.Type(); err == nil && typ != snap.TypeApp && typ != snap.TypeGadget {
			continue
		}
		if !snapst.IsInstalled() {
			continue
		}

		for _, si := range snapst.Sequence {
			snapInfo, err := snap.ReadInfo(name, si)
			if err == nil {
				if typ := snapInfo.GetType(); typ != snap.TypeApp && typ != snap.TypeGadget {
					continue
				}
				if !strutil.ListContains(baseNames, snapInfo.Base) {
					continue
				}
			}
			return true
		}
	}
	return false
}
