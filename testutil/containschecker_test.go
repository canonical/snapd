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

type example struct {
	a string
	b map[string]int
}

func (*containsCheckerSuite) TestDeepUnsortedMatchesSliceSuccess(c *check.C) {
	slice1 := []example{
		{a: "one", b: map[string]int{"a": 1}},
		{a: "two", b: map[string]int{"b": 2}},
	}
	slice2 := []example{
		{a: "two", b: map[string]int{"b": 2}},
		{a: "one", b: map[string]int{"a": 1}},
	}

	c.Check(slice1, DeepUnsortedMatches, slice2)
	c.Check(slice2, DeepUnsortedMatches, slice1)
	c.Check([]string{"a", "a"}, DeepUnsortedMatches, []string{"a", "a"})
	c.Check([]string{"a", "b", "a"}, DeepUnsortedMatches, []string{"b", "a", "a"})
	slice := [1]int{1}
	c.Check(slice, DeepUnsortedMatches, slice)
}

func (*containsCheckerSuite) TestDeepUnsortedMatchesSliceFailure(c *check.C) {
	slice1 := []string{"a", "a", "b"}
	slice2 := []string{"b", "a", "c"}

	testCheck(c, DeepUnsortedMatches, false, "element [1]=a was unmatched in the second container", slice1, slice2)
	testCheck(c, DeepUnsortedMatches, false, "element [2]=c was unmatched in the second container", slice2, slice1)
}

func (*containsCheckerSuite) TestDeepUnsortedMatchesMapSuccess(c *check.C) {
	map1 := map[string]example{
		"a": {a: "a", b: map[string]int{"a": 1, "b": 2}},
		"c": {a: "c", b: map[string]int{"c": 3, "d": 4}},
	}
	map2 := map[string]example{
		"c": {a: "c", b: map[string]int{"c": 3, "d": 4}},
		"a": {a: "a", b: map[string]int{"a": 1, "b": 2}},
	}

	c.Check(map1, DeepUnsortedMatches, map2)
	c.Check(map2, DeepUnsortedMatches, map1)
}

func (*containsCheckerSuite) TestDeepUnsortedMatchesMapStructFail(c *check.C) {
	map1 := map[string]example{
		"a": {a: "a", b: map[string]int{"a": 2, "b": 1}},
	}
	map2 := map[string]example{
		"a": {a: "a", b: map[string]int{"a": 1, "b": 2}},
	}

	testCheck(c, DeepUnsortedMatches, false, "maps don't match", map1, map2)
}

func (*containsCheckerSuite) TestDeepUnsortedMatchesMapUnmatchedKeyFailure(c *check.C) {
	map1 := map[string]int{"a": 1, "c": 2}
	map2 := map[string]int{"a": 1, "b": 2}

	testCheck(c, DeepUnsortedMatches, false, "maps don't match", map1, map2)
	testCheck(c, DeepUnsortedMatches, false, "maps don't match", map2, map1)
}

func (*containsCheckerSuite) TestDeepUnsortedMatchesMapUnmatchedValueFailure(c *check.C) {
	map1 := map[string]int{"a": 1, "b": 2}
	map2 := map[string]int{"a": 1, "b": 3}

	testCheck(c, DeepUnsortedMatches, false, "maps don't match", map1, map2)
	testCheck(c, DeepUnsortedMatches, false, "maps don't match", map2, map1)
}

func (*containsCheckerSuite) TestDeepUnsortedMatchesDifferentTypeFailure(c *check.C) {
	testCheck(c, DeepUnsortedMatches, false, "containers are of different types: slice != array", []int{}, [1]int{})
}

func (*containsCheckerSuite) TestDeepUnsortedMatchesDifferentElementType(c *check.C) {
	testCheck(c, DeepUnsortedMatches, false, "containers have different element types: int != string", []int{1}, []string{"a"})
}

func (*containsCheckerSuite) TestDeepUnsortedMatchesDifferentLengthFailure(c *check.C) {
	testCheck(c, DeepUnsortedMatches, false, "containers have different lengths: 1 != 2", []int{1}, []int{1, 1})
}

