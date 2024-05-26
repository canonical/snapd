// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019 Canonical Ltd
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
	"io"
	"strings"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/bootloader"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
)

// DebugDumpBootVars writes a dump of the snapd bootvars to the given writer
func DebugDumpBootVars(w io.Writer, dir string, uc20 bool) error {
	opts := &bootloader.Options{
		NoSlashBoot: dir != "" && dir != "/",
	}
	switch dir {
	// is it any of the well-known UC20 boot partition mount locations?
	case InitramfsUbuntuBootDir:
		opts.Role = bootloader.RoleRunMode
		uc20 = true
	case InitramfsUbuntuSeedDir:
		opts.Role = bootloader.RoleRecovery
		uc20 = true
	}
	if !opts.NoSlashBoot && !uc20 {
		// this may still be a UC20 system
		if osutil.FileExists(dirs.SnapModeenvFile) {
			uc20 = true
		}
	}
	allKeys := []string{
		"snap_mode",
		"snap_core",
		"snap_try_core",
		"snap_kernel",
		"snap_try_kernel",
	}
	if uc20 {
		if !opts.NoSlashBoot {
			// no root directory set, default to run mode
			opts.Role = bootloader.RoleRunMode
		}
		// keys relevant to all uc20 bootloader implementations
		allKeys = []string{
			"snapd_recovery_mode",
			"snapd_recovery_system",
			"snapd_recovery_kernel",
			"snap_kernel",
			"snap_try_kernel",
			"kernel_status",
			"recovery_system_status",
			"try_recovery_system",
			"snapd_good_recovery_systems",
			"snapd_extra_cmdline_args",
			"snapd_full_cmdline_args",
		}
	}
	bloader := mylog.Check2(bootloader.Find(dir, opts))

	bootVars := mylog.Check2(bloader.GetBootVars(allKeys...))

	for _, k := range allKeys {
		fmt.Fprintf(w, "%s=%s\n", k, bootVars[k])
	}
	return nil
}

// DebugSetBootVars is a debug helper that takes a list of <var>=<value> entries
// and sets them for the configured bootloader.
func DebugSetBootVars(dir string, recoveryBootloader bool, varEqVal []string) error {
	opts := &bootloader.Options{
		NoSlashBoot: dir != "" && dir != "/",
	}
	if opts.NoSlashBoot || osutil.FileExists(dirs.SnapModeenvFile) {
		// implied UC20 bootloader
		opts.Role = bootloader.RoleRunMode
	}
	// try some well known UC20 root dirs
	switch dir {
	case InitramfsUbuntuBootDir:
		if recoveryBootloader {
			return fmt.Errorf("cannot use run bootloader root-dir with a recovery flag")
		}
		opts.Role = bootloader.RoleRunMode
	case InitramfsUbuntuSeedDir:
		opts.Role = bootloader.RoleRecovery
	}
	if recoveryBootloader {
		// UC20 recovery bootloader
		opts.Role = bootloader.RoleRecovery
		if !opts.NoSlashBoot {
			// no root dir was provided, use the default one for a
			// recovery bootloader
			dir = InitramfsUbuntuSeedDir
		}
	}
	bloader := mylog.Check2(bootloader.Find(dir, opts))

	toSet := map[string]string{}

	for _, req := range varEqVal {
		split := strings.SplitN(req, "=", 2)
		if len(split) != 2 {
			return fmt.Errorf("incorrect setting %q", varEqVal)
		}
		toSet[split[0]] = split[1]
	}
	return bloader.SetBootVars(toSet)
}
