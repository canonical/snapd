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
	"runtime"

	"gopkg.in/check.v1"

	. "github.com/snapcore/snapd/testutil"
)

type containsCheckerSuite struct{}

var _ = check.Suite(&containsCheckerSuite{})

func (*containsCheckerSuite) TestUnsupportedTypes(c *check.C) {
	testInfo(c, Contains, "Contains", []string{"container", "elem"})
	testCheck(c, Contains, false, "int is not a supported container", 5, nil)
	testCheck(c, Contains, false, "bool is not a supported container", false, nil)
	testCheck(c, Contains, false, "element is a int but expected a string", "container", 1)
}

func (*containsCheckerSuite) TestContainsVerifiesTypes(c *check.C) {
	testInfo(c, Contains, "Contains", []string{"container", "elem"})
	testCheck(c, Contains,
		false, "container has items of type int but expected element is a string",
		[...]int{1, 2, 3}, "foo")
	testCheck(c, Contains,
		false, "container has items of type int but expected element is a string",
		[]int{1, 2, 3}, "foo")
	// This looks tricky, Contains looks at _values_, not at keys
	testCheck(c, Contains,
		false, "container has items of type int but expected element is a string",
		map[string]int{"foo": 1, "bar": 2}, "foo")
	testCheck(c, Contains,
		false, "container has items of type int but expected element is a string",
		map[string]int{"foo": 1, "bar": 2}, "foo")
}

type animal interface {
	Sound() string
}

type dog struct{}

func (d *dog) Sound() string {
	return "bark"
}

type cat struct{}

func (c *cat) Sound() string {
	return "meow"
}

type tree struct{}

func (*containsCheckerSuite) TestContainsVerifiesInterfaceTypes(c *check.C) {
	testCheck(c, Contains,
		false, "container has items of interface type testutil_test.animal but expected element does not implement it",
		[...]animal{&dog{}, &cat{}}, &tree{})
	testCheck(c, Contains,
		false, "container has items of interface type testutil_test.animal but expected element does not implement it",
		[]animal{&dog{}, &cat{}}, &tree{})
	testCheck(c, Contains,
		false, "container has items of interface type testutil_test.animal but expected element does not implement it",
		map[string]animal{"dog": &dog{}, "cat": &cat{}}, &tree{})
}

func (*containsCheckerSuite) TestContainsString(c *check.C) {
	c.Assert("foo", Contains, "f")
	c.Assert("foo", Contains, "fo")
	c.Assert("foo", check.Not(Contains), "foobar")
}

type myString string

func (*containsCheckerSuite) TestContainsCustomString(c *check.C) {
	c.Assert(myString("foo"), Contains, myString("f"))
	c.Assert(myString("foo"), Contains, myString("fo"))
	c.Assert(myString("foo"), check.Not(Contains), myString("foobar"))
	c.Assert("foo", Contains, myString("f"))
	c.Assert("foo", Contains, myString("fo"))
	c.Assert("foo", check.Not(Contains), myString("foobar"))
	c.Assert(myString("foo"), Contains, "f")
	c.Assert(myString("foo"), Contains, "fo")
	c.Assert(myString("foo"), check.Not(Contains), "foobar")
}

func (*containsCheckerSuite) TestContainsArray(c *check.C) {
	c.Assert([...]int{1, 2, 3}, Contains, 1)
	c.Assert([...]int{1, 2, 3}, Contains, 2)
	c.Assert([...]int{1, 2, 3}, Contains, 3)
	c.Assert([...]int{1, 2, 3}, check.Not(Contains), 4)
	c.Assert([...]animal{&dog{}, &cat{}}, Contains, &dog{})
	c.Assert([...]animal{&cat{}}, check.Not(Contains), &dog{})
}

func (*containsCheckerSuite) TestContainsSlice(c *check.C) {
	c.Assert([]int{1, 2, 3}, Contains, 1)
	c.Assert([]int{1, 2, 3}, Contains, 2)
	c.Assert([]int{1, 2, 3}, Contains, 3)
	c.Assert([]int{1, 2, 3}, check.Not(Contains), 4)
	c.Assert([]animal{&dog{}, &cat{}}, Contains, &dog{})
	c.Assert([]animal{&cat{}}, check.Not(Contains), &dog{})
}

func (*containsCheckerSuite) TestContainsMap(c *check.C) {
	c.Assert(map[string]int{"foo": 1, "bar": 2}, Contains, 1)
	c.Assert(map[string]int{"foo": 1, "bar": 2}, Contains, 2)
	c.Assert(map[string]int{"foo": 1, "bar": 2}, check.Not(Contains), 3)
	c.Assert(map[string]animal{"dog": &dog{}, "cat": &cat{}}, Contains, &dog{})
	c.Assert(map[string]animal{"cat": &cat{}}, check.Not(Contains), &dog{})
}

