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

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
)

type basePolicy struct {
	modelBase string
}

func (p *basePolicy) CanRemove(st *state.State, snapst *snapstate.SnapState, rev snap.Revision, dev snap.Device) error {
	name := snapst.InstanceName()
	if name == "" {
		// not installed, or something. What are you even trying to do.
		return errNoName
	}

	if ephemeral(dev) {
		return errEphemeralSnapsNotRemovable
	}

	if p.modelBase == name {
		if !rev.Unset() {
			mylog.Check(
				// TODO: tweak boot.InUse so that it DTRT when rev.Unset, call
				// it unconditionally as an extra precaution
				inUse(name, rev, snap.TypeBase, dev))

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
	usedBy := mylog.Check2(baseUsedBy(st, name))
	if len(usedBy) == 0 || err != nil {
		return err
	}
	return inUseByErr(usedBy)
}

func baseUsedBy(st *state.State, baseName string) ([]string, error) {
	snapStates := mylog.Check2(snapstate.All(st))

	// note snapstate.All doesn't currently return ErrNoState

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

	var usedBy []string
	for name, snapst := range snapStates {
		if typ := mylog.Check2(snapst.Type()); err == nil && typ != snap.TypeApp && typ != snap.TypeGadget {
			continue
		}

		for _, si := range snapst.Sequence.SideInfos() {
			snapInfo := mylog.Check2(snap.ReadInfo(name, si))
			if err == nil {
				if typ := snapInfo.Type(); typ != snap.TypeApp && typ != snap.TypeGadget {
					continue
				}
				if !(baseName == snapInfo.Base || (alsoCore16 && snapInfo.Base == "core16")) {
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
