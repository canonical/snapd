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
	"io/ioutil"
	"path/filepath"
	"reflect"
	"regexp"
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
