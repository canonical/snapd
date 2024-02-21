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

package osutil

import "golang.org/x/sys/unix"

// swapDirs swaps atomically (for the running system) two directories by using
// renameat2 syscall. The directories must be absolute.
func swapDirs(oldpath string, newpath string) (err error) {
	return unix.Renameat2(-1, oldpath, -1, newpath, unix.RENAME_EXCHANGE)
}
