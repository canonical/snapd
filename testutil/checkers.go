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
	"bytes"
	"fmt"
	"io/ioutil"
	"reflect"
	"regexp"
	"strings"

	"gopkg.in/check.v1"
)

type fileContentChecker struct {
	*check.CheckerInfo
	exact bool
}

// FileEquals verifies that the given file's content is equal
// to the string (or fmt.Stringer) or []byte provided.
var FileEquals check.Checker = &fileContentChecker{
	CheckerInfo: &check.CheckerInfo{Name: "FileEquals", Params: []string{"filename", "contents"}},
	exact:       true,
}

// FileContains verifies that the given file's content contains
// the string (or fmt.Stringer) or []byte provided.
var FileContains check.Checker = &fileContentChecker{
	CheckerInfo: &check.CheckerInfo{Name: "FileContains", Params: []string{"filename", "contents"}},
}

// FileMatches verifies that the given file's content matches
// the string provided.
var FileMatches check.Checker = &fileContentChecker{
	CheckerInfo: &check.CheckerInfo{Name: "FileMatches", Params: []string{"filename", "regex"}},
}

func (c *fileContentChecker) Check(params []interface{}, names []string) (result bool, error string) {
	filename, ok := params[0].(string)
	if !ok {
		return false, "Filename must be a string"
	}
	if names[1] == "regex" {
		regexpr, ok := params[1].(string)
		if !ok {
			return false, "Regex must be a string"
		}
		rx, err := regexp.Compile(regexpr)
		if err != nil {
			return false, fmt.Sprintf("Can't compile regexp %q: %v", regexpr, err)
		}
		params[1] = rx
	}
	return fileContentCheck(filename, params[1], c.exact)
}

func fileContentCheck(filename string, content interface{}, exact bool) (result bool, error string) {
	buf, err := ioutil.ReadFile(filename)
	if err != nil {
		return false, fmt.Sprintf("Can't read file %q: %v", filename, err)
	}
	if exact {
		switch content := content.(type) {
		case string:
			return string(buf) == content, ""
		case []byte:
			return bytes.Equal(buf, content), ""
		case fmt.Stringer:
			return string(buf) == content.String(), ""
		}
	} else {
		switch content := content.(type) {
		case string:
			return strings.Contains(string(buf), content), ""
		case []byte:
			return bytes.Contains(buf, content), ""
		case *regexp.Regexp:
			return content.Match(buf), ""
		case fmt.Stringer:
			return strings.Contains(string(buf), content.String()), ""
		}
	}
	return false, fmt.Sprintf("Can't compare file contents with something of type %T", content)
}

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
		return
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
	if commonEquals(container, elem, &result, &error) {
		return
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
