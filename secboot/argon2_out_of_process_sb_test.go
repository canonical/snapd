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

package secboot_test

import (
	"errors"
	"io"

	sb "github.com/snapcore/secboot"
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/secboot"
)

type argon2Suite struct {
}

var _ = Suite(&argon2Suite{})

func (*argon2Suite) TestMaybeRunArgon2OutOfProcessRequestHandler(c *C) {
	argon2Called := 0
	lockReleaseCalled := 0
	restore := secboot.MockSbWaitForAndRunArgon2OutOfProcessRequest(func(_ io.Reader, _ io.WriteCloser, _ sb.Argon2OutOfProcessWatchdogHandler) (lockRelease func(), err error) {
		argon2Called++
		return func() {
			lockReleaseCalled++
		}, nil
	})
	defer restore()

	exitCalled := 0
	restore = secboot.MockOsExit(func(code int) {
		exitCalled++
		c.Assert(code, Equals, 0)
	})
	defer restore()

	restore = secboot.MockOsArgs([]string{"/path/to/cmd", "--argon2-proc"})
	defer restore()

	// Since we override os.Exit(0), we expect to panic
	c.Assert(secboot.MaybeRunArgon2OutOfProcessRequestHandler, Panics, "internal error: not reachable")

	c.Check(argon2Called, Equals, 1)
	c.Check(exitCalled, Equals, 1)
	c.Check(lockReleaseCalled, Equals, 1)
}

func (*argon2Suite) TestMaybeRunArgon2OutOfProcessRequestHandlerNotTriggered(c *C) {
	argon2Called := 0
	lockReleaseCalled := 0
	restore := secboot.MockSbWaitForAndRunArgon2OutOfProcessRequest(func(_ io.Reader, _ io.WriteCloser, _ sb.Argon2OutOfProcessWatchdogHandler) (lockRelease func(), err error) {
		argon2Called++
		return func() {
			lockReleaseCalled++
		}, nil
	})
	defer restore()

	exitCalled := 0
	restore = secboot.MockOsExit(func(code int) {
		exitCalled++
		c.Assert(code, Equals, 0)
	})
	defer restore()

	for _, args := range [][]string{
		{},
		{"/path/to/cmd"},
		{"/path/to/cmd", "not-run-argon2"},
		{"/path/to/cmd", "not-run-argon2", "--argon2-proc"},
	} {
		restore := secboot.MockOsArgs(args)
		defer restore()
		err := secboot.MaybeRunArgon2OutOfProcessRequestHandler()
		c.Assert(err, IsNil)
	}

	c.Check(argon2Called, Equals, 0)
	c.Check(exitCalled, Equals, 0)
	c.Check(lockReleaseCalled, Equals, 0)
}

func (*argon2Suite) TestMaybeRunArgon2OutOfProcessRequestHandlerError(c *C) {
	argon2Called := 0
	lockReleaseCalled := 0
	restore := secboot.MockSbWaitForAndRunArgon2OutOfProcessRequest(func(_ io.Reader, _ io.WriteCloser, _ sb.Argon2OutOfProcessWatchdogHandler) (lockRelease func(), err error) {
		argon2Called++
		return func() {
			lockReleaseCalled++
		}, errors.New("boom!")
	})
	defer restore()

	exitCalled := 0
	restore = secboot.MockOsExit(func(code int) {
		exitCalled++
		c.Assert(code, Equals, 0)
	})
	defer restore()

	restore = secboot.MockOsArgs([]string{"/path/to/cmd", "--argon2-proc"})
	defer restore()

	c.Assert(secboot.MaybeRunArgon2OutOfProcessRequestHandler(), ErrorMatches, "cannot run request: boom!")

	c.Check(argon2Called, Equals, 1)
	c.Check(exitCalled, Equals, 0)
	c.Check(lockReleaseCalled, Equals, 1)
}
