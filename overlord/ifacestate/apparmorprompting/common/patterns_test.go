// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2024 Canonical Ltd
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

package common_test

import (
	"testing"

	. "gopkg.in/check.v1"

	doublestar "github.com/bmatcuk/doublestar/v4"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/overlord/ifacestate/apparmorprompting/common"
)

func Test(t *testing.T) { TestingT(t) }

type commonSuite struct {
	tmpdir string
}

var _ = Suite(&commonSuite{})

func (s *commonSuite) SetUpTest(c *C) {
	s.tmpdir = c.MkDir()
	dirs.SetRootDir(s.tmpdir)
}

func (s *commonSuite) TestExpandPathPattern(c *C) {
	for _, testCase := range []struct {
		pattern  string
		expanded []string
	}{
		{
			`/foo`,
			[]string{`/foo`},
		},
		{
			`/{foo,bar/}`,
			[]string{`/foo`, `/bar/`},
		},
		{
			`{/foo,/bar/}`,
			[]string{`/foo`, `/bar/`},
		},
		{
			`/foo**/bar/*/**baz/**/fizz*buzz/**`,
			[]string{`/foo*/bar/*/*baz/**/fizz*buzz/**`},
		},
		{
			`/{,//foo**/bar/*/**baz/**/fizz*buzz/**}`,
			[]string{`/`, `/foo*/bar/*/*baz/**/fizz*buzz/**`},
		},
		{
			`/{foo,bar,/baz}`,
			[]string{`/foo`, `/bar`, `/baz`},
		},
		{
			`/{foo,/bar,bar,/baz}`,
			[]string{`/foo`, `/bar`, `/baz`},
		},
		{
			`/foo/bar\**baz`,
			[]string{`/foo/bar\**baz`},
		},
		{
			`/foo/bar/baz/**/*.txt`,
			[]string{`/foo/bar/baz/**/*.txt`},
		},
		{
			`/foo/bar/baz/***.txt`,
			[]string{`/foo/bar/baz/*.txt`},
		},
		{
			`/foo/bar/baz******.txt`,
			[]string{`/foo/bar/baz*.txt`},
		},
		{
			`/foo/bar/baz/{?***,*?**,**?*,***?}.txt`,
			[]string{`/foo/bar/baz/?*.txt`},
		},
		{
			`/foo/bar/baz/{?***?,*?**?,**?*?,***??}.txt`,
			[]string{`/foo/bar/baz/??*.txt`},
		},
		{
			`/foo/bar/baz/{?***??,*?**??,**?*??,***???}.txt`,
			[]string{`/foo/bar/baz/???*.txt`},
		},
		{
			`/foo///bar/**/**/**/baz/***.txt/**/**/*`,
			[]string{`/foo/bar/**/baz/*.txt/**`},
		},
		{
			`{a,b}c{d,e}f{g,h}`,
			[]string{
				`acdfg`,
				`acdfh`,
				`acefg`,
				`acefh`,
				`bcdfg`,
				`bcdfh`,
				`bcefg`,
				`bcefh`,
			},
		},
		{
			`a{{b,c},d,{e{f,{,g}}}}h`,
			[]string{
				`abh`,
				`ach`,
				`adh`,
				`aefh`,
				`aeh`,
				`aegh`,
			},
		},
		{
			`a{{b,c},d,\{e{f,{,g\}}}}h`,
			[]string{
				`abh`,
				`ach`,
				`adh`,
				`a\{efh`,
				`a\{eh`,
				`a\{eg\}h`,
			},
		},
	} {
		expanded, err := common.ExpandPathPattern(testCase.pattern)
		c.Check(err, IsNil, Commentf("test case: %+v", testCase))
		c.Check(expanded, DeepEquals, testCase.expanded, Commentf("test case: %+v", testCase))
	}
}

