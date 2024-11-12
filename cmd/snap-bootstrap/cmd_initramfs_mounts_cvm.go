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
	osReadFile = os.ReadFile

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

type ImageManifestPartition struct {
	GptLabel string `json:"label"`
	RootHash string `json:"root_hash"`
	Overlay  string `json:"overlay"`
}

type ImageManifest struct {
	Partitions []ImageManifestPartition `json:"partitions"`
}

type ManifestError struct{}

func (e *ManifestError) Error() string {
	return fmt.Sprintf("")
}

func parseImageManifest(imageManifestFilePath string) (ImageManifest, error) {
	imageManifestFile, err := osReadFile(imageManifestFilePath)
	if err != nil {
		return ImageManifest{}, err
	}

	var im ImageManifest
	err = json.Unmarshal(imageManifestFile, &im)
	if err != nil {
		return ImageManifest{}, err
	}

	if len(im.Partitions) < 1 {
		return ImageManifest{}, fmt.Errorf("Invalid manifest: root partition not specified.")
	}

	if im.Partitions[0].Overlay != "lowerdir" {
		return ImageManifest{}, fmt.Errorf("Invalid manifest: expected first partition to be used as lowerdir, %s was found instead.", im.Partitions[0].Overlay)
	}

	return im, nil
}

// generateMountsFromManifest is used to parse a manifest file which contains information about which
// partitions should be used to compose the rootfs of the system using overlayfs.
//
// Only a single overlayfs lowerdir and a single overlayfs upperdir are supported. For the lowerdir, a dm-verity
// root hash can be supplied which will be used during mounting. The writable layer can be encrypted as in CVMv1.
//
// If a writable layer is not specified in the manifest, a tmpfs-based layer is mounted as the upperdir of the
// overlayfs. This is relevant in ephemeral confidential VM scenarios where the confidentiality of the writable
// data is achieved through hardware memory encryption and not disk encryption (the writable data/system state
// should never touch the disk).
func generateMountsFromManifest(im ImageManifest, disk disks.Disk) ([]partitionMount, error) {
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

		if p.Overlay == "lowerdir" {
			// XXX: currently only one lower layer is permitted. The rest, if found, are ignored.
			if foundReadOnlyPartition != "" {
				continue
			}
			foundReadOnlyPartition = pm.GptLabel

			// systemd-mount will run fsck by default when attempting to mount the partition and potentially corrupt it.
			// This will cause dm-verity to fail when attempting to set up the dm-verity mount.
			// fsck should be/is run by the encrypt-cloud-image tool prior to generating dm-verity data.
			pm.Opts.NeedsFsck = false
			pm.Opts.VerityRootHash = p.RootHash

			// Auto-discover verity device from disk for partition types meant to be used as lowerdir
			verityPartition, err := disk.FindMatchingPartitionWithPartLabel(p.GptLabel + "-verity")
			if err != nil {
				return []partitionMount{}, err
			}
			pm.Opts.VerityHashDevice = verityPartition.KernelDeviceNode
		} else {
			// Only one writable partition is permitted.
			if foundWritablePartition != "" {
				continue
			}
			// Manifest contains a partition meant to be used as a writable overlay for the non-ephemeral vm case.
			// If it is encrypted, its key will be autodiscovered based on its FsLabel later.
			foundWritablePartition = p.GptLabel
			pm.Opts.NeedsFsck = true
		}

		partitionMounts = append(partitionMounts, pm)
	}

	if foundReadOnlyPartition == "" {
		return nil, fmt.Errorf("manifest doesn't contain any partition with Overlay type lowerdir")
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
			Overlayfs: true,
			LowerDirs: []string{filepath.Join(boot.InitramfsRunMntDir, partitionMounts[0].GptLabel)},
		},
	}

	pm.Opts.UpperDir = filepath.Join(boot.InitramfsRunMntDir, foundWritablePartition, "upper")
	pm.Opts.WorkDir = filepath.Join(boot.InitramfsRunMntDir, foundWritablePartition, "work")

	partitionMounts = append(partitionMounts, pm)

	return partitionMounts, nil
}

var createOverlayDirs = func(path string) error {
	fi, err := os.Stat(path)
	if err != nil {
		return err
	}
	if err := os.Mkdir(path, fi.Mode()); err != nil && !os.IsExist(err) {
		return err
	}
	if err := os.Mkdir(filepath.Join(path, "upper"), fi.Mode()); err != nil && !os.IsExist(err) {
		return err
	}
	if err := os.Mkdir(filepath.Join(path, "work"), fi.Mode()); err != nil && !os.IsExist(err) {
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
	// Mount ESP as UbuntuSeedDir which has UEFI label
	if err := mountNonDataPartitionMatchingKernelDisk(boot.InitramfsUbuntuSeedDir, "UEFI"); err != nil {
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
	imageManifestFilePath := filepath.Join(boot.InitramfsUbuntuSeedDir, "EFI/ubuntu", "manifest.json")
	im, err := parseImageManifest(imageManifestFilePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			// XXX: if a manifest file is not found fall-back to CVM v1 behaviour
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
			return err
		}
	} else {
		partitionMounts, err = generateMountsFromManifest(im, disk)
		if err != nil {
			return err
		}
	}

	// Provision TPM
	if err := secbootProvisionForCVM(boot.InitramfsUbuntuSeedDir); err != nil {
		return err
	}

	// Mount partitions
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

		// Create overlayfs' upperdir and workdir in the writable tmpfs layer.
		if pm.Opts.Tmpfs {
			if err := createOverlayDirs(pm.Where); err != nil {
				return err
			}
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
