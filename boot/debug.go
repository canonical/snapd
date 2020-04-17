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
)

// DumpBootVars writes a dump of the snapd bootvars to the given writer
func DumpBootVars(w io.Writer, dir string, uc20 bool) error {
	bloader, err := bootloader.Find(dir, nil)
	if err != nil {
		return err
	}
	var allKeys []string
	if uc20 {
		// TODO:UC20: what about snapd_recovery_kernel, snapd_recovery_mode, and
		//            snapd_recovery_system?
		allKeys = []string{"kernel_status"}
	} else {
		allKeys = []string{
			"snap_mode",
			"snap_core",
			"snap_try_core",
			"snap_kernel",
			"snap_try_kernel",
		}
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
