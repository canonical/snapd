// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2022 Canonical Ltd
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

package metautil_test

import (
	"reflect"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/metautil"
)

type conversionssSuite struct{}

var _ = Suite(&conversionssSuite{})

func (s *conversionssSuite) TestConvertHappy(c *C) {
	data := []struct {
		inputValue    interface{}
		expectedValue interface{}
	}{
		// Basic types
		{"a string", "a string"},
		{42, 42},
		{true, true},

		// Complex types with no conversion
		{[]string{"one", "two"}, []string{"one", "two"}},
		{[]int{24, 42}, []int{24, 42}},

		// Complex types with conversion
		{[]interface{}{"one", "two"}, []string{"one", "two"}},
		{[]interface{}{24, 42}, []int{24, 42}},
	}

	for _, testData := range data {
		inputValue := reflect.ValueOf(testData.inputValue)
		outputType := reflect.TypeOf(testData.expectedValue)
		expectedValue := testData.expectedValue
		outputValue, err := metautil.ConvertValue(inputValue, outputType)
		testTag := Commentf("%v -> %v", inputValue, expectedValue)
		c.Check(err, IsNil, testTag)
		c.Check(outputValue.Interface(), DeepEquals, expectedValue, testTag)
	}
}

func (s *conversionssSuite) TestConvertUnhappy(c *C) {
	t := reflect.TypeOf
	data := []struct {
		inputValue    interface{}
		outputType    reflect.Type
		expectedError string
	}{
		// Basic types
		{"a string", t(42), `cannot convert value "a string" into a int`},
		{true, t(""), `cannot convert value "true" into a string`},

		// Complex types
		{[]interface{}{"one", "two", 3}, t([]string{}), `cannot convert value "3" into a string`},
		{[]interface{}{1, "two", 3}, t([]int{}), `cannot convert value "two" into a int`},
		{[]int{1, 2}, t([]string{}), `cannot convert value "1" into a string`},
		{[]int{1, 2}, t(1), `cannot convert value "\[1 2\]" into a int`},
	}

	for _, testData := range data {
		inputValue := reflect.ValueOf(testData.inputValue)
		outputType := testData.outputType
		expectedError := testData.expectedError
		outputValue, err := metautil.ConvertValue(inputValue, outputType)
		testTag := Commentf("%v -> %T", inputValue, outputType)
		c.Check(err, ErrorMatches, expectedError, testTag)
		c.Check(outputValue.IsValid(), Equals, false, testTag)
	}
}
