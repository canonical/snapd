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
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"gopkg.in/check.v1"

	. "github.com/snapcore/snapd/testutil"
)

type fileContentCheckerSuite struct{}

var _ = check.Suite(&fileContentCheckerSuite{})

type myStringer struct{ str string }

func (m myStringer) String() string { return m.str }

func (s *fileContentCheckerSuite) TestFileEquals(c *check.C) {
	d := c.MkDir()
	content := "not-so-random-string"
	filename := filepath.Join(d, "canary")
	c.Assert(os.WriteFile(filename, []byte(content), 0644), check.IsNil)
	equalRefereceFilename := filepath.Join(d, "canary-reference-equal")
	c.Assert(os.WriteFile(equalRefereceFilename, []byte(content), 0644), check.IsNil)
	notEqualRefereceFilename := filepath.Join(d, "canary-reference-not-equal")
	c.Assert(os.WriteFile(notEqualRefereceFilename, []byte("not-equal"), 0644), check.IsNil)

	testInfo(c, FileEquals, "FileEquals", []string{"filename", "contents"})
	testCheck(c, FileEquals, true, "", filename, content)
	testCheck(c, FileEquals, true, "", filename, []byte(content))
	testCheck(c, FileEquals, true, "", filename, myStringer{content})
	testCheck(c, FileEquals, true, "", filename, FileContentRef(filename))
	testCheck(c, FileEquals, true, "", filename, FileContentRef(equalRefereceFilename))

	twofer := content + content
	testCheck(c, FileEquals, false, "Failed to match with file contents:\nnot-so-random-string", filename, twofer)
	testCheck(c, FileEquals, false, "Failed to match with file contents:\n<binary data>", filename, []byte(twofer))
	testCheck(c, FileEquals, false, "Failed to match with file contents:\nnot-so-random-string", filename, myStringer{twofer})

	testCheck(c, FileEquals, false, `Cannot read file "": open : no such file or directory`, "", "")
	testCheck(c, FileEquals, false, "Filename must be a string", 42, "")
	testCheck(c, FileEquals, false, "Cannot compare file contents with something of type int", filename, 1)
	testCheck(c, FileEquals, false,
		fmt.Sprintf("Failed to match contents with reference file \"%s\":\nnot-so-random-string", notEqualRefereceFilename),
		filename, FileContentRef(notEqualRefereceFilename))
}

func (s *fileContentCheckerSuite) TestFileContains(c *check.C) {
	d := c.MkDir()
	content := "not-so-random-string"
	filename := filepath.Join(d, "canary")
	c.Assert(os.WriteFile(filename, []byte(content), 0644), check.IsNil)

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
	testCheck(c, FileContains, false, `Non-exact match with reference file is not supported`,
		filename, FileContentRef(filename))
}

func (s *fileContentCheckerSuite) TestFileMatches(c *check.C) {
	d := c.MkDir()
	content := "not-so-random-string"
	filename := filepath.Join(d, "canary")
	c.Assert(os.WriteFile(filename, []byte(content), 0644), check.IsNil)

	testInfo(c, FileMatches, "FileMatches", []string{"filename", "regex"})
	testCheck(c, FileMatches, true, "", filename, ".*")
	testCheck(c, FileMatches, true, "", filename, "^"+regexp.QuoteMeta(content)+"$")

	testCheck(c, FileMatches, false, "Failed to match with file contents:\nnot-so-random-string", filename, "^$")
	testCheck(c, FileMatches, false, "Failed to match with file contents:\nnot-so-random-string", filename, "123"+regexp.QuoteMeta(content))

	testCheck(c, FileMatches, false, `Cannot read file "": open : no such file or directory`, "", "")
	testCheck(c, FileMatches, false, "Filename must be a string", 42, ".*")
	testCheck(c, FileMatches, false, "Regex must be a string", filename, 1)
	testCheck(c, FileContains, false, `Non-exact match with reference file is not supported`,
		filename, FileContentRef(filename))
}
