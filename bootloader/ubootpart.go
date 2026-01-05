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
// two environment copies that can survive filesystem corruption.
//
// At prepare-image time, an initial environment image is created. At runtime, the environment is
// read from and written to the partition device node
// (e.g., /dev/disk/by-partlabel/ubuntu-boot-state).
//
// For security, the kernel command line parameter "snapd_ubootpart_disk" restricts which disk
// snapd will search for the boot state partition.
//
// Bootloader selection: Name() returns ubootName so that gadgets can use the standard uboot.conf
// marker file. This allows gadgets to opt into partition-based environment storage without
// changing their marker file.

package bootloader

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/snapcore/snapd/bootloader/ubootenv"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
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
	// ubuntuBootStateLabel is the partition label for the boot state partition
	ubuntuBootStateLabel = "ubuntu-boot-state"
)

type ubootpart struct {
	rootdir          string
	prepareImageTime bool
	role             Role
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
	// Returns ubootName since this is a U-Boot bootloader; the difference
	// from uboot.go is only the storage backend (partition vs file)
	return ubootName
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

// envDevice returns the path to the environment device/file.
func (u *ubootpart) envDevice() (string, error) {
	if u.prepareImageTime {
		// At prepare-image time, use a file in the build directory
		return filepath.Join(u.dir(), "ubuntu-boot-state.img"), nil
	}

	// At runtime, check kernel cmdline for disk restriction
	disk, err := u.getAllowedDisk()
	if err != nil {
		return "", err
	}

	if disk != "" {
		// Use the specific disk's partition
		// TODO: implement disk-specific partition lookup
		return filepath.Join(dirs.GlobalRootDir, "/dev/disk/by-partlabel/", ubuntuBootStateLabel), nil
	}

	// Use partition by label
	partPath := filepath.Join(dirs.GlobalRootDir, "/dev/disk/by-partlabel/", ubuntuBootStateLabel)
	resolved, err := filepath.EvalSymlinks(partPath)
	if err != nil {
		return "", fmt.Errorf("cannot resolve boot state partition: %v", err)
	}
	return resolved, nil
}

// getAllowedDisk returns the disk specified in kernel cmdline, if any.
func (u *ubootpart) getAllowedDisk() (string, error) {
	// Check for snapd_ubootpart_disk in kernel cmdline
	cmdline, err := kcmdline.KeyValues("snapd_ubootpart_disk")
	if err != nil {
		return "", err
	}
	if disk, ok := cmdline["snapd_ubootpart_disk"]; ok {
		return disk, nil
	}
	return "", nil
}

func (u *ubootpart) Present() (bool, error) {
	if u.prepareImageTime {
		// At prepare-image time, check for uboot.conf marker file
		markerConf := filepath.Join(u.rootdir, "uboot.conf")
		return osutil.FileExists(markerConf), nil
	}

	// At runtime, check for partition by label
	partPath := filepath.Join(dirs.GlobalRootDir, "/dev/disk/by-partlabel/", ubuntuBootStateLabel)
	return osutil.FileExists(partPath), nil
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

	// Create a new redundant environment (CreateRedundant writes to disk)
	_, err = ubootenv.CreateRedundant(envPath, ubootenv.DefaultRedundantEnvSize)
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
