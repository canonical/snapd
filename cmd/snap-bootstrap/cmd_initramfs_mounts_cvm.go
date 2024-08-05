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
	imageManifestFile, err := os.ReadFile(imageManifestFilePath)
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

// generateMountsModeRunCVM is used to generate mounts for the special "cloudimg-rootfs" mode which
// mounts the rootfs from a partition on the disk rather than a base snap. It supports TPM-backed FDE
// for the rootfs partition using a sealed key from the seed partition.
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

	// Provision TPM TODO: should become "try and provision TPM" to support the unencrypted root fs case
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
			fi, err := os.Stat(pm.Where)
			if err != nil {
				return err
			}
			if err := os.Mkdir(filepath.Join(pm.Where, "upper"), fi.Mode()); err != nil && !os.IsExist(err) {
				return err
			}
			if err := os.Mkdir(filepath.Join(pm.Where, "work"), fi.Mode()); err != nil && !os.IsExist(err) {
				return err
			}
		}

		// Verify that detected non tmpfs partitions come from where we expect them to
		if !pm.Opts.Tmpfs {
			diskOpts := &disks.Options{}
			if unlockRes.IsEncrypted {
				// then we need to specify that the data mountpoint is
				// expected to be a decrypted device
				diskOpts.IsDecryptedDevice = true
			}

			matches, err := disk.MountPointIsFromDisk(pm.Where, diskOpts)
			if err != nil {
				return err
			}
			if !matches {
				// failed to verify that manifest partition mountpoint
				// comes from the same disk as ESP
				return fmt.Errorf("cannot validate boot: %s mountpoint is expected to be from disk %s but is not", pm.GptLabel, disk.Dev())
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
