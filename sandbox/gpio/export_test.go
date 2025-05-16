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
package gpio

import (
	"io/fs"
	"syscall"
	"time"

	"github.com/snapcore/snapd/testutil"
)

var IoctlGetChipInfo = ioctlGetChipInfo
var ChardevChipInfo = chardevChipInfo

func MockUnixSyscall(f func(trap uintptr, a1 uintptr, a2 uintptr, a3 uintptr) (r1 uintptr, r2 uintptr, err syscall.Errno)) (restore func()) {
	return testutil.Mock(&unixSyscall, f)
}

func MockIoctlGetChipInfo(f func(path string) (name, label [32]byte, lines uint32, err error)) (restore func()) {
	return testutil.Mock(&ioctlGetChipInfo, func(path string) (*kernelChipInfo, error) {
		name, label, lines, err := f(path)
		return &kernelChipInfo{name, label, lines}, err
	})
}

func MockChardevChipInfo(f func(path string) (*ChardevChip, error)) (restore func()) {
	return testutil.Mock(&chardevChipInfo, f)
}

func MockOsStat(f func(path string) (fs.FileInfo, error)) (restore func()) {
	return testutil.Mock(&osStat, f)
}

func MockOsChmod(f func(path string, mode fs.FileMode) error) (restore func()) {
	return testutil.Mock(&osChmod, f)
}

func MockOsChown(f func(path string, uid int, gid int) error) (restore func()) {
	return testutil.Mock(&osChown, f)
}

func MockSyscallMknod(f func(path string, mode uint32, dev int) (err error)) (restore func()) {
	return testutil.Mock(&syscallMknod, f)
}

func MockAggregatorCreationTimeout(t time.Duration) (restore func()) {
	return testutil.Mock(&aggregatorCreationTimeout, t)
}

func MockLockAggregator(f func() (unlocker func(), err error)) (restore func()) {
	return testutil.Mock(&lockAggregator, f)
}

func MockKmodLoadModule(f func(module string, options []string) error) (restore func()) {
	return testutil.Mock(&kmodLoadModule, f)
}
