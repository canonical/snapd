// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2026 Canonical Ltd
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

// ubootpart implements a bootloader that stores the U-Boot environment in a raw partition with
// redundancy support. This is used when the gadget defines a system-boot-state partition role.
//
// Unlike the standard 'uboot' bootloader which stores the environment in a file on a FAT
// filesystem, ubootpart writes directly to a raw partition. This provides true redundancy with
// two environment copies that are not affected by filesystem corruption, as there is no
// filesystem.
//
// At prepare-image time, an initial environment image is created. At runtime, the environment is
// read from and written to the partition device node
// (e.g., /dev/disk/by-partlabel/ubuntu-boot-state).
//
// For security, the kernel command line parameter "snapd_system_disk" restricts which disk
// snapd will search for the boot state partition.
//
// Bootloader selection: Name() returns ubootpartName and gadgets use a ubootpart.conf marker
// file. This makes ubootpart a first-class bootloader discoverable through the standard
// bootloader machinery, without special-casing in ForGadget().

package bootloader

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/snapcore/snapd/bootloader/efi"
	"github.com/snapcore/snapd/bootloader/ubootenv"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/osutil/disks"
	"github.com/snapcore/snapd/osutil/kcmdline"
	"github.com/snapcore/snapd/snap"
)

// ubootpart implements the Bootloader interface for U-Boot with the
// environment stored in a raw partition (system-boot-state role).
var (
	_ Bootloader                             = (*ubootpart)(nil)
	_ ExtractedRecoveryKernelImageBootloader = (*ubootpart)(nil)
)

const (
	// ubootpartName is the bootloader name for the partition-based U-Boot implementation.
	ubootpartName = "ubootpart"

	// ubuntuBootStateLabel is the partition label for the boot state partition
	ubuntuBootStateLabel = "ubuntu-boot-state"
)

type ubootpart struct {
	rootdir          string
	prepareImageTime bool
	role             Role

	// blDisk is the disk to search for the boot state partition,
	// as specified by the snapd_system_disk kernel command line parameter
	blDisk disks.Disk
}

func (u *ubootpart) processBlOpts(blOpts *Options) {
	if blOpts != nil {
		u.prepareImageTime = blOpts.PrepareImageTime
		u.role = blOpts.Role
	}
}

// newUbootPart creates a new ubootpart bootloader instance.
func newUbootPart(rootdir string, blOpts *Options) Bootloader {
	u := &ubootpart{
		rootdir: rootdir,
	}
	u.processBlOpts(blOpts)
	return u
}

func (u *ubootpart) Name() string {
	return ubootpartName
}

func (u *ubootpart) dir() string {
	if u.rootdir == "" {
		panic("internal error: unset rootdir")
	}
	if u.prepareImageTime {
		return filepath.Join(u.rootdir, "/boot/uboot/")
	}
	// At runtime, we use the partition device
	return filepath.Join(dirs.GlobalRootDir, "/dev/disk/by-partlabel/")
}

// diskFromEFI attempts to find the boot disk using the EFI
// LoaderDevicePartUUID variable set by shim during boot. It returns nil
// if EFI is not available or the variable is not set.
func diskFromEFI() (disks.Disk, error) {
	const loaderDevicePartUUID = "LoaderDevicePartUUID-4a67b082-0a4c-41cf-b6c7-440b29bb8c4f"

	partuuid, _, err := efi.ReadVarString(loaderDevicePartUUID)
	if err != nil {
		if err == efi.ErrNoEFISystem {
			return nil, nil
		}
		return nil, nil
	}

	partNode := filepath.Join(dirs.GlobalRootDir, "/dev/disk/by-partuuid", strings.ToLower(partuuid))
	resolved, err := filepath.EvalSymlinks(partNode)
	if err != nil {
		return nil, fmt.Errorf("cannot resolve EFI boot partition %q: %v", partNode, err)
	}

	disk, err := disks.DiskFromPartitionDeviceNode(resolved)
	if err != nil {
		return nil, fmt.Errorf("cannot find disk for EFI boot partition %q: %v", resolved, err)
	}
	return disk, nil
}

