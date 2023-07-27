package accessrules_test

import (
	"fmt"
	"testing"
	"time"

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

func (s *accessruleSuite) TestValidatePathPattern(c *C) {
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
		c.Check(accessrules.ValidatePathPattern(pattern), IsNil, Commentf("valid path pattern `%s` was incorrectly not allowed", pattern))
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
		c.Check(accessrules.ValidatePathPattern(pattern), Equals, accessrules.ErrInvalidPathPattern, Commentf("invalid path pattern `%s` was incorrectly allowed", pattern))
	}
}

func (s *accessruleSuite) TestValidateOutcome(c *C) {
	c.Assert(accessrules.ValidateOutcome(accessrules.OutcomeAllow), Equals, nil)
	c.Assert(accessrules.ValidateOutcome(accessrules.OutcomeDeny), Equals, nil)
	c.Assert(accessrules.ValidateOutcome(accessrules.OutcomeUnset), Equals, accessrules.ErrInvalidOutcome)
	c.Assert(accessrules.ValidateOutcome(accessrules.OutcomeType("foo")), Equals, accessrules.ErrInvalidOutcome)
}

func (s *accessruleSuite) TestValidateLifespanParseDuration(c *C) {
	unsetDuration := ""
	sampleDuration := "10m"
	sampleDurationAsTime, err := time.ParseDuration(sampleDuration)
	c.Assert(err, IsNil)

	for _, lifespan := range []accessrules.LifespanType{
		accessrules.LifespanForever,
		accessrules.LifespanSession,
		accessrules.LifespanSingle,
	} {
		expiration, err := accessrules.ValidateLifespanParseDuration(lifespan, unsetDuration)
		c.Check(expiration, Equals, "")
		c.Check(err, IsNil)
		expiration, err = accessrules.ValidateLifespanParseDuration(lifespan, sampleDuration)
		c.Check(expiration, Equals, "")
		c.Check(err, Equals, accessrules.ErrInvalidDuration)
	}

	expiration, err := accessrules.ValidateLifespanParseDuration(accessrules.LifespanTimespan, unsetDuration)
	c.Check(expiration, Equals, "")
	c.Check(err, ErrorMatches, fmt.Sprintf("^%s: .*", accessrules.ErrInvalidDuration))

	expiration, err = accessrules.ValidateLifespanParseDuration(accessrules.LifespanTimespan, sampleDuration)
	c.Check(err, Equals, nil)
	expirationTime, err := time.Parse(time.RFC3339, expiration)
	c.Check(err, IsNil)
	c.Check(expirationTime.After(time.Now()), Equals, true)
	c.Check(expirationTime.Before(time.Now().Add(sampleDurationAsTime)), Equals, true)
}

