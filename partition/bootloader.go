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

import (
	"errors"
	"strings"
)

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

var (
	// ErrBootloader is returned if the bootloader can not be determined
	ErrBootloader = errors.New("cannot determine bootloader")
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

func bootloaderImpl() (bootLoader, error) {
	// try uboot
	if uboot := newUboot(); uboot != nil {
		return uboot, nil
	}

	// no, try grub
	if grub := newGrub(); grub != nil {
		return grub, nil
	}

	// no, weeeee
	return nil, ErrBootloader
}

func markBootSuccessful(bootloader bootLoader) error {
	// FIXME: we should have something better here, i.e. one write
	//        to the bootloader environment only (instead of three)
	//        We need to figure out if that is possible with grub/uboot
	// (if we could also do atomic writes to the boot env, that would
	//  be even better)
	for _, k := range []string{"snappy_os", "snappy_kernel"} {
		value, err := bootloader.GetBootVar(k)
		if err != nil {
			return err
		}

		// FIXME: ugly string replace
		newKey := strings.Replace(k, "snappy_", "snappy_good_", -1)
		if err := bootloader.SetBootVar(newKey, value); err != nil {
			return err
		}

		if err := bootloader.SetBootVar("snappy_mode", "regular"); err != nil {
			return err
		}

		if err := bootloader.SetBootVar("snappy_trial_boot", "0"); err != nil {
			return err
		}

	}

	return nil
}
