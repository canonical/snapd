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

// Package policy implements fine grained decision-making for snapstate
package policy

import (
	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
)

func init() {
	snapstate.PolicyFor = For
}

func For(typ snap.Type, model *asserts.Model) snapstate.Policy {
	switch typ {
	case snap.TypeKernel:
		return &kernelPolicy{modelKernel: model.Kernel()}
	case snap.TypeGadget:
		return &gadgetPolicy{modelGadget: model.Gadget()}
	case snap.TypeOS:
		return &osPolicy{modelBase: model.Base()}
	case snap.TypeBase:
		return &basePolicy{modelBase: model.Base()}
	case snap.TypeSnapd:
		return &snapdPolicy{onClassic: model.Classic()}
	default:
		return appPolicy{}
	}
}

func ephemeral(dev snap.Device) bool {
	return !dev.RunMode()
}

func inUse(name string, rev snap.Revision, typ snap.Type, dev snap.Device) error {
	check := mylog.Check2(boot.InUse(typ, dev))

	if check(name, rev) {
		return errInUseForBoot
	}
	return nil
}

type appPolicy struct{}

func (appPolicy) CanRemove(_ *state.State, snapst *snapstate.SnapState, rev snap.Revision, dev snap.Device) error {
	if ephemeral(dev) {
		return errEphemeralSnapsNotRemovable
	}

	if !rev.Unset() {
		return nil
	}

	if snapst.Required {
		return errRequired
	}

	return nil
}
