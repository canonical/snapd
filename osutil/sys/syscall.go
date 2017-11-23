// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017 Canonical Ltd
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

package sys

import (
	"os"
	"syscall"
	"unsafe"
)

// uid_t is an unsigned 32-bit integer in linux right now.
// so syscall.Gete?[ug]id are wrong, and break in 32 bits
// (see https://github.com/golang/go/issues/22739)
func Getuid() uint32 {
	return getid(syscall.SYS_GETUID)
}

func Geteuid() uint32 {
	return getid(syscall.SYS_GETEUID)
}

func Getgid() uint32 {
	return getid(syscall.SYS_GETGID)
}

// FlagID can be passed to chown-ish functions to mean "no change",
// and can be returned from getuid-ish functions to mean "not found".
const FlagID = uint32(1<<32 - 1)

func getid(id uintptr) uint32 {
	r0, _, errno := syscall.RawSyscall(id, 0, 0, 0)
	if errno != 0 {
		// -1 is used as a flag to mean 'no change' to chown(2), so it's safe
		// to use as a flag for ourselves as well.
		return FlagID
	}
	return uint32(r0)
}

func Chown(f *os.File, uid, gid uint32) error {
	return Fchown(f.Fd(), uid, gid)
}

func Fchown(fd uintptr, uid, gid uint32) error {
	_, _, errno := syscall.Syscall(syscall.SYS_FCHOWN, fd, uintptr(uid), uintptr(gid))
	if errno == 0 {
		return nil
	}
	return errno
}

func ChownPath(path string, uid, gid uint32) error {
	AT_FDCWD := -0x64
	return FchownAt(uintptr(AT_FDCWD), path, uid, gid, 0)
}

func FchownAt(dirfd uintptr, path string, uid uint32, gid uint32, flags int) error {
	p0, err := syscall.BytePtrFromString(path)
	if err != nil {
		return err
	}
	_, _, errno := syscall.Syscall6(syscall.SYS_FCHOWNAT, dirfd, uintptr(unsafe.Pointer(p0)), uintptr(uid), uintptr(gid), uintptr(flags), 0)
	if errno == 0 {
		return nil
	}
	return errno
}
