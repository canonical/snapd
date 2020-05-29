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

	"github.com/snapcore/snapd/asserts"
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

	// remove partitions added during a previous (failed) install attempt
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

	// generate keys externally so multiple encrypted partitions can use the same key
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

	// store the encryption key as the last part of the process to reduce the
	// possibility of exiting with an error after the TPM provisioning
	if options.Encrypt {
		if err := tpmSealKey(key, rkey, options); err != nil {
			return fmt.Errorf("cannot seal the encryption key: %v", err)
		}
	}

	return nil
}

func tpmSealKey(key partition.EncryptionKey, rkey partition.RecoveryKey, options Options) error {
	if err := rkey.Store(options.RecoveryKeyFile); err != nil {
		return fmt.Errorf("cannot store recovery key: %v", err)
	}

	tpm, err := secboot.NewTPMSupport()
	if err != nil {
		return fmt.Errorf("cannot initialize TPM: %v", err)
	}

	if options.TPMLockoutAuthFile != "" {
		if err := tpm.StoreLockoutAuth(options.TPMLockoutAuthFile); err != nil {
			return fmt.Errorf("cannot store TPM lockout authorization: %v", err)
		}
	}

	shim := filepath.Join(boot.InitramfsUbuntuBootDir, "EFI/boot/bootx64.efi")
	grub := filepath.Join(boot.InitramfsUbuntuBootDir, "EFI/boot/grubx64.efi")

	// TODO:UC20: Fix EFI image loading events
	//
	// The idea of EFIImageLoadEvent is to build a set of load paths for the current
	// device configuration. So you could have something like this:
	//
	// shim -> recovery grub -> recovery kernel 1
	//                      |-> recovery kernel 2
	//                      |-> recovery kernel ...
	//                      |-> normal grub -> run kernel good
	//                                     |-> run kernel try
	//
	// Or it could look like this, which is the same thing:
	//
	// shim -> recovery grub -> recovery kernel 1
	// shim -> recovery grub -> recovery kernel 2
	// shim -> recovery grub -> recovery kernel ...
	// shim -> recovery grub -> normal grub -> run kernel good
	// shim -> recovery grub -> normal grub -> run kernel try
	//
	// This implementation in #8476, seems to just build a tree of shim -> grub -> kernel
	// sequences for every combination of supplied input file, although the code here just
	// specifies a single shim, grub and kernel binary, so you get one load path that looks
	// like this:
	//
	// shim -> grub -> kernel
	//
	// This is ok for now because every boot path uses the same chain of trust, regardless
	// of which kernel you're booting or whether you're booting through both the recovery
	// and normal grubs. But when we add the ability to seal against specific binaries in
	// order to secure the system with the Microsoft chain of trust, then the actual trees
	// of EFIImageLoadEvents will need to match the exact supported boot sequences.

	if err := tpm.SetShimFiles(shim); err != nil {
		return err
	}
	if err := tpm.SetBootloaderFiles(grub); err != nil {
		return err
	}
	if options.KernelPath != "" {
		if err := tpm.SetKernelFiles(options.KernelPath); err != nil {
			return err
		}
	}

	// TODO:UC20: get cmdline definition from bootloaders
	kernelCmdlines := []string{
		// run mode
		"snapd_recovery_mode=run console=ttyS0 console=tty1 panic=-1",
		// recover mode
		fmt.Sprintf("snapd_recovery_mode=recover snapd_recovery_system=%s console=ttyS0 console=tty1 panic=-1", options.SystemLabel),
	}

	tpm.SetKernelCmdlines(kernelCmdlines)

	if options.Model != nil {
		tpm.SetModels([]*asserts.Model{options.Model})
	}

	// Provision the TPM as late as possible
	// TODO:UC20: ideally we should ask the firmware to clear the TPM and then reboot
	//            if the device has previously been provisioned, see
	//            https://godoc.org/github.com/snapcore/secboot#RequestTPMClearUsingPPI
	if err := tpm.Provision(); err != nil {
		return fmt.Errorf("cannot provision the TPM: %v", err)
	}

	if err := tpm.Seal(key[:], options.KeyFile, options.PolicyUpdateDataFile); err != nil {
		return fmt.Errorf("cannot seal and store encryption key: %v", err)
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
