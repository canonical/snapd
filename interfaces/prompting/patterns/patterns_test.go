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

package patterns_test

import (
	"strings"
	"testing"

	. "gopkg.in/check.v1"

	doublestar "github.com/bmatcuk/doublestar/v4"

	"github.com/snapcore/snapd/interfaces/prompting/patterns"
)

func Test(t *testing.T) { TestingT(t) }

type patternsSuite struct{}

var _ = Suite(&patternsSuite{})

func (s *patternsSuite) TestParsePathPatternHappy(c *C) {
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
		"/foo/{a,b}{c,d}{e,f}{g,h}{i,j}{k,l}{m,n}{o,p}{q,r}", // expands to 512
		"/foo/{a,{b,{c,{d,{e,{f,{g,{h,{i,{j,{k,{l,{m,{n,{o,p}}}}}}}}}}}}}}}",
		"/foo/{{{{{{{{{{{{{{{a,b},c},d},e},f},g},h},i},j},k},l},m},n},o},p}",
		"/foo/{a,b}{c,d}{e,f}{g,h,i,j,k}{l,m,n,o,p}{q,r,s,t,u}",       // expands to 1000
		"/foo/{a,b}{c,d}{e,f}{g,h,i,j,k}{l,m,n,o,p}{q,r,s,t,u},1,2,3", // expands to 1000, with commas outside groups
		"/" + strings.Repeat("{a,", 999) + "a" + strings.Repeat("}", 999),
		"/" + strings.Repeat("{", 999) + "a" + strings.Repeat(",a}", 999),
	} {
		_, err := patterns.ParsePathPattern(pattern)
		c.Check(err, IsNil, Commentf("valid path pattern %q was incorrectly not allowed", pattern))
	}
}

