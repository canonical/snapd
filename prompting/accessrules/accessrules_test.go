package accessrules_test

import (
	"fmt"
	"reflect"
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
	unsetDuration := 0
	sampleDuration := 600
	sampleDurationAsTime := time.Duration(sampleDuration) * time.Second

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
	c.Check(err, Equals, accessrules.ErrInvalidDuration)

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
	duration := 0
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

func (s *accessruleSuite) TestCreateDeleteAccessRuleSimple(c *C) {
	ardb, _ := accessrules.New()

	var user uint32 = 1000
	snap := "lxd"
	app := "lxc"
	pathPattern := "/home/test/Documents/**"
	outcome := accessrules.OutcomeAllow
	lifespan := accessrules.LifespanForever
	duration := 0
	permissions := []accessrules.PermissionType{
		accessrules.PermissionRead,
		accessrules.PermissionWrite,
		accessrules.PermissionExecute,
	}

	accessRule, err := ardb.CreateAccessRule(user, snap, app, pathPattern, outcome, lifespan, duration, permissions)
	c.Assert(err, IsNil)

	c.Assert(ardb.ByID, HasLen, 1)
	storedRule, exists := ardb.ByID[accessRule.ID]
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

		pathID, exists := permissionEntry.PathRules[pathPattern]
		c.Assert(exists, Equals, true)
		c.Assert(pathID, Equals, accessRule.ID)
	}

	deletedRule, err := ardb.DeleteAccessRule(user, accessRule.ID)
	c.Assert(err, IsNil)
	c.Assert(deletedRule, DeepEquals, accessRule)

	c.Assert(ardb.ByID, HasLen, 0)
	c.Assert(ardb.PerUser, HasLen, 1)

	userEntry, exists = ardb.PerUser[user]
	c.Assert(exists, Equals, true)
	c.Assert(userEntry.PerSnap, HasLen, 1)

	snapEntry, exists = userEntry.PerSnap[snap]
	c.Assert(exists, Equals, true)
	c.Assert(snapEntry.PerApp, HasLen, 1)

	appEntry, exists = snapEntry.PerApp[app]
	c.Assert(exists, Equals, true)
	c.Assert(appEntry.PerPermission, HasLen, 3)

	for _, permission := range permissions {
		permissionEntry, exists := appEntry.PerPermission[permission]
		c.Assert(exists, Equals, true)
		c.Assert(permissionEntry.PathRules, HasLen, 0)
	}
}

func (s *accessruleSuite) TestCreateAccessRuleUnhappy(c *C) {
	ardb, _ := accessrules.New()

	var user uint32 = 1000
	snap := "lxd"
	app := "lxc"
	pathPattern := "/home/test/Documents/**"
	outcome := accessrules.OutcomeAllow
	lifespan := accessrules.LifespanForever
	duration := 0
	permissions := []accessrules.PermissionType{
		accessrules.PermissionRead,
		accessrules.PermissionWrite,
		accessrules.PermissionExecute,
	}

	storedRule, err := ardb.CreateAccessRule(user, snap, app, pathPattern, outcome, lifespan, duration, permissions)
	c.Assert(err, IsNil)

	conflictingPermissions := permissions[:1]
	_, err = ardb.CreateAccessRule(user, snap, app, pathPattern, outcome, lifespan, duration, conflictingPermissions)
	c.Assert(err, ErrorMatches, fmt.Sprintf("^%s.*%s.*%s.*", accessrules.ErrPathPatternConflict, storedRule.ID, conflictingPermissions[0]))

	badPattern := "bad/pattern"
	_, err = ardb.CreateAccessRule(user, snap, app, badPattern, outcome, lifespan, duration, permissions)
	c.Assert(err, Equals, accessrules.ErrInvalidPathPattern)

	badOutcome := accessrules.OutcomeType("secret third thing")
	_, err = ardb.CreateAccessRule(user, snap, app, pathPattern, badOutcome, lifespan, duration, permissions)
	c.Assert(err, Equals, accessrules.ErrInvalidOutcome)
}

