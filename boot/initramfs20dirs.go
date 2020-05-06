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

	// InitramfsUbuntuBootDir is the location of ubuntu-boot during the
	// initramfs.
	InitramfsUbuntuBootDir string

	// InitramfsUbuntuSeedDir is the location of ubuntu-seed during the
	// initramfs.
	InitramfsUbuntuSeedDir string

	// InitramfsWritableDir is the location of the writable partition during the
	// initramfs.
	InitramfsWritableDir string

	// InitramfsWritableDir is the location of the encrypted partition keys
	// during the initramfs.
	InitramfsEncryptionKeyDir string
)

func setInitramfsDirVars(rootdir string) {
	InitramfsRunMntDir = filepath.Join(rootdir, "run/mnt")
	InitramfsDataDir = filepath.Join(InitramfsRunMntDir, "data")
	// TODO:UC20 remove the compatibility mount once the transition period is over
	InitramfsHostUbuntuDataDir = filepath.Join(InitramfsRunMntDir, "host", "ubuntu-data")
	InitramfsUbuntuBootDir = filepath.Join(InitramfsRunMntDir, "ubuntu-boot")
	InitramfsUbuntuSeedDir = filepath.Join(InitramfsRunMntDir, "ubuntu-seed")
	InitramfsWritableDir = filepath.Join(InitramfsDataDir, "system-data")
	InitramfsEncryptionKeyDir = filepath.Join(InitramfsUbuntuSeedDir, "device/fde")
}

func init() {
	setInitramfsDirVars(dirs.GlobalRootDir)
	// register to change the values of Initramfs* dir values when the global
	// root dir changes
	dirs.AddRootDirCallback(setInitramfsDirVars)
}
