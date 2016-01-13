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
	"github.com/ubuntu-core/snappy/helpers"
)

// BootloaderDir returns the full path to the (mounted and writable)
// bootloader-specific boot directory.
func BootloaderDir() string {
	if helpers.FileExists(ubootDir()) {
		return ubootDir()
	} else if helpers.FileExists(grubDir()) {
		return grubDir()
	}

	return ""
}

// SetBootVar sets the given boot variable.
func SetBootVar(key, val string) error {
	b, err := bootloader()
	if err != nil {
		return err
	}

	return b.SetBootVar(key, val)
}

// GetBootVar returns the value of the given boot variable.
func GetBootVar(key string) (string, error) {
	b, err := bootloader()
	if err != nil {
		return "", err
	}

	return b.GetBootVar(key)
}

// MarkBootSuccessful marks the boot as successful
func MarkBootSuccessful() error {
	bootloader, err := bootloader()
	if err != nil {
		return err
	}

	return markBootSuccessful(bootloader)
}
