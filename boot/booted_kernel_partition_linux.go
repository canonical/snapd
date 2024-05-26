// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020 Canonical Ltd
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
	"strings"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/bootloader/efi"
)

const (
	// note the vendor ID 4a67b082-0a4c-41cf-b6c7-440b29bb8c4f is systemd, this
	// variable is populated by shim
	loaderDevicePartUUID = "LoaderDevicePartUUID-4a67b082-0a4c-41cf-b6c7-440b29bb8c4f"
)

// FindPartitionUUIDForBootedKernelDisk returns the partition uuid for the
// partition that the booted kernel is located on.
func FindPartitionUUIDForBootedKernelDisk() (string, error) {
	// try efi variables first
	partuuid, _ := mylog.Check3(efi.ReadVarString(loaderDevicePartUUID))
	if err == nil {
		// the LoaderDevicePartUUID is in all caps, but lsblk,
		// etc. use lower case so for consistency just make it
		// lower case here too
		return strings.ToLower(partuuid), nil
	}
	if err == efi.ErrNoEFISystem {
		return "", err
	}

	// TODO:UC20: use the kernel command line parameter from the little kernel
	//            bootloader if we have a littlekernel bootloader

	// TODO:UC20: add more fallbacks here, even on amd64, when we don't have efi
	//            i.e. on bios?
	return "", fmt.Errorf("could not find partition uuid for booted kernel: %v", err)
}
