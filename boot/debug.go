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

	"github.com/snapcore/snapd/bootloader"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
)

// DumpBootVars writes a dump of the snapd bootvars to the given writer
func DumpBootVars(w io.Writer, dir string, uc20 bool) error {
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
			// no root directory set, default ot run mode
			opts.Role = bootloader.RoleRunMode
		}
		// keys relevant to all uc20 bootloader implementations
		allKeys = []string{
			"snapd_recovery_mode",
			"snapd_recovery_system",
			"snapd_recovery_kernel",
			"snap_kernel",
			"kernel_status",
		}
	}
	bloader, err := bootloader.Find(dir, opts)
	if err != nil {
		return err
	}

	bootVars, err := bloader.GetBootVars(allKeys...)
	if err != nil {
		return err
	}
	for _, k := range allKeys {
		fmt.Fprintf(w, "%s=%s\n", k, bootVars[k])
	}
	return nil
}
