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
package device

import (
	"syscall"

	"github.com/snapcore/snapd/testutil"
)

var IoctlGetChipInfo = ioctlGetChipInfo

func MockUnixSyscall(f func(trap uintptr, a1 uintptr, a2 uintptr, a3 uintptr) (r1 uintptr, r2 uintptr, err syscall.Errno)) (restore func()) {
	return testutil.Mock(&unixSyscall, f)
}

func MockIoctlGetChipInfo(f func(path string) (name, label [32]byte, lines uint32, err error)) (restore func()) {
	return testutil.Mock(&ioctlGetChipInfo, func(path string) (*kernelChipInfo, error) {
		name, label, lines, err := f(path)
		return &kernelChipInfo{name, label, lines}, err
	})
}
