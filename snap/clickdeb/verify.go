// -*- Mode: Go; indent-tabs-mode: t -*-

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

package clickdeb

import (
	"fmt"
	"os/exec"

	"github.com/ubuntu-core/snappy/helpers"
	"github.com/ubuntu-core/snappy/logger"
)

const (
	// from debsig-verify-0.9/debsigs.h (also in debsig-verify(1))
	dsSuccess           = 0
	dsFailNosigs        = 10
	dsFailUnknownOrigin = 11
	dsFailNopolicies    = 12
	dsFailBadsig        = 13
	dsFailInternal      = 14
)

// error messages from debsig-verify(1), edited for brevity
var dsErrMsg = map[int]string{
	dsFailNosigs:        "No signatures, or no origin signature.",
	dsFailUnknownOrigin: "No policies subdirectory for origin signature.",
	dsFailNopolicies:    "No policies passed selection; no verification done.",
	dsFailBadsig:        "Bad signature, or other verification criteria failed.",
	dsFailInternal:      "Internal error. E.g. Package corrupt, gpg failed, etc.",
}

// ErrSignature is returned if a snap failed the signature verification
type ErrSignature struct {
	exitCode int
	err      error
}

func (e *ErrSignature) Error() string {
	if e.err != nil {
		return fmt.Sprintf("Signature verification failed: %v", e.err)
	}

	dsc, ok := dsErrMsg[e.exitCode]
	if !ok {
		return fmt.Sprintf("Signature verification failed with exit status %v", e.exitCode)
	}

	return fmt.Sprintf("Signature verification failed: %s (exit code %d)", dsc, e.exitCode)
}

// This function checks if the given exitCode is "ok" when running with
// --allow-unauthenticated. We allow package with no signature or with
// a unknown policy or with no policies at all. We do not allow overriding
// bad signatures
func allowUnauthenticatedOkExitCode(exitCode int) bool {
	return (exitCode == dsFailNosigs ||
		exitCode == dsFailUnknownOrigin ||
		exitCode == dsFailNopolicies)
}

// Verify is a tiny wrapper around debsig-verify
func Verify(clickFile string, allowUnauthenticated bool) (err error) {
	cmd := exec.Command(VerifyCmd, clickFile)
	if err := cmd.Run(); err != nil {
		exitCode, err := helpers.ExitCode(err)
		if err == nil {
			if allowUnauthenticated && allowUnauthenticatedOkExitCode(exitCode) {
				logger.Noticef("Signature check failed, but installing anyway as requested")
				return nil
			}
			return &ErrSignature{exitCode: exitCode}
		}
		// not a exit code error, something else, pass on
		return &ErrSignature{err: err}
	}
	return nil
}

// VerifyCmd is the command to run for Verify
var VerifyCmd = "debsig-verify"
