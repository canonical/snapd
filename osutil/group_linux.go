// -*- Mode: Go; indent-tabs-mode: t -*-
// +build darwin freebsd linux
// +build cgo

/*
 * Copyright (C) 2014-2015 Canonical Ltd
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

/*
#include <grp.h>
#include <stdlib.h> // for free
#include <sys/types.h>
#include <unistd.h> // for sysconf
*/
import "C"

import (
	"fmt"
	"syscall"
	"unsafe"
)

func getgrnam(name string) (grp Group, err error) {
	var cgrp C.struct_group
	var result *C.struct_group

	nameC := C.CString(name)
	defer C.free(unsafe.Pointer(nameC))

	bufSize := C.sysconf(C._SC_GETGR_R_SIZE_MAX)
	if bufSize <= 0 || bufSize > 1<<20 {
		return grp, fmt.Errorf("unreasonable C._SC_GETGR_R_SIZE_MAX %d", bufSize)
	}
	buf := C.malloc(C.size_t(bufSize))
	defer C.free(buf)

	// getgrnam_r is harder to use (from cgo), but it is thread safe
	rv := C.getgrnam_r(nameC, &cgrp, (*C.char)(buf), C.size_t(bufSize), &result)
	if rv != 0 {
		return grp, fmt.Errorf("getgrnam_r failed for %s: %s", name, syscall.Errno(rv))
	}
	if result == nil {
		return grp, fmt.Errorf("group %q not found", name)
	}

	grp.Name = C.GoString(result.gr_name)
	grp.Passwd = C.GoString(result.gr_passwd)
	grp.Gid = uint(result.gr_gid)

	p := unsafe.Pointer(result.gr_mem)
	for p != nil && (*(**C.char)(p)) != nil {
		member := C.GoString((*(**C.char)(p)))
		grp.Mem = append(grp.Mem, member)
		p = unsafe.Pointer(uintptr(p) + unsafe.Sizeof(p))
	}

	return grp, nil
}
