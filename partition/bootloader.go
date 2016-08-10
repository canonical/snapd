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
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/snapcore/snapd/osutil"
)

const (
	// bootloader variable used to determine if boot was successful.
	// Set to value of either bootloaderBootmodeTry (when attempting
	// to boot a new rootfs) or bootloaderBootmodeSuccess (to denote
	// that the boot of the new rootfs was successful).
	bootmodeVar = "snap_mode"

	// Initial and final values
	modeTry     = "try"
	modeSuccess = ""
)

var (
	// ErrBootloader is returned if the bootloader can not be determined
	ErrBootloader = errors.New("cannot determine bootloader")
)

// Bootloader provides an interface to interact with the system
// bootloader
type Bootloader interface {
	// Return the value of the specified bootloader variable
	GetBootVar(name string) (string, error)

	// Set the value of the specified bootloader variable
	SetBootVar(name, value string) error

	// Dir returns the bootloader directory
	Dir() string

	// Name returns the bootloader name
	Name() string

	// ConfigFile returns the name of the config file
	ConfigFile() string
}

// InstallBootConfig installs the bootloader config from the gadget
// snap dir into the right place.
func InstallBootConfig(gadgetDir string) error {
	for _, bl := range []Bootloader{&grub{}, &uboot{}} {
		// FIXME: do not "find", instead force it to be in the top
		//        level of the snap
		gadgetFile, err := find(gadgetDir, filepath.Base(bl.ConfigFile()))
		if err != nil {
			continue
		}

		systemFile := bl.ConfigFile()
		if err := os.MkdirAll(filepath.Dir(systemFile), 0755); err != nil {
			return err
		}
		return osutil.CopyFile(gadgetFile, systemFile, osutil.CopyFlagOverwrite)
	}

	return fmt.Errorf("cannot find boot config in %q", gadgetDir)
}

var forcedBootloader Bootloader

// FindBootloader returns the bootloader for the given system
// or an error if no bootloader is found
func FindBootloader() (Bootloader, error) {
	if forcedBootloader != nil {
		return forcedBootloader, nil
	}

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

// ForceBootloader can be used to force setting a booloader to that FindBootloader will not use the usual lookup process, use nil to reset to normal lookup.
func ForceBootloader(booloader Bootloader) {
	forcedBootloader = booloader
}

// MarkBootSuccessful marks the current boot as sucessful. This means
// that snappy will consider this combination of kernel/os a valid
// target for rollback
func MarkBootSuccessful(bootloader Bootloader) error {
	// check if we need to do anything
	v, err := bootloader.GetBootVar("snap_mode")
	if err != nil {
		return err
	}
	// snap_mode goes from "" -> "try" -> "trying" -> ""
	if v != "trying" {
		return nil
	}

	// FIXME: we should have something better here, i.e. one write
	//        to the bootloader environment only (instead of three)
	//        We need to figure out if that is possible with grub/uboot
	// If we could also do atomic writes to the boot env, that would
	// be even better. The complication here is that the grub
	// environment is handled via grub-editenv and uboot is done
	// via the special uboot.env file on a vfat partition.
	for _, k := range []string{"snap_try_core", "snap_try_kernel"} {
		value, err := bootloader.GetBootVar(k)
		if err != nil {
			return err
		}

		// FIXME: ugly string replace
		newKey := strings.Replace(k, "_try_", "_", -1)
		if err := bootloader.SetBootVar(newKey, value); err != nil {
			return err
		}
		if err := bootloader.SetBootVar("snap_mode", modeSuccess); err != nil {
			return err
		}
		// clear "snap_try_{core,kernel}"
		if err := bootloader.SetBootVar(k, ""); err != nil {
			return err
		}
	}

	return nil
}