// envDevice returns the path to the environment device/file.
func (u *ubootpart) envDevice() (string, error) {
	if u.prepareImageTime {
		// At prepare-image time, use a file in the build directory
		return filepath.Join(u.dir(), "ubuntu-boot-state.img"), nil
	}

	// At runtime, lazily initialise the disk. Try EFI first, then
	// fall back to the snapd_system_disk kernel command line parameter.
	if u.blDisk == nil {
		disk, err := diskFromEFI()
		if err != nil {
			return "", err
		}
		u.blDisk = disk
	}

	if u.blDisk == nil {
		m, err := kcmdline.KeyValues("snapd_system_disk")
		if err != nil {
			return "", err
		}
		if diskName, ok := m["snapd_system_disk"]; ok {
			// Try device name first, then fall back to device path
			disk, err := disks.DiskFromDeviceName(diskName)
			if err != nil {
				disk, err = disks.DiskFromDevicePath(diskName)
				if err != nil {
					return "", fmt.Errorf("cannot find disk %q: %v", diskName, err)
				}
			}
			u.blDisk = disk
		}
	}

	if u.blDisk != nil {
		partUUID, err := u.blDisk.FindMatchingPartitionUUIDWithPartLabel(ubuntuBootStateLabel)
		if err != nil {
			return "", err
		}
		return filepath.Join(dirs.GlobalRootDir, "/dev/disk/by-partuuid", partUUID), nil
	}

	// Neither EFI nor snapd_system_disk available: fall back to
	// partition by label. This is the common path on first boot
	// before the kernel command line is configured, or on non-EFI
	// systems without snapd_system_disk set.
	partPath := filepath.Join(dirs.GlobalRootDir, "/dev/disk/by-partlabel/", ubuntuBootStateLabel)
	resolved, err := filepath.EvalSymlinks(partPath)
	if err != nil {
		return "", fmt.Errorf("cannot resolve boot state partition: %v", err)
	}
	return resolved, nil
}

func (u *ubootpart) Present() (bool, error) {
	if u.prepareImageTime {
		// At prepare-image time, check for the installed environment file
		envFile := filepath.Join(u.dir(), "ubuntu-boot-state.img")
		return osutil.FileExists(envFile), nil
	}

	// At runtime, check for partition by label
	partPath := filepath.Join(dirs.GlobalRootDir, "/dev/disk/by-partlabel/", ubuntuBootStateLabel)
	return osutil.FileExists(partPath), nil
}

// envSizeFromGadget reads the environment size from the gadget's reference
// boot.sel file. The environment size is a U-Boot compile option, so the
// gadget must specify it. Falls back to DefaultRedundantEnvSize if no
// reference file is present.
func (u *ubootpart) envSizeFromGadget(gadgetDir string) int {
	ref, err := ubootenv.OpenWithFlags(filepath.Join(gadgetDir, "boot.sel"), ubootenv.OpenBestEffort)
	if err != nil {
		return ubootenv.DefaultRedundantEnvSize
	}
	return ref.Size()
}

func (u *ubootpart) InstallBootConfig(gadgetDir string, blOpts *Options) error {
	u.processBlOpts(blOpts)

	envPath, err := u.envDevice()
	if err != nil {
		return err
	}

	// Create directory if needed (for prepare-image time)
	if u.prepareImageTime {
		if err := os.MkdirAll(filepath.Dir(envPath), 0755); err != nil {
			return err
		}
	}

	// Create a new redundant environment, honouring the size from the
	// gadget's reference boot.sel (the env size is a U-Boot compile option)
	envSize := u.envSizeFromGadget(gadgetDir)
	_, err = ubootenv.CreateRedundant(envPath, envSize)
	return err
}

func (u *ubootpart) SetBootVars(values map[string]string) error {
	envPath, err := u.envDevice()
	if err != nil {
		return err
	}

	env, err := ubootenv.OpenRedundantWithFlags(envPath, ubootenv.DefaultRedundantEnvSize, ubootenv.OpenBestEffort)
	if err != nil {
		return err
	}
	return setBootVarsInEnv(env, values)
}

func (u *ubootpart) GetBootVars(names ...string) (map[string]string, error) {
	envPath, err := u.envDevice()
	if err != nil {
		return nil, err
	}

	env, err := ubootenv.OpenRedundantWithFlags(envPath, ubootenv.DefaultRedundantEnvSize, ubootenv.OpenBestEffort)
	if err != nil {
		return nil, err
	}
	return getBootVarsFromEnv(env, names...), nil
}

func (u *ubootpart) ExtractKernelAssets(s snap.PlaceInfo, snapf snap.Container) error {
	dstDir := filepath.Join(u.rootdir, "/boot/uboot/", s.Filename())
	return extractKernelAssetsToBootDir(dstDir, snapf, ubootKernelAssets)
}

func (u *ubootpart) ExtractRecoveryKernelAssets(recoverySystemDir string, s snap.PlaceInfo, snapf snap.Container) error {
	if recoverySystemDir == "" {
		return fmt.Errorf("internal error: recoverySystemDir unset")
	}

	recoveryKernelAssetsDir := filepath.Join(u.rootdir, recoverySystemDir, "kernel")
	return extractKernelAssetsToBootDir(recoveryKernelAssetsDir, snapf, ubootKernelAssets)
}

func (u *ubootpart) RemoveKernelAssets(s snap.PlaceInfo) error {
	return removeKernelAssetsFromBootDir(filepath.Join(u.rootdir, "/boot/uboot/"), s)
}
