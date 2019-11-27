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
	"syscall"
)

type syscallNumPair struct {
	str string
	num int
}

// UMOUNT_NOFOLLOW is not defined in go's syscall package
const UMOUNT_NOFOLLOW = 8

var mountSyscalls = []syscallNumPair{
	{"MS_REMOUNT", syscall.MS_REMOUNT},
	{"MS_BIND", syscall.MS_BIND},
	{"MS_REC", syscall.MS_REC},
	{"MS_RDONLY", syscall.MS_RDONLY},
	{"MS_SHARED", syscall.MS_SHARED},
	{"MS_SLAVE", syscall.MS_SLAVE},
	{"MS_PRIVATE", syscall.MS_PRIVATE},
	{"MS_UNBINDABLE", syscall.MS_UNBINDABLE},
}

var unmountSyscalls = []syscallNumPair{
	{"UMOUNT_NOFOLLOW", UMOUNT_NOFOLLOW},
	{"MNT_FORCE", syscall.MNT_FORCE},
	{"MNT_DETACH", syscall.MNT_DETACH},
	{"MNT_EXPIRE", syscall.MNT_EXPIRE},
}

func flagOptSearch(flags int, lookupTable []syscallNumPair) (opts []string, unknown int) {
	var f, i int
	var sys syscallNumPair
	for i, sys = range lookupTable {
		f = sys.num
		if flags&f == f {
			flags ^= f
			opts = append(opts, lookupTable[i].str)
		}
	}
	return opts, flags
}

// MountFlagsToOpts returns the symbolic representation of mount flags.
func MountFlagsToOpts(flags int) (opts []string, unknown int) {
	return flagOptSearch(flags, mountSyscalls)
}

// UnmountFlagsToOpts returns the symbolic representation of unmount flags.
func UnmountFlagsToOpts(flags int) (opts []string, unknown int) {
	return flagOptSearch(flags, unmountSyscalls)
}
