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

import "syscall"

func DupFD(oldfd uintptr, newfd uintptr) error {
	// NB: This uses Dup3 instead of Dup2 because newer architectures like
	// linux/arm64 do not include legacy syscalls like Dup2. Dup3 was introduced
	// in Kernel 2.6.27
	//
	// See https://groups.google.com/forum/#!topic/golang-dev/zpeFtN2z5Fc.
	return syscall.Dup3(int(oldfd), int(newfd), 0)
}
