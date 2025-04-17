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
	"encoding/json"
	"errors"
	"fmt"
	"os"
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

type partitionMount struct {
	GptLabel string
	Where    string
	Opts     *systemdMountOptions
}

type imageManifestPartition struct {
	// GptLabel is the GPT partition label. It is used to identify the partition on the disk.
	GptLabel string `json:"label"`
	// RootHash is the expected dm-verity root hash of the partition. In CVM mode, no further
	// options are passed to veritysetup so this is expected to be a sha256 hash which is
	// veritysetup's default.
	RootHash string `json:"root_hash"`
	// ReadOnly marks the partition as read only. Partitions marked as read only can only be used
	// as lowerdir overlay fs partitions.
	ReadOnly bool `json:"read_only"`
}

type imageManifest struct {
	// Partitions is a list of partitions with their associated dm-verity root hashes and
	// intended overlayfs use.
	Partitions []imageManifestPartition `json:"partitions"`
}

func parseImageManifest(imageManifestFile []byte) (imageManifest, error) {
	var im imageManifest
	err := json.Unmarshal(imageManifestFile, &im)
	if err != nil {
		return imageManifest{}, err
	}

	return im, nil
}

// generateMountsFromManifest performs various coherence checks to partition information coming from an
// imageManifest struct and then creates the necessary overlay fs partitions in the format expected by
// doSystemdMount.
//
// Only a single read-only partition is allowed which will be used as the lowerdir parameter in the final
// overlay fs. This partition needs to have an associated dm-verity partition with the same GPT label followed
// by "-verity". A root hash is also required but not enforced here.
//
// Only a single writable partition is allowed which will be used to host the upperdir and workdir paths in the
// final overlay fs. This can be encrypted as in CVMv1. If a writable partition is not specified, a tmpfs-based
// one will host the upperdir and workdir paths of the overlayfs. This is relevant in ephemeral confidential VM
// scenarios where the confidentiality of the writable data is achieved through hardware memory encryption and
// not disk encryption (the writable data/system state should never touch the disk).
func generateMountsFromManifest(im imageManifest, disk disks.Disk) ([]partitionMount, error) {
	foundReadOnlyPartition := ""
	foundWritablePartition := ""

	partitionMounts := []partitionMount{}

	// Configure the overlay filesystems mounts from the manifest.
	for _, p := range im.Partitions {
		pm := partitionMount{
			Opts: &systemdMountOptions{},
		}

		pm.GptLabel = p.GptLabel

		// All detected partitions are mounted by default under /run/mnt/<GptLabel of partition>
		pm.Where = filepath.Join(boot.InitramfsRunMntDir, p.GptLabel)

		if p.ReadOnly {
			// XXX: currently only a single read-only partition/overlay fs lowerdir is permitted.
			// Support for multiple lowerdirs could be supported in the future.
			if foundReadOnlyPartition != "" {
				return nil, errors.New("manifest contains multiple partitions marked as read-only")

			}
			foundReadOnlyPartition = pm.GptLabel

			// systemd-mount will run fsck by default when attempting to mount the partition and potentially corrupt it.
			// This will cause dm-verity to fail when attempting to set up the dm-verity mount.
			// fsck should be/is run by the encrypt-cloud-image tool prior to generating dm-verity data.
			pm.Opts.NeedsFsck = false

			// Auto-discover verity device from disk.
			verityPartition, err := disk.FindMatchingPartitionWithPartLabel(p.GptLabel + "-verity")
			if err != nil {
				return []partitionMount{}, err
			}

			pm.Opts.FsOpts = &dmVerityOptions{
				RootHash:   p.RootHash,
				HashDevice: verityPartition.KernelDeviceNode,
			}
		} else {
			// Only one writable partition is permitted.
			if foundWritablePartition != "" {
				return nil, errors.New("manifest contains multiple writable partitions")
			}
			// Manifest contains a partition meant to be used as a writable overlay for the non-ephemeral vm case.
			// If it is encrypted, its key will be autodiscovered based on its FsLabel later.
			foundWritablePartition = p.GptLabel
			pm.Opts.NeedsFsck = true
		}

		partitionMounts = append(partitionMounts, pm)
	}

	if foundReadOnlyPartition == "" {
		return nil, errors.New("manifest doesn't contain any partition marked as read-only")
	}

	// If no writable partitions were found in the manifest, Configure a tmpfs filesystem for the upper and workdir layers
	// of the final rootfs mount.
	if foundWritablePartition == "" {
		foundWritablePartition = "writable-tmp"

		pm := partitionMount{
			Where:    filepath.Join(boot.InitramfsRunMntDir, "writable-tmp"),
			GptLabel: "writable-tmp",
			Opts: &systemdMountOptions{
				Tmpfs: true,
			},
		}

		partitionMounts = append(partitionMounts, pm)
	}

	// Configure the merged overlay filesystem mount.
	pm := partitionMount{
		Where:    boot.InitramfsDataDir,
		GptLabel: "cloudimg-rootfs",
		Opts: &systemdMountOptions{
			FsOpts: &overlayFsOptions{
				LowerDirs: []string{filepath.Join(boot.InitramfsRunMntDir, foundReadOnlyPartition)},
				UpperDir:  filepath.Join(boot.InitramfsRunMntDir, foundWritablePartition, "upper"),
				WorkDir:   filepath.Join(boot.InitramfsRunMntDir, foundWritablePartition, "work"),
			},
		},
	}

	partitionMounts = append(partitionMounts, pm)

	return partitionMounts, nil
}

