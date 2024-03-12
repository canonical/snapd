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
	// to save some typing:
	d := utils.GlobDefault
	n := utils.GlobNull

	data := []struct {
		pattern       string
		glob          utils.GlobFlags
		expectedRegex string
	}{
		{`/media/user/`, d, `^/media/user/$`},
		{`/dev/sd*`, d, `^/dev/sd[^/\x00]*$`},
		{`/dev/sd*`, n, `^/dev/sd[^/]*$`},
		{`/dev/sd?`, d, `^/dev/sd[^/\x00]$`},
		{`/dev/sd?`, n, `^/dev/sd[^/]$`},
		{`/etc/**`, d, `^/etc/[^/\x00][^\x00]*$`},
		{`/home/*/.bashrc`, d, `^/home/[^/\x00][^/\x00]*/\.bashrc$`},
		{`/home/*/.bashrc`, n, `^/home/[^/][^/]*/\.bashrc$`},
		{`/media/{user,loser}/`, d, `^/media/(user|loser)/$`},
		{`/nested/{a,b{c,d}}/`, d, `^/nested/(a|b(c|d))/$`},
		{`/media/\{in-braces\}/`, d, `^/media/\{in-braces\}/$`},
		{`/media/\[in-brackets\]/`, d, `^/media/\[in-brackets\]/$`},
		{`/dev/sd[abc][0-9]`, d, `^/dev/sd[abc][0-9]$`},
		{`/quoted/bracket/[ab\]c]`, d, `^/quoted/bracket/[ab\]c]$`},
		{`{[,],}`, d, `^([,]|)$`},
		{`/path/with/comma[,]`, d, `^/path/with/comma[,]$`},
		{`/path/with/commas,\,,`, d, `^/path/with/commas,,,$`},
		{`/path/with/comma,{and[,]group,with\,comma}`, d, `^/path/with/comma,(and[,]group|with,comma)$`},
		{`/$pecial/c^aracters`, d, `^/\$pecial/c\^aracters$`},
		{`/in/char/class[^$]`, d, `^/in/char/class[^$]$`},
	}

	for _, testData := range data {
		pattern := testData.pattern
		expectedRegex := testData.expectedRegex
		const allowCommas = true
		regex, err := utils.CreateRegex(pattern, testData.glob, allowCommas)
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
		{`/media/{}/`, `invalid number of items between {}:.*`},
		{`/media/{some/things`, `missing 1 closing brace\(s\):.*`},
		{`/media/}`, `invalid closing brace, no matching open { found:.*`},
		{`/media/[abc`, `missing closing bracket ']':.*`},
		{`/media/]`, `pattern contains unmatching ']':.*`},
		{`/media\`, `expected character after '\\':.*`},
		// 123456789012345678901234567890123456789012345678901, 51 of them
		{`/{{{{{{{{{{{{{{{{{{{{{{{{{{{{{{{{{{{{{{{{{{{{{{{{{{{`, `maximum group depth exceeded:.*`},
		{`/comma/not/in/group/a,b`, `cannot use ',' outside of group or character class`},
	}

	for _, testData := range data {
		pattern := testData.pattern
		expectedError := testData.expectedError
		const allowCommas = false
		pathPattern, err := utils.NewPathPattern(pattern, allowCommas)
		c.Assert(pathPattern, IsNil, Commentf("%s", pattern))
		c.Assert(err, ErrorMatches, expectedError, Commentf("%s", pattern))
	}
}

func (s *pathPatternsSuite) TestCreateRegexWithCommas(c *C) {
	data := []struct {
		pattern        string
		allowCommas    bool
		shouldError    bool
		expectedString string
	}{
		{`/dev/sd{a,b,c}`, false, false, `^/dev/sd(a|b|c)$`},
		{`/dev/sd{a,b,c}`, true, false, `^/dev/sd(a|b|c)$`},
		{`/dev/dma_heap/qcom,qseecom`, false, true, `cannot use ',' outside of group or character class`},
		{`/dev/dma_heap/qcom,qseecom`, true, false, `^/dev/dma_heap/qcom,qseecom$`},
		{`/path/with/comma[,]`, false, false, `^/path/with/comma[,]$`},
		{`/path/with/comma[,]`, true, false, `^/path/with/comma[,]$`},
		{`/path/with/commas,\,,`, false, true, `cannot use ',' outside of group or character class`},
		{`/path/with/commas,\,,`, true, false, `^/path/with/commas,,,$`},
		{`/path/with/comma,{and[,]group,with\,comma}`, false, true, `cannot use ',' outside of group or character class`},
		{`/path/with/comma,{and[,]group,with\,comma}`, true, false, `^/path/with/comma,(and[,]group|with,comma)$`},
		{`/path/with/empty{}group`, false, true, `invalid number of items between {}:.*`},
		{`/path/with/empty{}group`, true, true, `invalid number of items between {}:.*`},
	}

	for _, testData := range data {
		pattern := testData.pattern
		allowCommas := testData.allowCommas
		shouldError := testData.shouldError
		expectedString := testData.expectedString
		regex, err := utils.CreateRegex(pattern, utils.GlobDefault, allowCommas)
		if shouldError {
			c.Assert(regex, Equals, "", Commentf("%s", pattern))
			c.Assert(err, ErrorMatches, expectedString, Commentf("%s", pattern))
		} else {
			c.Assert(regex, Equals, expectedString, Commentf("%s", pattern))
			c.Assert(err, IsNil, Commentf("%s", pattern))
		}
	}
}

func (s *pathPatternsSuite) TestPatternMatches(c *C) {
	data := []struct {
		pattern       string
		testPath      string
		expectedMatch bool
	}{
		{`/same/path/`, `/same/path/`, true},
		{`/path/*`, `/path/here`, true},
		{`/path/*`, `/path/too/deep`, false},
		{`/path/**`, `/path/here`, true},
		{`/path/**`, `/path/here/too`, true},
		{`/dev/sd?`, `/dev/sda`, true},
		{`/dev/sd?`, `/dev/sdb1`, false},
		{`/media/{user,loser}/`, `/media/user/`, true},
		{`/media/{user,loser}/`, `/media/other/`, false},
		{`/nested/{a,b{c,d}}/`, `/nested/a/`, true},
		{`/nested/{a,b{c,d}}/`, `/nested/bd/`, true},
		{`/nested/{a,b{c,d}}/`, `/nested/ad/`, false},
		{`/dev/sd[abc][0-9]`, `/dev/sda0`, true},
		{`/dev/sd[abc][0-9]`, `/dev/sdb4`, true},
		{`/dev/sd[abc][0-9]`, `/dev/sda10`, false},
		{`/dev/sd[abc][0-9]`, `/dev/sdd0`, false},
	}

	for _, testData := range data {
		pattern := testData.pattern
		testPath := testData.testPath
		expectedMatch := testData.expectedMatch
		const allowCommas = false
		pathPattern, err := utils.NewPathPattern(pattern, allowCommas)
		c.Assert(err, IsNil, Commentf("%s", pattern))
		c.Assert(pathPattern.Matches(testPath), Equals, expectedMatch, Commentf("%s", pattern))
	}
}

func (s *pathPatternsSuite) TestPatternWithCommasMatches(c *C) {
	data := []struct {
		pattern       string
		testPath      string
		expectedMatch bool
	}{
		{`/dev/dma_heap/qcom,qseecom`, `/dev/dma_heap/qcom,qseecom`, true},
		{`/dev/dma_heap/qcom,[,]qseecom`, `/dev/dma_heap/qcom,qseecom`, false},
		{`/dev/dma_heap/qcom,[,]qseecom`, `/dev/dma_heap/qcom,,qseecom`, true},
		{`/dev/dma_heap/qcom,{,[,]}qseecom`, `/dev/dma_heap/qcom,qseecom`, true},
		{`/dev/dma_heap/qcom,{,[,]}qseecom`, `/dev/dma_heap/qcom,,qseecom`, true},
		{`/dev/dma_heap/qcom,{\,,}qseecom`, `/dev/dma_heap/qcom,qseecom`, true},
		{`/dev/dma_heap/qcom,{\,,}qseecom`, `/dev/dma_heap/qcom,,qseecom`, true},
		{`/dev/dma_heap/qcom,{\,,[,]}qseecom`, `/dev/dma_heap/qcom,,,qseecom`, false},
	}

	const commaError = `cannot use ',' outside of group or character class`

	for _, testData := range data {
		pattern := testData.pattern
		testPath := testData.testPath
		expectedMatch := testData.expectedMatch
		const allowCommasFalse = false
		pathPattern, err := utils.NewPathPattern(pattern, allowCommasFalse)
		c.Assert(pathPattern, IsNil, Commentf("%s", pattern))
		c.Assert(err, ErrorMatches, commaError, Commentf("%s", pattern))
		const allowCommasTrue = true
		pathPattern, err = utils.NewPathPattern(pattern, allowCommasTrue)
		c.Assert(err, IsNil, Commentf("%s", pattern))
		c.Assert(pathPattern.Matches(testPath), Equals, expectedMatch, Commentf("%s", pattern))
	}
}
