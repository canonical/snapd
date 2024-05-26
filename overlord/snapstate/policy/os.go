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
	"errors"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
)

type osPolicy struct {
	modelBase string
}

func (p *osPolicy) CanRemove(st *state.State, snapst *snapstate.SnapState, rev snap.Revision, dev snap.Device) error {
	name := snapst.InstanceName()
	if name == "" {
		// not installed, or something. What are you even trying to do.
		return errNoName
	}

	if ephemeral(dev) {
		return errEphemeralSnapsNotRemovable
	}

	if name == "ubuntu-core" {
		return nil
	}

	// consider the case of a UC16 system, where the model does not specify a base,
	// since 'core' is already implied and is actively used by the system,
	// which boots in the UC way
	if p.modelBase == "" && dev.IsCoreBoot() {
		if !rev.Unset() {
			mylog.Check(
				// TODO: tweak boot.InUse so that it DTRT when rev.Unset, call
				// it unconditionally as an extra precaution
				inUse(name, rev, snap.TypeOS, dev))

			return nil
		}
		return errIsModel
	}

	if rev.Unset() {
		// revision will be unset if we're attempting to remove all snaps or
		// just the one last remaining revision. in either case, we need to
		// ensure that the snapd snap is there

		var snapdState snapstate.SnapState
		mylog.Check(snapstate.Get(st, "snapd", &snapdState))
		if err != nil && !errors.Is(err, state.ErrNoState) {
			return err
		}

		// if snapd snap is not installed, then this might be a system that has
		// received snapd updates via the core snap. in that case, we can't remove
		// the core snap.
		if !snapdState.IsInstalled() {
			return errSnapdNotInstalled
		}
	}

	if !rev.Unset() {
		return nil
	}

	// a core18 system could have core required in the model due to dependencies for ex
	if snapst.Required {
		return errRequired
	}

	usedBy := mylog.Check2(baseUsedBy(st, ""))
	if len(usedBy) == 0 || err != nil {
		return err
	}
	return inUseByErr(usedBy)
}
