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

package bootstrap

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/cmd/snap-bootstrap/partition"
	"github.com/snapcore/snapd/gadget"
	"github.com/snapcore/snapd/secboot"
)

const (
	ubuntuDataLabel = "ubuntu-data"
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
			return gadget.ParentDiskFromPartition(device)
		}
	}
	return "", fmt.Errorf("cannot find role %s in gadget", role)
}

// Run bootstraps the partitions of a device, by either creating
// missing ones or recreating installed ones.
func Run(gadgetRoot, device string, options Options) error {
	if options.Encrypt && (options.KeyFile == "" || options.RecoveryKeyFile == "") {
		return fmt.Errorf("key file and recovery key file must be specified when encrypting")
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

	// remove partitions added during a previous install attempt
	if err := diskLayout.RemoveCreated(); err != nil {
		return fmt.Errorf("cannot remove partitions from previous install: %v", err)
	}
	// at this point we removed any existing partition, nuke any
	// of the existing sealed key files placed outside of the
	// encrypted partitions (LP: #1879338)
	if options.KeyFile != "" {
		if err := os.Remove(options.KeyFile); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("cannot cleanup obsolete key file: %v", options.KeyFile)
		}

	}

	created, err := diskLayout.CreateMissing(lv)
	if err != nil {
		return fmt.Errorf("cannot create the partitions: %v", err)
	}

	// We're currently generating a single encryption key, this may change later
	// if we create multiple encrypted partitions.
	var key partition.EncryptionKey
	var rkey partition.RecoveryKey

	if options.Encrypt {
		key, err = partition.NewEncryptionKey()
		if err != nil {
			return fmt.Errorf("cannot create encryption key: %v", err)
		}

		rkey, err = partition.NewRecoveryKey()
		if err != nil {
			return fmt.Errorf("cannot create recovery key: %v", err)
		}
	}

	for _, part := range created {
		if options.Encrypt && part.Role == gadget.SystemData {
			dataPart, err := partition.NewEncryptedDevice(&part, key, ubuntuDataLabel)
			if err != nil {
				return err
			}

			if err := dataPart.AddRecoveryKey(key, rkey); err != nil {
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
			if err := partition.MountFilesystem(part, boot.InitramfsRunMntDir); err != nil {
				return err
			}
		}
	}

	if !options.Encrypt {
		return nil
	}

	// Write the recovery key
	if err := rkey.Save(options.RecoveryKeyFile); err != nil {
		return fmt.Errorf("cannot store recovery key: %v", err)
	}

	// TODO:UC20: binaries are EFI/bootloader-specific, hardcoded for now
	loadChain := []string{
		// the path to the shim EFI binary
		filepath.Join(boot.InitramfsUbuntuSeedDir, "EFI/boot/bootx64.efi"),
		// the path to the recovery grub EFI binary
		filepath.Join(boot.InitramfsUbuntuSeedDir, "EFI/boot/grubx64.efi"),
		// the path to the run mode grub EFI binary
		filepath.Join(boot.InitramfsUbuntuBootDir, "EFI/boot/grubx64.efi"),
	}
	if options.KernelPath != "" {
		// the path to the kernel EFI binary
		loadChain = append(loadChain, options.KernelPath)
	}

	// TODO:UC20: get cmdline definition from bootloaders
	kernelCmdlines := []string{
		// run mode
		"snapd_recovery_mode=run console=ttyS0 console=tty1 panic=-1",
		// recover mode
		fmt.Sprintf("snapd_recovery_mode=recover snapd_recovery_system=%s console=ttyS0 console=tty1 panic=-1", options.SystemLabel),
	}

	sealKeyParams := secboot.SealKeyParams{
		ModelParams: []*secboot.SealKeyModelParams{
			{
				Model:          options.Model,
				KernelCmdlines: kernelCmdlines,
				EFILoadChains:  [][]string{loadChain},
			},
		},
		KeyFile:                 options.KeyFile,
		TPMPolicyUpdateDataFile: options.TPMPolicyUpdateDataFile,
		TPMLockoutAuthFile:      options.TPMLockoutAuthFile,
	}

	if err := secboot.SealKey(key, &sealKeyParams); err != nil {
		return fmt.Errorf("cannot seal the encryption key: %v", err)
	}

	return nil
}

func ensureLayoutCompatibility(gadgetLayout *gadget.LaidOutVolume, diskLayout *partition.DeviceLayout) error {
	eq := func(ds partition.DeviceStructure, gs gadget.LaidOutStructure) bool {
		dv := ds.VolumeStructure
		gv := gs.VolumeStructure
		nameMatch := gv.Name == dv.Name
		if gadgetLayout.Schema == "mbr" {
			// partitions have no names in MBR
			nameMatch = true
		}
		// Previous installation may have failed before filesystem creation or partition may be encrypted
		check := nameMatch && ds.StartOffset == gs.StartOffset && (ds.CreatedDuringInstall || dv.Filesystem == gv.Filesystem)
		if gv.Role == gadget.SystemData {
			// system-data may have been expanded
			return check && dv.Size >= gv.Size
		}
		return check && dv.Size == gv.Size
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

	// Check if all existing device partitions are also in gadget
	for _, ds := range diskLayout.Structure {
		if !contains(gadgetLayout.LaidOutStructure, ds) {
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
