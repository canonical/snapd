package accessrules_test

import (
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/prompting/accessrules"
)

func Test(t *testing.T) { TestingT(t) }

type accessruleSuite struct {
	tmpdir string
}

var _ = Suite(&accessruleSuite{})

func (s *accessruleSuite) SetUpTest(c *C) {
	s.tmpdir = c.MkDir()
	dirs.SetRootDir(s.tmpdir)
}

func (s *accessruleSuite) TestPermissionsListContains(c *C) {
	permissionsList := []accessrules.PermissionType{
		accessrules.PermissionExecute,
		accessrules.PermissionWrite,
		accessrules.PermissionRead,
		accessrules.PermissionAppend,
		accessrules.PermissionOpen,
	}
	for _, perm := range []accessrules.PermissionType{
		accessrules.PermissionExecute,
		accessrules.PermissionWrite,
		accessrules.PermissionRead,
		accessrules.PermissionAppend,
		accessrules.PermissionOpen,
	} {
		c.Check(accessrules.PermissionsListContains(permissionsList, perm), Equals, true)
	}
	for _, perm := range []accessrules.PermissionType{
		accessrules.PermissionCreate,
		accessrules.PermissionDelete,
		accessrules.PermissionRename,
		accessrules.PermissionChangeOwner,
		accessrules.PermissionChangeGroup,
	} {
		c.Check(accessrules.PermissionsListContains(permissionsList, perm), Equals, false)
	}
}

func (s *accessruleSuite) TestPathPatternAllowed(c *C) {
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
		c.Check(accessrules.PathPatternAllowed(pattern), Equals, true, Commentf("valid path pattern `%s` was incorrectly not allowed", pattern))
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
		c.Check(accessrules.PathPatternAllowed(pattern), Equals, false, Commentf("invalid path pattern `%s` was incorrectly allowed", pattern))
	}
}

func (s *accessruleSuite) TestGetHighestPrecedencePattern(c *C) {
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
		highestPrecedence, err := accessrules.GetHighestPrecedencePattern(testCase.Patterns)
		c.Check(err, IsNil, Commentf("Error occurred during test case %d:\n%+v", i, testCase))
		c.Check(highestPrecedence, Equals, testCase.HighestPrecedence, Commentf("Highest precedence pattern incorrect for test case %d:\n%+v", i, testCase))
	}
}

func (s *accessruleSuite) TestCreateAccessRuleSimple(c *C) {
	ardb, _ := accessrules.New()

	var user uint32 = 1000
	snap := "lxd"
	app := "lxc"
	pathPattern := "/home/test/Documents/**"
	outcome := "allow"
	lifespan := accessrules.LifespanForever
	duration := ""
	permissions := []accessrules.PermissionType{
		accessrules.PermissionRead,
		accessrules.PermissionWrite,
		accessrules.PermissionExecute,
	}

	accessRule, err := ardb.CreateAccessRule(user, snap, app, pathPattern, outcome, lifespan, duration, permissions)
	c.Assert(err, IsNil)

	c.Assert(len(ardb.ById), Equals, 1)
	storedRule, exists := ardb.ById[accessRule.Id]
	c.Assert(exists, Equals, true)
	c.Assert(storedRule, DeepEquals, accessRule)

	c.Assert(len(ardb.PerUser), Equals, 1)

	userEntry, exists := ardb.PerUser[user]
	c.Assert(exists, Equals, true)
	c.Assert(len(userEntry.PerSnap), Equals, 1)

	snapEntry, exists := userEntry.PerSnap[snap]
	c.Assert(exists, Equals, true)
	c.Assert(len(snapEntry.PerApp), Equals, 1)

	appEntry, exists := snapEntry.PerApp[app]
	c.Assert(exists, Equals, true)
	c.Assert(len(appEntry.PerPermission), Equals, 3)

	for _, permission := range permissions {
		permissionEntry, exists := appEntry.PerPermission[permission]
		c.Assert(exists, Equals, true)
		c.Assert(len(permissionEntry.PathRules), Equals, 1)

		pathId, exists := permissionEntry.PathRules[pathPattern]
		c.Assert(exists, Equals, true)
		c.Assert(pathId, Equals, accessRule.Id)
	}
}

func (s *accessruleSuite) TestRefreshTreeEnforceConsistencySimple(c *C) {
	ardb, _ := accessrules.New()

	var user uint32 = 1000
	snap := "lxd"
	app := "lxc"
	pathPattern := "/home/test/Documents/**"
	outcome := "allow"
	lifespan := accessrules.LifespanForever
	duration := ""
	permissions := []accessrules.PermissionType{
		accessrules.PermissionRead,
		accessrules.PermissionWrite,
		accessrules.PermissionExecute,
	}

	accessRule := ardb.PopulateNewAccessRule(user, snap, app, pathPattern, outcome, lifespan, duration, permissions)
	ardb.ById[accessRule.Id] = accessRule
	ardb.RefreshTreeEnforceConsistency()

	c.Assert(len(ardb.PerUser), Equals, 1)

	userEntry, exists := ardb.PerUser[user]
	c.Assert(exists, Equals, true)
	c.Assert(len(userEntry.PerSnap), Equals, 1)

	snapEntry, exists := userEntry.PerSnap[snap]
	c.Assert(exists, Equals, true)
	c.Assert(len(snapEntry.PerApp), Equals, 1)

	appEntry, exists := snapEntry.PerApp[app]
	c.Assert(exists, Equals, true)
	c.Assert(len(appEntry.PerPermission), Equals, 3)

	for _, permission := range permissions {
		permissionEntry, exists := appEntry.PerPermission[permission]
		c.Assert(exists, Equals, true)
		c.Assert(len(permissionEntry.PathRules), Equals, 1)

		pathId, exists := permissionEntry.PathRules[pathPattern]
		c.Assert(exists, Equals, true)
		c.Assert(pathId, Equals, accessRule.Id)
	}
}
