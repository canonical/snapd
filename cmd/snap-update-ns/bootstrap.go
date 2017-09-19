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
	"errors"
	"fmt"
	"syscall"
	"unsafe"
)

var (
	// ErrNoNamespace is returned when a snap namespace does not exist.
	ErrNoNamespace = errors.New("cannot update mount namespace that was not created yet")
)

// IMPORTANT: all the code in this section may be run with elevated privileges
// when invoking snap-update-ns from the setuid snap-confine.

// BootstrapError returns error (if any) encountered in pre-main C code.
func BootstrapError() error {
	if C.bootstrap_msg == nil {
		return nil
	}
	errno := syscall.Errno(C.bootstrap_errno)
	// Translate EINVAL from setns or ENOENT from open into a dedicated error.
	if errno == syscall.EINVAL || errno == syscall.ENOENT {
		return ErrNoNamespace
	}
	if errno != 0 {
		return fmt.Errorf("%s: %s", C.GoString(C.bootstrap_msg), errno)
	}
	return fmt.Errorf("%s", C.GoString(C.bootstrap_msg))
}

// END IMPORTANT

// readCmdline is a wrapper around the C function read_cmdline.
func readCmdline(buf []byte) C.ssize_t {
	return C.read_cmdline((*C.char)(unsafe.Pointer(&buf[0])), C.size_t(cap(buf)))
}

// findArgv0 parses the argv-like array and finds the 0st argument.
func findArgv0(buf []byte) *string {
	if ptr := C.find_argv0((*C.char)(unsafe.Pointer(&buf[0])), C.size_t(len(buf))); ptr != nil {
		str := C.GoString(ptr)
		return &str
	}
	return nil
}

// findSnapName parses the argv-like array and finds the 1st argument.
func findSnapName(buf []byte) *string {
	if ptr := C.find_snap_name((*C.char)(unsafe.Pointer(&buf[0])), C.size_t(len(buf))); ptr != nil {
		str := C.GoString(ptr)
		return &str
	}
	return nil
}

// validateSnapName checks if snap name is valid.
// This also sets bootstrap_msg on failure.
func validateSnapName(snapName string) int {
	cStr := C.CString(snapName)
	return int(C.validate_snap_name(cStr))
}

// processArguments parses commnad line arguments.
// The argument cmdline is a string with embedded
// NUL bytes, separating particular arguments.
func processArguments(cmdline []byte) (snapName string, shouldSetNs bool) {
	var snapNameOut *C.char
	var shouldSetNsOut C.bool
	C.process_arguments((*C.char)(unsafe.Pointer(&cmdline[0])), C.size_t(len(cmdline)), &snapNameOut, &shouldSetNsOut)
	if snapNameOut != nil {
		snapName = C.GoString(snapNameOut)
	}
	shouldSetNs = bool(shouldSetNsOut)
	return
}
