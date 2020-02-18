// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019-2020 Canonical Ltd
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
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/gadget"
)

const (
	ubuntuDataLabel = "ubuntu-data"
)

type Options struct {
	// Also mount the filesystems after creation
	Mount bool
	// Encrypt the data partition
	Encrypt bool
	// KeyFile is the location where the encryption key is written to
	KeyFile string
}

func deviceFromRole(lv *gadget.LaidOutVolume, role string) (device string, err error) {
	for _, vs := range lv.LaidOutStructure {
		// XXX: this part of the finding maybe should be a
		// method on gadget.*Volume
		if vs.Role == role {
			device, err = gadget.FindDeviceForStructure(&vs)
			if err != nil {
				return "", fmt.Errorf("cannot find device for role %q: %v", role, err)
			}
			return gadget.ParentDiskFromPartition(device)
		}
	}
	return "", fmt.Errorf("cannot find role %s in gadget", role)
}

func Run(gadgetRoot, device string, options Options) error {
	if options.Encrypt && options.KeyFile == "" {
		return fmt.Errorf("key file must be specified when encrypting")
	}

	if gadgetRoot == "" {
		return fmt.Errorf("cannot use empty gadget root directory")
	}

	lv, err := gadget.PositionedVolumeFromGadget(gadgetRoot)
	if err != nil {
		return fmt.Errorf("cannot layout the volume: %v", err)
	}

	// XXX: the only situation where auto-detect is not desired is
	//      in (spread) testing - consider to remove forcing a device
	//
	// auto-detect device if no device is forced
	if device == "" {
		device, err = deviceFromRole(lv, gadget.SystemSeed)
		if err != nil {
			return fmt.Errorf("cannot find device to create partitions on: %v", err)
		}
	}

	diskLayout, err := partition.DeviceLayoutFromDisk(device)
	if err != nil {
		return fmt.Errorf("cannot read %v partitions: %v", device, err)
	}

	// check if the current partition table is compatible with the gadget,
	// ignoring partitions added by the installer (will be removed later)
	if err := ensureLayoutCompatibility(lv, diskLayout); err != nil {
		return fmt.Errorf("gadget and %v partition table not compatible: %v", device, err)
	}

	// remove partitions added during a previous (failed) install attempt
	if err := diskLayout.RemoveCreated(); err != nil {
		return fmt.Errorf("cannot remove partitions from previous install: %v", err)
	}

	created, err := diskLayout.CreateMissing(lv)
	if err != nil {
		return fmt.Errorf("cannot create the partitions: %v", err)
	}

	// generate key externally so multiple encrypted partitions can use the same key
	var key partition.EncryptionKey
	if options.Encrypt {
		key, err = partition.NewEncryptionKey()
		if err != nil {
			return fmt.Errorf("cannot create encryption key: %v", err)
		}
	}

	for _, part := range created {
		if options.Encrypt && part.Role == gadget.SystemData {
			dataPart, err := partition.NewEncryptedDevice(&part, key, ubuntuDataLabel)
			if err != nil {
				return err
			}
			// update the encrypted device node
			part.Node = dataPart.Node
		}

		if err := partition.MakeFilesystem(part); err != nil {
			return err
		}

		if err := partition.DeployContent(part, gadgetRoot); err != nil {
			return err
		}

		if options.Mount && part.Label != "" && part.HasFilesystem() {
			if err := partition.MountFilesystem(part, dirs.RunMnt); err != nil {
				return err
			}
		}
	}

	// store the encryption key as the last part of the process to reduce the
	// possiblity of exiting with an error after the TPM provisioning
	if options.Encrypt {
		if err := key.Store(options.KeyFile); err != nil {
			return err
		}
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

	if gadgetLayout.Size > diskLayout.Size {
		return fmt.Errorf("device %v (%s) is too small to fit the requested layout (%s)", diskLayout.Device,
			diskLayout.Size.IECString(), gadgetLayout.Size.IECString())
	}

	// Check if top level properties match
	if !isCompatibleSchema(gadgetLayout.Volume.Schema, diskLayout.Schema) {
		return fmt.Errorf("disk partitioning schema %q doesn't match gadget schema %q", diskLayout.Schema, gadgetLayout.Volume.Schema)
	}
	if gadgetLayout.Volume.ID != "" && gadgetLayout.Volume.ID != diskLayout.ID {
		return fmt.Errorf("disk ID %q doesn't match gadget volume ID %q", diskLayout.ID, gadgetLayout.Volume.ID)
	}

	// Check if all existing device partitions, except those that we added
	// ourselves, are also in gadget
	for _, ds := range diskLayout.Structure {
		if !ds.Created && !contains(gadgetLayout.LaidOutStructure, ds) {
			return fmt.Errorf("cannot find disk partition %s (starting at %d) in gadget", ds.Node, ds.StartOffset)
		}
	}

	return nil
}

func isCompatibleSchema(gadgetSchema, diskSchema string) bool {
	switch gadgetSchema {
	// XXX: "mbr,gpt" is currently unsupported
	case "", "gpt":
		return diskSchema == "gpt"
	case "mbr":
		return diskSchema == "dos"
	default:
		return false
	}
}
