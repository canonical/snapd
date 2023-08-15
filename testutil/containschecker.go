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

type deepUnsortedMatchChecker struct {
	*check.CheckerInfo
}

// DeepUnsortedMatches checks if two containers contain the same elements in
// the same number (but possibly different order) using DeepEqual. The container
// can be an array, a slice or a map.
var DeepUnsortedMatches check.Checker = &deepUnsortedMatchChecker{
	&check.CheckerInfo{Name: "DeepUnsortedMatches", Params: []string{"container1", "container2"}},
}

func (c *deepUnsortedMatchChecker) Check(params []interface{}, _ []string) (bool, string) {
	container1 := reflect.ValueOf(params[0])
	container2 := reflect.ValueOf(params[1])

	// if both args are nil, return true
	if container1.Kind() == reflect.Invalid && container2.Kind() == reflect.Invalid {
		return true, ""
	}

	if container1.Kind() == reflect.Invalid || container2.Kind() == reflect.Invalid {
		return false, "only one container was nil"
	}

	if container1.Kind() != container2.Kind() {
		return false, fmt.Sprintf("containers are of different types: %s != %s", container1.Kind(), container2.Kind())
	}

	switch container1.Kind() {
	case reflect.Array, reflect.Slice:
		return deepSequenceMatch(container1, container2)
	case reflect.Map:
		return deepMapMatch(container1, container2)
	default:
		return false, fmt.Sprintf("'%s' is not a supported type: must be slice, array or map", container1.Kind().String())
	}
}

func deepMapMatch(container1, container2 reflect.Value) (bool, string) {
	if valid, output := validateContainerTypesAndLengths(container1, container2); !valid {
		return false, output
	}

	switch container1.Type().Elem().Kind() {
	case reflect.Slice, reflect.Array, reflect.Map:
	// only run the unsorted match if the map values are containers
	default:
		if !reflect.DeepEqual(container1.Interface(), container2.Interface()) {
			return false, "maps don't match"
		}
		return true, ""
	}

	for _, key := range container1.MapKeys() {
		el1 := container1.MapIndex(key)
		el2 := container2.MapIndex(key)

		absent := el2 == reflect.Value{}
		if absent {
			return false, fmt.Sprintf("key %q from one map is absent from the other map", key)
		}

		var ok bool
		var msg string
		switch el1.Kind() {
		case reflect.Array, reflect.Slice:
			ok, msg = deepSequenceMatch(el1, el2)
		case reflect.Map:
			ok, msg = deepMapMatch(el1, el2)
		}

		if !ok {
			return false, msg
		}
	}

	return true, ""
}

func deepSequenceMatch(container1, container2 reflect.Value) (bool, string) {
	if valid, output := validateContainerTypesAndLengths(container1, container2); !valid {
		return false, output
	}

	matched := make([]bool, container1.Len())
out:
	for i := 0; i < container1.Len(); i++ {
		el1 := container1.Index(i).Interface()

		for e := 0; e < container2.Len(); e++ {
			el2 := container2.Index(e).Interface()

			if !matched[e] && reflect.DeepEqual(el1, el2) {
				// mark already matched elements, so that duplicate elements in
				// one container can't be matched to the same element in the other.
				matched[e] = true
				continue out
			}
		}

		return false, fmt.Sprintf("element [%d]=%s was unmatched in the second container", i, el1)
	}

	return true, ""
}

func validateContainerTypesAndLengths(container1, container2 reflect.Value) (bool, string) {
	if container1.Len() != container2.Len() {
		return false, fmt.Sprintf("containers have different lengths: %d != %d", container1.Len(), container2.Len())
	} else if container1.Type().Elem() != container2.Type().Elem() {
		return false, fmt.Sprintf("containers have different element types: %s != %s", container1.Type().Elem(), container2.Type().Elem())
	}

	if container1.Kind() == reflect.Map && container2.Kind() == reflect.Map {
		keyType1, keyType2 := container1.Type().Key(), container2.Type().Key()
		if keyType1 != keyType2 {
			return false, fmt.Sprintf("maps have different key types: %s != %s", keyType1, keyType2)
		}
	}

	return true, ""
}
