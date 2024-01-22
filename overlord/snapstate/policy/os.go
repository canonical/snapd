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
		return errEphemeralSnapsNotRemovalable
	}

	if name == "ubuntu-core" {
		return nil
	}

	// if the base is unset and dev.IsCoreBoot is true, then we know this is a
	// UC16 system. note that base might be unset on classic models, so we must
	// check dev.IsCoreBoot as well.
	if p.modelBase == "" && dev.IsCoreBoot() {
		if !rev.Unset() {
			// TODO: tweak boot.InUse so that it DTRT when rev.Unset, call
			// it unconditionally as an extra precaution
			if err := inUse(name, rev, snap.TypeOS, dev); err != nil {
				return err
			}
			return nil
		}
		return errIsModel
	}

	var snapdState snapstate.SnapState
	err := snapstate.Get(st, "snapd", &snapdState)
	if err != nil && !errors.Is(err, state.ErrNoState) {
		return err
	}

	// if snapd snap is not installed, then this might be a system that has
	// received snapd updates via the core snap. in that case, we can't remove
	// the core snap.
	if !snapdState.IsInstalled() {
		return errSnapdNotInstalled
	}

	if !rev.Unset() {
		return nil
	}

	// a core18 system could have core required in the model due to dependencies for ex
	if snapst.Required {
		return errRequired
	}

	usedBy, err := baseUsedBy(st, "")
	if len(usedBy) == 0 || err != nil {
		return err
	}
	return inUseByErr(usedBy)
}