func (s *accessruleSuite) TestPopulateNewAccessRule(c *C) {
	ardb, _ := accessrules.New()

	var user uint32 = 1000
	snap := "lxd"
	app := "lxc"
	outcome := accessrules.OutcomeAllow
	lifespan := accessrules.LifespanSession
	duration := ""
	permissions := []accessrules.PermissionType{
		accessrules.PermissionRead,
		accessrules.PermissionWrite,
		accessrules.PermissionExecute,
	}

	for _, pattern := range []string{
		"/home/test/Documents/**",
		"/home/test/**/*.pdf",
		"/home/test/*",
	} {
		rule, err := ardb.PopulateNewAccessRule(user, snap, app, pattern, outcome, lifespan, duration, permissions)
		c.Check(err, IsNil)
		c.Check(rule.User, Equals, user)
		c.Check(rule.Snap, Equals, snap)
		c.Check(rule.App, Equals, app)
		c.Check(rule.PathPattern, Equals, pattern)
		c.Check(rule.Outcome, Equals, outcome)
		c.Check(rule.Lifespan, Equals, lifespan)
		c.Check(rule.Expiration, Equals, "")
		c.Check(rule.Permissions, DeepEquals, permissions)
	}

	for _, pattern := range []string{
		"/home/test/**/foo.conf",
		"/home/test/*/**",
		"/home/test/*/*.txt",
	} {
		rule, err := ardb.PopulateNewAccessRule(user, snap, app, pattern, outcome, lifespan, duration, permissions)
		c.Assert(err, Equals, accessrules.ErrInvalidPathPattern)
		c.Assert(rule, IsNil)
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
	outcome := accessrules.OutcomeAllow
	lifespan := accessrules.LifespanForever
	duration := ""
	permissions := []accessrules.PermissionType{
		accessrules.PermissionRead,
		accessrules.PermissionWrite,
		accessrules.PermissionExecute,
	}

	accessRule, err := ardb.CreateAccessRule(user, snap, app, pathPattern, outcome, lifespan, duration, permissions)
	c.Assert(err, IsNil)

	c.Assert(ardb.ById, HasLen, 1)
	storedRule, exists := ardb.ById[accessRule.Id]
	c.Assert(exists, Equals, true)
	c.Assert(storedRule, DeepEquals, accessRule)

	c.Assert(ardb.PerUser, HasLen, 1)

	userEntry, exists := ardb.PerUser[user]
	c.Assert(exists, Equals, true)
	c.Assert(userEntry.PerSnap, HasLen, 1)

	snapEntry, exists := userEntry.PerSnap[snap]
	c.Assert(exists, Equals, true)
	c.Assert(snapEntry.PerApp, HasLen, 1)

	appEntry, exists := snapEntry.PerApp[app]
	c.Assert(exists, Equals, true)
	c.Assert(appEntry.PerPermission, HasLen, 3)

	for _, permission := range permissions {
		permissionEntry, exists := appEntry.PerPermission[permission]
		c.Assert(exists, Equals, true)
		c.Assert(permissionEntry.PathRules, HasLen, 1)

		pathId, exists := permissionEntry.PathRules[pathPattern]
		c.Assert(exists, Equals, true)
		c.Assert(pathId, Equals, accessRule.Id)
	}
}

func (s *accessruleSuite) TestRuleWithId(c *C) {
	ardb, _ := accessrules.New()

	var user uint32 = 1000
	snap := "lxd"
	app := "lxc"
	pathPattern := "/home/test/Documents/**"
	outcome := accessrules.OutcomeAllow
	lifespan := accessrules.LifespanForever
	duration := ""
	permissions := []accessrules.PermissionType{
		accessrules.PermissionRead,
		accessrules.PermissionWrite,
		accessrules.PermissionExecute,
	}

	newRule, err := ardb.CreateAccessRule(user, snap, app, pathPattern, outcome, lifespan, duration, permissions)
	c.Assert(err, IsNil)
	c.Assert(newRule, NotNil)

	accessedRule, err := ardb.RuleWithId(user, newRule.Id)
	c.Check(err, IsNil)
	c.Check(accessedRule, DeepEquals, newRule)

	accessedRule, err = ardb.RuleWithId(user, "nonexistent")
	c.Check(err, Equals, accessrules.ErrRuleIdNotFound)
	c.Check(accessedRule, IsNil)

	accessedRule, err = ardb.RuleWithId(user+1, newRule.Id)
	c.Check(err, Equals, accessrules.ErrUserNotAllowed)
	c.Check(accessedRule, IsNil)
}

func (s *accessruleSuite) TestRefreshTreeEnforceConsistencySimple(c *C) {
	ardb, _ := accessrules.New()

	var user uint32 = 1000
	snap := "lxd"
	app := "lxc"
	pathPattern := "/home/test/Documents/**"
	outcome := accessrules.OutcomeAllow
	lifespan := accessrules.LifespanForever
	duration := ""
	permissions := []accessrules.PermissionType{
		accessrules.PermissionRead,
		accessrules.PermissionWrite,
		accessrules.PermissionExecute,
	}

	accessRule, err := ardb.PopulateNewAccessRule(user, snap, app, pathPattern, outcome, lifespan, duration, permissions)
	c.Assert(err, IsNil)
	ardb.ById[accessRule.Id] = accessRule
	ardb.RefreshTreeEnforceConsistency()

	c.Assert(ardb.PerUser, HasLen, 1)

	userEntry, exists := ardb.PerUser[user]
	c.Assert(exists, Equals, true)
	c.Assert(userEntry.PerSnap, HasLen, 1)

	snapEntry, exists := userEntry.PerSnap[snap]
	c.Assert(exists, Equals, true)
	c.Assert(snapEntry.PerApp, HasLen, 1)

	appEntry, exists := snapEntry.PerApp[app]
	c.Assert(exists, Equals, true)
	c.Assert(appEntry.PerPermission, HasLen, 3)

	for _, permission := range permissions {
		permissionEntry, exists := appEntry.PerPermission[permission]
		c.Assert(exists, Equals, true)
		c.Assert(permissionEntry.PathRules, HasLen, 1)

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
	outcome := accessrules.OutcomeAllow
	lifespan := accessrules.LifespanForever
	duration := ""
	permissions := []accessrules.PermissionType{
		accessrules.PermissionRead,
		accessrules.PermissionWrite,
		accessrules.PermissionExecute,
	}

	// Create two rules with bad timestamps
	badTsRule1, err := ardb.PopulateNewAccessRule(user, snap, app, pathPattern, outcome, lifespan, duration, permissions[:1])
	c.Assert(err, IsNil)
	badTsRule1.Timestamp = "bar"
	badTsRule2, err := ardb.PopulateNewAccessRule(user, snap, app, pathPattern, outcome, lifespan, duration, permissions[:1])
	c.Assert(err, IsNil)
	badTsRule2.Timestamp = "baz"
	ardb.ById[badTsRule1.Id] = badTsRule1
	ardb.ById[badTsRule2.Id] = badTsRule2

	// The former should be overwritten by RefreshTreeEnforceConsistency
	ardb.RefreshTreeEnforceConsistency()
	c.Assert(ardb.ById, HasLen, 1, Commentf("ardb.ById: %+v", ardb.ById))
	_, exists := ardb.ById[badTsRule2.Id]
	c.Assert(exists, Equals, true)
	// The latter should be overwritten by any conflicting rule which has a valid timestamp

	// Create a rule with the earliest timestamp, which will be totally overwritten when attempting to add later
	earliestRule, err := ardb.PopulateNewAccessRule(user, snap, app, pathPattern, outcome, lifespan, duration, permissions)
	c.Assert(err, IsNil)

	// Create and add a rule which will overwrite the rule with bad timestamp
	initialRule, err := ardb.PopulateNewAccessRule(user, snap, app, pathPattern, outcome, lifespan, duration, permissions)
	c.Assert(err, IsNil)
	ardb.ById[initialRule.Id] = initialRule
	ardb.RefreshTreeEnforceConsistency()

	// Check that rule with bad timestamp was overwritten
	c.Assert(initialRule.Permissions, HasLen, len(permissions))
	c.Assert(ardb.ById, HasLen, 1, Commentf("ardb.ById: %+v", ardb.ById))
	initialRuleRet, exists := ardb.ById[initialRule.Id]
	c.Assert(exists, Equals, true)
	c.Assert(initialRuleRet.Permissions, DeepEquals, permissions)

	// Create newer rule which will overwrite all but the first permission of initialRule
	newRulePermissions := permissions[1:]
	newRule, err := ardb.PopulateNewAccessRule(user, snap, app, pathPattern, outcome, lifespan, duration, newRulePermissions)
	c.Assert(err, IsNil)

	ardb.ById[newRule.Id] = newRule
	ardb.ById[earliestRule.Id] = earliestRule
	ardb.RefreshTreeEnforceConsistency()

	c.Assert(ardb.ById, HasLen, 2, Commentf("ardb.ById: %+v", ardb.ById))

	_, exists = ardb.ById[earliestRule.Id]
	c.Assert(exists, Equals, false)

	initialRuleRet, exists = ardb.ById[initialRule.Id]
	c.Assert(exists, Equals, true)
	c.Assert(initialRuleRet.Permissions, DeepEquals, permissions[:1])

	newRuleRet, exists := ardb.ById[newRule.Id]
	c.Assert(exists, Equals, true)
	c.Assert(newRuleRet.Permissions, DeepEquals, newRulePermissions, Commentf("newRulePermissions: %+v", newRulePermissions))

	c.Assert(ardb.PerUser, HasLen, 1)

	userEntry, exists := ardb.PerUser[user]
	c.Assert(exists, Equals, true)
	c.Assert(userEntry.PerSnap, HasLen, 1)

	snapEntry, exists := userEntry.PerSnap[snap]
	c.Assert(exists, Equals, true)
	c.Assert(snapEntry.PerApp, HasLen, 1)

	appEntry, exists := snapEntry.PerApp[app]
	c.Assert(exists, Equals, true)
	c.Assert(appEntry.PerPermission, HasLen, 3)

	for i, permission := range permissions {
		permissionEntry, exists := appEntry.PerPermission[permission]
		c.Assert(exists, Equals, true)
		c.Assert(permissionEntry.PathRules, HasLen, 1)

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

	patterns := make(map[string]accessrules.OutcomeType)
	patterns["/home/test/Documents/**"] = accessrules.OutcomeAllow
	patterns["/home/test/Documents/foo/**"] = accessrules.OutcomeDeny
	patterns["/home/test/Documents/foo/bar/**"] = accessrules.OutcomeAllow
	patterns["/home/test/Documents/foo/bar/baz/**"] = accessrules.OutcomeDeny
	patterns["/home/test/**/*.png"] = accessrules.OutcomeAllow
	patterns["/home/test/**/*.jpg"] = accessrules.OutcomeDeny

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

func (s *accessruleSuite) TestRuleExpiration(c *C) {
	ardb, _ := accessrules.New()

	var user uint32 = 1000
	snap := "lxd"
	app := "lxc"
	permissions := []accessrules.PermissionType{
		accessrules.PermissionRead,
		accessrules.PermissionWrite,
		accessrules.PermissionExecute,
	}

	pathPattern := "/home/test/**"
	outcome := accessrules.OutcomeAllow
	lifespan := accessrules.LifespanSingle
	duration := ""
	_, err := ardb.CreateAccessRule(user, snap, app, pathPattern, outcome, lifespan, duration, permissions)
	c.Assert(err, IsNil)

	pathPattern = "/home/test/Pictures/**"
	outcome = accessrules.OutcomeDeny
	lifespan = accessrules.LifespanTimespan
	duration = "2s"
	_, err = ardb.CreateAccessRule(user, snap, app, pathPattern, outcome, lifespan, duration, permissions)
	c.Assert(err, IsNil)

	pathPattern = "/home/test/Pictures/**/*.png"
	outcome = accessrules.OutcomeAllow
	lifespan = accessrules.LifespanTimespan
	duration = "1s"
	_, err = ardb.CreateAccessRule(user, snap, app, pathPattern, outcome, lifespan, duration, permissions)
	c.Assert(err, IsNil)

	path1 := "/home/test/Pictures/img.png"
	path2 := "/home/test/Pictures/img.jpg"

	allowed, err := ardb.IsPathAllowed(user, snap, app, path1, accessrules.PermissionRead)
	c.Assert(err, IsNil)
	c.Assert(allowed, Equals, true)
	allowed, err = ardb.IsPathAllowed(user, snap, app, path2, accessrules.PermissionRead)
	c.Assert(err, IsNil)
	c.Assert(allowed, Equals, false)

	time.Sleep(time.Second)

	allowed, err = ardb.IsPathAllowed(user, snap, app, path1, accessrules.PermissionRead)
	c.Assert(err, IsNil)
	c.Assert(allowed, Equals, false)
	allowed, err = ardb.IsPathAllowed(user, snap, app, path2, accessrules.PermissionRead)
	c.Assert(err, IsNil)
	c.Assert(allowed, Equals, false)

	time.Sleep(time.Second)

	allowed, err = ardb.IsPathAllowed(user, snap, app, path1, accessrules.PermissionRead)
	c.Assert(err, Equals, nil)
	c.Assert(allowed, Equals, true)

	allowed, err = ardb.IsPathAllowed(user, snap, app, path2, accessrules.PermissionRead)
	c.Assert(err, Equals, accessrules.ErrNoMatchingRule)
	c.Assert(allowed, Equals, false)

	allowed, err = ardb.IsPathAllowed(user, snap, app, path1, accessrules.PermissionRead)
	c.Assert(err, Equals, accessrules.ErrNoMatchingRule)
	c.Assert(allowed, Equals, false)
	allowed, err = ardb.IsPathAllowed(user, snap, app, path2, accessrules.PermissionRead)
	c.Assert(err, Equals, accessrules.ErrNoMatchingRule)
	c.Assert(allowed, Equals, false)
}