// Arbitrary type that is not comparable
type myStruct struct {
	attrs map[string]string
}

func (*containsCheckerSuite) TestContainsUncomparableType(c *check.C) {
	if runtime.Compiler != "gc" {
		c.Skip("this test only works on go (not gccgo)")
	}

	elem := myStruct{map[string]string{"k": "v"}}
	containerArray := [...]myStruct{elem}
	containerSlice := []myStruct{elem}
	containerMap := map[string]myStruct{"foo": elem}
	errMsg := "runtime error: comparing uncomparable type testutil_test.myStruct"
	testInfo(c, Contains, "Contains", []string{"container", "elem"})
	testCheck(c, Contains, false, errMsg, containerArray, elem)
	testCheck(c, Contains, false, errMsg, containerSlice, elem)
	testCheck(c, Contains, false, errMsg, containerMap, elem)
}

func (*containsCheckerSuite) TestDeepContainsUnsupportedTypes(c *check.C) {
	testInfo(c, DeepContains, "DeepContains", []string{"container", "elem"})
	testCheck(c, DeepContains, false, "int is not a supported container", 5, nil)
	testCheck(c, DeepContains, false, "bool is not a supported container", false, nil)
	testCheck(c, DeepContains, false, "element is a int but expected a string", "container", 1)
}

func (*containsCheckerSuite) TestDeepContainsVerifiesTypes(c *check.C) {
	testInfo(c, DeepContains, "DeepContains", []string{"container", "elem"})
	testCheck(c, DeepContains,
		false, "container has items of type int but expected element is a string",
		[...]int{1, 2, 3}, "foo")
	testCheck(c, DeepContains,
		false, "container has items of type int but expected element is a string",
		[]int{1, 2, 3}, "foo")
	// This looks tricky, DeepContains looks at _values_, not at keys
	testCheck(c, DeepContains,
		false, "container has items of type int but expected element is a string",
		map[string]int{"foo": 1, "bar": 2}, "foo")
}

func (*containsCheckerSuite) TestDeepContainsString(c *check.C) {
	c.Assert("foo", DeepContains, "f")
	c.Assert("foo", DeepContains, "fo")
	c.Assert("foo", check.Not(DeepContains), "foobar")
}

func (*containsCheckerSuite) TestDeepContainsCustomString(c *check.C) {
	c.Assert(myString("foo"), DeepContains, myString("f"))
	c.Assert(myString("foo"), DeepContains, myString("fo"))
	c.Assert(myString("foo"), check.Not(DeepContains), myString("foobar"))
	c.Assert("foo", DeepContains, myString("f"))
	c.Assert("foo", DeepContains, myString("fo"))
	c.Assert("foo", check.Not(DeepContains), myString("foobar"))
	c.Assert(myString("foo"), DeepContains, "f")
	c.Assert(myString("foo"), DeepContains, "fo")
	c.Assert(myString("foo"), check.Not(DeepContains), "foobar")
}

func (*containsCheckerSuite) TestDeepContainsArray(c *check.C) {
	c.Assert([...]int{1, 2, 3}, DeepContains, 1)
	c.Assert([...]int{1, 2, 3}, DeepContains, 2)
	c.Assert([...]int{1, 2, 3}, DeepContains, 3)
	c.Assert([...]int{1, 2, 3}, check.Not(DeepContains), 4)
}

func (*containsCheckerSuite) TestDeepContainsSlice(c *check.C) {
	c.Assert([]int{1, 2, 3}, DeepContains, 1)
	c.Assert([]int{1, 2, 3}, DeepContains, 2)
	c.Assert([]int{1, 2, 3}, DeepContains, 3)
	c.Assert([]int{1, 2, 3}, check.Not(DeepContains), 4)
}

func (*containsCheckerSuite) TestDeepContainsMap(c *check.C) {
	c.Assert(map[string]int{"foo": 1, "bar": 2}, DeepContains, 1)
	c.Assert(map[string]int{"foo": 1, "bar": 2}, DeepContains, 2)
	c.Assert(map[string]int{"foo": 1, "bar": 2}, check.Not(DeepContains), 3)
}

func (*containsCheckerSuite) TestDeepContainsUncomparableType(c *check.C) {
	elem := myStruct{map[string]string{"k": "v"}}
	containerArray := [...]myStruct{elem}
	containerSlice := []myStruct{elem}
	containerMap := map[string]myStruct{"foo": elem}
	testInfo(c, DeepContains, "DeepContains", []string{"container", "elem"})
	testCheck(c, DeepContains, true, "", containerArray, elem)
	testCheck(c, DeepContains, true, "", containerSlice, elem)
	testCheck(c, DeepContains, true, "", containerMap, elem)
}
