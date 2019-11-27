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

// MountFlagsToOpts returns the symbolic representation of mount flags.
func MountFlagsToOpts(flags int) (opts []string, unknown int) {
	if f := syscall.MS_REMOUNT; flags&f == f {
		flags ^= f
		opts = append(opts, "MS_REMOUNT")
	}
	if f := syscall.MS_BIND; flags&f == f {
		flags ^= f
		opts = append(opts, "MS_BIND")
	}
	if f := syscall.MS_REC; flags&f == f {
		flags ^= f
		opts = append(opts, "MS_REC")
	}
	if f := syscall.MS_RDONLY; flags&f == f {
		flags ^= f
		opts = append(opts, "MS_RDONLY")
	}
	if f := syscall.MS_SHARED; flags&f == f {
		flags ^= f
		opts = append(opts, "MS_SHARED")
	}
	if f := syscall.MS_SLAVE; flags&f == f {
		flags ^= f
		opts = append(opts, "MS_SLAVE")
	}
	if f := syscall.MS_PRIVATE; flags&f == f {
		flags ^= f
		opts = append(opts, "MS_PRIVATE")
	}
	if f := syscall.MS_UNBINDABLE; flags&f == f {
		flags ^= f
		opts = append(opts, "MS_UNBINDABLE")
	}
	return opts, flags
}

// UnmountFlagsToOpts returns the symbolic representation of unmount flags.
func UnmountFlagsToOpts(flags int) (opts []string, unknown int) {
	const UMOUNT_NOFOLLOW = 8
	if f := UMOUNT_NOFOLLOW; flags&f == f {
		flags ^= f
		opts = append(opts, "UMOUNT_NOFOLLOW")
	}
	if f := syscall.MNT_FORCE; flags&f == f {
		flags ^= f
		opts = append(opts, "MNT_FORCE")
	}
	if f := syscall.MNT_DETACH; flags&f == f {
		flags ^= f
		opts = append(opts, "MNT_DETACH")
	}
	if f := syscall.MNT_EXPIRE; flags&f == f {
		flags ^= f
		opts = append(opts, "MNT_EXPIRE")
	}
	return opts, flags
}
