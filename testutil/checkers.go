// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2015 Canonical Ltd
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
	"reflect"
	"strings"
)

type containsChecker struct {
	*check.CheckerInfo
}

// Contains is a Checker that looks for a needle in a haystack.
// The needle can be any object. The haystack can be an array, slice or string.
var Contains check.Checker = &containsChecker{
	&check.CheckerInfo{Name: "Contains", Params: []string{"haystack", "needle"}},
}

func (c *containsChecker) Check(params []interface{}, names []string) (result bool, error string) {
	defer func() {
		if v := recover(); v != nil {
			result = false
			error = fmt.Sprint(v)
		}
	}()
	var haystack interface{} = params[0]
	var needle interface{} = params[1]
	switch haystackV := reflect.ValueOf(haystack); haystackV.Kind() {
	case reflect.Slice, reflect.Array:
		// Ensure that type of elements in haystack is compatible with needle
		if needleV := reflect.ValueOf(needle); haystackV.Type().Elem() != needleV.Type() {
			panic(fmt.Sprintf("haystack contains items of type %s but needle is a %s",
				haystackV.Type().Elem(), needleV.Type()))
		}
		for len, i := haystackV.Len(), 0; i < len; i++ {
			itemV := haystackV.Index(i)
			if itemV.Interface() == needle {
				return true, ""
			}
		}
		return false, ""
	case reflect.Map:
		// Ensure that type of elements in haystack is compatible with needle
		if needleV := reflect.ValueOf(needle); haystackV.Type().Elem() != needleV.Type() {
			panic(fmt.Sprintf("haystack contains items of type %s but needle is a %s",
				haystackV.Type().Elem(), needleV.Type()))
		}
		for _, keyV := range haystackV.MapKeys() {
			itemV := haystackV.MapIndex(keyV)
			if itemV.Interface() == needle {
				return true, ""
			}
		}
		return false, ""
	case reflect.String:
		// When haystack is a string, we expect needle to be a string as well
		needle := params[1].(string)
		haystack := params[0].(string)
		return strings.Contains(haystack, needle), ""
	default:
		panic(fmt.Sprintf("haystack is of unsupported type %T", params[0]))
	}
}
