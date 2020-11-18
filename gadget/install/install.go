// -*- Mode: Go; indent-tabs-mode: t -*-
// +build !nosecboot

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

package install

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/gadget"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/secboot"
)

const (
	ubuntuDataLabel = "ubuntu-data"
	ubuntuSaveLabel = "ubuntu-save"
)

func deviceFromRole(lv *gadget.LaidOutVolume, role string) (device string, err error) {
	for _, vs := range lv.LaidOutStructure {
		// XXX: this part of the finding maybe should be a
		// method on gadget.*Volume
		if vs.Role == role {
			device, err = gadget.FindDeviceForStructure(&vs)
			if err != nil {
				return "", fmt.Errorf("cannot find device for role %q: %v", role, err)
			}
			return gadget.ParentDiskFromMountSource(device)
		}
	}
	return "", fmt.Errorf("cannot find role %s in gadget", role)
}

// Run bootstraps the partitions of a device, by either creating
// missing ones or recreating installed ones.
func Run(gadgetRoot, device string, options Options, observer gadget.ContentObserver) (*InstalledSystemSideData, error) {
	logger.Noticef("installing a new system")
	logger.Noticef("        gadget data from: %v", gadgetRoot)
	if options.Encrypt {
		logger.Noticef("        encryption: on")
	}
	if gadgetRoot == "" {
		return nil, fmt.Errorf("cannot use empty gadget root directory")
	}

	lv, err := gadget.PositionedVolumeFromGadget(gadgetRoot)
	if err != nil {
		return nil, fmt.Errorf("cannot layout the volume: %v", err)
	}

	// XXX: the only situation where auto-detect is not desired is
	//      in (spread) testing - consider to remove forcing a device
	//
	// auto-detect device if no device is forced
	if device == "" {
		device, err = deviceFromRole(lv, gadget.SystemSeed)
		if err != nil {
			return nil, fmt.Errorf("cannot find device to create partitions on: %v", err)
		}
	}

	diskLayout, err := gadget.OnDiskVolumeFromDevice(device)
	if err != nil {
		return nil, fmt.Errorf("cannot read %v partitions: %v", device, err)
	}

	// check if the current partition table is compatible with the gadget,
	// ignoring partitions added by the installer (will be removed later)
	if err := ensureLayoutCompatibility(lv, diskLayout); err != nil {
		return nil, fmt.Errorf("gadget and %v partition table not compatible: %v", device, err)
	}

	// remove partitions added during a previous install attempt
	if err := removeCreatedPartitions(lv, diskLayout); err != nil {
		return nil, fmt.Errorf("cannot remove partitions from previous install: %v", err)
	}
	// at this point we removed any existing partition, nuke any
	// of the existing sealed key files placed outside of the
	// encrypted partitions (LP: #1879338)
	sealedKeyFiles, _ := filepath.Glob(filepath.Join(boot.InitramfsSeedEncryptionKeyDir, "*.sealed-key"))
	for _, keyFile := range sealedKeyFiles {
		if err := os.Remove(keyFile); err != nil && !os.IsNotExist(err) {
			return nil, fmt.Errorf("cannot cleanup obsolete key file: %v", keyFile)
		}
	}

	created, err := createMissingPartitions(diskLayout, lv)
	if err != nil {
		return nil, fmt.Errorf("cannot create the partitions: %v", err)
	}

	makeKeySet := func() (*EncryptionKeySet, error) {
		key, err := secboot.NewEncryptionKey()
		if err != nil {
			return nil, fmt.Errorf("cannot create encryption key: %v", err)
		}

		rkey, err := secboot.NewRecoveryKey()
		if err != nil {
			return nil, fmt.Errorf("cannot create recovery key: %v", err)
		}
		return &EncryptionKeySet{
			Key:         key,
			RecoveryKey: rkey,
		}, nil
	}
	roleNeedsEncryption := func(role string) bool {
		return role == gadget.SystemData || role == gadget.SystemSave
	}
	var keysForRoles map[string]*EncryptionKeySet

	for _, part := range created {
		roleFmt := ""
		if part.Role != "" {
			roleFmt = fmt.Sprintf("role %v", part.Role)
		}
		logger.Noticef("created new partition %v for structure %v (size %v) %s",
			part.Node, part, part.Size.IECString(), roleFmt)
		if options.Encrypt && roleNeedsEncryption(part.Role) {
			keys, err := makeKeySet()
			if err != nil {
				return nil, err
			}
			logger.Noticef("encrypting partition device %v", part.Node)
			dataPart, err := newEncryptedDevice(&part, keys.Key, part.Label)
			if err != nil {
				return nil, err
			}

			if err := dataPart.AddRecoveryKey(keys.Key, keys.RecoveryKey); err != nil {
				return nil, err
			}

			// update the encrypted device node
			part.Node = dataPart.Node
			if keysForRoles == nil {
				keysForRoles = map[string]*EncryptionKeySet{}
			}
			keysForRoles[part.Role] = keys
			logger.Noticef("encrypted device %v", part.Node)
		}

		if err := makeFilesystem(&part); err != nil {
			return nil, err
		}

		if err := writeContent(&part, gadgetRoot, observer); err != nil {
			return nil, err
		}

		if options.Mount && part.Label != "" && part.HasFilesystem() {
			if err := mountFilesystem(&part, boot.InitramfsRunMntDir); err != nil {
				return nil, err
			}
		}
	}

	return &InstalledSystemSideData{
		KeysForRoles: keysForRoles,
	}, nil
}

