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

	// this matches a very specific setup we are asked to resolve a dm-verity device eg.
	// /dev/mapper/foo into an actual device node eg. /dev/sdb2; in contrast to  LUKS where
	// there is a LUKS volume with a specific UUID which is then repeated in the dm-crypt
	// device UUID, in case of verity the UUID included in dm-verity device UUID corresponds
	// to the hash tree device and not the back store device with the content we want to protect;
	// ideally the resolver should be able to perform what `veritysetup status <dev>` does, but
	// for simplicity use the fact that backing store device contains a filesystem with its own UUID
	// which shows up as ID_FS_UUID on the dm-verity device and can be used to resolve back to
	// the actual fs device.
	// XXX this will cease to work if the backing store filesystem has no UUID, eg squashfs
	byUUIDPath := filepath.Join("/dev/disk/by-uuid", opts.idFsUUID)
	return byUUIDPath, true
}

func init() {
	RegisterDeviceMapperBackResolver("crypt-verity", cryptVerityDeviceMapperBackResolver)
}
