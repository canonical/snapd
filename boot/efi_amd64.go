// -*- Mode: Go; indent-tabs-mode: t -*-
// +build amd64

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
	"io/ioutil"
	"path/filepath"
	"strings"

	"github.com/snapcore/snapd/dirs"
)

var (
	efiVarDir = "/sys/firmware/efi/vars"
	// note the vendor ID 4a67b082-0a4c-41cf-b6c7-440b29bb8c4f is systemd, this
	// variable is populated by shim
	loaderDevicePartUUID = "LoaderDevicePartUUID-4a67b082-0a4c-41cf-b6c7-440b29bb8c4f"
)

func readEfiVar(name string) ([]byte, error) {
	path := filepath.Join(dirs.GlobalRootDir, efiVarDir, name, "data")
	return ioutil.ReadFile(path)
}

func findPartitionUUIDForBootedKernelDisk() (string, error) {
	// find the partition uuid using LoaderDevicePartUUID
	b, err := readEfiVar(loaderDevicePartUUID)
	if err != nil {
		return "", err
	}
	// the LoaderDevicePartUUID is in all caps, but lsblk, etc. use lower case
	// so for consistency just make it lower case here too
	return strings.ToLower(string(b)), nil
}