func (s *patternsSuite) TestParsePathPatternUnhappy(c *C) {
	for _, testCase := range []struct {
		pattern string
		errStr  string
	}{
		{
			``,
			`cannot parse path pattern .*: pattern has length 0`,
		},
		{
			`file.txt`,
			`cannot parse path pattern "file.txt": pattern must start with '/'`,
		},
		{
			`{/,/foo}`,
			`cannot parse path pattern .*: pattern must start with '/'`,
		},
		{
			`/foo{bar`,
			`cannot parse path pattern .*: unmatched '{' character`,
		},
		{
			`/foo}bar`,
			`cannot parse path pattern .*: unmatched '}' character.*`,
		},
		{
			`/foo/bar\`,
			`cannot parse path pattern .*: trailing unescaped '\\' character`,
		},
		{
			`/foo/bar{`,
			`cannot parse path pattern .*: unmatched '{' character`,
		},
		{
			`/foo/bar{baz\`,
			`cannot parse path pattern .*: trailing unescaped '\\' character`,
		},
		{
			`/foo/bar{baz{\`,
			`cannot parse path pattern .*: trailing unescaped '\\' character`,
		},
		{
			`/foo/bar{baz{`,
			`cannot parse path pattern .*: unmatched '{' character`,
		},
		{
			`/foo/ba[rz]`,
			`cannot parse path pattern .*: cannot contain unescaped '\[' or '\]' character`,
		},
		{
			`/foo/{a,b}{c,d}{e,f}{g,h}{i,j}{k,l}{m,n}{o,p}{q,r}{s,t}`, // expands to 1024
			`cannot parse path pattern .*: exceeded maximum number of expanded path patterns \(1000\): 1024`,
		},
		{
			`/foo/{a,b,c,d,e,f,g}{h,i,j,k,l,m,n,o,p,q,r}{s,t,u,v,w,x,y,z,1,2,3,4,5}`, // expands to 1001
			`cannot parse path pattern .*: exceeded maximum number of expanded path patterns \(1000\): 1001`,
		},
		{
			"/" + strings.Repeat("{a,", 1000) + "a" + strings.Repeat("}", 1000),
			`cannot parse path pattern .*: nested group depth exceeded maximum number of expanded path patterns \(1000\)`,
		},
		{
			"/" + strings.Repeat("{", 1000) + "a" + strings.Repeat(",a}", 1000),
			`cannot parse path pattern .*: nested group depth exceeded maximum number of expanded path patterns \(1000\)`,
		},
		{
			"/" + strings.Repeat("{", 10000),
			`cannot parse path pattern .*: nested group depth exceeded maximum number of expanded path patterns \(1000\)`,
		},
	} {
		pathPattern, err := patterns.ParsePathPattern(testCase.pattern)
		c.Check(err, ErrorMatches, testCase.errStr, Commentf("testCase: %+v", testCase))
		c.Check(pathPattern, IsNil)
	}
}

func (s *patternsSuite) TestPathPatternMatch(c *C) {
	for _, testCase := range []struct {
		pattern string
		path    string
		matches bool
	}{
		{
			"/foo",
			"/foo",
			true,
		},
		{
			"/foo",
			"/foo/",
			true, // we override doublestar here
		},
		{
			"/foo/ba{r,z}/**",
			"/foo/bar/baz/qux",
			true,
		},
		{
			"/foo/ba{r,z}/**",
			"/foo/baz/fizz/buzz",
			true,
		},
		{
			"/foo/ba{r,z}/**",
			"/foo/bar",
			true,
		},
		{
			"/foo/ba{r,z}/**",
			"/foo/baz/",
			true,
		},
		{
			"/foo/ba{r,z}/**",
			"/foo/ba/r",
			false,
		},
		{
			"/{a,b}{c,d}{e,f}{g,h}",
			"/adeh",
			true,
		},
		{
			"/{a,b}{c,d}{e,f}{g,h}",
			"/abcd",
			false,
		},
	} {
		pathPattern, err := patterns.ParsePathPattern(testCase.pattern)
		c.Check(err, IsNil, Commentf("testCase: %+v", testCase))
		matches, err := pathPattern.Match(testCase.path)
		c.Check(err, IsNil, Commentf("testCase: %+v", testCase))
		c.Check(matches, Equals, testCase.matches, Commentf("testCase: %+v", testCase))
	}
}

func (s *patternsSuite) TestPathPatternMarshalJSON(c *C) {
	for _, pattern := range []string{
		"/foo",
		"/foo/ba{r,s}/**",
		"/{a,b}{c,d}{e,f}{g,h}",
	} {
		pathPattern, err := patterns.ParsePathPattern(pattern)
		c.Check(err, IsNil)
		marshalled, err := pathPattern.MarshalJSON()
		c.Check(err, IsNil)
		c.Check(marshalled, DeepEquals, []byte(pattern))
	}
}

func (s *patternsSuite) TestPathPatternUnmarshalJSONHappy(c *C) {
	for _, pattern := range [][]byte{
		[]byte(`"/foo"`),
		[]byte(`"/foo/ba{r,s}/**"`),
		[]byte(`"/{a,b}{c,d}{e,f}{g,h}"`),
	} {
		pathPattern := patterns.PathPattern{}
		err := pathPattern.UnmarshalJSON(pattern)
		c.Check(err, IsNil)
		marshalled, err := pathPattern.MarshalJSON()
		c.Check(err, IsNil)
		// Marshalled pattern excludes surrounding '"' for some reason
		c.Check(marshalled, DeepEquals, pattern[1:len(pattern)-1])
	}
}

func (s *patternsSuite) TestPathPatternUnmarshalJSONUnhappy(c *C) {
	for _, testCase := range []struct {
		json []byte
		err  string
	}{
		{
			[]byte(`"foo"`),
			`cannot parse path pattern "foo": pattern must start with '/'.*`,
		},
		{
			[]byte{'"', 0x00, '"'},
			`invalid character '\\x00' in string literal`,
		},
	} {
		pathPattern := patterns.PathPattern{}
		err := pathPattern.UnmarshalJSON(testCase.json)
		c.Check(err, ErrorMatches, testCase.err)
	}
}

func (s *patternsSuite) TestPathPatternRenderAllVariants(c *C) {
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
			`/{/foo,/bar/}`,
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
			[]string{`/foo`, `/bar`, `/bar`, `/baz`},
		},
		{
			`/foo/bar\**baz`,
			[]string{`/foo/bar\**baz`},
		},
		{
			`/foo/bar\{baz`,
			[]string{`/foo/bar\{baz`},
		},
		{
			`/foo/bar\}baz`,
			[]string{`/foo/bar\}baz`},
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
			[]string{`/foo/bar/baz/?*.txt`, `/foo/bar/baz/?*.txt`, `/foo/bar/baz/?*.txt`, `/foo/bar/baz/?*.txt`},
		},
		{
			`/foo/bar/baz/{?***?,*?**?,**?*?,***??}.txt`,
			[]string{`/foo/bar/baz/??*.txt`, `/foo/bar/baz/??*.txt`, `/foo/bar/baz/??*.txt`, `/foo/bar/baz/??*.txt`},
		},
		{
			`/foo/bar/baz/{?***??,*?**??,**?*??,***???}.txt`,
			[]string{`/foo/bar/baz/???*.txt`, `/foo/bar/baz/???*.txt`, `/foo/bar/baz/???*.txt`, `/foo/bar/baz/???*.txt`},
		},
		{
			`/foo///bar/**/**/**/baz/***.txt/**/**/*`,
			[]string{`/foo/bar/**/baz/*.txt/**`},
		},
		{
			`/{a,b}c{d,e}f{g,h}`,
			[]string{
				`/acdfg`,
				`/acdfh`,
				`/acefg`,
				`/acefh`,
				`/bcdfg`,
				`/bcdfh`,
				`/bcefg`,
				`/bcefh`,
			},
		},
		{
			`/a{{b,c},d,{e{f,{,g}}}}h`,
			[]string{
				`/abh`,
				`/ach`,
				`/adh`,
				`/aefh`,
				`/aeh`,
				`/aegh`,
			},
		},
		{
			`/a{{b,c},d,\{e{f,{,g\}}}}h`,
			[]string{
				`/abh`,
				`/ach`,
				`/adh`,
				`/a\{efh`,
				`/a\{eh`,
				`/a\{eg\}h`,
			},
		},
		{
			"/foo/{a,{b,{c,{d,{e,{f,{g,{h,{i,{j,k}}}}}}}}}}",
			[]string{
				"/foo/a",
				"/foo/b",
				"/foo/c",
				"/foo/d",
				"/foo/e",
				"/foo/f",
				"/foo/g",
				"/foo/h",
				"/foo/i",
				"/foo/j",
				"/foo/k",
			},
		},
		{
			"/foo/{{{{{{{{{{a,b},c},d},e},f},g},h},i},j},k}",
			[]string{
				"/foo/a",
				"/foo/b",
				"/foo/c",
				"/foo/d",
				"/foo/e",
				"/foo/f",
				"/foo/g",
				"/foo/h",
				"/foo/i",
				"/foo/j",
				"/foo/k",
			},
		},
		{
			"/foo,bar,baz",
			[]string{"/foo,bar,baz"},
		},
	} {
		pathPattern, err := patterns.ParsePathPattern(testCase.pattern)
		c.Check(err, IsNil, Commentf("testCase: %+v", testCase))
		expanded := make([]string, 0, pathPattern.NumVariants())
		pathPattern.RenderAllVariants(func(i int, variant patterns.PatternVariant) {
			expanded = append(expanded, variant.String())
		})
		c.Check(expanded, DeepEquals, testCase.expanded, Commentf("test case: %+v", testCase))
	}
}

func (s *patternsSuite) TestPathPatternMatches(c *C) {
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
			"/foo/bar/",
			true,
		},
		{
			"/foo/bar?",
			"/foo/bar/",
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
		{
			"/foo/ba{r,z}",
			"/foo/bar",
			true,
		},
		{
			"/foo/ba{r,z}",
			"/foo/baz",
			true,
		},
		{
			"/foo/ba{r,z}",
			"/foo/ba,",
			false,
		},
		{
			"/foo/ba{r,z}",
			"/foo/ba",
			false,
		},
		{
			"/foo/ba{r,z{,fizz,buzz}}",
			"/foo/bar",
			true,
		},
		{
			"/foo/ba{r,z{,fizz,buzz}}",
			"/foo/baz",
			true,
		},
		{
			"/foo/ba{r,z{,/qux}}",
			"/foo/baz/qux",
			true,
		},
		{
			"/foo/ba{r,z{,/qux}}",
			"/foo/bar/qux",
			false,
		},
	}
	for _, testCase := range cases {
		matches, err := patterns.PathPatternMatches(testCase.pattern, testCase.path)
		c.Check(err, IsNil, Commentf("test case: %+v", testCase))
		c.Check(matches, Equals, testCase.matches, Commentf("test case: %+v", testCase))
	}
}

func (s *patternsSuite) TestPathPatternMatchesErrors(c *C) {
	badPattern := `badpattern\`
	matches, err := patterns.PathPatternMatches(badPattern, "foo")
	c.Check(err, Equals, doublestar.ErrBadPattern)
	c.Check(matches, Equals, false)
}

func (s *patternsSuite) TestHighestPrecedencePattern(c *C) {
	for i, testCase := range []struct {
		matchingPath      string
		patterns          []string
		highestPrecedence string
	}{
		{
			"/foo",
			[]string{
				"/foo",
			},
			"/foo",
		},
		{
			"/foo",
			[]string{
				"/f?o",
				"/fo?",
			},
			"/fo?",
		},
		{
			"/foo/bar/baz",
			[]string{
				"/foo/bar/baz",
				"/foo/bar/ba?",
			},
			"/foo/bar/baz",
		},
		{
			"/foo/bar/baz",
			[]string{
				"/foo/bar/b?z",
				"/foo/bar/baz",
			},
			"/foo/bar/baz",
		},
		{
			"/foo/bar/baz",
			[]string{
				"/foo/bar/baz*",
				"/foo/bar/baz",
			},
			"/foo/bar/baz",
		},
		{
			"/foo/bar/baz",
			[]string{
				"/foo/b?r/baz",
				"/foo/bar/**",
			},
			"/foo/bar/**",
		},
		{
			"/foo/bar/",
			[]string{
				"/foo/bar/",
				"/foo/bar",
			},
			"/foo/bar/",
		},
		{
			"/foo/bar/",
			[]string{
				"/foo/bar/",
				"/foo/bar/*",
			},
			"/foo/bar/",
		},
		{
			"/foo/bar/",
			[]string{
				"/foo/bar/",
				"/foo/bar/**",
			},
			"/foo/bar/",
		},
		{
			"/foo/bar/",
			[]string{
				"/foo/bar/",
				"/foo/bar/**/",
			},
			"/foo/bar/",
		},
		{
			"/foo/bar",
			[]string{
				"/foo/bar",
				"/foo/bar/**",
			},
			"/foo/bar",
		},
		{
			"/foo/barxbaz",
			[]string{
				"/foo/bar?baz",
				"/foo/bar*baz",
			},
			"/foo/bar?baz",
		},
		{
			"/foo/barxbaz",
			[]string{
				"/foo/bar?baz",
				"/foo/bar**baz",
			},
			"/foo/bar?baz",
		},
		{
			"/foo/bar/baz/",
			[]string{
				"/foo/bar/baz",
				"/foo/b*r/baz/",
			},
			"/foo/bar/baz",
		},
		{
			"/foo/bar/x/baz",
			[]string{
				"/foo/bar/*/baz",
				"/foo/bar/*/*baz",
			},
			"/foo/bar/*/baz",
		},
		{
			"/foo/bar/x/baz",
			[]string{
				"/foo/bar/*/baz",
				"/foo/bar/*/*",
			},
			"/foo/bar/*/baz",
		},
		{
			"/foo/bar/baz/",
			[]string{
				"/foo/bar/*/",
				"/foo/bar/*",
			},
			"/foo/bar/*/",
		},
		{
			"/foo/bar/baz/",
			[]string{
				"/foo/bar/*/",
				"/foo/bar/*/**/",
			},
			"/foo/bar/*/",
		},
		{
			"/foo/bar/baz/",
			[]string{
				"/foo/bar/*/",
				"/foo/bar/*/**",
			},
			"/foo/bar/*/",
		},
		{
			"/foo/bar/x/baz",
			[]string{
				"/foo/bar/*/*baz",
				"/foo/bar/*/*",
			},
			"/foo/bar/*/*baz",
		},
		{
			"/foo/bar/x/baz",
			[]string{
				"/foo/bar/*/*baz",
				"/foo/bar/*/**",
			},
			"/foo/bar/*/*baz",
		},
		{
			"/foo/bar/x/baz",
			[]string{
				"/foo/bar/*/*",
				"/foo/bar/*/**",
			},
			"/foo/bar/*/*",
		},
		{
			"/foo/bar/baz",
			[]string{
				"/foo/bar/*",
				"/foo/bar/*/**",
			},
			"/foo/bar/*",
		},
		{
			"/foo/bar/baz",
			[]string{
				"/foo/bar/*",
				"/foo/bar/**/baz",
			},
			"/foo/bar/*",
		},
		{
			"/foo/bar/baz",
			[]string{
				"/foo/bar/*/**",
				"/foo/bar/**/baz",
			},
			"/foo/bar/*/**",
		},
		{
			"/foo/barxbaz",
			[]string{
				"/foo/bar*baz",
				"/foo/bar*",
			},
			"/foo/bar*baz",
		},
		{
			"/foo/bar/baz",
			[]string{
				"/foo/bar*/baz",
				"/foo/bar*/*",
			},
			"/foo/bar*/baz",
		},
		{
			"/foo/bar/baz",
			[]string{
				"/foo/bar*/baz",
				"/foo/bar*/baz/**",
			},
			"/foo/bar*/baz",
		},
		{
			"/foo/bar/baz",
			[]string{
				"/foo/bar*/baz",
				"/foo/bar/**",
			},
			"/foo/bar/**",
		},
		{
			"/foo/barxxx/xxxbaz",
			[]string{
				"/foo/bar*/*",
				"/foo/bar*/*baz",
			},
			"/foo/bar*/*baz",
		},
		{
			"/foo/barxxx",
			[]string{
				"/foo/bar*/**",
				"/foo/bar*",
			},
			"/foo/bar*",
		},
		{
			"/foo/bar/",
			[]string{
				"/foo/bar*/",
				"/foo/bar*/**",
			},
			"/foo/bar*/",
		},
		{
			"/foo/bar/",
			[]string{
				"/foo/bar*/",
				"/foo/bar*/**/",
			},
			"/foo/bar*/",
		},
		{
			"/foo/bar/",
			[]string{
				"/foo/bar*/",
				"/foo/bar/**/",
			},
			"/foo/bar/**/",
		},
		{
			"/foo/bar/baz/",
			[]string{
				"/foo/bar*/*baz",
				"/foo/bar/**/baz",
			},
			"/foo/bar/**/baz",
		},
		{
			"/foo/bar/baz/",
			[]string{
				"/foo/bar*/*baz",
				"/foo/bar*/**/baz",
			},
			"/foo/bar*/*baz",
		},
		{
			"/foo/bar/x/baz/",
			[]string{
				"/foo/bar*/*/baz",
				"/foo/bar*/*/*",
			},
			"/foo/bar*/*/baz",
		},
		{
			"/foo/bar/x/baz/",
			[]string{
				"/foo/bar*/*/baz",
				"/foo/bar/**/baz",
			},
			"/foo/bar/**/baz",
		},
		{
			"/foo/bar/baz/",
			[]string{
				"/foo/bar*/*/",
				"/foo/bar*/*",
			},
			"/foo/bar*/*/",
		},
		{
			"/foo/bar/baz/",
			[]string{
				"/foo/bar/**/baz",
				"/foo/bar/**/*baz",
			},
			"/foo/bar/**/baz",
		},
		{
			"/foo/bar/baz/",
			[]string{
				"/foo/bar/**/baz",
				"/foo/bar/**",
			},
			"/foo/bar/**/baz",
		},
		// Prioritize earlier matches after /**/
		{
			"/foo/bar/fizz/buzz/file.txt",
			[]string{
				"/foo/bar/**/fizz/**",
				"/foo/bar/**/buzz/**",
			},
			"/foo/bar/**/fizz/**",
		},
		{
			"/foo/bar/buzz/fizz/file.txt",
			[]string{
				"/foo/bar/**/fizz/**",
				"/foo/bar/**/buzz/**",
			},
			"/foo/bar/**/buzz/**",
		},
		{
			"/foo/bar/baz/",
			[]string{
				"/foo/bar/**/*baz/",
				"/foo/bar/**/*baz",
			},
			"/foo/bar/**/*baz/",
		},
		{
			"/foo/bar/baz/",
			[]string{
				"/foo/bar/**/*baz/",
				"/foo/bar/**/",
			},
			"/foo/bar/**/*baz/",
		},
		{
			"/foo/bar/baz/",
			[]string{
				"/foo/bar/**/*baz",
				"/foo/bar/**/",
			},
			"/foo/bar/**/*baz",
		},
		{
			"/foo/bar/x/baz",
			[]string{
				"/foo/bar/**/*baz",
				"/foo/bar/**",
			},
			"/foo/bar/**/*baz",
		},
		{
			"/foo/bar/fizz/buzz/baz",
			[]string{
				"/foo/bar/**/*baz",
				"/foo/bar*/**/baz",
			},
			"/foo/bar/**/*baz",
		},
		{
			"/foo/bar/fizz/buzz/baz/",
			[]string{
				"/foo/bar/**/",
				"/foo/bar/**",
			},
			"/foo/bar/**/",
		},
		{
			"/foo/bar/fizz/buzz/baz/",
			[]string{
				"/foo/bar/**/",
				"/foo/bar*/**/baz/",
			},
			"/foo/bar/**/",
		},
		{
			"/foo/bar/fizz/buzz/baz/",
			[]string{
				"/foo/bar/**",
				"/foo/bar*/**/baz/",
			},
			"/foo/bar/**",
		},
		{
			"/foo/bar/fizz/buzz/baz/",
			[]string{
				"/foo/bar*/**/baz",
				"/foo/bar*/**/",
			},
			"/foo/bar*/**/baz",
		},
		{
			"/foo/barfizz/buzz/baz/",
			[]string{
				"/foo/bar*/**/",
				"/foo/bar*/**",
			},
			"/foo/bar*/**/",
		},
		{
			"/foo/bar/file.tar.gz",
			[]string{
				"/foo/bar/*.gz",
				"/foo/bar/*.tar.gz",
			},
			"/foo/bar/*.tar.gz",
		},
		{
			"/foo/bar/file.tar.gz",
			[]string{
				"/foo/bar/**/*.gz",
				"/foo/**/*.tar.gz",
			},
			"/foo/bar/**/*.gz",
		},
		{
			"/foo/bar/x/y/z/file.tar.gz",
			[]string{
				"/foo/bar/x/**/*.gz",
				"/foo/bar/**/*.tar.gz",
			},
			"/foo/bar/x/**/*.gz",
		},
		{
			"/foo/bar/file.tar.gz",
			[]string{
				"/foo/bar/**/*.tar.gz",
				"/foo/bar/*",
			},
			"/foo/bar/*",
		},
		{
			"/foo/bar/baz/x/y/z/file.txt",
			[]string{
				"/foo/bar/**",
				"/foo/bar/baz/**",
				"/foo/bar/baz/**/*.txt",
			},
			"/foo/bar/baz/**/*.txt",
		},
		{
			"/foo/bar",
			[]string{
				"/foo/bar*",
				"/foo/bar/**",
			},
			"/foo/bar/**",
		},
		{
			`/foo/\`,
			[]string{
				`/foo/\\`,
				`/foo/*/**`,
			},
			`/foo/\\`,
		},
		{
			`/foo/*fizz/bar/x*`,
			[]string{
				`/foo/\**/b\ar/*\*`,
				`/foo/*/bar/x\*`,
			},
			`/foo/\**/bar/*\*`,
		},
		{
			"/foo/barxxxbaz",
			[]string{
				"/foo/bar**",
				"/foo/bar**baz",
			},
			"/foo/bar*baz",
		},
		{
			"/foo/xxxbar",
			[]string{
				"/foo/**",
				"/foo/**bar",
			},
			"/foo/*bar",
		},
		{
			"/foo/x/y/z/bar/baz/",
			[]string{
				"/foo/**/bar/*/",
				"/foo/**/b?r/baz/",
			},
			"/foo/**/bar/*/",
		},
		{
			"/foo/x/y/z/bar/fizz",
			[]string{
				"/foo/**/fizz",
				"/foo/**/bar/fizz",
			},
			"/foo/**/bar/fizz",
		},
		{
			"/foo/x/y/z/bar",
			[]string{
				"/foo/**/*/bar",
				"/foo/**/*",
			},
			"/foo/*/**/bar",
		},
		{
			"/foo/bar",
			[]string{
				"/foo/**/*",
				"/**",
			},
			"/foo/**",
		},
		// Duplicate patterns should never be passed into HighestPrecedencePattern,
		// but if they are, handle them correctly.
		{
			"/foo/bar/",
			[]string{
				"/foo/bar/",
				"/foo/bar/",
			},
			"/foo/bar/",
		},
		{
			"/foo/bar/",
			[]string{
				"/foo/bar/",
				"/foo/bar/",
				"/foo/bar",
			},
			"/foo/bar/",
		},
		{
			"/foo/bar/baz/",
			[]string{
				"/foo/bar/**",
				"/foo/bar/**",
				"/foo/bar/*",
			},
			"/foo/bar/*",
		},
	} {
		variants := make([]patterns.PatternVariant, len(testCase.patterns))
		variantsReversed := make([]patterns.PatternVariant, len(testCase.patterns))
		for i, pattern := range testCase.patterns {
			variant, err := patterns.ParsePatternVariant(pattern)
			c.Assert(err, IsNil, Commentf("pattern: %s", pattern))
			variants[i] = variant
			variantsReversed[len(variantsReversed)-1-i] = variant
			// Check that the rendered variant actually matches the path
			matches, err := patterns.PathPatternMatches(variant.String(), testCase.matchingPath)
			c.Check(err, IsNil, Commentf("testCase: %+v\npath: %s\nvariant: %s", testCase, testCase.matchingPath, variant.String()))
			c.Check(matches, Equals, true, Commentf("testCase: %+v\npath: %s\nvariant: %s", testCase, testCase.matchingPath, variant.String()))
		}
		highestPrecedence, err := patterns.HighestPrecedencePattern(variants, testCase.matchingPath)
		c.Check(err, IsNil, Commentf("Error occurred during test case %d:\n%+v\nerror: %v", i, testCase, err))
		if err != nil {
			continue
		}
		c.Check(highestPrecedence.String(), Equals, testCase.highestPrecedence, Commentf("Highest precedence pattern incorrect for test case %d:\n%+v", i, testCase))
		highestPrecedence, err = patterns.HighestPrecedencePattern(variantsReversed, testCase.matchingPath)
		c.Check(err, IsNil, Commentf("Error occurred during test case %d:\n%+v\nerror: %v", i, testCase, err))
		if err != nil {
			continue
		}
		c.Check(highestPrecedence.String(), Equals, testCase.highestPrecedence, Commentf("Highest precedence pattern incorrect for reversed test case %d:\n%+v", i, testCase))
	}
}

func (s *patternsSuite) TestHighestPrecedencePatternOrdered(c *C) {
	matchingPath := "/foo/bar/baz/myfile.txt"
	orderedPatterns := []string{
		"/foo/bar/baz/myfile.txt",
		"/foo/bar/baz/m?file.*",
		"/foo/bar/baz/m*file.txt",
		"/foo/bar/baz/m*file*",
		"/foo/bar/baz/*",
		"/foo/bar/*/myfile.txt",
		"/foo/bar/*/myfile*",
		"/foo/bar/*/*.txt",
		"/foo/bar/*/*",
		"/foo/bar/**/baz/myfile.txt",
		"/foo/bar/**/baz/*.txt",
		"/foo/bar/**/baz/*",
		"/foo/bar/**/myfile.txt",
		"/foo/bar/**",
		"/foo/ba*r/baz/myfile.txt",
		"/foo/b?r/baz/myfile.txt",
		"/foo/b*r/baz/myfile.txt",
		"/foo/?*/baz/myfile.txt",
		"/foo/?*/**",
		"/foo/*/baz/myfile.txt",
		"/foo/*/baz/*",
		"/foo/*/*/*",
		"/foo/*/**/baz/myfile.txt",
		"/foo/**/bar/baz/myfile.txt",
		"/foo/**/baz/myfile.txt",
		"/foo/**/baz/**",
		"/foo/**/myfile.txt",
		"/**/foo/bar/baz/myfile.txt",
		"/**/myfile.txt",
		"/**",
	}
	for i := 0; i < len(orderedPatterns); i++ {
		window := orderedPatterns[i:]
		variants := make([]patterns.PatternVariant, 0, len(window))
		for _, pattern := range window {
			variant, err := patterns.ParsePatternVariant(pattern)
			c.Assert(err, IsNil, Commentf("pattern: %s", pattern))
			variants = append(variants, variant)
		}
		result, err := patterns.HighestPrecedencePattern(variants, matchingPath)
		c.Assert(err, IsNil, Commentf("Error occurred while computing precedence between %v: %v", window, err))
		c.Check(result.String(), Equals, window[0], Commentf("patterns: %+v", window))
	}
}

func (s *patternsSuite) TestHighestPrecedencePatternUnhappy(c *C) {
	result, err := patterns.HighestPrecedencePattern([]patterns.PatternVariant{}, "")
	c.Check(err, Equals, patterns.ErrNoPatterns)
	c.Check(result, DeepEquals, patterns.PatternVariant{})
}
