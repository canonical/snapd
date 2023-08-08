package common_test

import (
	"testing"

	. "gopkg.in/check.v1"

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

func (s *commonSuite) TestValidatePathPattern(c *C) {
	for _, pattern := range []string{
		"/",
		"/*",
		"/**",
		"/**/*.txt",
		"/foo",
		"/foo",
		"/foo/file.txt",
		"/foo/bar",
		"/foo/bar/*",
		"/foo/bar/*.tar.gz",
		"/foo/bar/**",
		"/foo/bar/**/*.zip",
	} {
		c.Check(common.ValidatePathPattern(pattern), IsNil, Commentf("valid path pattern `%s` was incorrectly not allowed", pattern))
	}

	for _, pattern := range []string{
		"file.txt",
		"/**/*",
		"/foo/*/bar",
		"/foo/**/bar",
		"/foo/bar/",
		"/foo/bar*",
		"/foo/bar*.txt",
		"/foo/bar**",
		"/foo/bar/*txt",
		"/foo/bar/**.txt",
		"/foo/bar/*/file.txt",
		"/foo/bar/**/file.txt",
		"/foo/bar/**/*",
		"/foo/bar/**/*txt",
	} {
		c.Check(common.ValidatePathPattern(pattern), Equals, common.ErrInvalidPathPattern, Commentf("invalid path pattern `%s` was incorrectly allowed", pattern))
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
				"/foo/**",
				"/foo/bar",
			},
			"/foo/bar",
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
				"/foo/**/*.txt",
				"/foo/bar/**",
			},
			"/foo/**/*.txt",
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
			"/foo/**/*.tar.gz",
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
