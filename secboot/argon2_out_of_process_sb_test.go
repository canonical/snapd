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

package secboot_test

import (
	"errors"
	"io"
	"os/exec"
	"time"

	sb "github.com/snapcore/secboot"
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/secboot"
)

type argon2Suite struct {
}

var _ = Suite(&argon2Suite{})

func (*argon2Suite) TestHijackAndRunArgon2OutOfProcessHandlerOnArgArgon2Mode(c *C) {
	runArgon2Called := 0
	restore := secboot.MockSbWaitForAndRunArgon2OutOfProcessRequest(func(_ io.Reader, _ io.WriteCloser, _ sb.Argon2OutOfProcessWatchdogHandler) (lockRelease func(), err error) {
		runArgon2Called++
		return nil, nil
	})
	defer restore()

	setArgon2Called := 0
	restore = secboot.MockSbSetArgon2KDF(func(kdf sb.Argon2KDF) sb.Argon2KDF {
		setArgon2Called++
		return nil
	})
	defer restore()

	exitCalled := 0
	restore = secboot.MockOsExit(func(code int) {
		exitCalled++
		c.Assert(code, Equals, 0)
		panic("os.Exit(0)")
	})
	defer restore()

	restore = secboot.MockOsArgs([]string{"/path/to/cmd", "--test-special-argon2-mode"})
	defer restore()

	// Since we override os.Exit(0), we expect to panic (injected above)
	f := func() { secboot.HijackAndRunArgon2OutOfProcessHandlerOnArg([]string{"--test-special-argon2-mode"}) }
	c.Assert(f, Panics, "os.Exit(0)")

	c.Check(setArgon2Called, Equals, 0)
	c.Check(runArgon2Called, Equals, 1)
	c.Check(exitCalled, Equals, 1)
}

func (*argon2Suite) TestHijackAndRunArgon2OutOfProcessHandlerOnArgArgon2ModeError(c *C) {
	runArgon2Called := 0
	restore := secboot.MockSbWaitForAndRunArgon2OutOfProcessRequest(func(_ io.Reader, _ io.WriteCloser, _ sb.Argon2OutOfProcessWatchdogHandler) (lockRelease func(), err error) {
		runArgon2Called++
		return nil, errors.New("boom!")
	})
	defer restore()

	setArgon2Called := 0
	restore = secboot.MockSbSetArgon2KDF(func(kdf sb.Argon2KDF) sb.Argon2KDF {
		setArgon2Called++
		return nil
	})
	defer restore()

	exitCalled := 0
	restore = secboot.MockOsExit(func(code int) {
		exitCalled++
		c.Assert(code, Equals, 1)
		panic("os.Exit(1)")
	})
	defer restore()

	restore = secboot.MockOsArgs([]string{"/path/to/cmd", "--test-special-argon2-mode"})
	defer restore()

	f := func() { secboot.HijackAndRunArgon2OutOfProcessHandlerOnArg([]string{"--test-special-argon2-mode"}) }
	c.Assert(f, Panics, "os.Exit(1)")

	c.Check(setArgon2Called, Equals, 0)
	c.Check(runArgon2Called, Equals, 1)
	c.Check(exitCalled, Equals, 1)
}

type mockArgon2KDF struct{}

func (*mockArgon2KDF) Derive(passphrase string, salt []byte, mode sb.Argon2Mode, params *sb.Argon2CostParams, keyLen uint32) ([]byte, error) {
	return nil, nil
}

func (*mockArgon2KDF) Time(mode sb.Argon2Mode, params *sb.Argon2CostParams) (time.Duration, error) {
	return 0, nil
}

func (*argon2Suite) TestHijackAndRunArgon2OutOfProcessHandlerOnArgNormalMode(c *C) {
	runArgon2Called := 0
	restore := secboot.MockSbWaitForAndRunArgon2OutOfProcessRequest(func(_ io.Reader, _ io.WriteCloser, _ sb.Argon2OutOfProcessWatchdogHandler) (lockRelease func(), err error) {
		runArgon2Called++
		return nil, nil
	})
	defer restore()

	exitCalled := 0
	restore = secboot.MockOsExit(func(code int) {
		exitCalled++
		c.Assert(code, Equals, 0)
		panic("injected panic")
	})
	defer restore()

	restore = secboot.MockOsReadlink(func(name string) (string, error) {
		c.Assert(name, Equals, "/proc/self/exe")
		return "/path/to/cmd", nil
	})
	defer restore()

	argon2KDF := &mockArgon2KDF{}

	restore = secboot.MockSbNewOutOfProcessArgon2KDF(func(newHandlerCmd func() (*exec.Cmd, error), timeout time.Duration, watchdog sb.Argon2OutOfProcessWatchdogMonitor) sb.Argon2KDF {
		c.Check(timeout, Equals, 100*time.Millisecond)
		c.Check(watchdog, IsNil)

		cmd, err := newHandlerCmd()
		c.Assert(err, IsNil)
		c.Check(cmd.Path, Equals, "/path/to/cmd")
		c.Check(cmd.Args, DeepEquals, []string{"/path/to/cmd", "--test-special-argon2-mode"})

		return argon2KDF
	})
	defer restore()

	setArgon2Called := 0
	restore = secboot.MockSbSetArgon2KDF(func(kdf sb.Argon2KDF) sb.Argon2KDF {
		setArgon2Called++
		// Check pointer points to mock implementation
		c.Assert(kdf, Equals, argon2KDF)
		return nil
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
		// This should exit early before running the argon2 helper and calling os.Exit (and no injected panic)
		secboot.HijackAndRunArgon2OutOfProcessHandlerOnArg([]string{"--test-special-argon2-mode"})
	}

	c.Check(setArgon2Called, Equals, 4)
	c.Check(runArgon2Called, Equals, 0)
	c.Check(exitCalled, Equals, 0)
}
