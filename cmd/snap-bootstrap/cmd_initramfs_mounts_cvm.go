// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019-2024 Canonical Ltd
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

package main

import (
	"fmt"
	"path/filepath"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/osutil/disks"
	"github.com/snapcore/snapd/secboot"
)

var (
	secbootProvisionForCVM func(initramfsUbuntuSeedDir string) error
)

// XXX: workaround for the lack of model in CVM systems
type genericCVMModel struct{}

func (*genericCVMModel) Classic() bool {
	return true
}

func (*genericCVMModel) Grade() asserts.ModelGrade {
	return "signed"
}

// generateMountsModeRunCVM is used to generate mounts for the special "cloudimg-rootfs" mode which
// mounts the rootfs from a partition on the disk rather than a base snap. It supports TPM-backed FDE
// for the rootfs partition using a sealed key from the seed partition.
func generateMountsModeRunCVM(mst *initramfsMountsState) error {
	mountOpts := &systemdMountOptions{
		// always fsck the partition when we are mounting it, as this is the
		// first partition we will be mounting, we can't know if anything is
		// corrupted yet
		NeedsFsck: true,
		Private:   true,
	}

	// Mount ESP as UbuntuSeedDir which has UEFI label
	if err := mountNonDataPartitionMatchingKernelDisk(boot.InitramfsUbuntuSeedDir, "UEFI", mountOpts); err != nil {
		return err
	}

	// get the disk that we mounted the ESP from as a reference
	// point for future mounts
	disk, err := disks.DiskFromMountPoint(boot.InitramfsUbuntuSeedDir, nil)
	if err != nil {
		return err
	}

	// Mount rootfs
	if err := secbootProvisionForCVM(boot.InitramfsUbuntuSeedDir); err != nil {
		return err
	}
	runModeCVMKey := filepath.Join(boot.InitramfsSeedEncryptionKeyDir, "cloudimg-rootfs.sealed-key")
	opts := &secboot.UnlockVolumeUsingSealedKeyOptions{
		AllowRecoveryKey: true,
	}
	unlockRes, err := secbootUnlockVolumeUsingSealedKeyIfEncrypted(disk, "cloudimg-rootfs", runModeCVMKey, opts)
	if err != nil {
		return err
	}
	fsckSystemdOpts := &systemdMountOptions{
		NeedsFsck: true,
		Ephemeral: true,
	}
	if err := doSystemdMount(unlockRes.FsDevice, boot.InitramfsDataDir, fsckSystemdOpts); err != nil {
		return err
	}

	// Verify that cloudimg-rootfs comes from where we expect it to
	diskOpts := &disks.Options{}
	if unlockRes.IsEncrypted {
		// then we need to specify that the data mountpoint is
		// expected to be a decrypted device
		diskOpts.IsDecryptedDevice = true
	}

	matches, err := disk.MountPointIsFromDisk(boot.InitramfsDataDir, diskOpts)
	if err != nil {
		return err
	}
	if !matches {
		// failed to verify that cloudimg-rootfs mountpoint
		// comes from the same disk as ESP
		return fmt.Errorf("cannot validate boot: cloudimg-rootfs mountpoint is expected to be from disk %s but is not", disk.Dev())
	}

	// Unmount ESP because otherwise unmounting is racy and results in booted systems without ESP
	if err := doSystemdMount("", boot.InitramfsUbuntuSeedDir, &systemdMountOptions{Umount: true, Ephemeral: true}); err != nil {
		return err
	}

	// There is no real model on a CVM device but minimal model
	// information is required by the later code
	mst.SetVerifiedBootModel(&genericCVMModel{})

	return nil
}
