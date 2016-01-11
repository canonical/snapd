// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2015 Canonical Ltd
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

package partition

const (
	// bootloader variable used to determine if boot was successful.
	// Set to value of either bootloaderBootmodeTry (when attempting
	// to boot a new rootfs) or bootloaderBootmodeSuccess (to denote
	// that the boot of the new rootfs was successful).
	bootloaderBootmodeVar = "snappy_mode"

	bootloaderTrialBootVar = "snappy_trial_boot"

	// Initial and final values
	bootloaderBootmodeTry     = "try"
	bootloaderBootmodeSuccess = "regular"
)

type bootloaderName string

type bootLoader interface {
	// Name of the bootloader
	Name() bootloaderName

	// Return the value of the specified bootloader variable
	GetBootVar(name string) (string, error)

	// Set the value of the specified bootloader variable
	SetBootVar(name, value string) error

	// BootDir returns the (writable) bootloader-specific boot
	// directory.
	BootDir() string
}

// Factory method that returns a new bootloader for the given partition
var bootloader = bootloaderImpl

func bootloaderImpl(p *Partition) (bootLoader, error) {
	// try uboot
	if uboot := newUboot(p); uboot != nil {
		return uboot, nil
	}

	// no, try grub
	if grub := newGrub(p); grub != nil {
		return grub, nil
	}

	// no, weeeee
	return nil, ErrBootloader
}

type bootloaderType struct {
	// FIXME: this should /boot if possible
	// the dir that the bootloader lives in (e.g. /boot/uboot)
	bootloaderDir string
}

func newBootLoader(partition *Partition, bootloaderDir string) *bootloaderType {
	return &bootloaderType{
		bootloaderDir: bootloaderDir,
	}
}
