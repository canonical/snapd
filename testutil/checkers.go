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
	"reflect"
	"strings"

	"gopkg.in/check.v1"
)

type containsChecker struct {
	*check.CheckerInfo
}

// Contains is a Checker that looks for a elem in a container.
// The elem can be any object. The container can be an array, slice or string.
var Contains check.Checker = &containsChecker{
	&check.CheckerInfo{Name: "Contains", Params: []string{"container", "elem"}},
}

func (c *containsChecker) Check(params []interface{}, names []string) (result bool, error string) {
	defer func() {
		if v := recover(); v != nil {
			result = false
			error = fmt.Sprint(v)
		}
	}()
	var container interface{} = params[0]
	var elem interface{} = params[1]
	switch containerV := reflect.ValueOf(container); containerV.Kind() {
	case reflect.Slice, reflect.Array, reflect.Map:
		// Ensure that type of elements in container is compatible with elem
		if elemV := reflect.ValueOf(elem); containerV.Type().Elem() != elemV.Type() {
			return false, fmt.Sprintf(
				"container has items of type %s but expected element is a %s",
				containerV.Type().Elem(), elemV.Type())
		}
	case reflect.String:
		// When container is a string, we expect elem to be a string as well
		elemV := reflect.ValueOf(elem)
		if elemV.Kind() != reflect.String {
			return false, fmt.Sprintf("element is a %T but expected a string", elem)
		}
		return strings.Contains(containerV.String(), elemV.String()), ""
	}
	// Do the actual test using ==
	switch containerV := reflect.ValueOf(container); containerV.Kind() {
	case reflect.Slice, reflect.Array:
		for length, i := containerV.Len(), 0; i < length; i++ {
			itemV := containerV.Index(i)
			if itemV.Interface() == elem {
				return true, ""
			}
		}
		return false, ""
	case reflect.Map:
		for _, keyV := range containerV.MapKeys() {
			itemV := containerV.MapIndex(keyV)
			if itemV.Interface() == elem {
				return true, ""
			}
		}
		return false, ""
	default:
		return false, fmt.Sprintf("%T is not a supported container", container)
	}
}

type deepContainsChecker struct {
	*check.CheckerInfo
}

// DeepContains is a Checker that looks for a elem in a container using
// DeepEqual.  The elem can be any object. The container can be an array, slice
// or string.
var DeepContains check.Checker = &deepContainsChecker{
	&check.CheckerInfo{Name: "DeepContains", Params: []string{"container", "elem"}},
}

func (c *deepContainsChecker) Check(params []interface{}, names []string) (result bool, error string) {
	var container interface{} = params[0]
	var elem interface{} = params[1]
	switch containerV := reflect.ValueOf(container); containerV.Kind() {
	case reflect.Slice, reflect.Array, reflect.Map:
		// Ensure that type of elements in container is compatible with elem
		if elemV := reflect.ValueOf(elem); containerV.Type().Elem() != elemV.Type() {
			return false, fmt.Sprintf(
				"container has items of type %s but expected element is a %s",
				containerV.Type().Elem(), elemV.Type())
		}
	case reflect.String:
		// When container is a string, we expect elem to be a string as well
		elemV := reflect.ValueOf(elem)
		if elemV.Kind() != reflect.String {
			return false, fmt.Sprintf("element is a %T but expected a string", elem)
		}
		return strings.Contains(containerV.String(), elemV.String()), ""
	}
	// Do the actual test using reflect.DeepEqual
	switch containerV := reflect.ValueOf(container); containerV.Kind() {
	case reflect.Slice, reflect.Array:
		for length, i := containerV.Len(), 0; i < length; i++ {
			itemV := containerV.Index(i)
			if reflect.DeepEqual(itemV.Interface(), elem) {
				return true, ""
			}
		}
		return false, ""
	case reflect.Map:
		for _, keyV := range containerV.MapKeys() {
			itemV := containerV.MapIndex(keyV)
			if reflect.DeepEqual(itemV.Interface(), elem) {
				return true, ""
			}
		}
		return false, ""
	default:
		return false, fmt.Sprintf("%T is not a supported container", container)
	}
}
