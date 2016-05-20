// -*- Mode: Go; indent-tabs-mode: t -*-
// +build !excludeintegration

/*
 * Copyright (C) 2015, 2016 Canonical Ltd
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

package tests

import (
	"fmt"

	"github.com/ubuntu-core/snappy/integration-tests/testutils/cli"
	"github.com/ubuntu-core/snappy/integration-tests/testutils/common"

	"gopkg.in/check.v1"
)

var _ = check.Suite(&searchSuite{})

type searchSuite struct {
	common.SnappySuite
}

func (s *searchSuite) TestSearchMustPrintMatch(c *check.C) {
	// XXX: Summary is empty atm, waiting for store support
	expected := "(?ms)" +
		"Name +Version +(Price +)?Summary *\n" +
		".*" +
		"^hello-world +.* *\n" +
		".*"

	for _, searchTerm := range []string{"hello-", "hello-world"} {
		searchOutput := cli.ExecCommand(c, "snap", "find", searchTerm)

		c.Check(searchOutput, check.Matches, expected)
	}
}

// SNAP_FIND_001: list all packages available on the store
func (s *searchSuite) TestFindMustPrintCompleteList(c *check.C) {
	fullListPattern := "(?ms)" +
		"Name +Version +(Price +)?Summary *\n" +
		".*" +
		"^canonical-pc +.* *\n" +
		".*" +
		"^canonical-pc-linux +.* *\n" +
		".*" +
		"^go-example-webserver +.* *\n" +
		".*" +
		"^hello-world +.* *\n" +
		".*" +
		"^ubuntu-clock-app +.* *\n" +
		".*" +
		"^ubuntu-core +.* *\n" +
		".*"

	searchOutput := cli.ExecCommand(c, "snap", "find")

	c.Assert(searchOutput, check.Matches, fullListPattern)
}

// SNAP_FIND_002: find packages on store with different name formats
func (s *searchSuite) TestFindWorksWithDifferentFormats(c *check.C) {
	for _, snapName := range []string{"http", "ubuntu-clock-app", "go-example-webserver"} {
		expected := "(?ms)" +
			"Name +Version +(Price +)?Summary *\n" +
			".*" +
			"^" + snapName + " +.* *\n" +
			".*"
		searchOutput := cli.ExecCommand(c, "snap", "find", snapName)

		c.Check(searchOutput, check.Matches, expected)
	}
}

// SNAP_FIND_003: --help prints the detailed help test for the command
func (s *searchSuite) TestFindShowsHelp(c *check.C) {
	expected := "(?ms)" +
		"^Usage:\n" +
		`  snap \[OPTIONS\] find.*\n` +
		"\n^The find command .*\n" +
		"^Help Options:\n" +
		"^  -h, --help +Show this help message\n" +
		".*"

	actual := cli.ExecCommand(c, "snap", "find", "--help")

	c.Assert(actual, check.Matches, expected)
}

// SNAP_FIND_007: find packages with search string containing special characters
func (s *searchSuite) TestFindWithSpecialCharsInSearchString(c *check.C) {
	c.Skip("Reenable when LP: 1583952 is fixed")
	specialChars := "!@#$%^&*/=[]+_:;,.?{}"

	for _, char := range specialChars {
		s := string(char)
		expected := fmt.Sprintf(`error: no snaps found for "%s"`, s)
		searchOutput := cli.ExecCommand(c, "snap", "find", s)

		c.Check(searchOutput, check.Matches, expected)
	}
}
