// -*- Mode: Go; indent-tabs-mode: t -*-

// +build cgo

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

package osutil

// #include <stdlib.h>
// #include <sys/types.h>
// #include <grp.h>
// #include <unistd.h>
import "C"

import (
	"fmt"
	"strconv"
	"syscall"
	"unsafe"
)

// Group represents a grouping of users.
// Based on: https://golang.org/src/os/user/user.go
//
func buildGroup(grp *C.struct_group) *Group {
	g := &Group{
		Gid:  strconv.Itoa(int(grp.gr_gid)),
		Name: C.GoString(grp.gr_name),
	}
	return g
}

// hrm, user.LookupGroup() doesn't exist yet:
// https://github.com/golang/go/issues/2617
//
// Use implementation from upcoming releases:
// https://golang.org/src/os/user/lookup_unix.go
func lookupGroup(groupname string) (*Group, error) {
	var grp C.struct_group
	var result *C.struct_group

	buf := alloc(groupBuffer)
	defer buf.free()
	cname := C.CString(groupname)
	defer C.free(unsafe.Pointer(cname))

	err := retryWithBuffer(buf, func() syscall.Errno {
		return syscall.Errno(C.getgrnam_r(cname,
			&grp,
			(*C.char)(buf.ptr),
			C.size_t(buf.size),
			&result))
	})
	if err != nil {
		return nil, fmt.Errorf("group: cannot lookup groupname %s: %v", groupname, err)
	}
	if result == nil {
		return nil, fmt.Errorf("group: cannot find group %s", groupname)
	}
	return buildGroup(&grp), nil
}

// Use implementation from:
// https://golang.org/src/os/user/cgo_lookup_unix.go
func lookupGroupByGid(gid uint64) (*Group, error) {
	var grp C.struct_group
	var result *C.struct_group

	buf := alloc(groupBuffer)
	defer buf.free()
	err := retryWithBuffer(buf, func() syscall.Errno {
		return syscall.Errno(C.getgrgid_r(C.__gid_t(gid),
			&grp,
			(*C.char)(buf.ptr),
			C.size_t(buf.size),
			&result))

	})
	if err != nil {
		return nil, fmt.Errorf("group: cannot lookup groupid %d: %v", gid, err)
	}
	if result == nil {
		return nil, fmt.Errorf("group: cannot find group %d", gid)
	}
	return buildGroup(&grp), nil
}

type bufferKind C.int

const (
	groupBuffer = bufferKind(C._SC_GETGR_R_SIZE_MAX)
)

func (k bufferKind) initialSize() C.size_t {
	sz := C.sysconf(C.int(k))
	if sz == -1 {
		// DragonFly and FreeBSD do not have _SC_GETPW_R_SIZE_MAX.
		// Additionally, not all Linux systems have it, either. For
		// example, the musl libc returns -1.
		return 1024
	}
	if !isSizeReasonable(int64(sz)) {
		// Truncate.  If this truly isn't enough, retryWithBuffer will error on the first run.
		return maxBufferSize
	}
	return C.size_t(sz)
}

type memBuffer struct {
	ptr  unsafe.Pointer
	size C.size_t
}

func alloc(kind bufferKind) *memBuffer {
	sz := kind.initialSize()
	return &memBuffer{
		ptr:  C.malloc(sz),
		size: sz,
	}
}

func (mb *memBuffer) resize(newSize C.size_t) {
	mb.ptr = C.realloc(mb.ptr, newSize)
	mb.size = newSize
}

func (mb *memBuffer) free() {
	C.free(mb.ptr)
}

// retryWithBuffer repeatedly calls f(), increasing the size of the
// buffer each time, until f succeeds, fails with a non-ERANGE error,
// or the buffer exceeds a reasonable limit.
func retryWithBuffer(buf *memBuffer, f func() syscall.Errno) error {
	for {
		errno := f()
		if errno == 0 {
			return nil
		} else if errno != syscall.ERANGE {
			return errno
		}
		newSize := buf.size * 2
		if !isSizeReasonable(int64(newSize)) {
			return fmt.Errorf("internal buffer exceeds %d bytes", maxBufferSize)
		}
		buf.resize(newSize)
	}
}

const maxBufferSize = 1 << 20

func isSizeReasonable(sz int64) bool {
	return sz > 0 && sz <= maxBufferSize
}

// end code from https://golang.org/src/os/user/lookup_unix.go
