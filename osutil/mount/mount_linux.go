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

package mount

import (
	"math/bits"
	"syscall"
)

type flagInfo struct {
	mask uint32
	name string
}

func knownMask(knownFlags []flagInfo) uint32 {
	var mask uint32
	for _, fi := range knownFlags {
		mask |= fi.mask
	}
	return mask
}

// UMOUNT_NOFOLLOW is not defined in go's syscall package
const UMOUNT_NOFOLLOW = 8

var mountFlags = []flagInfo{
	{name: "MS_REMOUNT", mask: syscall.MS_REMOUNT},
	{name: "MS_BIND", mask: syscall.MS_BIND},
	{name: "MS_REC", mask: syscall.MS_REC},
	{name: "MS_RDONLY", mask: syscall.MS_RDONLY},
	{name: "MS_SHARED", mask: syscall.MS_SHARED},
	{name: "MS_SLAVE", mask: syscall.MS_SLAVE},
	{name: "MS_PRIVATE", mask: syscall.MS_PRIVATE},
	{name: "MS_UNBINDABLE", mask: syscall.MS_UNBINDABLE},
}

var mountFlagsMask = knownMask(mountFlags)

var unmountFlags = []flagInfo{
	{name: "UMOUNT_NOFOLLOW", mask: UMOUNT_NOFOLLOW},
	{name: "MNT_FORCE", mask: syscall.MNT_FORCE},
	{name: "MNT_DETACH", mask: syscall.MNT_DETACH},
	{name: "MNT_EXPIRE", mask: syscall.MNT_EXPIRE},
}

var unmountFlagsMask = knownMask(unmountFlags)

func flagOptSearch(flags int, knownFlags []flagInfo, knownMask uint32) (opts []string, unknown int) {
	var f uint32 = uint32(flags)
	for _, fi := range knownFlags {
		if f&knownMask == 0 {
			break
		}
		if f&fi.mask != 0 {
			if opts == nil {
				opts = make([]string, 0, bits.OnesCount32(f))
			}
			f ^= fi.mask
			opts = append(opts, fi.name)
		}
	}
	return opts, int(f)
}

// MountFlagsToOpts returns the symbolic representation of mount flags.
func MountFlagsToOpts(flags int) (opts []string, unknown int) {
	return flagOptSearch(flags, mountFlags, mountFlagsMask)
}

// UnmountFlagsToOpts returns the symbolic representation of unmount flags.
func UnmountFlagsToOpts(flags int) (opts []string, unknown int) {
	return flagOptSearch(flags, unmountFlags, unmountFlagsMask)
}
