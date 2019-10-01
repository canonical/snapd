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
	"sort"

	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
)

type basePolicy struct {
	modelBase string
}

func (p *basePolicy) CanRemove(st *state.State, snapst *snapstate.SnapState, rev snap.Revision) error {
	name := snapst.InstanceName()
	if name == "" {
		// not installed, or something. What are you even trying to do.
		return errNoName
	}

	if !rev.Unset() {
		if boot.InUse(name, rev) {
			return errInUseForBoot
		}
		return nil
	}

	if p.modelBase == name {
		return errIsModel
	}

	// a core system could have core18 required in the model due to dependencies for ex
	if snapst.Required {
		return errRequired
	}

	// here we use that bases can't be instantiated (InstanceName == SnapName always)
	usedBy, err := baseUsedBy(st, name)
	if len(usedBy) == 0 || err != nil {
		return err
	}
	return inUseByErr(usedBy)
}

func baseUsedBy(st *state.State, baseName string) ([]string, error) {
	snapStates, err := snapstate.All(st)
	if err != nil {
		// note snapstate.All doesn't currently return ErrNoState
		return nil, err
	}
	alsoCore16 := false
	if baseName == "" {
		// core -> core16 aliasing
		if snapst, ok := snapStates["core16"]; !ok || !snapst.IsInstalled() {
			// this base is not installed
			alsoCore16 = true
		}
	}

	var usedBy []string
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
				if baseName != snapInfo.Base && !(alsoCore16 && snapInfo.Base == "core16") {
					continue
				}
				usedBy = append(usedBy, snapInfo.InstanceName())
				break
			}
		}
	}
	sort.Strings(usedBy)
	return usedBy, nil
}