func (s *accessruleSuite) TestModifyAccessRule(c *C) {
	ardb, _ := accessrules.New()

	var user uint32 = 1000
	snap := "lxd"
	app := "lxc"
	pathPattern := "/home/test/Documents/**"
	outcome := accessrules.OutcomeAllow
	lifespan := accessrules.LifespanForever
	duration := 0
	permissions := []accessrules.PermissionType{
		accessrules.PermissionRead,
		accessrules.PermissionWrite,
		accessrules.PermissionExecute,
	}

	storedRule, err := ardb.CreateAccessRule(user, snap, app, pathPattern, outcome, lifespan, duration, permissions)
	c.Assert(err, IsNil)
	c.Assert(ardb.ByID, HasLen, 1)

	conflictingPermission := accessrules.PermissionRename

	otherPathPattern := "/home/test/Pictures/**/*.png"
	otherPermissions := []accessrules.PermissionType{
		conflictingPermission,
	}
	otherRule, err := ardb.CreateAccessRule(user, snap, app, otherPathPattern, outcome, lifespan, duration, otherPermissions)
	c.Assert(err, IsNil)
	c.Assert(ardb.ByID, HasLen, 2)

	unchangedRule1, err := ardb.ModifyAccessRule(user, storedRule.ID, pathPattern, outcome, lifespan, duration, permissions)
	c.Assert(err, IsNil)
	// Timestamp should be different, the rest should be the same
	unchangedRule1.Timestamp = storedRule.Timestamp
	c.Assert(unchangedRule1, DeepEquals, storedRule)
	c.Assert(ardb.ByID, HasLen, 2)

	unchangedRule2, err := ardb.ModifyAccessRule(user, storedRule.ID, "", accessrules.OutcomeUnset, accessrules.LifespanUnset, 0, nil)
	c.Assert(err, IsNil)
	// Timestamp should be different, the rest should be the same
	unchangedRule2.Timestamp = storedRule.Timestamp
	c.Assert(unchangedRule2, DeepEquals, storedRule)
	c.Assert(ardb.ByID, HasLen, 2)

	newPathPattern := otherPathPattern
	newOutcome := accessrules.OutcomeDeny
	newLifespan := accessrules.LifespanTimespan
	newDuration := 1
	newPermissions := []accessrules.PermissionType{accessrules.PermissionAppend}
	modifiedRule, err := ardb.ModifyAccessRule(user, storedRule.ID, newPathPattern, newOutcome, newLifespan, newDuration, newPermissions)
	c.Assert(err, IsNil)
	c.Assert(ardb.ByID, HasLen, 2)

	badPathPattern := "bad/pattern"
	output, err := ardb.ModifyAccessRule(user, storedRule.ID, badPathPattern, outcome, lifespan, duration, permissions)
	c.Assert(err, Equals, accessrules.ErrInvalidPathPattern)
	c.Assert(output, IsNil)
	c.Assert(ardb.ByID, HasLen, 2)

	currentRule, exists := ardb.ByID[storedRule.ID]
	c.Assert(exists, Equals, true)
	c.Assert(currentRule, DeepEquals, modifiedRule)

	conflictingPermissions := append(newPermissions, conflictingPermission)
	output, err = ardb.ModifyAccessRule(user, storedRule.ID, newPathPattern, newOutcome, newLifespan, newDuration, conflictingPermissions)
	c.Assert(err, ErrorMatches, fmt.Sprintf("^%s.*%s.*%s.*", accessrules.ErrPathPatternConflict, otherRule.ID, conflictingPermission))
	c.Assert(output, IsNil)
	c.Assert(ardb.ByID, HasLen, 2)

	currentRule, exists = ardb.ByID[storedRule.ID]
	c.Assert(exists, Equals, true)
	c.Assert(currentRule, DeepEquals, modifiedRule)
}

