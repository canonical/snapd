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

	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
)

type basePolicy struct {
	modelBase string
}

func (p *basePolicy) CanRemove(st *state.State, snapst *snapstate.SnapState, rev snap.Revision, dev snap.Device, removals map[string]bool) error {
	name := snapst.InstanceName()
	if name == "" {
		// not installed, or something. What are you even trying to do.
		return errNoName
	}

	if ephemeral(dev) {
		return errEphemeralSnapsNotRemovable
	}

	if p.modelBase == name.String() {
		if !rev.Unset() {
			// TODO: tweak boot.InUse so that it DTRT when rev.Unset, call
			// it unconditionally as an extra precaution
			if err := inUse(name.String(), rev, snap.TypeBase, dev); err != nil {
				return err
			}
			return nil
		}
		return errIsModel
	}

	if !rev.Unset() {
		return nil
	}

	// a core system could have core18 required in the model due to dependencies for ex
	if snapst.Required {
		return errRequired
	}

	// here we use that bases can't be instantiated (InstanceName == SnapName always)
	return validateBaseOnlyUsedByRemoved(st, name.String(), removals)
}

// validateBaseOnlyUsedByRemoved checks that the base is only used by snaps
// being removed alongside it.
func validateBaseOnlyUsedByRemoved(st *state.State, baseName string, removals map[string]bool) error {
	usedBy, err := baseUsedBy(st, baseName)
	if err != nil {
		return err
	}

	var usedByAndNotRemoved []string
	for _, snap := range usedBy {
		if !removals[snap] {
			usedByAndNotRemoved = append(usedByAndNotRemoved, snap)
		}
	}

	if len(usedByAndNotRemoved) > 0 {
		return inUseByErr(usedByAndNotRemoved)
	}
	return nil
}

func changeCannotIntroduceBaseUsage(chg *state.Change) bool {
	// we don't strictly need to skip some of these types of changes because they
	// require an installed snap which would then get picked up when we check
	// snapstate for snaps that use the base. However, conceptually they still
	// make sense to skip as they wouldn't affect base usage.
	switch chg.Kind() {
	case "pre-download", "remove-snap", "enable-snap", "disable-snap",
		"switch-snap", "install-component", "snapctl-install", "snapctl-remove",
		"migrate-home", "alias", "unalias", "prefer":
		return true
	default:
		return false
	}
}

func baseUsedBy(st *state.State, baseName string) ([]string, error) {
	snapStates, err := snapstate.All(st)
	if err != nil {
		// note snapstate.All doesn't currently return ErrNoState
		return nil, err
	}
	alsoCore16 := false
	if baseName == "" {
		// if core is installed, a snap having base: core16 will not
		// pull in core16 itself but use core instead. So if we are
		// looking at core (a base of ""), and core16 is not installed,
		// then we need to look out for things having base: core16 as
		// well as "".
		//
		// TODO: if we ever do the converse, using core16 for snaps
		//       having a base of "", then this needs a tweak.
		if _, ok := snapStates["core16"]; !ok {
			alsoCore16 = true
		}
	}

	usedBy := make(map[string]bool)
	for name, snapst := range snapStates {
		if typ, err := snapst.Type(); err == nil && typ != snap.TypeApp && typ != snap.TypeGadget {
			continue
		}

		for _, si := range snapst.Sequence.SideInfos() {
			snapInfo, err := snap.ReadInfo(name, si)
			if err == nil {
				if typ := snapInfo.Type(); typ != snap.TypeApp && typ != snap.TypeGadget {
					continue
				}
				if !(baseName == snapInfo.Base || (alsoCore16 && snapInfo.Base == "core16")) {
					continue
				}
				usedBy[snapInfo.InstanceName()] = true
				break
			}
		}
	}

	for _, chg := range st.Changes() {
		if chg.IsReady() || changeCannotIntroduceBaseUsage(chg) {
			continue
		}

		for _, t := range chg.Tasks() {
			if !t.Has("snap-setup") && !t.Has("snap-setup-task") {
				continue
			}

			snapsup, err := snapstate.TaskSnapSetup(t)
			if err != nil {
				return nil, err
			}

			// only apps and gadgets have bases
			if snapsup.Type != snap.TypeApp && snapsup.Type != snap.TypeGadget {
				continue
			}

			if snapsup.Base == baseName || (alsoCore16 && snapsup.Base == "core16") {
				usedBy[snapsup.InstanceName().String()] = true
			}
		}
	}

	usedByNames := make([]string, 0, len(usedBy))
	for name := range usedBy {
		usedByNames = append(usedByNames, name)
	}
	sort.Strings(usedByNames)
	return usedByNames, nil
}
