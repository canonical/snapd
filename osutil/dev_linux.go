// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2025 Canonical Ltd
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

package osutil

// #include <sys/sysmacros.h>
import "C"

// Major obtains the major device number.
func Major(dev uint64) uint64 {
	return uint64(C.gnu_dev_major((C.ulong)(dev)))
}

// Minor obtains the minor device number.
func Minor(dev uint64) uint64 {
	return uint64(C.gnu_dev_minor((C.ulong)(dev)))
}

// Makedev constructs device number from major/minor numbers.
func Makedev(maj, min uint64) uint64 {
	return uint64(C.gnu_dev_makedev((C.uint)(maj), (C.uint)(min)))
}
