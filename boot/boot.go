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

package boot

import (
	"github.com/snapcore/snapd/snap"
)

// A BootParticipant is a snap that is involved in a device's boot
// process.
type BootParticipant interface {
	// SetNextBoot will schedule the snap to be used in the next boot. For
	// base snaps it is up to the caller to select the right bootable base
	// (from the model assertion).
	SetNextBoot() error
	// ChangeRequiresReboot returns whether a reboot is required to switch
	// to the snap.
	ChangeRequiresReboot() bool
}

// A Kernel is a snap.TypeKernel BootParticipant
type Kernel interface {
	BootParticipant
	// RemoveKernelAssets removes the unpacked kernel/initrd for the given
	// kernel snap.
	RemoveKernelAssets() error
	// ExtractKernelAssets extracts kernel/initrd/dtb data from the given
	// kernel snap, if required, to a versioned bootloader directory so
	// that the bootloader can use it.
	ExtractKernelAssets(snap.Container) error
}

// Model carries information about the model that is relevant to boot.
// Note *asserts.Model implements this, and that's the expected use case.
type Model interface {
	Kernel() string
	Base() string
}

// Lookup figures out what the boot participant is for the given arguments, and
// returns it. The second return value indicates whether no boot participant
// exists, and if false the first return value will be nil.
//
// Currently, on classic, nothing is a boot participant (applicable
// will always be false).
func Lookup(s snap.PlaceInfo, t snap.Type, model Model, onClassic bool) (bp BootParticipant, applicable bool) {
	if onClassic {
		return nil, false
	}
	if t != snap.TypeOS && t != snap.TypeKernel && t != snap.TypeBase {
		// note we don't currently have anything useful to do with gadgets
		return nil, false
	}

	if model != nil {
		switch t {
		case snap.TypeKernel:
			if s.InstanceName() != model.Kernel() {
				// a remodel might leave you in this state
				return nil, false
			}
		case snap.TypeBase, snap.TypeOS:
			base := model.Base()
			if base == "" {
				base = "core"
			}
			if s.InstanceName() != base {
				return nil, false
			}
		}
	}

	cbp := &coreBootParticipant{s: s, t: t}
	bp = cbp
	if t == snap.TypeKernel {
		bp = &coreKernel{cbp}
	}

	return bp, true
}