func (*containsCheckerSuite) TestDeepUnsortedMatchesNilArgFailure(c *check.C) {
	testCheck(c, DeepUnsortedMatches, false, "only one container was nil", nil, []int{1})
}

func (*containsCheckerSuite) TestDeepUnsortedMatchesBothNilArgSuccess(c *check.C) {
	c.Check(nil, DeepUnsortedMatches, nil)
}

func (*containsCheckerSuite) TestDeepUnsortedMatchesNonContainerValues(c *check.C) {
	testCheck(c, DeepUnsortedMatches, false, "'string' is not a supported type: must be slice, array or map", "a", "a")
	testCheck(c, DeepUnsortedMatches, false, "'int' is not a supported type: must be slice, array or map", 1, 2)
	testCheck(c, DeepUnsortedMatches, false, "'bool' is not a supported type: must be slice, array or map", true, false)
	testCheck(c, DeepUnsortedMatches, false, "'ptr' is not a supported type: must be slice, array or map", &[]string{"a", "b"}, &[]string{"a", "b"})
	testCheck(c, DeepUnsortedMatches, false, "'func' is not a supported type: must be slice, array or map", func() {}, func() {})
}

func (*containsCheckerSuite) TestDeepUnsortedMatchesMapsOfSlices(c *check.C) {
	map1 := map[string][]string{"a": {"foo", "bar"}, "b": {"foo", "bar"}}
	map2 := map[string][]string{"a": {"bar", "foo"}, "b": {"bar", "foo"}}

	c.Check(map1, DeepUnsortedMatches, map2)
}

func (*containsCheckerSuite) TestDeepUnsortedMatchesMapsDifferentKeyTypes(c *check.C) {
	map1 := map[string][]string{"a": {"foo", "bar"}}
	map2 := map[int][]string{1: {"bar", "foo"}}

	testCheck(c, DeepUnsortedMatches, false, "maps have different key types: string != int", map1, map2)
}

func (*containsCheckerSuite) TestDeepUnsortedMatchesMapsDifferentValueTypes(c *check.C) {
	map1 := map[string][]string{"a": {"foo", "bar"}}
	map2 := map[string][2]string{"a": {"foo", "bar"}}

	testCheck(c, DeepUnsortedMatches, false, "containers have different element types: []string != [2]string", map1, map2)
}

func (*containsCheckerSuite) TestDeepUnsortedMatchesMapsDifferentLengths(c *check.C) {
	map1 := map[string][]string{"a": {"foo", "bar"}, "b": {"foo", "bar"}}
	map2 := map[string][]string{"a": {"bar", "foo"}}

	testCheck(c, DeepUnsortedMatches, false, "containers have different lengths: 2 != 1", map1, map2)
}

func (*containsCheckerSuite) TestDeepUnsortedMatchesMapsMissingKey(c *check.C) {
	map1 := map[string][]string{"a": {"foo", "bar"}}
	map2 := map[string][]string{"b": {"bar", "foo"}}

	testCheck(c, DeepUnsortedMatches, false, "key \"a\" from one map is absent from the other map", map1, map2)
}

func (*containsCheckerSuite) TestDeepUnsortedMatchesNestedMaps(c *check.C) {
	map1 := map[string]map[string][]string{"a": {"b": []string{"foo", "bar"}}}
	map2 := map[string]map[string][]string{"a": {"b": []string{"bar", "foo"}}}
	c.Check(map1, DeepUnsortedMatches, map2)

	map1 = map[string]map[string][]string{"a": {"b": []string{"foo", "bar"}}}
	map2 = map[string]map[string][]string{"a": {"c": []string{"bar", "foo"}}}
	testCheck(c, DeepUnsortedMatches, false, "key \"b\" from one map is absent from the other map", map1, map2)

	map1 = map[string]map[string][]string{"a": {"b": []string{"foo", "bar"}}, "c": {"b": []string{"foo"}}}
	map2 = map[string]map[string][]string{"a": {"b": []string{"bar", "foo"}}, "c": {"b": []string{"bar"}}}
	testCheck(c, DeepUnsortedMatches, false, "element [0]=foo was unmatched in the second container", map1, map2)
}
