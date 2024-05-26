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
	"errors"
	"reflect"

	. "gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/metautil"
	"github.com/snapcore/snapd/testutil"
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
		{[]interface{}{[]string{"one"}, []string{"two"}}, [][]string{{"one"}, {"two"}}},
		{[]interface{}{map[string]int{"one": 1}, map[string]int{"two": 2}}, []map[string]int{{"one": 1}, {"two": 2}}},
		{map[interface{}]interface{}{"one": 1, "two": 2}, map[string]int{"one": 1, "two": 2}},
	}

	for _, testData := range data {
		inputValue := reflect.ValueOf(testData.inputValue)
		outputType := reflect.TypeOf(testData.expectedValue)
		expectedValue := testData.expectedValue
		outputValue := mylog.Check2(metautil.ConvertValue(inputValue, outputType))
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
		{map[interface{}]interface{}{"one": 1}, t(map[int]int{}), `cannot convert value "one" into a int`},
		{map[interface{}]interface{}{1: 2}, t(map[int]string{}), `cannot convert value "2" into a string`},
		{map[interface{}]interface{}{"one": 1}, t([]string{}), `cannot convert value "map\[one:1\]" into a \[\]string`},
	}

	for _, testData := range data {
		inputValue := reflect.ValueOf(testData.inputValue)
		outputType := testData.outputType
		expectedError := testData.expectedError
		outputValue := mylog.Check2(metautil.ConvertValue(inputValue, outputType))
		testTag := Commentf("%v -> %T", inputValue, outputType)
		c.Check(err, ErrorMatches, expectedError, testTag)
		c.Check(outputValue.IsValid(), Equals, false, testTag)
	}
}

func (s *conversionssSuite) TestSetValueFromAttributeHappy(c *C) {
	interfaceArray := []interface{}{12, -3}
	var outputValue []int
	mylog.Check(metautil.SetValueFromAttribute("snap0", "iface0", "attr0", interfaceArray, &outputValue))

	c.Check(outputValue, DeepEquals, []int{12, -3})
}

func (s *conversionssSuite) TestSetValueFromAttributeUnhappy(c *C) {
	var outputBool bool
	data := []struct {
		snapName      string
		ifaceName     string
		attrName      string
		inputValue    interface{}
		outputValue   interface{}
		expectedError string
	}{
		// error if output value parameter is not a pointer
		{
			"snap1",
			"iface1",
			"attr1",
			"input value",
			"I'm not a pointer",
			`internal error: cannot get "attr1" attribute of interface "iface1" with non-pointer value`,
		},

		// error if value cannot be converted
		{
			"snap2",
			"iface2",
			"attr2",
			"input value",
			&outputBool,
			`snap "snap2" has interface "iface2" with invalid value type string for "attr2" attribute: \*bool`,
		},
	}

	for _, td := range data {
		mylog.Check(metautil.SetValueFromAttribute(td.snapName, td.ifaceName, td.attrName, td.inputValue, td.outputValue))
		c.Check(err, ErrorMatches, td.expectedError, Commentf("input value %v", td.inputValue))
	}
}

func (s *conversionssSuite) TestAttributeNotCompatibleIsTypeCheck(c *C) {
	c.Assert(metautil.AttributeNotCompatibleError{}, testutil.ErrorIs, metautil.AttributeNotCompatibleError{})
	c.Assert(metautil.AttributeNotCompatibleError{}, Not(testutil.ErrorIs), errors.New(""))
}
