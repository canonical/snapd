// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2018-2020 Canonical Ltd
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

import (
	"syscall"
	"unsafe"
)

// Symlinkat is a direct pass-through to the symlinkat(2) system call.
func Symlinkat(target string, dirfd int, linkpath string) error {
	targetPtr, err := syscall.BytePtrFromString(target)
	if err != nil {
		return err
	}
	linkpathPtr, err := syscall.BytePtrFromString(linkpath)
	if err != nil {
		return err
	}
	_, _, errno := syscall.Syscall(syscall.SYS_SYMLINKAT, uintptr(unsafe.Pointer(targetPtr)), uintptr(dirfd), uintptr(unsafe.Pointer(linkpathPtr)))
	if errno != 0 {
		return errno
	}
	return nil
}

// Readlinkat is a direct pass-through to the readlinkat(2) system call.
func Readlinkat(dirfd int, path string, buf []byte) (n int, err error) {
	var zero uintptr

	pathPtr, err := syscall.BytePtrFromString(path)
	if err != nil {
		return 0, err
	}
	var bufPtr unsafe.Pointer
	if len(buf) > 0 {
		bufPtr = unsafe.Pointer(&buf[0])
	} else {
		bufPtr = unsafe.Pointer(&zero)
	}
	r0, _, errno := syscall.Syscall6(syscall.SYS_READLINKAT, uintptr(dirfd), uintptr(unsafe.Pointer(pathPtr)), uintptr(bufPtr), uintptr(len(buf)), 0, 0)
	n = int(r0)
	if errno != 0 {
		return 0, errno
	}
	return n, nil
}

// renameExchange is the RENAME_EXCHANGE flag to renameat2.
const renameExchange = 1 << 1

// ExchangeFiles atomically exchanges two files on one file system.
//
// All kinds of file objects, regular files, directories, symbolic links,
// etc are supported. Not all file systems are supported. Notably ZFS
// is not yet supported: https://github.com/openzfs/zfs/pull/9414
func ExchangeFiles(oldDirFd int, oldPath string, newDirFd int, newPath string) error {
	oldPathPtr, err := syscall.BytePtrFromString(oldPath)
	if err != nil {
		return err
	}
	newPathPtr, err := syscall.BytePtrFromString(newPath)
	if err != nil {
		return err
	}
	_, _, errno := syscall.Syscall6(sysRenameAt2, uintptr(oldDirFd), uintptr(unsafe.Pointer(oldPathPtr)), uintptr(newDirFd), uintptr(unsafe.Pointer(newPathPtr)), renameExchange, 0)
	if errno != 0 {
		return errno
	}
	return nil
}
