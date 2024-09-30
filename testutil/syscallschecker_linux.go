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

package testutil

import (
	"fmt"
	"reflect"

	"gopkg.in/check.v1"
)

type syscallsEqualChecker struct {
	*check.CheckerInfo
}

// SyscallsEqual checks that one sequence of system calls is equal to another.
var SyscallsEqual check.Checker = &syscallsEqualChecker{
	CheckerInfo: &check.CheckerInfo{Name: "SyscallsEqual", Params: []string{"actualList", "expectedList"}},
}

func (c *syscallsEqualChecker) Check(params []interface{}, names []string) (result bool, error string) {
	actualList, ok := params[0].([]CallResultError)
	if !ok {
		return false, "left-hand-side argument must be of type []CallResultError"
	}
	expectedList, ok := params[1].([]CallResultError)
	if !ok {
		return false, "right-hand-side argument must be of type []CallResultError"
	}

	for i, actual := range actualList {
		if i >= len(expectedList) {
			return false, fmt.Sprintf("system call #%d %#q unexpectedly present, got %d system call(s) but expected only %d",
				i, actual.C, len(actualList), len(expectedList))
		}
		expected := expectedList[i]
		if actual.C != expected.C {
			return false, fmt.Sprintf("system call #%d differs in operation, actual %#q, expected %#q", i, actual.C, expected.C)
		}
		if !reflect.DeepEqual(actual, expected) {
			switch {
			case actual.E == nil && expected.E == nil && !reflect.DeepEqual(actual.R, expected.R):
				// The call succeeded but not like we expected.
				return false, fmt.Sprintf("system call #%d %#q differs in result, actual: %#v, expected: %#v",
					i, actual.C, actual.R, expected.R)
			case actual.E != nil && expected.E != nil && !reflect.DeepEqual(actual.E, expected.E):
				// The call failed but not like we expected.
				return false, fmt.Sprintf("system call #%d %#q differs in error, actual: %s, expected: %s",
					i, actual.C, actual.E, expected.E)
			case actual.E != nil && expected.E == nil && expected.R == nil:
				// The call failed but we expected it to succeed.
				return false, fmt.Sprintf("system call #%d %#q unexpectedly failed, actual error: %s", i, actual.C, actual.E)
			case actual.E != nil && expected.E == nil && expected.R != nil:
				// The call failed but we expected it to succeed with some result.
				return false, fmt.Sprintf("system call #%d %#q unexpectedly failed, actual error: %s, expected result: %#v", i, actual.C, actual.E, expected.R)
			case actual.E == nil && expected.E != nil && actual.R == nil:
				// The call succeeded with some result but we expected it to fail.
				return false, fmt.Sprintf("system call #%d %#q unexpectedly succeeded, expected error: %s", i, actual.C, expected.E)
			case actual.E == nil && expected.E != nil && actual.R != nil:
				// The call succeeded but we expected it to fail.
				return false, fmt.Sprintf("system call #%d %#q unexpectedly succeeded, actual result: %#v, expected error: %s", i, actual.C, actual.R, expected.E)
			default:
				panic("unexpected call-result-error case")
			}
		}
	}
	if len(actualList) < len(expectedList) && len(expectedList) > 0 {
		return false, fmt.Sprintf("system call #%d %#q unexpectedly absent, got only %d system call(s) but expected %d",
			len(actualList), expectedList[len(actualList)].C, len(actualList), len(expectedList))
	}
	return true, ""
}
