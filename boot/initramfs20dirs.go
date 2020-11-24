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

	// InitramfsHostWritableDir is the location of the host writable
	// partition during the initramfs, typically used in recover mode.
	InitramfsHostWritableDir string

	// InitramfsUbuntuBootDir is the location of ubuntu-boot during the
	// initramfs.
	InitramfsUbuntuBootDir string

	// InitramfsUbuntuSeedDir is the location of ubuntu-seed during the
	// initramfs.
	InitramfsUbuntuSeedDir string

	// InitramfsUbuntuSaveDir is the location of ubuntu-save during the
	// initramfs.
	InitramfsUbuntuSaveDir string

	// InitramfsWritableDir is the location of the writable partition during the
	// initramfs. Note that this may refer to a temporary filesystem or a
	// physical partition depending on what system mode the system is in.
	InitramfsWritableDir string

	// InstallHostWritableDir is the location of the writable partition of the
	// installed host during install mode. This should always be on a physical
	// partition.
	InstallHostWritableDir string

	// InstallHostFDEDataDir is the location of the FDE data during install mode.
	InstallHostFDEDataDir string

	// InstallHostFDESaveDir is the directory of the FDE data on the
	// ubuntu-save partition during install mode. For other modes,
	// use dirs.SnapSaveFDEDirUnder().
	InstallHostFDESaveDir string

	// InitramfsSeedEncryptionKeyDir is the location of the encrypted partition
	// keys during the initramfs on ubuntu-seed.
	InitramfsSeedEncryptionKeyDir string

	// InitramfsBootEncryptionKeyDir is the location of the encrypted partition
	// keys during the initramfs on ubuntu-boot.
	InitramfsBootEncryptionKeyDir string
)

func setInitramfsDirVars(rootdir string) {
	InitramfsRunMntDir = filepath.Join(rootdir, "run/mnt")
	InitramfsDataDir = filepath.Join(InitramfsRunMntDir, "data")
	InitramfsHostUbuntuDataDir = filepath.Join(InitramfsRunMntDir, "host", "ubuntu-data")
	InitramfsHostWritableDir = filepath.Join(InitramfsHostUbuntuDataDir, "system-data")
	InitramfsUbuntuBootDir = filepath.Join(InitramfsRunMntDir, "ubuntu-boot")
	InitramfsUbuntuSeedDir = filepath.Join(InitramfsRunMntDir, "ubuntu-seed")
	InitramfsUbuntuSaveDir = filepath.Join(InitramfsRunMntDir, "ubuntu-save")
	InstallHostWritableDir = filepath.Join(InitramfsRunMntDir, "ubuntu-data", "system-data")
	InstallHostFDEDataDir = dirs.SnapFDEDirUnder(InstallHostWritableDir)
	InstallHostFDESaveDir = filepath.Join(InitramfsUbuntuSaveDir, "device/fde")
	InitramfsWritableDir = filepath.Join(InitramfsDataDir, "system-data")
	InitramfsSeedEncryptionKeyDir = filepath.Join(InitramfsUbuntuSeedDir, "device/fde")
	InitramfsBootEncryptionKeyDir = filepath.Join(InitramfsUbuntuBootDir, "device/fde")
}

func init() {
	setInitramfsDirVars(dirs.GlobalRootDir)
	// register to change the values of Initramfs* dir values when the global
	// root dir changes
	dirs.AddRootDirCallback(setInitramfsDirVars)
}