func (s *accessruleSuite) TestRuleWithID(c *C) {
	ardb, _ := accessrules.New()

	var user uint32 = 1000
	snap := "lxd"
	app := "lxc"
	pathPattern := "/home/test/Documents/**"
	outcome := accessrules.OutcomeAllow
	lifespan := accessrules.LifespanForever
	duration := 0
	permissions := []accessrules.PermissionType{
		accessrules.PermissionRead,
		accessrules.PermissionWrite,
		accessrules.PermissionExecute,
	}

	newRule, err := ardb.CreateAccessRule(user, snap, app, pathPattern, outcome, lifespan, duration, permissions)
	c.Assert(err, IsNil)
	c.Assert(newRule, NotNil)

	accessedRule, err := ardb.RuleWithID(user, newRule.ID)
	c.Check(err, IsNil)
	c.Check(accessedRule, DeepEquals, newRule)

	accessedRule, err = ardb.RuleWithID(user, "nonexistent")
	c.Check(err, Equals, accessrules.ErrRuleIDNotFound)
	c.Check(accessedRule, IsNil)

	accessedRule, err = ardb.RuleWithID(user+1, newRule.ID)
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
	duration := 0
	permissions := []accessrules.PermissionType{
		accessrules.PermissionRead,
		accessrules.PermissionWrite,
		accessrules.PermissionExecute,
	}

	accessRule, err := ardb.PopulateNewAccessRule(user, snap, app, pathPattern, outcome, lifespan, duration, permissions)
	c.Assert(err, IsNil)
	ardb.ByID[accessRule.ID] = accessRule
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

		pathID, exists := permissionEntry.PathRules[pathPattern]
		c.Assert(exists, Equals, true)
		c.Assert(pathID, Equals, accessRule.ID)
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
	duration := 0
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
	ardb.ByID[badTsRule1.ID] = badTsRule1
	ardb.ByID[badTsRule2.ID] = badTsRule2

	// The former should be overwritten by RefreshTreeEnforceConsistency
	ardb.RefreshTreeEnforceConsistency()
	c.Assert(ardb.ByID, HasLen, 1, Commentf("ardb.ByID: %+v", ardb.ByID))
	_, exists := ardb.ByID[badTsRule2.ID]
	c.Assert(exists, Equals, true)
	// The latter should be overwritten by any conflicting rule which has a valid timestamp

	// Create a rule with the earliest timestamp, which will be totally overwritten when attempting to add later
	earliestRule, err := ardb.PopulateNewAccessRule(user, snap, app, pathPattern, outcome, lifespan, duration, permissions)
	c.Assert(err, IsNil)

	// Create and add a rule which will overwrite the rule with bad timestamp
	initialRule, err := ardb.PopulateNewAccessRule(user, snap, app, pathPattern, outcome, lifespan, duration, permissions)
	c.Assert(err, IsNil)
	ardb.ByID[initialRule.ID] = initialRule
	ardb.RefreshTreeEnforceConsistency()

	// Check that rule with bad timestamp was overwritten
	c.Assert(initialRule.Permissions, HasLen, len(permissions))
	c.Assert(ardb.ByID, HasLen, 1, Commentf("ardb.ByID: %+v", ardb.ByID))
	initialRuleRet, exists := ardb.ByID[initialRule.ID]
	c.Assert(exists, Equals, true)
	c.Assert(initialRuleRet.Permissions, DeepEquals, permissions)

	// Create newer rule which will overwrite all but the first permission of initialRule
	newRulePermissions := permissions[1:]
	newRule, err := ardb.PopulateNewAccessRule(user, snap, app, pathPattern, outcome, lifespan, duration, newRulePermissions)
	c.Assert(err, IsNil)

	ardb.ByID[newRule.ID] = newRule
	ardb.ByID[earliestRule.ID] = earliestRule
	ardb.RefreshTreeEnforceConsistency()

	c.Assert(ardb.ByID, HasLen, 2, Commentf("ardb.ByID: %+v", ardb.ByID))

	_, exists = ardb.ByID[earliestRule.ID]
	c.Assert(exists, Equals, false)

	initialRuleRet, exists = ardb.ByID[initialRule.ID]
	c.Assert(exists, Equals, true)
	c.Assert(initialRuleRet.Permissions, DeepEquals, permissions[:1])

	newRuleRet, exists := ardb.ByID[newRule.ID]
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

		pathID, exists := permissionEntry.PathRules[pathPattern]
		c.Assert(exists, Equals, true)
		if i == 0 {
			c.Assert(pathID, Equals, initialRule.ID)
		} else {
			c.Assert(pathID, Equals, newRule.ID)
		}
	}
}

func (s *accessruleSuite) TestNewSaveLoad(c *C) {
	ardb, _ := accessrules.New()

	var user uint32 = 1000
	snap := "lxd"
	app := "lxc"
	pathPattern := "/home/test/Documents/**"
	outcome := accessrules.OutcomeAllow
	lifespan := accessrules.LifespanForever
	duration := 0
	permissions := []accessrules.PermissionType{
		accessrules.PermissionRead,
		accessrules.PermissionWrite,
		accessrules.PermissionExecute,
	}

	_, err := ardb.CreateAccessRule(user, snap, app, pathPattern, outcome, lifespan, duration, permissions[:1])
	c.Assert(err, IsNil)
	_, err = ardb.CreateAccessRule(user, snap, app, pathPattern, outcome, lifespan, duration, permissions[1:2])
	c.Assert(err, IsNil)
	_, err = ardb.CreateAccessRule(user, snap, app, pathPattern, outcome, lifespan, duration, permissions[2:])
	c.Assert(err, IsNil)

	loadedArdb, err := accessrules.New()
	c.Assert(err, IsNil)
	c.Assert(ardb, DeepEquals, loadedArdb)
}

