// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020 Canonical Ltd
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

package boot

import (
	"path/filepath"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/gadget"
)

var (
	// InitramfsRunMntDir is the directory where ubuntu partitions are mounted
	// during the initramfs.
	InitramfsRunMntDir string

	// InitramfsDataDir is the location of system-data role partition
	// (typically a partition labeled "ubuntu-data") during the initramfs.
	InitramfsDataDir string

	// InitramfsHostUbuntuDataDir is the location of the host ubuntu-data
	// during the initramfs, typically used in recover mode.
	InitramfsHostUbuntuDataDir string

	// InitramfsUbuntuBootDir is the location of ubuntu-boot during the
	// initramfs.
	InitramfsUbuntuBootDir string

	// InitramfsUbuntuSeedDir is the location of ubuntu-seed during the
	// initramfs.
	InitramfsUbuntuSeedDir string

	// InitramfsUbuntuSaveDir is the location of ubuntu-save during the
	// initramfs.
	InitramfsUbuntuSaveDir string

	// InstallHostFDESaveDir is the directory of the FDE data on the
	// ubuntu-save partition during install mode. For other modes,
	// use dirs.SnapSaveFDEDirUnder().
	InstallHostFDESaveDir string

	// InstallHostSaveDir is the directory of device data on ubuntu-save during
	// install mode. For other modes use dirs.SnapDeviceSaveDir
	InstallHostDeviceSaveDir string

	// InitramfsSeedEncryptionKeyDir is the location of the encrypted partition
	// keys during the initramfs on ubuntu-seed.
	InitramfsSeedEncryptionKeyDir string

	// InitramfsBootEncryptionKeyDir is the location of the encrypted partition
	// keys during the initramfs on ubuntu-boot.
	InitramfsBootEncryptionKeyDir string

	// snapBootFlagsFile is the location of the file that is used
	// internally for saving the current boot flags active for this boot.
	snapBootFlagsFile string
)

// InstallHostWritableDir is the location of the writable partition of the
// installed host during install mode. This should always be on a physical
// partition.
func InstallHostWritableDir(mod gadget.Model) string {
	if mod.Classic() {
		return filepath.Join(InitramfsRunMntDir, "ubuntu-data")
	}
	return filepath.Join(InitramfsRunMntDir, "ubuntu-data", "system-data")
}

// InitramfsHostWritableDir is the location of the host writable
// partition during the initramfs, typically used in recover mode.
func InitramfsHostWritableDir(mod gadget.Model) string {
	if mod.Classic() {
		return InitramfsHostUbuntuDataDir
	}
	return filepath.Join(InitramfsHostUbuntuDataDir, "system-data")
}

// InitramfsWritableDir is the location of the writable partition during the
// initramfs. Note that this may refer to a temporary filesystem or a
// physical partition depending on what system mode the system is in.
//
// This needs the "isRunMode" in the future for when we implement a
// recovery system on "classic+modes". In this scenario in "run" mode
// we have the debian based rootfs in /run/mnt/ubuntu-data *but* in
// "recover" mode the rootfs comes from a base snap like "core22" so
// we need to generate "ubuntu-core" like paths.
func InitramfsWritableDir(mod gadget.Model, isRunMode bool) string {
	if mod.Classic() && isRunMode {
		return InitramfsDataDir
	}
	return filepath.Join(InitramfsDataDir, "system-data")
}

// InstallHostFDEDataDir is the location of the FDE data during install mode.
func InstallHostFDEDataDir(mod gadget.Model) string {
	return dirs.SnapFDEDirUnder(InstallHostWritableDir(mod))
}

func setInitramfsDirVars(rootdir string) {
	InitramfsRunMntDir = filepath.Join(rootdir, "run/mnt")
	InitramfsDataDir = filepath.Join(InitramfsRunMntDir, "data")
	InitramfsHostUbuntuDataDir = filepath.Join(InitramfsRunMntDir, "host", "ubuntu-data")
	InitramfsUbuntuBootDir = filepath.Join(InitramfsRunMntDir, "ubuntu-boot")
	InitramfsUbuntuSeedDir = filepath.Join(InitramfsRunMntDir, "ubuntu-seed")
	InitramfsUbuntuSaveDir = filepath.Join(InitramfsRunMntDir, "ubuntu-save")

	InstallHostDeviceSaveDir = filepath.Join(InitramfsUbuntuSaveDir, "device")
	InstallHostFDESaveDir = filepath.Join(InstallHostDeviceSaveDir, "fde")
	InitramfsSeedEncryptionKeyDir = filepath.Join(InitramfsUbuntuSeedDir, "device/fde")
	InitramfsBootEncryptionKeyDir = filepath.Join(InitramfsUbuntuBootDir, "device/fde")

	snapBootFlagsFile = filepath.Join(dirs.SnapRunDir, "boot-flags")
}

func init() {
	setInitramfsDirVars(dirs.GlobalRootDir)
	// register to change the values of Initramfs* dir values when the global
	// root dir changes
	dirs.AddRootDirCallback(setInitramfsDirVars)
}