// isCreatableAtInstall returns whether the gadget structure would be created at
// install - currently that is only ubuntu-save, ubuntu-data, and ubuntu-boot
func isCreatableAtInstall(gv *gadget.VolumeStructure) bool {
	// a structure is creatable at install if it is one of the roles for
	// system-save, system-data, or system-boot
	switch gv.Role {
	case gadget.SystemSave, gadget.SystemData, gadget.SystemBoot:
		return true
	default:
		return false
	}
}

func ensureLayoutCompatibility(gadgetLayout *gadget.LaidOutVolume, diskLayout *gadget.OnDiskVolume) error {
	eq := func(ds gadget.OnDiskStructure, gs gadget.LaidOutStructure) (bool, string) {
		dv := ds.VolumeStructure
		gv := gs.VolumeStructure
		nameMatch := gv.Name == dv.Name
		if gadgetLayout.Schema == "mbr" {
			// partitions have no names in MBR so bypass the name check
			nameMatch = true
		}
		// Previous installation may have failed before filesystem creation or
		// partition may be encrypted, so if the on disk offset matches the
		// gadget offset, and the gadget structure is creatable during install,
		// then they are equal
		// otherwise, if they are not created during installation, the
		// filesystem must be the same
		check := nameMatch && ds.StartOffset == gs.StartOffset && (isCreatableAtInstall(gv) || dv.Filesystem == gv.Filesystem)
		sizeMatches := dv.Size == gv.Size
		if gv.Role == gadget.SystemData {
			// system-data may have been expanded
			sizeMatches = dv.Size >= gv.Size
		}
		if check && sizeMatches {
			return true, ""
		}
		switch {
		case !nameMatch:
			// don't return a reason if the names don't match
			return false, ""
		case ds.StartOffset != gs.StartOffset:
			return false, fmt.Sprintf("start offsets do not match (disk: %d (%s) and gadget: %d (%s))", ds.StartOffset, ds.StartOffset.IECString(), gs.StartOffset, gs.StartOffset.IECString())
		case !isCreatableAtInstall(gv) && dv.Filesystem != gv.Filesystem:
			return false, "filesystems do not match and the partition is not creatable at install"
		case dv.Size < gv.Size:
			return false, "on disk size is smaller than gadget size"
		case gv.Role != gadget.SystemData && dv.Size > gv.Size:
			return false, "on disk size is larger than gadget size (and the role should not be expanded)"
		default:
			return false, "some other logic condition (should be impossible?)"
		}
	}

	contains := func(haystack []gadget.LaidOutStructure, needle gadget.OnDiskStructure) (bool, string) {
		reasonAbsent := ""
		for _, h := range haystack {
			matches, reasonNotMatches := eq(needle, h)
			if matches {
				return true, ""
			}
			// this has the effect of only returning the last non-empty reason
			// string
			if reasonNotMatches != "" {
				reasonAbsent = reasonNotMatches
			}
		}
		return false, reasonAbsent
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

	// Check if all existing device partitions are also in gadget
	for _, ds := range diskLayout.Structure {
		present, reasonAbsent := contains(gadgetLayout.LaidOutStructure, ds)
		if !present {
			if reasonAbsent != "" {
				// use the right format so that it can be
				// appended to the error message
				reasonAbsent = fmt.Sprintf(": %s", reasonAbsent)
			}
			return fmt.Errorf("cannot find disk partition %s (starting at %d) in gadget%s", ds.Node, ds.StartOffset, reasonAbsent)
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
