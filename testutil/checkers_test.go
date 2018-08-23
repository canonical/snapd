// -*- Mode: Go; indent-tabs-mode: t -*-
//
// 20160229: The tests with gccgo on powerpc fail for this file
//           and it will loop endlessly. This is not reproducible
//           with gccgo on amd64. Given that it's a relatively little
//           used arch we disable the tests in here to workaround this
//           gccgo bug.
// +build !ppc

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
	"errors"
	"io/ioutil"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"testing"

	"gopkg.in/check.v1"
)

func Test(t *testing.T) {
	check.TestingT(t)
}

type CheckersS struct{}

var _ = check.Suite(&CheckersS{})

func testInfo(c *check.C, checker check.Checker, name string, paramNames []string) {
	info := checker.Info()
	if info.Name != name {
		c.Fatalf("Got name %s, expected %s", info.Name, name)
	}
	if !reflect.DeepEqual(info.Params, paramNames) {
		c.Fatalf("Got param names %#v, expected %#v", info.Params, paramNames)
	}
}

func testCheck(c *check.C, checker check.Checker, result bool, error string, params ...interface{}) ([]interface{}, []string) {
	info := checker.Info()
	if len(params) != len(info.Params) {
		c.Fatalf("unexpected param count in test; expected %d got %d", len(info.Params), len(params))
	}
	names := append([]string{}, info.Params...)
	resultActual, errorActual := checker.Check(params, names)
	if resultActual != result || errorActual != error {
		c.Fatalf("%s.Check(%#v) returned (%#v, %#v) rather than (%#v, %#v)",
			info.Name, params, resultActual, errorActual, result, error)
	}
	return params, names
}

func (s *CheckersS) TestUnsupportedTypes(c *check.C) {
	testInfo(c, Contains, "Contains", []string{"container", "elem"})
	testCheck(c, Contains, false, "int is not a supported container", 5, nil)
	testCheck(c, Contains, false, "bool is not a supported container", false, nil)
	testCheck(c, Contains, false, "element is a int but expected a string", "container", 1)
}

