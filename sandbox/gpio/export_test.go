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

	"github.com/snapcore/snapd/testutil"
)

type ChardevChip = chardevChip

func (c *ChardevChip) Path() string   { return c.path }
func (c *ChardevChip) Name() string   { return c.name }
func (c *ChardevChip) Label() string  { return c.label }
func (c *ChardevChip) NumLines() uint { return c.numLines }

func MockChardevChip(path, name, label string, numLines uint) *chardevChip {
	return &chardevChip{path, name, label, numLines}
}

var IoctlGetChipInfo = ioctlGetChipInfo
var GetChardevChipInfo = getChardevChipInfo

func MockUnixSyscall(f func(trap uintptr, a1 uintptr, a2 uintptr, a3 uintptr) (r1 uintptr, r2 uintptr, err syscall.Errno)) (restore func()) {
	return testutil.Mock(&unixSyscall, f)
}

func MockIoctlGetChipInfo(f func(path string) (name, label [32]byte, lines uint32, err error)) (restore func()) {
	return testutil.Mock(&ioctlGetChipInfo, func(path string) (*kernelChipInfo, error) {
		name, label, lines, err := f(path)
		return &kernelChipInfo{name, label, lines}, err
	})
}

func MockGetChardevChipInfo(f func(path string) (*chardevChip, error)) (restore func()) {
	return testutil.Mock(&getChardevChipInfo, f)
}

func MockOsMkdir(f func(path string, perm fs.FileMode) error) (restore func()) {
	return testutil.Mock(&osMkdir, f)
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

func MockOsWriteFile(f func(name string, data []byte, perm fs.FileMode) error) (restore func()) {
	return testutil.Mock(&osWriteFile, f)
}

func MockSyscallMknod(f func(path string, mode uint32, dev int) (err error)) (restore func()) {
	return testutil.Mock(&syscallMknod, f)
}

func MockKmodLoadModule(f func(module string, options []string) error) (restore func()) {
	return testutil.Mock(&kmodLoadModule, f)
}
