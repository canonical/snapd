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

package secboot

import (
	"fmt"
	"os"
	"os/exec"
	"time"

	sb "github.com/snapcore/secboot"

	"github.com/snapcore/snapd/logger"
)

func init() {
	setArgon2KDF()
}

const outOfProcessArgon2KDFTimeout = 100 * time.Millisecond
const outOfProcessArgon2Arg = "--argon2-proc"

func setArgon2KDF() error {
	// This assumes that the calling binary uses MaybeRunArgon2OutOfProcessRequestHandler early in main().
	exe, err := os.Readlink("/proc/self/exe")
	if err != nil {
		return err
	}

	handlerCmd := func() (*exec.Cmd, error) {
		cmd := exec.Command(exe, outOfProcessArgon2Arg)
		return cmd, nil
	}
	argon2KDF := sb.NewOutOfProcessArgon2KDF(handlerCmd, outOfProcessArgon2KDFTimeout, nil)
	sb.SetArgon2KDF(argon2KDF)

	return nil
}

var osExit = os.Exit
var sbWaitForAndRunArgon2OutOfProcessRequest = sb.WaitForAndRunArgon2OutOfProcessRequest

// MaybeRunArgon2OutOfProcessRequestHandler is supposed to be called
// from the main() of binaries involved with sealing/unsealing of
// keys (i.e. snapd and snap-bootstrap).
//
// This switches the binary to a special mode when the --argon2-proc arg
// is detected where it acts as an argon2 out-of-process helper command
// and exits when its work is done.
//
// For more context, check docs for sb.WaitForAndRunArgon2OutOfProcessRequest
// and sb.NewOutOfProcessArgon2KDF for details on how the flow works
// in secboot.
func MaybeRunArgon2OutOfProcessRequestHandler() error {
	if len(os.Args) < 2 || os.Args[1] != outOfProcessArgon2Arg {
		return nil
	}

	watchdog := sb.NoArgon2OutOfProcessWatchdogHandler()

	logger.Noticef("running argon2 out-of-process helper")
	// Lock will be released implicitly on process termination, but let's also
	// explicitly release lock.
	lockRelease, err := sbWaitForAndRunArgon2OutOfProcessRequest(os.Stdin, os.Stdout, watchdog)
	defer lockRelease()
	if err != nil {
		return fmt.Errorf("cannot run request: %w", err)
	}

	// Argon2 request was processed successfully
	osExit(0)

	panic("internal error: not reachable")
}
