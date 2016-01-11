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

// Package partition manipulate snappy disk partitions
package partition

import (
	"errors"
	"strings"
)

const (
	// Name of writable user data partition label as created by
	// ubuntu-device-flash(1).
	writablePartitionLabel = "writable"

	// name of boot partition label as created by ubuntu-device-flash(1).
	bootPartitionLabel = "system-boot"

	// File creation mode used when any directories are created
	dirMode = 0750
)

var (
	// ErrBootloader is returned if the bootloader can not be determined
	ErrBootloader = errors.New("Unable to determine bootloader")
)

// Interface provides the interface to interact with a partition
type Interface interface {
	MarkBootSuccessful() error
}

// Partition has no role anymore
type Partition struct{}

// New creates a new partition type
func New() *Partition {
	return &Partition{}
}

// MarkBootSuccessful marks the boot as successful
func (p *Partition) MarkBootSuccessful() error {
	bootloader, err := bootloader(p)
	if err != nil {
		return err
	}

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

// BootloaderDir returns the full path to the (mounted and writable)
// bootloader-specific boot directory.
func (p *Partition) BootloaderDir() string {
	bootloader, err := bootloader(p)
	if err != nil {
		return ""
	}

	return bootloader.BootDir()
}
