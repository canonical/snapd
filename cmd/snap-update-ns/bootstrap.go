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

// The bootstrap function is called by the loader before passing
// control to main. We are using `preinit_array` rather than
// `init_array` because the Go linker adds its own initialisation
// function to `init_array`, and having ours run second would defeat
// the purpose of the C bootstrap code.
//
// The `used` attribute ensures that the compiler doesn't oprimise out
// the variable on the mistaken belief that it isn't used.
__attribute__((section(".preinit_array"), used)) static typeof(&bootstrap) init = &bootstrap;

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

func clearBootstrapError() {
	C.bootstrap_msg = nil
	C.bootstrap_errno = 0
}

// END IMPORTANT

func makeArgv(args []string) []*C.char {
	// Create argv array with terminating NULL element
	argv := make([]*C.char, len(args)+1)
	for i, arg := range args {
		argv[i] = C.CString(arg)
	}
	return argv
}

func freeArgv(argv []*C.char) {
	for _, arg := range argv {
		C.free(unsafe.Pointer(arg))
	}
}

// validateInstanceName checks if snap instance name is valid.
// This also sets bootstrap_msg on failure.
func validateInstanceName(instanceName string) int {
	cStr := C.CString(instanceName)
	defer C.free(unsafe.Pointer(cStr))
	return int(C.validate_instance_name(cStr))
}

// processArguments parses commnad line arguments.
// The argument cmdline is a string with embedded
// NUL bytes, separating particular arguments.
func processArguments(args []string) (snapName string, shouldSetNs bool, processUserFstab bool) {
	argv := makeArgv(args)
	defer freeArgv(argv)

	var snapNameOut *C.char
	var shouldSetNsOut C.bool
	var processUserFstabOut C.bool
	C.process_arguments(C.int(len(args)), &argv[0], &snapNameOut, &shouldSetNsOut, &processUserFstabOut)
	if snapNameOut != nil {
		snapName = C.GoString(snapNameOut)
	}
	shouldSetNs = bool(shouldSetNsOut)
	processUserFstab = bool(processUserFstabOut)

	return snapName, shouldSetNs, processUserFstab
}
