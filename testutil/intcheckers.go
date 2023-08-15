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

	"gopkg.in/check.v1"
)

type intChecker struct {
	*check.CheckerInfo
	rel string
}

func (checker *intChecker) Check(params []interface{}, names []string) (result bool, error string) {
	a, ok := params[0].(int)
	if !ok {
		return false, "left-hand-side argument must be an int"
	}
	b, ok := params[1].(int)
	if !ok {
		return false, "right-hand-side argument must be an int"
	}
	switch checker.rel {
	case "<":
		result = a < b
	case "<=":
		result = a <= b
	case "==":
		result = a == b
	case "!=":
		result = a != b
	case ">":
		result = a > b
	case ">=":
		result = a >= b
	default:
		return false, fmt.Sprintf("unexpected relation %q", checker.rel)
	}
	if !result {
		error = fmt.Sprintf("relation %d %s %d is not true", a, checker.rel, b)
	}
	return result, error
}

// IntLessThan checker verifies that one integer is less than other integer.
//
// For example:
//
//	c.Assert(1, IntLessThan, 2)
var IntLessThan = &intChecker{CheckerInfo: &check.CheckerInfo{Name: "IntLessThan", Params: []string{"a", "b"}}, rel: "<"}

// IntLessEqual checker verifies that one integer is less than or equal to other integer.
//
// For example:
//
//	c.Assert(1, IntLessEqual, 1)
var IntLessEqual = &intChecker{CheckerInfo: &check.CheckerInfo{Name: "IntLessEqual", Params: []string{"a", "b"}}, rel: "<="}

// IntEqual checker verifies that one integer is equal to other integer.
//
// For example:
//
//	c.Assert(1, IntEqual, 1)
var IntEqual = &intChecker{CheckerInfo: &check.CheckerInfo{Name: "IntEqual", Params: []string{"a", "b"}}, rel: "=="}

// IntNotEqual checker verifies that one integer is not equal to other integer.
//
// For example:
//
//	c.Assert(1, IntNotEqual, 2)
var IntNotEqual = &intChecker{CheckerInfo: &check.CheckerInfo{Name: "IntNotEqual", Params: []string{"a", "b"}}, rel: "!="}

// IntGreaterThan checker verifies that one integer is greater than other integer.
//
// For example:
//
//	c.Assert(2, IntGreaterThan, 1)
var IntGreaterThan = &intChecker{CheckerInfo: &check.CheckerInfo{Name: "IntGreaterThan", Params: []string{"a", "b"}}, rel: ">"}

// IntGreaterEqual checker verifies that one integer is greater than or equal to other integer.
//
// For example:
//
//	c.Assert(1, IntGreaterEqual, 2)
var IntGreaterEqual = &intChecker{CheckerInfo: &check.CheckerInfo{Name: "IntGreaterEqual", Params: []string{"a", "b"}}, rel: ">="}
