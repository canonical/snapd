// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2021 Canonical Ltd
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

package utils_test

import (
	"regexp"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/interfaces/utils"
)

type pathPatternsSuite struct{}

var _ = Suite(&pathPatternsSuite{})

func (s *pathPatternsSuite) TestRegexCreationHappy(c *C) {
	data := []struct {
		pattern       string
		expectedRegex string
	}{
		{`/media/user/`, `^/media/user/$`},
		{`/dev/sd*`, `^/dev/sd[^/]*$`},
		{`/dev/sd?`, `^/dev/sd[^/]$`},
		{`/etc/**`, `^/etc/[^/].*$`},
		{`/home/*/.bashrc`, `^/home/[^/][^/]*/\.bashrc$`},
		{`/media/{user,loser}/`, `^/media/(user|loser)/$`},
		{`/nested/{a,b{c,d}}/`, `^/nested/(a|b(c|d))/$`},
		{`/media/\{in-braces\}/`, `^/media/\{in-braces\}/$`},
		{`/media/\[in-brackets\]/`, `^/media/\[in-brackets\]/$`},
		{`/dev/sd[abc][0-9]`, `^/dev/sd[abc][0-9]$`},
		{`/comma/not/in/group/a,b`, `^/comma/not/in/group/a,b$`},
		{`/quoted/bracket/[ab\]c]`, `^/quoted/bracket/[ab\]c]$`},
	}

	for _, testData := range data {
		pattern := testData.pattern
		expectedRegex := testData.expectedRegex
		regex, err := utils.CreateRegex(pattern)
		c.Assert(err, IsNil, Commentf("%s", pattern))
		c.Assert(regex, Equals, expectedRegex, Commentf("%s", pattern))
		// Also, make sure that the obtained regex is valid
		_, err = regexp.Compile(regex)
		c.Assert(err, IsNil, Commentf("%s", pattern))
	}
}

func (s *pathPatternsSuite) TestRegexCreationUnhappy(c *C) {
	data := []struct {
		pattern       string
		expectedError string
	}{
		{`/media/{}/`, `Invalid number of items between {}:.*`},
		{`/media/{some/things`, `Missing 1 closing brace\(s\):.*`},
		{`/media/}`, `Invalid closing brace, no matching open { found:.*`},
		{`/media/[abc`, `Missing closing bracket ']':.*`},
		{`/media/]`, `Pattern contains unmatching ']':.*`},
		{`/media\`, `Expected character after '\\':.*`},
		// 123456789012345678901234567890123456789012345678901, 51 of them
		{`/{{{{{{{{{{{{{{{{{{{{{{{{{{{{{{{{{{{{{{{{{{{{{{{{{{{`, `Maximum group depth exceeded:.*`},
	}

	for _, testData := range data {
		pattern := testData.pattern
		expectedError := testData.expectedError
		regex, err := utils.CreateRegex(pattern)
		c.Assert(regex, Equals, "", Commentf("%s", pattern))
		c.Assert(err, ErrorMatches, expectedError, Commentf("%s", pattern))
	}
}
