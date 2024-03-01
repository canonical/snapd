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
			`/foo///bar/baz/***.txt`,
			[]string{`/foo/bar/baz/*.txt`},
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
			`/foo{bar`,
			`invalid path pattern: unmatched '{' character.*`,
		},
		{
			`/foo}bar`,
			`invalid path pattern: unmatched '}' character.*`,
		},
		{
			`/foo/bar\`,
			`invalid path pattern: trailing non-escaping '\\' character.*`,
		},
		{
			`/foo/bar{`,
			`invalid path pattern: trailing unescaped '{' character.*`,
		},
	} {
		result, err := common.ExpandPathPattern(testCase.pattern)
		c.Check(result, IsNil)
		c.Check(err, ErrorMatches, testCase.errStr)
	}
}

func (s *commonSuite) TestGetHighestPrecedencePattern(c *C) {
	for i, testCase := range []struct {
		Patterns          []string
		HighestPrecedence string
	}{
		{
			[]string{
				"/foo",
			},
			"/foo",
		},
		{
			[]string{
				"/foo",
				"/foo/*",
			},
			"/foo",
		},
		{
			[]string{
				"/foo",
				"/foo/**",
			},
			"/foo",
		},
		{
			[]string{
				"/foo/*",
				"/foo/**",
			},
			"/foo/*",
		},
		{
			[]string{
				"/foo/**",
				"/foo/*",
			},
			"/foo/*",
		},
		{
			[]string{
				"/foo",
				"/foo/*",
				"/foo/**",
			},
			"/foo",
		},
		{
			[]string{
				"/foo/*",
				"/foo/bar",
			},
			"/foo/bar",
		},
		{
			[]string{
				"/foo/*",
				"/foo/*bar",
			},
			"/foo/*bar",
		},
		{
			[]string{
				"/foo/**",
				"/foo/bar",
			},
			"/foo/bar",
		},
		{
			[]string{
				"/foo/**",
				"/foo/**bar",
			},
			"/foo/**bar",
		},
		{
			[]string{
				"/foo/**",
				"/foo/**/bar",
			},
			"/foo/**/bar",
		},
		{
			[]string{
				"/foo/**",
				"/foo/bar/*",
			},
			"/foo/bar/*",
		},
		{
			[]string{
				"/foo/bar/**",
				"/foo/**",
			},
			"/foo/bar/**",
		},
		{
			[]string{
				"/foo/**",
				"/foo/bar/file.txt",
			},
			"/foo/bar/file.txt",
		},
		{
			[]string{
				"/foo/**",
				"/foo/**/*.tar.gz",
			},
			"/foo/**/*.tar.gz",
		},
		{
			[]string{
				"/foo/**",
				"/foo/**/bar/*.tar.gz",
			},
			"/foo/**/bar/*.tar.gz",
		},
		{
			[]string{
				"/foo/**/*.txt",
				"/foo/bar/**",
			},
			"/foo/bar/**",
		},
		{
			[]string{
				"/foo/**/*.gz",
				"/foo/**/*.tar.gz",
			},
			"/foo/**/*.tar.gz",
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
				"/foo/bar/*.gz",
				"/foo/bar/**/*.tar.gz",
			},
			"/foo/bar/*.gz",
		},
		{
			[]string{
				"/foo/bar/x/**/*.gz",
				"/foo/bar/**/*.tar.gz",
			},
			"/foo/bar/x/**/*.gz",
		},
		{
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
			// would match /foo/bar/, except '*' doesn't match '/'
			[]string{
				"/foo/bar*",
				"/foo/bar/**",
			},
			"/foo/bar/**",
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
	} {
		highestPrecedence, err := common.GetHighestPrecedencePattern(testCase.Patterns)
		c.Check(err, IsNil, Commentf("Error occurred during test case %d:\n%+v", i, testCase))
		c.Check(highestPrecedence, Equals, testCase.HighestPrecedence, Commentf("Highest precedence pattern incorrect for test case %d:\n%+v", i, testCase))
	}

	empty, err := common.GetHighestPrecedencePattern([]string{})
	c.Check(err, Equals, common.ErrNoPatterns)
	c.Check(empty, Equals, "")
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
		"/**/*",
		"/foo/bar**",
		"/foo/bar/**.txt",
		"/foo/bar/**/*",
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
	} {
		c.Check(common.ValidatePathPattern(pattern), IsNil, Commentf("valid path pattern %q was incorrectly not allowed", pattern))
	}

	for _, pattern := range []string{
		"file.txt",
		"/foo/bar{/**/*.txt",
		"/foo/bar/**/*.{txt",
		"{,/foo}",
		"{/,foo}",
		"/foo{bar,/baz}{fizz,buzz}",
		"/foo{bar,/baz}/{fizz,buzz}",
		"/foo?bar",
		"/foo/ba[rz]",
	} {
		c.Check(common.ValidatePathPattern(pattern), ErrorMatches, "invalid path pattern.*", Commentf("invalid path pattern %q was incorrectly allowed", pattern))
	}
}

func (*commonSuite) TestStripTrailingSlashes(c *C) {
	cases := []struct {
		orig     string
		stripped string
	}{
		{
			"foo",
			"foo",
		},
		{
			"foo/",
			"foo",
		},
		{
			"/foo",
			"/foo",
		},
		{
			"/foo/",
			"/foo",
		},
		{
			"/foo//",
			"/foo",
		},
		{
			"/foo///",
			"/foo",
		},
		{
			"/foo/bar",
			"/foo/bar",
		},
		{
			"/foo/bar/",
			"/foo/bar",
		},
		{
			"/foo/bar//",
			"/foo/bar",
		},
		{
			"/foo/bar///",
			"/foo/bar",
		},
	}

	for _, testCase := range cases {
		result := common.StripTrailingSlashes(testCase.orig)
		c.Check(result, Equals, testCase.stripped)
	}
}

func (*commonSuite) TestPathPatternMatches(c *C) {
	cases := []struct {
		pattern string
		path    string
		matches bool
	}{
		{
			"/home/test/Documents/foo.txt",
			"/home/test/Documents/foo.txt",
			true,
		},
		{
			"/home/test/Documents/foo",
			"/home/test/Documents/foo.txt",
			false,
		},
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
			"/home/test/Documents/*",
			"/home/test/Documents/foo.txt",
			true,
		},
		{
			"/home/test/Documents/**",
			"/home/test/Documents/foo.txt",
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
			"/home/test/Documents/**",
			"/home/test/Documents/foo/bar.tar.gz",
			true,
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
			"/home/test/*",
			"/home/test/Documents/foo/bar.tar.gz",
			false,
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
			"/foo*bar",
			"/foobar",
			true,
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
			"/foo/bar/**/*",
			"/foo/bar",
			false,
		},
		{
			"/foo/bar/**/*",
			"/foo/bar/",
			false,
		},
		{
			"/foo/bar/**/*",
			"/foo/bar/baz",
			true,
		},
		{
			"/foo/bar/**/*/",
			"/foo/bar/baz",
			false,
		},
		{
			"/foo/bar/**/*",
			"/foo/bar/baz/",
			true,
		},
		{
			"/foo/bar/**/*/",
			"/foo/bar/baz/",
			true,
		},
		{
			"/foo/bar/**/*",
			"/foo/bar/baz/fizz",
			true,
		},
		{
			"/foo/bar/**/*/",
			"/foo/bar/baz/fizz",
			false,
		},
		{
			"/foo/bar/**/*.txt",
			"/foo/bar/baz.txt",
			true,
		},
	}
	for _, testCase := range cases {
		matches, err := common.PathPatternMatches(testCase.pattern, testCase.path)
		c.Check(err, IsNil, Commentf("test case: %+v", testCase))
		c.Check(matches, Equals, testCase.matches, Commentf("test case: %+v", testCase))
	}
}

func (s *commonSuite) TestPathPatternMatchesUnhappy(c *C) {
	badPattern := `badpattern\`
	matches, err := common.PathPatternMatches(badPattern, "foo")
	c.Check(err, Equals, doublestar.ErrBadPattern)
	c.Check(matches, Equals, false)
}
