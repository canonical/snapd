// -*- Mode: Go; indent-tabs-mode: t -*-
//go:build !nosecboot

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

package secboot

import (
	"fmt"
	"os"
	"os/exec"
	"time"

	sb "github.com/snapcore/secboot"

	"github.com/snapcore/snapd/logger"
)

var (
	osExit     = os.Exit
	osReadlink = os.Readlink

	sbWaitForAndRunArgon2OutOfProcessRequest = sb.WaitForAndRunArgon2OutOfProcessRequest
	sbNewOutOfProcessArgon2KDF               = sb.NewOutOfProcessArgon2KDF
	sbSetArgon2KDF                           = sb.SetArgon2KDF
)

const (
	outOfProcessArgon2KDFTimeout = 100 * time.Millisecond
)

// HijackAndRunArgon2OutOfProcessHandlerOnArg is supposed to be called from the
// main() of binaries involved with sealing/unsealing of keys (i.e. snapd and
// snap-bootstrap).
//
// This switches the binary to a special argon2 mode when the matching args are
// detected where it hijacks the process and acts as an argon2 out-of-process
// helper command and exits when its work is done, otherwise (in normal mode)
// it sets the default argon2 kdf implementation to be self-invoking into the
// special argon2 mode of the calling binary.
//
// For more context, check docs for sb.WaitForAndRunArgon2OutOfProcessRequest
// and sb.NewOutOfProcessArgon2KDF for details on how the flow works
// in secboot.
func HijackAndRunArgon2OutOfProcessHandlerOnArg(args []string) {
	if !isOutOfProcessArgon2KDFMode(args) {
		// Binary was invoked in normal mode, let's setup default argon2 kdf implementation
		// to point to this binary when invoked using special args.
		exe, err := osReadlink("/proc/self/exe")
		if err != nil {
			logger.Noticef("internal error: failed to read symlink of /proc/self/exe: %v", err)
			return
		}

		handlerCmd := func() (*exec.Cmd, error) {
			cmd := exec.Command(exe, args...)
			return cmd, nil
		}
		argon2KDF := sbNewOutOfProcessArgon2KDF(handlerCmd, outOfProcessArgon2KDFTimeout, nil)
		sbSetArgon2KDF(argon2KDF)

		return
	}

	logger.Noticef("running argon2 out-of-process helper")
	// Ignore the lock release callback and use implicit release on process termination.
	_, err := sbWaitForAndRunArgon2OutOfProcessRequest(os.Stdin, os.Stdout, sb.NoArgon2OutOfProcessWatchdogHandler())
	if err != nil {
		fmt.Fprintf(os.Stderr, "cannot run argon2 out-of-process request: %v", err)
		osExit(1)
	}

	// Argon2 request was processed successfully
	osExit(0)

	panic("internal error: not reachable")
}
