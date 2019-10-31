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
package bootstrap

import (
	"fmt"

	"github.com/snapcore/snapd/cmd/snap-bootstrap/partition"
	"github.com/snapcore/snapd/gadget"
)

type Options struct {
	// will contain encryption later
}

func Run(gadgetRoot, device string, options *Options) error {
	if gadgetRoot == "" {
		return fmt.Errorf("cannot use empty gadget root directory")
	}
	if device == "" {
		return fmt.Errorf("cannot use empty device node")
	}

	lv, err := gadget.PositionedVolumeFromGadget(gadgetRoot)
	if err != nil {
		return fmt.Errorf("cannot layout the volume: %v", err)
	}

	diskLayout, err := partition.NewDeviceLayout(device)
	if err != nil {
		return fmt.Errorf("cannot read %v partitions: %v", device, err)
	}

	// check if the current partition table is compatible with the gadget
	if err := ensureLayoutCompatibility(lv, diskLayout); err != nil {
		return fmt.Errorf("gadget and %v partition table not compatible: %v", device, err)
	}

	created, err := diskLayout.Create(lv)
	if err != nil {
		return fmt.Errorf("cannot create the partitions: %v", err)
	}
	if err := partition.MakeFilesystems(created); err != nil {
		return err
	}

	if err := partition.DeployContent(created, gadgetRoot); err != nil {
		return err
	}

	return nil
}

func ensureLayoutCompatibility(gadgetLayout *gadget.LaidOutVolume, diskLayout *partition.DeviceLayout) error {
	eq := func(ds partition.DeviceStructure, gs gadget.LaidOutStructure) bool {
		dv := ds.VolumeStructure
		gv := gs.VolumeStructure
		return dv.Name == gv.Name && ds.StartOffset == gs.StartOffset && dv.Size == gv.Size && dv.Filesystem == gv.Filesystem
	}
	contains := func(haystack []gadget.LaidOutStructure, needle partition.DeviceStructure) bool {
		for _, h := range haystack {
			if eq(needle, h) {
				return true
			}
		}
		return false
	}

	// Check if all existing device partitions are also in gadget
	for _, ds := range diskLayout.Structure {
		if !contains(gadgetLayout.LaidOutStructure, ds) {
			return fmt.Errorf("cannot find disk partition %q (starting at %d) in gadget", ds.VolumeStructure.Label, ds.StartOffset)
		}
	}

	return nil
}
