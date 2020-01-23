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

func commonEquals(container, elem interface{}, result *bool, error *string) bool {
	containerV := reflect.ValueOf(container)
	elemV := reflect.ValueOf(elem)
	switch containerV.Kind() {
	case reflect.Slice, reflect.Array, reflect.Map:
		containerElemType := containerV.Type().Elem()
		if containerElemType.Kind() == reflect.Interface {
			// Ensure that element implements the type of elements stored in the container.
			if !elemV.Type().Implements(containerElemType) {
				*result = false
				*error = fmt.Sprintf(""+
					"container has items of interface type %s but expected"+
					" element does not implement it", containerElemType)
				return true
			}
		} else {
			// Ensure that type of elements in container is compatible with elem
			if containerElemType != elemV.Type() {
				*result = false
				*error = fmt.Sprintf(
					"container has items of type %s but expected element is a %s",
					containerElemType, elemV.Type())
				return true
			}
		}
	case reflect.String:
		// When container is a string, we expect elem to be a string as well
		if elemV.Kind() != reflect.String {
			*result = false
			*error = fmt.Sprintf("element is a %T but expected a string", elem)
		} else {
			*result = strings.Contains(containerV.String(), elemV.String())
			*error = ""
		}
		return true
	}
	return false
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
	if commonEquals(container, elem, &result, &error) {
		return result, error
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
// DeepEqual. The elem can be any object. The container can be an array, slice
// or string.
var DeepContains check.Checker = &deepContainsChecker{
	&check.CheckerInfo{Name: "DeepContains", Params: []string{"container", "elem"}},
}

func (c *deepContainsChecker) Check(params []interface{}, names []string) (result bool, error string) {
	var container interface{} = params[0]
	var elem interface{} = params[1]
	if commonEquals(container, elem, &result, &error) {
		return result, error
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