func (s *accessruleSuite) TestIsPathAllowed(c *C) {
	ardb, _ := accessrules.New()

	var user uint32 = 1000
	snap := "lxd"
	app := "lxc"
	lifespan := accessrules.LifespanForever
	duration := 0
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
		c.Assert(err, IsNil, Commentf("path: %s: error: %v\nardb.ByID: %+v", path, err, ardb.ByID))
		c.Assert(result, Equals, expected, Commentf("path: %s: expected %b but got %b\nardb.ByID: %+v", path, expected, result, ardb.ByID))
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
	duration := 0
	_, err := ardb.CreateAccessRule(user, snap, app, pathPattern, outcome, lifespan, duration, permissions)
	c.Assert(err, IsNil)

	pathPattern = "/home/test/Pictures/**"
	outcome = accessrules.OutcomeDeny
	lifespan = accessrules.LifespanTimespan
	duration = 2
	_, err = ardb.CreateAccessRule(user, snap, app, pathPattern, outcome, lifespan, duration, permissions)
	c.Assert(err, IsNil)

	pathPattern = "/home/test/Pictures/**/*.png"
	outcome = accessrules.OutcomeAllow
	lifespan = accessrules.LifespanTimespan
	duration = 1
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

func (s *accessruleSuite) TestRulesLookup(c *C) {
	ardb, _ := accessrules.New()

	var origUser uint32 = 1000
	snap := "lxd"
	app := "lxc"
	pathPattern := "/home/test/Documents/**"
	outcome := accessrules.OutcomeAllow
	lifespan := accessrules.LifespanForever
	duration := 0
	permissions := []accessrules.PermissionType{
		accessrules.PermissionRead,
		accessrules.PermissionWrite,
		accessrules.PermissionExecute,
	}

	rule1, err := ardb.CreateAccessRule(origUser, snap, app, pathPattern, outcome, lifespan, duration, permissions)
	c.Assert(err, IsNil)

	user := origUser + 1
	rule2, err := ardb.CreateAccessRule(user, snap, app, pathPattern, outcome, lifespan, duration, permissions)
	c.Assert(err, IsNil)

	snap = "nextcloud"
	app = "occ"
	rule3, err := ardb.CreateAccessRule(user, snap, app, pathPattern, outcome, lifespan, duration, permissions)
	c.Assert(err, IsNil)

	app = "export"
	rule4, err := ardb.CreateAccessRule(user, snap, app, pathPattern, outcome, lifespan, duration, permissions)
	c.Assert(err, IsNil)

	origUserRules := ardb.Rules(origUser)
	c.Assert(origUserRules, HasLen, 1)
	c.Assert(origUserRules[0], DeepEquals, rule1)

	userRules := ardb.Rules(user)
	c.Assert(userRules, HasLen, 3)
OUTER_LOOP_USER:
	for _, rule := range []*accessrules.AccessRule{rule2, rule3, rule4} {
		for _, userRule := range userRules {
			if reflect.DeepEqual(rule, userRule) {
				continue OUTER_LOOP_USER
			}
		}
		c.Assert(rule, DeepEquals, userRules[2])
	}

	userSnapRules := ardb.RulesForSnap(user, snap)
	c.Assert(userSnapRules, HasLen, 2)
OUTER_LOOP_USER_SNAP:
	for _, rule := range []*accessrules.AccessRule{rule3, rule4} {
		for _, userRule := range userRules {
			if reflect.DeepEqual(rule, userRule) {
				continue OUTER_LOOP_USER_SNAP
			}
		}
		c.Assert(rule, DeepEquals, userRules[1])
	}

	userSnapAppRules := ardb.RulesForSnapApp(user, snap, app)
	c.Assert(userSnapAppRules, HasLen, 1)
	c.Assert(userSnapAppRules[0], DeepEquals, rule4)
}