func (s *commonSuite) TestExpandPathPatternUnhappy(c *C) {
	for _, testCase := range []struct {
		pattern string
		errStr  string
	}{
		{
			``,
			`invalid path pattern: pattern has length 0`,
		},
		{
			`/foo{bar`,
			`invalid path pattern: unmatched '{' character.*`,
		},
		{
			`/foo}bar`,
			`invalid path pattern: unmatched '}' character.*`,
		},
		{
			`/foo/bar\`,
			`invalid path pattern: trailing unescaped '\\' character.*`,
		},
		{
			`/foo/bar{`,
			`invalid path pattern: unmatched '{' character.*`,
		},
		{
			`/foo/bar{baz\`,
			`invalid path pattern: trailing unescaped '\\' character.*`,
		},
		{
			`/foo/bar{baz{\`,
			`invalid path pattern: trailing unescaped '\\' character.*`,
		},
		{
			`/foo/bar{baz{`,
			`invalid path pattern: unmatched '{' character.*`,
		},
	} {
		result, err := common.ExpandPathPattern(testCase.pattern)
		c.Check(result, IsNil)
		c.Check(err, ErrorMatches, testCase.errStr)
	}
}

func (s *commonSuite) TestGetHighestPrecedencePattern(c *C) {
	for i, testCase := range []struct {
		patterns          []string
		highestPrecedence string
	}{
		{
			[]string{
				"/foo",
			},
			"/foo",
		},
		{
			[]string{
				"/foo/bar",
				"/foo",
				"/foo/bar/baz",
			},
			"/foo/bar/baz",
		},
		{
			[]string{
				"/foo",
				"/foo/barbaz",
				"/foobar",
			},
			"/foo/barbaz",
		},
		// Literals
		{
			[]string{
				"/foo/bar/baz",
				"/foo/bar/",
			},
			"/foo/bar/baz",
		},
		{
			[]string{
				"/foo/bar/baz",
				"/foo/bar/ba?",
			},
			"/foo/bar/baz",
		},
		{
			[]string{
				"/foo/bar/baz",
				"/foo/bar/b?z",
			},
			"/foo/bar/baz",
		},
		{
			[]string{
				"/foo/bar/",
				"/foo/bar",
			},
			"/foo/bar/",
		},
		{
			[]string{
				"/foo/bar",
				"/foo/ba?",
			},
			"/foo/bar",
		},
		{
			[]string{
				"/foo/bar/",
				"/foo/bar/*",
			},
			"/foo/bar/",
		},
		{
			[]string{
				"/foo/bar/",
				"/foo/bar/**",
			},
			"/foo/bar/",
		},
		{
			[]string{
				"/foo/bar/",
				"/foo/bar/**/",
			},
			"/foo/bar/",
		},
		// Terminated
		{
			[]string{
				"/foo/bar",
				"/foo/bar/**",
			},
			"/foo/bar",
		},
		{
			[]string{
				"/foo/bar",
				"/foo/bar*",
			},
			"/foo/bar",
		},
		// Any single character
		{
			[]string{
				"/foo/bar?baz",
				"/foo/bar*baz",
			},
			"/foo/bar?baz",
		},
		{
			[]string{
				"/foo/bar?baz",
				"/foo/bar**baz",
			},
			"/foo/bar?baz",
		},
		{
			[]string{
				"/foo/ba?",
				"/foo/ba*",
			},
			"/foo/ba?",
		},
		{
			[]string{
				"/foo/ba?/",
				"/foo/ba?",
			},
			"/foo/ba?/",
		},
		// Singlestars
		{
			[]string{
				"/foo/bar/*/baz",
				"/foo/bar/*/*baz",
			},
			"/foo/bar/*/baz",
		},
		{
			[]string{
				"/foo/bar/*/baz",
				"/foo/bar/*/*",
			},
			"/foo/bar/*/baz",
		},
		{
			[]string{
				"/foo/bar/*/",
				"/foo/bar/*/*",
			},
			"/foo/bar/*/",
		},
		{
			[]string{
				"/foo/bar/*/",
				"/foo/bar/*",
			},
			"/foo/bar/*/",
		},
		{
			[]string{
				"/foo/bar/*/",
				"/foo/bar/*/**/",
			},
			"/foo/bar/*/",
		},
		{
			[]string{
				"/foo/bar/*/",
				"/foo/bar/*/**",
			},
			"/foo/bar/*/",
		},
		{
			[]string{
				"/foo/bar/*/*baz",
				"/foo/bar/*/*",
			},
			"/foo/bar/*/*baz",
		},
		{
			[]string{
				"/foo/bar/*/*baz",
				"/foo/bar/*/**",
			},
			"/foo/bar/*/*baz",
		},
		{
			[]string{
				"/foo/bar/*/*",
				"/foo/bar/*/**",
			},
			"/foo/bar/*/*",
		},
		{
			[]string{
				"/foo/bar/*",
				"/foo/bar/*/**",
			},
			"/foo/bar/*",
		},
		{
			[]string{
				"/foo/bar/*",
				"/foo/bar/**/baz",
			},
			"/foo/bar/*",
		},
		{
			[]string{
				"/foo/bar/*/**",
				"/foo/bar/**/baz",
			},
			"/foo/bar/*/**",
		},
		// Globs
		{
			[]string{
				"/foo/bar*baz",
				"/foo/bar*",
			},
			"/foo/bar*baz",
		},
		{
			[]string{
				"/foo/bar*/baz",
				"/foo/bar*/",
			},
			"/foo/bar*/baz",
		},
		{
			[]string{
				"/foo/bar*/baz",
				"/foo/bar*/baz/**",
			},
			"/foo/bar*/baz",
		},
		{
			[]string{
				"/foo/bar*/baz",
				"/foo/bar/**/baz",
			},
			"/foo/bar*/baz",
		},
		{
			[]string{
				"/foo/bar*/baz",
				"/foo/bar/**/*baz/",
			},
			"/foo/bar*/baz",
		},
		{
			[]string{
				"/foo/bar*/baz",
				"/foo/bar/**",
			},
			"/foo/bar*/baz",
		},
		{
			[]string{
				"/foo/bar*/baz/**",
				"/foo/bar/**",
			},
			"/foo/bar*/baz/**",
		},
		{
			[]string{
				"/foo/bar*/",
				"/foo/bar*/*baz",
			},
			"/foo/bar*/",
		},
		{
			[]string{
				"/foo/bar*/",
				"/foo/bar*/*",
			},
			"/foo/bar*/",
		},
		{
			[]string{
				"/foo/bar*/",
				"/foo/bar*/**/",
			},
			"/foo/bar*/",
		},
		{
			[]string{
				"/foo/bar*/",
				"/foo/bar*/**",
			},
			"/foo/bar*/",
		},
		{
			[]string{
				"/foo/bar*/",
				"/foo/bar/**/",
			},
			"/foo/bar*/",
		},
		{
			[]string{
				"/foo/bar*/",
				"/foo/bar*/**/",
			},
			"/foo/bar*/",
		},
		{
			[]string{
				"/foo/bar*/*baz",
				"/foo/bar*/*",
			},
			"/foo/bar*/*baz",
		},
		{
			[]string{
				"/foo/bar*/*baz",
				"/foo/bar/**/baz",
			},
			"/foo/bar*/*baz",
		},
		{
			[]string{
				"/foo/bar*/*baz",
				"/foo/bar*/**/baz",
			},
			"/foo/bar*/*baz",
		},
		{
			[]string{
				"/foo/bar*/*/baz",
				"/foo/bar*/*/*",
			},
			"/foo/bar*/*/baz",
		},
		{
			[]string{
				"/foo/bar*/*/baz",
				"/foo/bar/**/baz",
			},
			"/foo/bar*/*/baz",
		},
		{
			[]string{
				"/foo/bar*/*/",
				"/foo/bar*/*",
			},
			"/foo/bar*/*/",
		},
		{
			[]string{
				"/foo/bar*/*/baz",
				"/foo/bar*/**/baz",
			},
			"/foo/bar*/*/baz",
		},
		{
			[]string{
				"/foo/bar*/*/",
				"/foo/bar/**/baz/",
			},
			"/foo/bar*/*/",
		},
		{
			[]string{
				"/foo/bar*/*/",
				"/foo/bar*/**/baz/",
			},
			"/foo/bar*/*/",
		},
		{
			[]string{
				"/foo/bar*/*",
				"/foo/bar/**/baz/",
			},
			"/foo/bar*/*",
		},
		{
			[]string{
				"/foo/bar*/*",
				"/foo/bar*/**/baz/",
			},
			"/foo/bar*/*",
		},
		{
			[]string{
				"/foo/bar*",
				"/foo/bar/**/",
			},
			"/foo/bar*",
		},
		{
			[]string{
				"/foo/bar*",
				"/foo/bar*/**/",
			},
			"/foo/bar*",
		},
		// Doublestars
		{
			[]string{
				"/foo/bar/**/baz",
				"/foo/bar/**/*baz",
			},
			"/foo/bar/**/baz",
		},
		{
			[]string{
				"/foo/bar/**/baz",
				"/foo/bar/**",
			},
			"/foo/bar/**/baz",
		},
		{
			[]string{
				"/foo/bar/**/*baz/",
				"/foo/bar/**/*baz",
			},
			"/foo/bar/**/*baz/",
		},
		{
			[]string{
				"/foo/bar/**/*baz/",
				"/foo/bar/**/",
			},
			"/foo/bar/**/*baz/",
		},
		{
			[]string{
				"/foo/bar/**/*baz",
				"/foo/bar/**/",
			},
			"/foo/bar/**/*baz",
		},
		{
			[]string{
				"/foo/bar/**/*baz",
				"/foo/bar/**",
			},
			"/foo/bar/**/*baz",
		},
		{
			[]string{
				"/foo/bar/**/*baz",
				"/foo/bar*/**/baz",
			},
			"/foo/bar/**/*baz",
		},
		{
			[]string{
				"/foo/bar/**/",
				"/foo/bar/**",
			},
			"/foo/bar/**/",
		},
		{
			[]string{
				"/foo/bar/**/",
				"/foo/bar*/**/baz/",
			},
			"/foo/bar/**/",
		},
		{
			[]string{
				"/foo/bar/**",
				"/foo/bar*/**/baz/",
			},
			"/foo/bar/**",
		},
		// Globs followed by doublestars
		{
			[]string{
				"/foo/bar*/**/baz",
				"/foo/bar*/**/",
			},
			"/foo/bar*/**/baz",
		},
		{
			[]string{
				"/foo/bar*/**/",
				"/foo/bar*/**",
			},
			"/foo/bar*/**/",
		},
		// Miscellaneous
		{
			[]string{
				"/foo/bar/*.gz",
				"/foo/bar/*.tar.gz",
			},
			"/foo/bar/*.tar.gz",
		},
		{
			[]string{
				"/foo/bar/**/*.gz",
				"/foo/**/*.tar.gz",
			},
			"/foo/bar/**/*.gz",
		},
		{
			[]string{
				"/foo/bar/x/**/*.gz",
				"/foo/bar/**/*.tar.gz",
			},
			"/foo/bar/x/**/*.gz",
		},
		{
			// Both match `/foo/bar/baz.tar.gz`
			[]string{
				"/foo/bar/**/*.tar.gz",
				"/foo/bar/*",
			},
			"/foo/bar/*",
		},
		{
			[]string{
				"/foo/bar/**",
				"/foo/bar/baz/**",
				"/foo/bar/baz/**/*.txt",
			},
			"/foo/bar/baz/**/*.txt",
		},
		{
			// both match /foo/bar
			[]string{
				"/foo/bar*",
				"/foo/bar/**",
			},
			"/foo/bar*",
		},
		{
			[]string{
				"/foo/bar/*/baz*/**/fizz/*buzz",
				"/foo/bar/*/baz*/**/fizz/bu*zz",
				"/foo/bar/*/baz*/**/fizz/buzz",
				"/foo/bar/*/baz*/**/fizz/buzz*",
			},
			"/foo/bar/*/baz*/**/fizz/buzz",
		},
		{
			[]string{
				"/foo/*/bar/**",
				"/foo/**/bar/*",
			},
			"/foo/*/bar/**",
		},
		{
			[]string{
				`/foo/\\\b\a\r`,
				`/foo/barbaz`,
			},
			`/foo/barbaz`,
		},
		{
			[]string{
				`/foo/\\`,
				`/foo/*/bar/x`,
			},
			`/foo/\\`,
		},
		{
			[]string{
				`/foo/\**/b\ar/*\*`,
				`/foo/*/bar/x`,
			},
			`/foo/\**/b\ar/*\*`,
		},
		// Patterns with "**[^/]" should not be emitted from ExpandPathPattern
		{
			[]string{
				"/foo/**",
				"/foo/**bar",
			},
			"/foo/**bar",
		},
		// Duplicate patterns should never be passed into GetHighestPrecedencePattern,
		// but if they are, handle them correctly.
		{
			[]string{
				"/foo/bar/",
				"/foo/bar/",
			},
			"/foo/bar/",
		},
		{
			[]string{
				"/foo/bar/",
				"/foo/bar/",
				"/foo/bar",
			},
			"/foo/bar/",
		},
		{
			[]string{
				"/foo/bar/**",
				"/foo/bar/**",
				"/foo/bar/*",
			},
			"/foo/bar/*",
		},
	} {
		highestPrecedence, err := common.GetHighestPrecedencePattern(testCase.patterns)
		c.Check(err, IsNil, Commentf("Error occurred during test case %d:\n%+v", i, testCase))
		c.Check(highestPrecedence, Equals, testCase.highestPrecedence, Commentf("Highest precedence pattern incorrect for test case %d:\n%+v", i, testCase))
	}

	// Check that unicode in patterns treated as a single rune, and that escape
	// characters are not counted, even when escaping unicode runes.
	for i, testCase := range []struct {
		longerRunes string
		longerBytes string
	}{
		{
			`/foo/bar`,
			`/foo/🚵🚵`,
		},
		{
			`/foo/barbar`,
			`/foo/\🚵\🚵\🚵\🚵\🚵`,
		},
		{
			`/foo/🚵🚵🚵🚵🚵🚵`,
			`/foo/\🚵\🚵\🚵\🚵\🚵`,
		},
	} {
		patterns := []string{testCase.longerRunes, testCase.longerBytes}
		highestPrecedence, err := common.GetHighestPrecedencePattern(patterns)
		c.Check(err, IsNil, Commentf("Error occurred during test case %d:\n%+v", i, testCase))
		c.Check(highestPrecedence, Equals, testCase.longerRunes, Commentf("Highest precedence pattern incorrect for test case %d:\n%+v", i, testCase))
		c.Check(len(testCase.longerRunes) < len(testCase.longerBytes), Equals, true, Commentf("Higher precedence pattern incorrectly does not have fewer bytes: len(%q) == %d >= len(%q) == %d:\n%+v", testCase.longerRunes, len(testCase.longerRunes), testCase.longerBytes, len(testCase.longerBytes), testCase))
	}
}

func (s *commonSuite) TestGetHighestPrecedencePatternUnhappy(c *C) {
	empty, err := common.GetHighestPrecedencePattern([]string{})
	c.Check(err, Equals, common.ErrNoPatterns)
	c.Check(empty, Equals, "")

	result, err := common.GetHighestPrecedencePattern([]string{
		`/foo/bar`,
		`/foo/bar\`,
	})
	c.Check(err, ErrorMatches, "invalid path pattern.*")
	c.Check(result, Equals, "")
}

func (s *commonSuite) TestValidatePathPattern(c *C) {
	for _, pattern := range []string{
		"/",
		"/*",
		"/**",
		"/**/*.txt",
		"/foo",
		"/foo/",
		"/foo/file.txt",
		"/foo*",
		"/foo*bar",
		"/foo*bar/baz",
		"/foo/bar*baz",
		"/foo/*",
		"/foo/*bar",
		"/foo/*bar/",
		"/foo/*bar/baz",
		"/foo/*bar/baz/",
		"/foo/*/",
		"/foo/*/bar",
		"/foo/*/bar/",
		"/foo/*/bar/baz",
		"/foo/*/bar/baz/",
		"/foo/**/bar",
		"/foo/**/bar/",
		"/foo/**/bar/baz",
		"/foo/**/bar/baz/",
		"/foo/**/bar*",
		"/foo/**/bar*baz",
		"/foo/**/bar*baz/",
		"/foo/**/bar*/",
		"/foo/**/bar*/baz",
		"/foo/**/bar*/baz/fizz/",
		"/foo/**/bar/*",
		"/foo/**/bar/*.tar.gz",
		"/foo/**/bar/*baz",
		"/foo/**/bar/*baz/fizz/",
		"/foo/**/bar/*/",
		"/foo/**/bar/*baz",
		"/foo/**/bar/buzz/*baz/",
		"/foo/**/bar/*baz/fizz",
		"/foo/**/bar/buzz/*baz/fizz/",
		"/foo/**/bar/*/baz",
		"/foo/**/bar/buzz/*/baz/",
		"/foo/**/bar/*/baz/fizz",
		"/foo/**/bar/buzz/*/baz/fizz/",
		"/foo/**/bar/buzz*baz/fizz/",
		"/foo/**/*bar",
		"/foo/**/*bar/",
		"/foo/**/*bar/baz.tar.gz",
		"/foo/**/*bar/baz/",
		"/foo/**/*/",
		"/foo/**/*/bar",
		"/foo/**/*/bar/baz/",
		"/foo{,/,bar,*baz,*.baz,/*fizz,/*.fizz,/**/*buzz}",
		"/foo/{,*.bar,**/baz}",
		"/foo/bar/*",
		"/foo/bar/*.tar.gz",
		"/foo/bar/**",
		"/foo/bar/**/*.zip",
		"/foo/bar/**/*.tar.gz",
		`/foo/bar\,baz`,
		`/foo/bar\{baz`,
		`/foo/bar\\baz`,
		`/foo/bar\*baz`,
		`/foo/bar{,/baz/*,/fizz/**/*.txt}`,
		"/foo/*/bar",
		"/foo/bar/",
		"/foo/**/bar",
		"/foo/bar*",
		"/foo/bar*.txt",
		"/foo/bar/*txt",
		"/foo/bar/**/file.txt",
		"/foo/bar/*/file.txt",
		"/foo/bar/**/*txt",
		"/foo/bar**",
		"/foo/bar/**.txt",
		"/foo/ba,r",
		"/foo/ba,r/**/*.txt",
		"/foo/bar/**/*.txt,md",
		"/foo//bar",
		"/foo{//,bar}",
		"/foo{//*.bar,baz}",
		"/foo/{/*.bar,baz}",
		"/foo/*/**",
		"/foo/*/bar/**",
		"/foo/*/bar/*",
		"/foo{bar,/baz}{fizz,buzz}",
		"/foo{bar,/baz}/{fizz,buzz}",
		"/foo?bar",
		"/foo/{a,b}{c,d}{e,f}{g,h}{i,j}{k,l}{m,n}{o,p}{q,r}{s,t}",
		"/foo/{a,{b,{c,{d,{e,{f,{g,{h,{i,{j,k}}}}}}}}}}",
		"/foo/{{{{{{{{{{a,b},c},d},e},f},g},h},i},j},k}",
	} {
		c.Check(common.ValidatePathPattern(pattern), IsNil, Commentf("valid path pattern %q was incorrectly not allowed", pattern))
	}

	for _, pattern := range []string{
		"file.txt",
		"/foo/bar{/**/*.txt",
		"/foo/bar/**/*.{txt",
		"{,/foo}",
		"{/,foo}",
		"/foo/ba[rz]",
		`/foo/bar\`,
		"/foo/{a,b}{c,d}{e,f}{g,h}{i,j}{k,l}{m,n}{o,p}{q,r}{s,t}{w,x}",
		"/foo/{a,{b,{c,{d,{e,{f,{g,{h,{i,{j,{k,l}}}}}}}}}}}",
		"/foo/{{{{{{{{{{{a,b},c},d},e},f},g},h},i},j},k},l}",
	} {
		c.Check(common.ValidatePathPattern(pattern), ErrorMatches, "invalid path pattern.*", Commentf("invalid path pattern %q was incorrectly allowed", pattern))
	}
}

func (*commonSuite) TestPathPatternMatch(c *C) {
	cases := []struct {
		pattern string
		path    string
		matches bool
	}{
		{
			"/home/test/Documents",
			"/home/test/Documents",
			true,
		},
		{
			"/home/test/Documents",
			"/home/test/Documents/",
			true,
		},
		{
			"/home/test/Documents/",
			"/home/test/Documents",
			false,
		},
		{
			"/home/test/Documents/",
			"/home/test/Documents/",
			true,
		},
		{
			"/home/test/Documents/*",
			"/home/test/Documents",
			false,
		},
		{
			"/home/test/Documents/*",
			"/home/test/Documents/",
			true,
		},
		{
			"/home/test/Documents/*",
			"/home/test/Documents/foo",
			true,
		},
		{
			"/home/test/Documents/*",
			"/home/test/Documents/foo/",
			true,
		},
		{
			"/home/test/Documents/*/",
			"/home/test/Documents",
			false,
		},
		{
			"/home/test/Documents/*/",
			"/home/test/Documents/",
			false,
		},
		{
			"/home/test/Documents/*/",
			"/home/test/Documents/foo",
			false,
		},
		{
			"/home/test/Documents/*/",
			"/home/test/Documents/foo/",
			true,
		},
		{
			"/home/test/Documents/**",
			"/home/test/Documents",
			true,
		},
		{
			"/home/test/Documents/**",
			"/home/test/Documents/",
			true,
		},
		{
			"/home/test/Documents/**",
			"/home/test/Documents/foo",
			true,
		},
		{
			"/home/test/Documents/**",
			"/home/test/Documents/foo/",
			true,
		},
		{
			// Even though doublestar lets /path/to/a/**/ match /path/to/a, we
			// want the ability to match only directories, so we impose the
			// additional constraint that patterns ending in /**/ only match
			// paths which end in /
			"/home/test/Documents/**/",
			"/home/test/Documents",
			false,
		},
		{
			"/home/test/Documents/**/",
			"/home/test/Documents/",
			true,
		},
		{
			"/home/test/Documents/**/",
			"/home/test/Documents/foo",
			false,
		},
		{
			"/home/test/Documents/**/",
			"/home/test/Documents/foo/",
			true,
		},
		{
			"/home/test/Documents/**/",
			"/home/test/Documents/foo/bar",
			false,
		},
		{
			"/home/test/Documents/**/",
			"/home/test/Documents/foo/bar/",
			true,
		},
		{
			"/home/test/Documents/**/*.txt",
			"/home/test/Documents/foo.txt",
			true,
		},
		{
			"/home/test/Documents/**/*.txt",
			"/home/test/Documents/foo/bar.tar.gz",
			false,
		},
		{
			"/home/test/Documents/**/*.gz",
			"/home/test/Documents/foo/bar.tar.gz",
			true,
		},
		{
			"/home/test/Documents/**/*.tar.gz",
			"/home/test/Documents/foo/bar.tar.gz",
			true,
		},
		{
			"/home/test/Documents/*.tar.gz",
			"/home/test/Documents/foo/bar.tar.gz",
			false,
		},
		{
			"/home/test/Documents/foo",
			"/home/test/Documents/foo.txt",
			false,
		},
		{
			"/home/test/Documents/foo*",
			"/home/test/Documents/foo.txt",
			true,
		},
		{
			"/home/test/Documents/foo?*",
			"/home/test/Documents/foo.txt",
			true,
		},
		{
			"/home/test/Documents/foo????",
			"/home/test/Documents/foo.txt",
			true,
		},
		{
			"/home/test/Documents/foo????*",
			"/home/test/Documents/foo.txt",
			true,
		},
		{
			"/home/test/Documents/foo?????*",
			"/home/test/Documents/foo.txt",
			false,
		},
		{
			"/home/test/Documents/*",
			"/home/test/Documents/foo/bar.tar.gz",
			false,
		},
		{
			"/home/test/**",
			"/home/test/Documents/foo/bar.tar.gz",
			true,
		},
		{
			"/home/test/**/*.tar.gz",
			"/home/test/Documents/foo/bar.tar.gz",
			true,
		},
		{
			"/home/test/**/*.gz",
			"/home/test/Documents/foo/bar.tar.gz",
			true,
		},
		{
			"/home/test/**/*.txt",
			"/home/test/Documents/foo/bar.tar.gz",
			false,
		},
		{
			"/foo/bar*",
			"/hoo/bar/",
			false,
		},
		{
			"/foo/bar?",
			"/hoo/bar/",
			false,
		},
		{
			"/foo/bar/**",
			"/foo/bar/",
			true,
		},
		{
			"/foo/*/bar/**/baz**/fi*z/**buzz",
			"/foo/abc/bar/baznm/fizz/xyzbuzz",
			true,
		},
		{
			"/foo/*/bar/**/baz**/fi*z/**buzz",
			"/foo/abc/bar/baz/nm/fizz/xyzbuzz",
			false,
		},
		{
			"/foo*bar",
			"/foobar",
			true,
		},
		{
			"/foo*bar",
			"/fooxbar",
			true,
		},
		{
			"/foo*bar",
			"/foo/bar",
			false,
		},
		{
			"/foo?bar",
			"/foobar",
			false,
		},
		{
			"/foo?bar",
			"/fooxbar",
			true,
		},
		{
			"/foo?bar",
			"/foo/bar",
			false,
		},
		{
			"/foo/*/bar",
			"/foo/bar",
			false,
		},
		{
			"/foo/**/bar",
			"/foo/bar",
			true,
		},
		{
			"/foo/**/bar",
			"/foo/bar/",
			true,
		},
		{
			"/foo/**/bar",
			"/foo/fizz/buzz/bar/",
			true,
		},
		{
			"/foo**/bar",
			"/fooabc/bar",
			true,
		},
		{
			"/foo**/bar",
			"/foo/bar",
			true,
		},
		{
			"/foo**/bar",
			"/foo/fizz/bar",
			false,
		},
		{
			"/foo/**bar",
			"/foo/abcbar",
			true,
		},
		{
			"/foo/**bar",
			"/foo/bar",
			true,
		},
		{
			"/foo/**bar",
			"/foo/fizz/bar",
			false,
		},
		{
			"/foo/*/bar/**/baz**/fi*z/**buzz",
			"/foo/abc/bar/baz/fiz/buzz",
			true,
		},
		{
			"/foo/*/bar/**/baz**/fi*z/**buzz",
			"/foo/abc/bar/baz/abc/fiz/buzz",
			false,
		},
		{
			"/foo/*/bar/**/baz**/fi*z/**buzz",
			"/foo/bar/bazmn/fizz/xyzbuzz",
			false,
		},
		{
			"/foo/bar/**/*/",
			"/foo/bar/baz",
			false,
		},
		{
			"/foo/bar/**/*/",
			"/foo/bar/baz/",
			true,
		},
	}
	for _, testCase := range cases {
		matches, err := common.PathPatternMatch(testCase.pattern, testCase.path)
		c.Check(err, IsNil, Commentf("test case: %+v", testCase))
		c.Check(matches, Equals, testCase.matches, Commentf("test case: %+v", testCase))
	}
}

func (s *commonSuite) TestPathPatternMatchUnhappy(c *C) {
	badPattern := `badpattern\`
	matches, err := common.PathPatternMatch(badPattern, "foo")
	c.Check(err, Equals, doublestar.ErrBadPattern)
	c.Check(matches, Equals, false)
}
