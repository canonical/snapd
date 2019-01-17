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
	// bootloader variable used to determine if boot was
	// successful.  Set to value of either modeTry (when
	// attempting to boot a new rootfs) or modeSuccess (to denote
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
	for _, bl := range []Bootloader{&grub{}, &uboot{}, &androidboot{}} {
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

	// no, try androidboot
	if androidboot := newAndroidBoot(); androidboot != nil {
		return androidboot, nil
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
// target for rollback.
//
// The states that a boot goes through are the following:
// - By default snap_mode is "" in which case the bootloader loads
//   two squashfs'es denoted by variables snap_core and snap_kernel.
// - On a refresh of core/kernel snapd will set snap_mode=try and
//   will also set snap_try_{core,kernel} to the core/kernel that
//   will be tried next.
// - On reboot the bootloader will inspect the snap_mode and if the
//   mode is set to "try" it will set "snap_mode=trying" and then
//   try to boot the snap_try_{core,kernel}".
// - On a successful boot snapd resets snap_mode to "" and copies
//   snap_try_{core,kernel} to snap_{core,kernel}. The snap_try_*
//   values are cleared afterwards.
// - On a failing boot the bootloader will see snap_mode=trying which
//   means snapd did not start successfully. In this case the bootloader
//   will set snap_mode="" and the system will boot with the known good
//   values from snap_{core,kernel}
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
