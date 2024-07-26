// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2024 Canonical Ltd
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

package disks

import (
	"path/filepath"
	"strings"
)

func cryptVerityDeviceMapperBackResolver(opts *deviceMapperBackResolversOpts) (dev string, ok bool) {
	if !strings.HasPrefix(string(opts.dmUUID), "CRYPT-VERITY") {
		return "", false
	}

	// this is a verity mounted device

	byUUIDPath := filepath.Join("/dev/disk/by-uuid", opts.idFsUUID)
	return byUUIDPath, true
}

func init() {
	RegisterDeviceMapperBackResolver("crypt-verity", cryptVerityDeviceMapperBackResolver)
}
