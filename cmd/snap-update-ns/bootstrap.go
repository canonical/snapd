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

package main

// Use a pre-main helper to switch the mount namespace. This is required as
// golang creates threads at will and setns(..., CLONE_NEWNS) fails if any
// threads apart from the main thread exist.

/*

#include <stdlib.h>
#include "bootstrap.h"

__attribute__((constructor)) static void init(void) {
	bootstrap();
}

// NOTE: do not add anything before the following `import "C"'
*/
import "C"

import (
	"fmt"
	"syscall"
	"unsafe"
)

// Error returns error (if any) encountered in pre-main C code.
func BootstrapError() error {
	if C.bootstrap_msg == nil {
		return nil
	}
	errno := syscall.Errno(C.bootstrap_errno)
	if errno != 0 {
		return fmt.Errorf("%s: %s", C.GoString(C.bootstrap_msg), errno)
	}
	return fmt.Errorf("%s", C.GoString(C.bootstrap_msg))
}

// readCmdline is a wrapper around the C function read_cmdline.
func readCmdline(buf []byte) C.ssize_t {
	return C.read_cmdline((*C.char)(unsafe.Pointer(&buf[0])), C.size_t(cap(buf)))
}

// findSnapName parses the argv-like array and finds the 1st argument.
// Subsequent arguments are spearated by NUL-bytes.
func findSnapName(buf []byte) *string {
	if ptr := C.find_snap_name((*C.char)(unsafe.Pointer(&buf[0])), C.size_t(len(buf))); ptr != nil {
		str := C.GoString(ptr)
		return &str
	}
	return nil
}

// partiallyValidateSnapName checks if snap name is seemingly valid.
// The real part of the validation happens on the go side.
func partiallyValidateSnapName(snapName string) int {
	cStr := C.CString(snapName)
	return int(C.partially_validate_snap_name(cStr))
}
