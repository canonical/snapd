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

func (s *accessruleSuite) TestRefreshTreeEnforceConsistencyComplex(c *C) {
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

	// create a rule with the earliest timestamp, which will be totally overwritten when attempting to add later
	earliestRule := ardb.PopulateNewAccessRule(user, snap, app, pathPattern, outcome, lifespan, duration, permissions)

	initialRule, err := ardb.CreateAccessRule(user, snap, app, pathPattern, outcome, lifespan, duration, permissions)
	c.Assert(err, IsNil)
	c.Assert(len(initialRule.Permissions), Equals, len(permissions))

	// overwrite all but the first permission
	newRulePermissions := permissions[1:]
	newRule := ardb.PopulateNewAccessRule(user, snap, app, pathPattern, outcome, lifespan, duration, newRulePermissions)

	ardb.ById[newRule.Id] = newRule
	ardb.ById[earliestRule.Id] = earliestRule
	ardb.RefreshTreeEnforceConsistency()

	c.Assert(len(ardb.ById), Equals, 2, Commentf("ardb.ById: %+v", ardb.ById))

	_, exists := ardb.ById[earliestRule.Id]
	c.Assert(exists, Equals, false)

	initialRuleRet, exists := ardb.ById[initialRule.Id]
	c.Assert(exists, Equals, true)
	c.Assert(initialRuleRet.Permissions, DeepEquals, permissions[:1])

	newRuleRet, exists := ardb.ById[newRule.Id]
	c.Assert(exists, Equals, true)
	c.Assert(newRuleRet.Permissions, DeepEquals, newRulePermissions, Commentf("newRulePermissions: %+v", newRulePermissions))

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

	for i, permission := range permissions {
		permissionEntry, exists := appEntry.PerPermission[permission]
		c.Assert(exists, Equals, true)
		c.Assert(len(permissionEntry.PathRules), Equals, 1)

		pathId, exists := permissionEntry.PathRules[pathPattern]
		c.Assert(exists, Equals, true)
		if i == 0 {
			c.Assert(pathId, Equals, initialRule.Id)
		} else {
			c.Assert(pathId, Equals, newRule.Id)
		}
	}
}

func (s *accessruleSuite) TestIsPathAllowed(c *C) {
	ardb, _ := accessrules.New()

	var user uint32 = 1000
	snap := "lxd"
	app := "lxc"
	lifespan := accessrules.LifespanForever
	duration := ""
	permissions := []accessrules.PermissionType{
		accessrules.PermissionRead,
		accessrules.PermissionWrite,
		accessrules.PermissionExecute,
	}

	patterns := make(map[string]string)
	patterns["/home/test/Documents/**"] = "allow"
	patterns["/home/test/Documents/foo/**"] = "deny"
	patterns["/home/test/Documents/foo/bar/**"] = "allow"
	patterns["/home/test/Documents/foo/bar/baz/**"] = "deny"
	patterns["/home/test/**/*.png"] = "allow"
	patterns["/home/test/**/*.jpg"] = "deny"

	for pattern, outcome := range patterns {
		_, err := ardb.CreateAccessRule(user, snap, app, pattern, outcome, lifespan, duration, permissions)
		c.Assert(err, IsNil)
	}

	cases := make(map[string]bool)
	cases["/home/test/Documents"] = true
	cases["/home/test/Documents/file"] = true
	cases["/home/test/Documents/foo"] = false
	cases["/home/test/Documents/foo/file"] = false
	cases["/home/test/Documents/foo/bar"] = true
	cases["/home/test/Documents/foo/bar/file"] = true
	cases["/home/test/Documents/foo/bar/baz"] = false
	cases["/home/test/Documents/foo/bar/baz/file"] = false
	cases["/home/test/file.png"] = true
	cases["/home/test/file.jpg"] = false
	cases["/home/test/Documents/file.png"] = true
	cases["/home/test/Documents/file.jpg"] = false
	cases["/home/test/Documents/foo/file.png"] = true
	cases["/home/test/Documents/foo/file.jpg"] = false
	cases["/home/test/Documents/foo/bar/file.png"] = true
	cases["/home/test/Documents/foo/bar/file.jpg"] = false
	cases["/home/test/Documents/foo/bar/baz/file.png"] = true
	cases["/home/test/Documents/foo/bar/baz/file.jpg"] = false
	cases["/home/test/Documents.png"] = true
	cases["/home/test/Documents.jpg"] = false
	cases["/home/test/Documents/foo.png"] = true
	cases["/home/test/Documents/foo.jpg"] = false
	cases["/home/test/Documents/foo.txt"] = true
	cases["/home/test/Documents/foo/bar.png"] = true
	cases["/home/test/Documents/foo/bar.jpg"] = false
	cases["/home/test/Documents/foo/bar.txt"] = false
	cases["/home/test/Documents/foo/bar/baz.png"] = true
	cases["/home/test/Documents/foo/bar/baz.jpg"] = false
	cases["/home/test/Documents/foo/bar/baz.png"] = true
	cases["/home/test/Documents/foo/bar/baz/file.png"] = true
	cases["/home/test/Documents/foo/bar/baz/file.jpg"] = false
	cases["/home/test/Documents/foo/bar/baz/file.txt"] = false
	cases["/home/test/file.jpg.png"] = true
	cases["/home/test/file.png.jpg"] = false

	for path, expected := range cases {
		result, err := ardb.IsPathAllowed(user, snap, app, path, permissions[0])
		c.Assert(err, IsNil, Commentf("path: %s: error: %v\nardb.ById: %+v", path, err, ardb.ById))
		c.Assert(result, Equals, expected, Commentf("path: %s: expected %b but got %b\nardb.ById: %+v", path, expected, result, ardb.ById))
	}
}