func (s *CheckersS) TestContainsVerifiesTypes(c *check.C) {
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

func (s *CheckersS) TestContainsVerifiesInterfaceTypes(c *check.C) {
	testCheck(c, Contains,
		false, "container has items of interface type testutil.animal but expected element does not implement it",
		[...]animal{&dog{}, &cat{}}, &tree{})
	testCheck(c, Contains,
		false, "container has items of interface type testutil.animal but expected element does not implement it",
		[]animal{&dog{}, &cat{}}, &tree{})
	testCheck(c, Contains,
		false, "container has items of interface type testutil.animal but expected element does not implement it",
		map[string]animal{"dog": &dog{}, "cat": &cat{}}, &tree{})
}

func (s *CheckersS) TestContainsString(c *check.C) {
	c.Assert("foo", Contains, "f")
	c.Assert("foo", Contains, "fo")
	c.Assert("foo", check.Not(Contains), "foobar")
}

type myString string

func (s *CheckersS) TestContainsCustomString(c *check.C) {
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

func (s *CheckersS) TestContainsArray(c *check.C) {
	c.Assert([...]int{1, 2, 3}, Contains, 1)
	c.Assert([...]int{1, 2, 3}, Contains, 2)
	c.Assert([...]int{1, 2, 3}, Contains, 3)
	c.Assert([...]int{1, 2, 3}, check.Not(Contains), 4)
	c.Assert([...]animal{&dog{}, &cat{}}, Contains, &dog{})
	c.Assert([...]animal{&cat{}}, check.Not(Contains), &dog{})
}

func (s *CheckersS) TestContainsSlice(c *check.C) {
	c.Assert([]int{1, 2, 3}, Contains, 1)
	c.Assert([]int{1, 2, 3}, Contains, 2)
	c.Assert([]int{1, 2, 3}, Contains, 3)
	c.Assert([]int{1, 2, 3}, check.Not(Contains), 4)
	c.Assert([]animal{&dog{}, &cat{}}, Contains, &dog{})
	c.Assert([]animal{&cat{}}, check.Not(Contains), &dog{})
}

func (s *CheckersS) TestContainsMap(c *check.C) {
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

func (s *CheckersS) TestContainsUncomparableType(c *check.C) {
	if runtime.Compiler != "go" {
		c.Skip("this test only works on go (not gccgo)")
	}

	elem := myStruct{map[string]string{"k": "v"}}
	containerArray := [...]myStruct{elem}
	containerSlice := []myStruct{elem}
	containerMap := map[string]myStruct{"foo": elem}
	errMsg := "runtime error: comparing uncomparable type testutil.myStruct"
	testInfo(c, Contains, "Contains", []string{"container", "elem"})
	testCheck(c, Contains, false, errMsg, containerArray, elem)
	testCheck(c, Contains, false, errMsg, containerSlice, elem)
	testCheck(c, Contains, false, errMsg, containerMap, elem)
}

func (s *CheckersS) TestDeepContainsUnsupportedTypes(c *check.C) {
	testInfo(c, DeepContains, "DeepContains", []string{"container", "elem"})
	testCheck(c, DeepContains, false, "int is not a supported container", 5, nil)
	testCheck(c, DeepContains, false, "bool is not a supported container", false, nil)
	testCheck(c, DeepContains, false, "element is a int but expected a string", "container", 1)
}

func (s *CheckersS) TestDeepContainsVerifiesTypes(c *check.C) {
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

func (s *CheckersS) TestDeepContainsString(c *check.C) {
	c.Assert("foo", DeepContains, "f")
	c.Assert("foo", DeepContains, "fo")
	c.Assert("foo", check.Not(DeepContains), "foobar")
}

func (s *CheckersS) TestDeepContainsCustomString(c *check.C) {
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

func (s *CheckersS) TestDeepContainsArray(c *check.C) {
	c.Assert([...]int{1, 2, 3}, DeepContains, 1)
	c.Assert([...]int{1, 2, 3}, DeepContains, 2)
	c.Assert([...]int{1, 2, 3}, DeepContains, 3)
	c.Assert([...]int{1, 2, 3}, check.Not(DeepContains), 4)
}

func (s *CheckersS) TestDeepContainsSlice(c *check.C) {
	c.Assert([]int{1, 2, 3}, DeepContains, 1)
	c.Assert([]int{1, 2, 3}, DeepContains, 2)
	c.Assert([]int{1, 2, 3}, DeepContains, 3)
	c.Assert([]int{1, 2, 3}, check.Not(DeepContains), 4)
}

func (s *CheckersS) TestDeepContainsMap(c *check.C) {
	c.Assert(map[string]int{"foo": 1, "bar": 2}, DeepContains, 1)
	c.Assert(map[string]int{"foo": 1, "bar": 2}, DeepContains, 2)
	c.Assert(map[string]int{"foo": 1, "bar": 2}, check.Not(DeepContains), 3)
}

func (s *CheckersS) TestDeepContainsUncomparableType(c *check.C) {
	elem := myStruct{map[string]string{"k": "v"}}
	containerArray := [...]myStruct{elem}
	containerSlice := []myStruct{elem}
	containerMap := map[string]myStruct{"foo": elem}
	testInfo(c, DeepContains, "DeepContains", []string{"container", "elem"})
	testCheck(c, DeepContains, true, "", containerArray, elem)
	testCheck(c, DeepContains, true, "", containerSlice, elem)
	testCheck(c, DeepContains, true, "", containerMap, elem)
}

type myStringer struct{ str string }

func (m myStringer) String() string { return m.str }

func (s *CheckersS) TestFileEquals(c *check.C) {
	d := c.MkDir()
	content := "not-so-random-string"
	filename := filepath.Join(d, "canary")
	c.Assert(ioutil.WriteFile(filename, []byte(content), 0644), check.IsNil)

	testInfo(c, FileEquals, "FileEquals", []string{"filename", "contents"})
	testCheck(c, FileEquals, true, "", filename, content)
	testCheck(c, FileEquals, true, "", filename, []byte(content))
	testCheck(c, FileEquals, true, "", filename, myStringer{content})

	twofer := content + content
	testCheck(c, FileEquals, false, "Failed to match with file contents:\nnot-so-random-string", filename, twofer)
	testCheck(c, FileEquals, false, "Failed to match with file contents:\n<binary data>", filename, []byte(twofer))
	testCheck(c, FileEquals, false, "Failed to match with file contents:\nnot-so-random-string", filename, myStringer{twofer})

	testCheck(c, FileEquals, false, `Cannot read file "": open : no such file or directory`, "", "")
	testCheck(c, FileEquals, false, "Filename must be a string", 42, "")
	testCheck(c, FileEquals, false, "Cannot compare file contents with something of type int", filename, 1)
}

func (s *CheckersS) TestFileContains(c *check.C) {
	d := c.MkDir()
	content := "not-so-random-string"
	filename := filepath.Join(d, "canary")
	c.Assert(ioutil.WriteFile(filename, []byte(content), 0644), check.IsNil)

	testInfo(c, FileContains, "FileContains", []string{"filename", "contents"})
	testCheck(c, FileContains, true, "", filename, content[1:])
	testCheck(c, FileContains, true, "", filename, []byte(content[1:]))
	testCheck(c, FileContains, true, "", filename, myStringer{content[1:]})
	// undocumented
	testCheck(c, FileContains, true, "", filename, regexp.MustCompile(".*"))

	twofer := content + content
	testCheck(c, FileContains, false, "Failed to match with file contents:\nnot-so-random-string", filename, twofer)
	testCheck(c, FileContains, false, "Failed to match with file contents:\n<binary data>", filename, []byte(twofer))
	testCheck(c, FileContains, false, "Failed to match with file contents:\nnot-so-random-string", filename, myStringer{twofer})
	// undocumented
	testCheck(c, FileContains, false, "Failed to match with file contents:\nnot-so-random-string", filename, regexp.MustCompile("^$"))

	testCheck(c, FileContains, false, `Cannot read file "": open : no such file or directory`, "", "")
	testCheck(c, FileContains, false, "Filename must be a string", 42, "")
	testCheck(c, FileContains, false, "Cannot compare file contents with something of type int", filename, 1)
}

func (s *CheckersS) TestFileMatches(c *check.C) {
	d := c.MkDir()
	content := "not-so-random-string"
	filename := filepath.Join(d, "canary")
	c.Assert(ioutil.WriteFile(filename, []byte(content), 0644), check.IsNil)

	testInfo(c, FileMatches, "FileMatches", []string{"filename", "regex"})
	testCheck(c, FileMatches, true, "", filename, ".*")
	testCheck(c, FileMatches, true, "", filename, "^"+regexp.QuoteMeta(content)+"$")

	testCheck(c, FileMatches, false, "Failed to match with file contents:\nnot-so-random-string", filename, "^$")
	testCheck(c, FileMatches, false, "Failed to match with file contents:\nnot-so-random-string", filename, "123"+regexp.QuoteMeta(content))

	testCheck(c, FileMatches, false, `Cannot read file "": open : no such file or directory`, "", "")
	testCheck(c, FileMatches, false, "Filename must be a string", 42, ".*")
	testCheck(c, FileMatches, false, "Regex must be a string", filename, 1)
}

func (s *CheckersS) TestSystemCallSequenceEqual(c *check.C) {
	c.Assert([]CallResultError{}, SyscallsEqual, []CallResultError{})
	c.Assert([]CallResultError{}, SyscallsEqual, []CallResultError(nil))
	c.Assert([]CallResultError{{C: `foo`}}, SyscallsEqual, []CallResultError{{C: `foo`}})
	c.Assert([]CallResultError{{C: `foo`}, {C: `bar`}}, SyscallsEqual, []CallResultError{{C: `foo`}, {C: `bar`}})
	c.Assert([]CallResultError{{C: `foo`, R: 123}}, SyscallsEqual, []CallResultError{{C: `foo`, R: 123}})
	c.Assert([]CallResultError{{C: `foo`, E: errors.New("bad")}}, SyscallsEqual, []CallResultError{{C: `foo`, E: errors.New("bad")}})

	// Wrong argument types.
	testCheck(c, SyscallsEqual, false, "left-hand-side argument must be of type []CallResultError",
		true, []CallResultError{{C: `bar`}})
	testCheck(c, SyscallsEqual, false, "right-hand-side argument must be of type []CallResultError",
		[]CallResultError{{C: `bar`}}, true)
	// Different system call operations.
	testCheck(c, SyscallsEqual, false, "system call #0 differs in operation, actual `foo`, expected `bar`",
		[]CallResultError{{C: `foo`}}, []CallResultError{{C: `bar`}})
	// Different system call results.
	testCheck(c, SyscallsEqual, false, "system call #0 `foo` differs in result, actual: 1, expected: 2",
		[]CallResultError{{C: `foo`, R: 1}}, []CallResultError{{C: `foo`, R: 2}})
	// Different system call errors.
	testCheck(c, SyscallsEqual, false, "system call #0 `foo` differs in error, actual: barf, expected: bork",
		[]CallResultError{{C: `foo`, E: errors.New("barf")}}, []CallResultError{{C: `foo`, E: errors.New("bork")}})
	// Unexpected success with non-nil result.
	testCheck(c, SyscallsEqual, false, "system call #0 `foo` unexpectedly succeeded, actual result: 1, expected error: broken",
		[]CallResultError{{C: `foo`, R: 1}}, []CallResultError{{C: `foo`, E: errors.New("broken")}})
	// Unexpected success with nil result.
	testCheck(c, SyscallsEqual, false, "system call #0 `foo` unexpectedly succeeded, expected error: broken",
		[]CallResultError{{C: `foo`}}, []CallResultError{{C: `foo`, E: errors.New("broken")}})
	// Unexpected failure with expected non-nil result.
	testCheck(c, SyscallsEqual, false, "system call #0 `foo` unexpectedly failed, actual error: broken, expected result: 1",
		[]CallResultError{{C: `foo`, E: errors.New("broken")}}, []CallResultError{{C: `foo`, R: 1}})
	// Unexpected failure with expected nil result.
	testCheck(c, SyscallsEqual, false, "system call #0 `foo` unexpectedly failed, actual error: broken",
		[]CallResultError{{C: `foo`, E: errors.New("broken")}}, []CallResultError{{C: `foo`}})
	// More system calls than expected.
	testCheck(c, SyscallsEqual, false, "system call #1 `bar` unexpectedly present, got 2 system call(s) but expected only 1",
		[]CallResultError{{C: `foo`}, {C: `bar`}}, []CallResultError{{C: `foo`}})
	testCheck(c, SyscallsEqual, false, "system call #0 `foo` unexpectedly present, got 2 system call(s) but expected only 0",
		[]CallResultError{{C: `foo`}, {C: `bar`}}, []CallResultError{})
	// Fewer system calls than expected.
	testCheck(c, SyscallsEqual, false, "system call #1 `bar` unexpectedly absent, got only 1 system call(s) but expected 2",
		[]CallResultError{{C: `foo`}}, []CallResultError{{C: `foo`}, {C: `bar`}})
	testCheck(c, SyscallsEqual, false, "system call #0 `foo` unexpectedly absent, got only 0 system call(s) but expected 1",
		[]CallResultError{}, []CallResultError{{C: `foo`}})
}
