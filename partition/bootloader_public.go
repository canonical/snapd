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
	"fmt"

	"github.com/ubuntu-core/snappy/helpers"
)

/*
This file contains all the public interfaces that the partition
code should expose for the all-snap world.

FIXME: remove the other public interfaces once snappy a/b is gone.
*/

// BootloaderDir returns the full path to the (mounted and writable)
// bootloader-specific boot directory.
func BootloaderDir() string {
	if helpers.FileExists(bootloaderUbootDir()) {
		return bootloaderUbootDir()
	} else if helpers.FileExists(bootloaderGrubDir()) {
		return bootloaderGrubDir()
	}

	return ""
}

// SetBootVar sets the given boot variable.
func SetBootVar(key, val string) error {
	p := New()
	if p == nil {
		return fmt.Errorf("cannot set %s boot variable: cannot find partition", key)
	}
	b, err := bootloader(p)
	if err != nil {
		return err
	}

	return b.SetBootVar(key, val)
}

// GetBootVar returns the value of the given boot variable.
func GetBootVar(key string) (string, error) {
	p := New()
	if p == nil {
		return "", fmt.Errorf("cannot get %s boot variable: cannot find partition", key)
	}
	b, err := bootloader(p)
	if err != nil {
		return "", err
	}

	return b.GetBootVar(key)
}
