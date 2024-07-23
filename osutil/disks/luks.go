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

package disks

import (
	"bytes"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

var (
	// this regexp is for the DM_UUID udev property, or equivalently the dm/uuid
	// sysfs entry for a luks2 device mapper volume dynamically created by
	// systemd-cryptsetup when unlocking
	// the actual value that is returned also has "-some-name" appended to this
	// pattern, but we delete that from the string before matching with this
	// regexp to prevent issues like a mapper volume name that has CRYPT-LUKS2-
	// in the name and thus we might accidentally match it
	// see also the comments in DiskFromMountPoint about this value
	luksUUIDPatternRe = regexp.MustCompile(`^CRYPT-LUKS2-([0-9a-f]{32})$`)
)

func cryptLuks2DeviceMapperBackResolver(opts *deviceMapperBackResolversOpts) (dev string, ok bool) {
	if !strings.HasPrefix(string(opts.dmUUID), "CRYPT-LUKS") {
		return "", false
	}

	// this is a LUKS encrypted device

	// trim the suffix of the dm name from the dm uuid to safely match the
	// regex - the dm uuid contains the dm name, and the dm name is user
	// controlled, so we want to remove that and just use the luks pattern
	// to match the device uuid
	// we are extra safe here since the dm name could be hypothetically user
	// controlled via an external USB disk with LVM partition names, etc.
	dmUUIDSafe := bytes.TrimSuffix(
		bytes.TrimSpace(opts.dmUUID),
		append([]byte("-"), bytes.TrimSpace(opts.dmName)...),
	)
	matches := luksUUIDPatternRe.FindSubmatch(dmUUIDSafe)
	if len(matches) != 2 {
		// the format of the uuid is different - different luks version
		// maybe?
		return "", false
	}

	// the uuid is the first and only submatch, but it is not in the same
	// format exactly as we want to use, namely it is missing all of the "-"
	// characters in a typical uuid, i.e. it is of the form:
	// ae6e79de00a9406f80ee64ba7c1966bb but we want it to be like:
	// ae6e79de-00a9-406f-80ee-64ba7c1966bb so we need to add in 4 "-"
	// characters
	compactUUID := string(matches[1])
	canonicalUUID := fmt.Sprintf(
		"%s-%s-%s-%s-%s",
		compactUUID[0:8],
		compactUUID[8:12],
		compactUUID[12:16],
		compactUUID[16:20],
		compactUUID[20:],
	)

	// now finally, we need to use this uuid, which is the device uuid of
	// the actual physical encrypted partition to get the path, which will
	// be something like /dev/vda4, etc.
	byUUIDPath := filepath.Join("/dev/disk/by-uuid", canonicalUUID)
	return byUUIDPath, true
}

func init() {
	RegisterDeviceMapperBackResolver("crypt-luks2", cryptLuks2DeviceMapperBackResolver)
}
