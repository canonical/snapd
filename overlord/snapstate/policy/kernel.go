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
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
)

type kernelPolicy struct {
	modelKernel string
}

func (p *kernelPolicy) CanRemove(_ *state.State, snapst *snapstate.SnapState, rev snap.Revision, dev snap.Device) error {
	name := snapst.InstanceName()
	if name == "" {
		// not installed, or something. What are you even trying to do.
		return errNoName
	}

	if ephemeral(dev) {
		return errEphemeralSnapsNotRemovable
	}

	if p.modelKernel == name {
		if !rev.Unset() {
			// TODO: tweak boot.InUse so that it DTRT when rev.Unset, call
			// it unconditionally as an extra precaution
			if err := inUse(name, rev, snap.TypeKernel, dev); err != nil {
				return err
			}
			return nil
		}

		return errIsModel
	}

	return nil
}
