// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2021 Canonical Ltd
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
	"fmt"
	"regexp"
	"strings"

	"github.com/snapcore/snapd/bootloader"
)

func blForDev(dev Device) (bootloader.Bootloader, error) {
	opts := &bootloader.Options{}
	dir := ""
	if dev.RunMode() {
		opts.Role = bootloader.RoleRunMode
	} else {
		opts.Role = bootloader.RoleRecovery
		// meh this isn't being used in the initramfs but it's probably fine
		dir = InitramfsUbuntuSeedDir
	}

	return bootloader.Find(dir, opts)
}

// NextBootFlags returns the set of boot flags for the current active boot and
// possibly for the next boot. By default, the flags should only be used on one
// boot ever after being set and the system being rebooted with the flags
// cleared by snapd in userspace when that boot happens. The mode parameter is
// necessary to determine the current active bootloader. Only to be used on
// UC20+ on systems with recovery systems.
func NextBootFlags(dev Device) ([]string, error) {
	if !dev.HasModeenv() {
		return nil, fmt.Errorf("cannot get boot flags on non-UC20 device")
	}

	bl, err := blForDev(dev)
	if err != nil {
		return nil, err
	}

	m, err := bl.GetBootVars("snapd_next_boot_flags")
	if err != nil {
		return nil, err
	}

	// remove empty flags from the bootenv
	flags := []string{}
	for _, flag := range strings.Split(m["snapd_next_boot_flags"], ",") {
		if flag != "" {
			flags = append(flags, flag)
		}
	}

	// TODO: is this the right format? (comma separated values)
	return flags, nil
}

// TODO: how strict should we be in flags here? Should we instead just have an
// allow list of supported flags?
var validFlagRegexp = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// SetNextBootFlags sets the flags for the next boot by assigning the flags to
// the bootenv var snapd_next_boot_flags. It completely overwrites the current
// value of snapd_next_boot_flags. The serialized flags must be less than 255
// characters long to fit into some constrained device's bootenv, and each flag
// must consist of only letters, numbers and '_' or '-'.
func SetNextBootFlags(dev Device, flags []string) error {
	if !dev.HasModeenv() {
		return fmt.Errorf("cannot get boot flags on non-UC20 device")
	}

	bl, err := blForDev(dev)
	if err != nil {
		return err
	}

	// verify that the flags when combined are not longer than 255, otherwise
	// we won't be able to fit the value into boot env on i.e. lk bootloaders

	for _, flag := range flags {
		if !validFlagRegexp.MatchString(flag) {
			return fmt.Errorf("cannot set boot flags: invalid flag %q", flag)
		}
	}

	serializedFlags := strings.Join(flags, ",")
	if len(serializedFlags) > 255 {
		return fmt.Errorf("cannot set boot flags: combined serialized length (%d) is too long", len(serializedFlags))
	}

	return bl.SetBootVars(map[string]string{"snapd_next_boot_flags": serializedFlags})
}
