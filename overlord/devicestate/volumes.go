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

// Package devicestate implements the manager and state aspects responsible
// for the device identity and policies.
package devicestate

import (
	"fmt"

	"github.com/snapcore/snapd/gadget"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/fdestate"
	"github.com/snapcore/snapd/testutil"
)

var (
	fdeManagerGetKeyslots = (*fdestate.FDEManager).GetKeyslots
)

// VolumeStructureWithKeyslots is gadget.VolumeStructure with
// the corresponding key slots attached.
type VolumeStructureWithKeyslots struct {
	gadget.VolumeStructure

	Keyslots []fdestate.Keyslot
}

// GetGadgetVolumeStructuresWithKeyslots returns the gadget volume
// structures with their corresponding key slots attached.
func GetGadgetVolumeStructuresWithKeyslots(fdemgr *fdestate.FDEManager, gadgetInfo *gadget.Info) ([]VolumeStructureWithKeyslots, error) {
	keyslots, _, err := fdeManagerGetKeyslots(fdemgr, nil)
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

func MockFDEManagerGetKeyslots(f func(m *fdestate.FDEManager, keyslotRefs []fdestate.KeyslotRef) (keyslots []fdestate.Keyslot, missingRefs []fdestate.KeyslotRef, err error)) (restore func()) {
	osutil.MustBeTestBinary("mocking fdeManagerGetKeyslots can be done only from tests")
	return testutil.Mock(&fdeManagerGetKeyslots, f)
}