var createOverlayDirs = func(path string) error {
	if err := os.Mkdir(path, 0755); err != nil && !os.IsExist(err) {
		return err
	}
	if err := os.Mkdir(filepath.Join(path, "upper"), 0755); err != nil && !os.IsExist(err) {
		return err
	}
	if err := os.Mkdir(filepath.Join(path, "work"), 0755); err != nil && !os.IsExist(err) {
		return err
	}

	return nil
}

// generateMountsModeRunCVM is used to generate mounts for the special "cloudimg-rootfs" mode which
// mounts the rootfs from a partition on the disk rather than a base snap. It supports TPM-backed FDE
// for the rootfs partition using a sealed key from the seed partition.
//
// It also supports retrieving partition information using a manifest from the seed partition. If a
// manifest file is found under the specified path, it will parse the manifest for mount information,
// otherwise it will follow the default behaviour of auto-discovering a disk with the "cloudimg-rootfs"
// label.
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

	var partitionMounts []partitionMount

	// try searching for a manifest that contains mount information
	imageManifestFile, err := os.ReadFile(filepath.Join(boot.InitramfsUbuntuSeedDir, "EFI/ubuntu", "manifest.json"))
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return err
		}

		// If a manifest file is not found fall-back to CVM v1 behaviour
		partitionMounts = []partitionMount{
			{
				Where:    boot.InitramfsDataDir,
				GptLabel: "cloudimg-rootfs",
				Opts: &systemdMountOptions{
					NeedsFsck: true,
				},
			},
		}
	} else {
		im, err := parseImageManifest(imageManifestFile)
		if err != nil {
			return err
		} else {

			// TODO: the manifest will be also accompanied by a public key and a signature.
			// Here we will also need to validate the signature of the manifest against
			// the public key and then measure a digest of the public key to the TPM.
			// A later remote attestation step will be able to verify that the public
			// key that was measured is an expected one.

			partitionMounts, err = generateMountsFromManifest(im, disk)
			if err != nil {
				return err
			}
		}
	}

	// Provision TPM
	if err := secbootProvisionForCVM(boot.InitramfsUbuntuSeedDir); err != nil {
		return err
	}

	// Mount partitions. In case a manifest is used, generateMountsFromManifest will return
	// the partitions in specific order 1) ro 2) rw 3) overlay fs.
	for _, pm := range partitionMounts {
		var what string
		var unlockRes secboot.UnlockResult

		if !pm.Opts.Tmpfs {
			runModeCVMKey := filepath.Join(boot.InitramfsSeedEncryptionKeyDir, pm.GptLabel+".sealed-key")
			opts := &secboot.UnlockVolumeUsingSealedKeyOptions{
				AllowRecoveryKey: true,
			}
			// UnlovkVolumeUsingSealedKeyIfEncrypted is searching for partitions based on their filesystem label and
			// not the GPT label. Images that are created for CVM mode set both to the same label. The GPT label
			// is used for partition discovery and the filesystem label for auto-discovery of a potentially encrypted
			// partition.
			unlockRes, err = secbootUnlockVolumeUsingSealedKeyIfEncrypted(disk, pm.GptLabel, runModeCVMKey, opts)
			if err != nil {
				return err
			}

			what = unlockRes.FsDevice
		}

		if err := doSystemdMount(what, pm.Where, pm.Opts); err != nil {
			return err
		}

		// Create overlayfs' upperdir and workdir in the writable tmpfs layer. In case there is a writable layer,
		// these directories should have been created during the image creation process.
		if pm.Opts.Tmpfs {
			if err := createOverlayDirs(pm.Where); err != nil {
				return err
			}
		}
	}

	if createSysrootMount() {
		// Create unit for sysroot. We restrict this to Ubuntu 24+ for
		// the moment, until we backport necessary changes to the
		// UC20/22 initramfs. Note that a transient unit is not used as
		// it tries to be restarted after the switch root, and fails.
		rootfsDir := boot.InitramfsDataDir
		if err := writeSysrootMountUnit(rootfsDir, "", nil); err != nil {
			return fmt.Errorf("cannot write sysroot.mount (what: %s): %w", rootfsDir, err)
		}
		if err := recalculateRootfsTarget(); err != nil {
			return err
		}
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
