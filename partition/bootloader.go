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
	GetBootVars(names ...string) (map[string]string, error)

	// Set the value of the specified bootloader variable
	SetBootVars(values map[string]string) error

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
		// the bootloader config file has to be root of the gadget snap
		gadgetFile := filepath.Join(gadgetDir, bl.Name()+".conf")
		if !osutil.FileExists(gadgetFile) {
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

// MarkBootSuccessful marks the current boot as successful. This means
// that snappy will consider this combination of kernel/os a valid
// target for rollback
func MarkBootSuccessful(bootloader Bootloader) error {
	m, err := bootloader.GetBootVars("snap_mode", "snap_try_core", "snap_try_kernel")
	if err != nil {
		return err
	}

	// snap_mode goes from "" -> "try" -> "trying" -> ""
	// so if we are not in "trying" mode, nothing to do here
	if m["snap_mode"] != "trying" {
		return nil
	}

	// update the boot vars
	for _, k := range []string{"kernel", "core"} {
		tryBootVar := fmt.Sprintf("snap_try_%s", k)
		bootVar := fmt.Sprintf("snap_%s", k)
		// update the boot vars
		if m[tryBootVar] != "" {
			m[bootVar] = m[tryBootVar]
			m[tryBootVar] = ""
		}
	}
	m["snap_mode"] = modeSuccess

	return bootloader.SetBootVars(m)
}
