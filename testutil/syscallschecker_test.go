// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2015-2018 Canonical Ltd
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

package testutil_test

import (
	"errors"

	"gopkg.in/check.v1"

	. "github.com/snapcore/snapd/testutil"
)

type syscallsCheckerSuite struct{}

var _ = check.Suite(&syscallsCheckerSuite{})

func (*syscallsCheckerSuite) TestSystemCallSequenceEqual(c *check.C) {
	c.Assert([]CallResultError{}, SyscallsEqual, []CallResultError{})
	c.Assert([]CallResultError{}, SyscallsEqual, []CallResultError(nil))
	c.Assert([]CallResultError{{C: `foo`}}, SyscallsEqual, []CallResultError{{C: `foo`}})
	c.Assert([]CallResultError{{C: `foo`}, {C: `bar`}}, SyscallsEqual, []CallResultError{{C: `foo`}, {C: `bar`}})
	c.Assert([]CallResultError{{C: `foo`, R: 123}}, SyscallsEqual, []CallResultError{{C: `foo`, R: 123}})
	c.Assert([]CallResultError{{C: `foo`, E: errors.New("bad")}}, SyscallsEqual, []CallResultError{{C: `foo`, E: errors.New("bad")}})

	// Wrong argument types.
	testCheck(c, SyscallsEqual, false, "left-hand-side argument must be of type []CallResultError",
		true, []CallResultError{{C: `bar`}})
	testCheck(c, SyscallsEqual, false, "right-hand-side argument must be of type []CallResultError",
		[]CallResultError{{C: `bar`}}, true)
	// Different system call operations.
	testCheck(c, SyscallsEqual, false, "system call #0 differs in operation, actual `foo`, expected `bar`",
		[]CallResultError{{C: `foo`}}, []CallResultError{{C: `bar`}})
	// Different system call results.
	testCheck(c, SyscallsEqual, false, "system call #0 `foo` differs in result, actual: 1, expected: 2",
		[]CallResultError{{C: `foo`, R: 1}}, []CallResultError{{C: `foo`, R: 2}})
	// Different system call errors.
	testCheck(c, SyscallsEqual, false, "system call #0 `foo` differs in error, actual: barf, expected: bork",
		[]CallResultError{{C: `foo`, E: errors.New("barf")}}, []CallResultError{{C: `foo`, E: errors.New("bork")}})
	// Unexpected success with non-nil result.
	testCheck(c, SyscallsEqual, false, "system call #0 `foo` unexpectedly succeeded, actual result: 1, expected error: broken",
		[]CallResultError{{C: `foo`, R: 1}}, []CallResultError{{C: `foo`, E: errors.New("broken")}})
	// Unexpected success with nil result.
	testCheck(c, SyscallsEqual, false, "system call #0 `foo` unexpectedly succeeded, expected error: broken",
		[]CallResultError{{C: `foo`}}, []CallResultError{{C: `foo`, E: errors.New("broken")}})
	// Unexpected failure with expected non-nil result.
	testCheck(c, SyscallsEqual, false, "system call #0 `foo` unexpectedly failed, actual error: broken, expected result: 1",
		[]CallResultError{{C: `foo`, E: errors.New("broken")}}, []CallResultError{{C: `foo`, R: 1}})
	// Unexpected failure with expected nil result.
	testCheck(c, SyscallsEqual, false, "system call #0 `foo` unexpectedly failed, actual error: broken",
		[]CallResultError{{C: `foo`, E: errors.New("broken")}}, []CallResultError{{C: `foo`}})
	// More system calls than expected.
	testCheck(c, SyscallsEqual, false, "system call #1 `bar` unexpectedly present, got 2 system call(s) but expected only 1",
		[]CallResultError{{C: `foo`}, {C: `bar`}}, []CallResultError{{C: `foo`}})
	testCheck(c, SyscallsEqual, false, "system call #0 `foo` unexpectedly present, got 2 system call(s) but expected only 0",
		[]CallResultError{{C: `foo`}, {C: `bar`}}, []CallResultError{})
	// Fewer system calls than expected.
	testCheck(c, SyscallsEqual, false, "system call #1 `bar` unexpectedly absent, got only 1 system call(s) but expected 2",
		[]CallResultError{{C: `foo`}}, []CallResultError{{C: `foo`}, {C: `bar`}})
	testCheck(c, SyscallsEqual, false, "system call #0 `foo` unexpectedly absent, got only 0 system call(s) but expected 1",
		[]CallResultError{}, []CallResultError{{C: `foo`}})
}
