// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2025 Canonical Ltd
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

package devicestate

import (
	"fmt"

	"github.com/snapcore/snapd/gadget"
	"github.com/snapcore/snapd/overlord/fdestate"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
)

var (
	fdestateGetKeyslots = fdestate.GetKeyslots
	snapstateGadgetInfo = snapstate.GadgetInfo
)

// VolumeStructureWithKeyslots is gadget.VolumeStructure with
// the corresponding key slots attached.
type VolumeStructureWithKeyslots struct {
	gadget.VolumeStructure

	Keyslots []fdestate.Keyslot
}

// GetVolumeStructuresWithKeyslots returns the current gadget
// volume structures with their corresponding key slots attached.
//
// The state needs to be locked by the caller.
func GetVolumeStructuresWithKeyslots(st *state.State) ([]VolumeStructureWithKeyslots, error) {
	deviceCtx, err := DeviceCtx(st, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("cannot get device context: %v", err)
	}
	gadgetSnapInfo, err := snapstateGadgetInfo(st, deviceCtx)
	if err != nil {
		return nil, fmt.Errorf("cannot get gadget snap info: %v", err)
	}

	gadgetInfo, err := gadget.ReadInfo(gadgetSnapInfo.MountDir(), deviceCtx.Model())
	if err != nil {
		return nil, fmt.Errorf("cannot read gadget: %v", err)
	}

	keyslots, _, err := fdestateGetKeyslots(st, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get key slots: %v", err)
	}
	keyslotsByContainerRole := make(map[string][]fdestate.Keyslot)
	for _, keyslot := range keyslots {
		keyslotsByContainerRole[keyslot.ContainerRole] = append(keyslotsByContainerRole[keyslot.ContainerRole], keyslot)
	}

	var structuresWithKeyslots []VolumeStructureWithKeyslots
	for _, gv := range gadgetInfo.Volumes {
		for _, gs := range gv.Structure {
			structuresWithKeyslots = append(structuresWithKeyslots, VolumeStructureWithKeyslots{
				VolumeStructure: gs,
				Keyslots:        keyslotsByContainerRole[gs.Role],
			})
		}
	}

	return structuresWithKeyslots, nil
}
