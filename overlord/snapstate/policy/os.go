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

type osPolicy struct {
	modelBase string
}

func (p *osPolicy) CanRemove(st *state.State, snapst *snapstate.SnapState, rev snap.Revision) error {
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

	if name == "ubuntu-core" {
		return nil
	}

	if p.modelBase == "" {
		return errIsModel
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
