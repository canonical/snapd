package accessrules_test

import (
	"fmt"
	"reflect"
	"testing"
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/overlord/ifacestate/apparmorprompting/accessrules"
	"github.com/snapcore/snapd/overlord/ifacestate/apparmorprompting/common"
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

func (s *accessruleSuite) TestPopulateNewAccessRule(c *C) {
	ardb, _ := accessrules.New()

	var user uint32 = 1000
	snap := "lxd"
	app := "lxc"
	outcome := common.OutcomeAllow
	lifespan := common.LifespanSession
	duration := ""
	permissions := []common.PermissionType{
		common.PermissionRead,
		common.PermissionWrite,
		common.PermissionExecute,
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
		c.Assert(err, Equals, common.ErrInvalidPathPattern)
		c.Assert(rule, IsNil)
	}
}

func (s *accessruleSuite) TestCreateDeleteAccessRuleSimple(c *C) {
	ardb, _ := accessrules.New()

	var user uint32 = 1000
	snap := "lxd"
	app := "lxc"
	pathPattern := "/home/test/Documents/**"
	outcome := common.OutcomeAllow
	lifespan := common.LifespanForever
	duration := ""
	permissions := []common.PermissionType{
		common.PermissionRead,
		common.PermissionWrite,
		common.PermissionExecute,
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
	outcome := common.OutcomeAllow
	lifespan := common.LifespanForever
	duration := ""
	permissions := []common.PermissionType{
		common.PermissionRead,
		common.PermissionWrite,
		common.PermissionExecute,
	}

	storedRule, err := ardb.CreateAccessRule(user, snap, app, pathPattern, outcome, lifespan, duration, permissions)
	c.Assert(err, IsNil)

	conflictingPermissions := permissions[:1]
	_, err = ardb.CreateAccessRule(user, snap, app, pathPattern, outcome, lifespan, duration, conflictingPermissions)
	c.Assert(err, ErrorMatches, fmt.Sprintf("^%s.*%s.*%s.*", accessrules.ErrPathPatternConflict, storedRule.ID, conflictingPermissions[0]))

	badPattern := "bad/pattern"
	_, err = ardb.CreateAccessRule(user, snap, app, badPattern, outcome, lifespan, duration, permissions)
	c.Assert(err, Equals, common.ErrInvalidPathPattern)

	badOutcome := common.OutcomeType("secret third thing")
	_, err = ardb.CreateAccessRule(user, snap, app, pathPattern, badOutcome, lifespan, duration, permissions)
	c.Assert(err, Equals, common.ErrInvalidOutcome)
}

func (s *accessruleSuite) TestModifyAccessRule(c *C) {
	ardb, _ := accessrules.New()

	var user uint32 = 1000
	snap := "lxd"
	app := "lxc"
	pathPattern := "/home/test/Documents/**"
	outcome := common.OutcomeAllow
	lifespan := common.LifespanForever
	duration := ""
	permissions := []common.PermissionType{
		common.PermissionRead,
		common.PermissionWrite,
		common.PermissionExecute,
	}

	storedRule, err := ardb.CreateAccessRule(user, snap, app, pathPattern, outcome, lifespan, duration, permissions)
	c.Assert(err, IsNil)
	c.Assert(ardb.ByID, HasLen, 1)

	conflictingPermission := common.PermissionRename

	otherPathPattern := "/home/test/Pictures/**/*.png"
	otherPermissions := []common.PermissionType{
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

	unchangedRule2, err := ardb.ModifyAccessRule(user, storedRule.ID, "", common.OutcomeUnset, common.LifespanUnset, "", nil)
	c.Assert(err, IsNil)
	// Timestamp should be different, the rest should be the same
	unchangedRule2.Timestamp = storedRule.Timestamp
	c.Assert(unchangedRule2, DeepEquals, storedRule)
	c.Assert(ardb.ByID, HasLen, 2)

	newPathPattern := otherPathPattern
	newOutcome := common.OutcomeDeny
	newLifespan := common.LifespanTimespan
	newDuration := "1s"
	newPermissions := []common.PermissionType{common.PermissionAppend}
	modifiedRule, err := ardb.ModifyAccessRule(user, storedRule.ID, newPathPattern, newOutcome, newLifespan, newDuration, newPermissions)
	c.Assert(err, IsNil)
	c.Assert(ardb.ByID, HasLen, 2)

	badPathPattern := "bad/pattern"
	output, err := ardb.ModifyAccessRule(user, storedRule.ID, badPathPattern, outcome, lifespan, duration, permissions)
	c.Assert(err, Equals, common.ErrInvalidPathPattern)
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
	outcome := common.OutcomeAllow
	lifespan := common.LifespanForever
	duration := ""
	permissions := []common.PermissionType{
		common.PermissionRead,
		common.PermissionWrite,
		common.PermissionExecute,
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
	outcome := common.OutcomeAllow
	lifespan := common.LifespanForever
	duration := ""
	permissions := []common.PermissionType{
		common.PermissionRead,
		common.PermissionWrite,
		common.PermissionExecute,
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
	outcome := common.OutcomeAllow
	lifespan := common.LifespanForever
	duration := ""
	permissions := []common.PermissionType{
		common.PermissionRead,
		common.PermissionWrite,
		common.PermissionExecute,
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
	outcome := common.OutcomeAllow
	lifespan := common.LifespanForever
	duration := ""
	permissions := []common.PermissionType{
		common.PermissionRead,
		common.PermissionWrite,
		common.PermissionExecute,
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
	lifespan := common.LifespanForever
	duration := ""
	permissions := []common.PermissionType{
		common.PermissionRead,
		common.PermissionWrite,
		common.PermissionExecute,
	}

	patterns := make(map[string]common.OutcomeType)
	patterns["/home/test/Documents/**"] = common.OutcomeAllow
	patterns["/home/test/Documents/foo/**"] = common.OutcomeDeny
	patterns["/home/test/Documents/foo/bar/**"] = common.OutcomeAllow
	patterns["/home/test/Documents/foo/bar/baz/**"] = common.OutcomeDeny
	patterns["/home/test/**/*.png"] = common.OutcomeAllow
	patterns["/home/test/**/*.jpg"] = common.OutcomeDeny

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
	permissions := []common.PermissionType{
		common.PermissionRead,
		common.PermissionWrite,
		common.PermissionExecute,
	}

	pathPattern := "/home/test/**"
	outcome := common.OutcomeAllow
	lifespan := common.LifespanSingle
	duration := ""
	_, err := ardb.CreateAccessRule(user, snap, app, pathPattern, outcome, lifespan, duration, permissions)
	c.Assert(err, IsNil)

	pathPattern = "/home/test/Pictures/**"
	outcome = common.OutcomeDeny
	lifespan = common.LifespanTimespan
	duration = "2s"
	_, err = ardb.CreateAccessRule(user, snap, app, pathPattern, outcome, lifespan, duration, permissions)
	c.Assert(err, IsNil)

	pathPattern = "/home/test/Pictures/**/*.png"
	outcome = common.OutcomeAllow
	lifespan = common.LifespanTimespan
	duration = "1s"
	_, err = ardb.CreateAccessRule(user, snap, app, pathPattern, outcome, lifespan, duration, permissions)
	c.Assert(err, IsNil)

	path1 := "/home/test/Pictures/img.png"
	path2 := "/home/test/Pictures/img.jpg"

	allowed, err := ardb.IsPathAllowed(user, snap, app, path1, common.PermissionRead)
	c.Assert(err, IsNil)
	c.Assert(allowed, Equals, true)
	allowed, err = ardb.IsPathAllowed(user, snap, app, path2, common.PermissionRead)
	c.Assert(err, IsNil)
	c.Assert(allowed, Equals, false)

	time.Sleep(time.Second)

	allowed, err = ardb.IsPathAllowed(user, snap, app, path1, common.PermissionRead)
	c.Assert(err, IsNil)
	c.Assert(allowed, Equals, false)
	allowed, err = ardb.IsPathAllowed(user, snap, app, path2, common.PermissionRead)
	c.Assert(err, IsNil)
	c.Assert(allowed, Equals, false)

	time.Sleep(time.Second)

	allowed, err = ardb.IsPathAllowed(user, snap, app, path1, common.PermissionRead)
	c.Assert(err, Equals, nil)
	c.Assert(allowed, Equals, true)

	allowed, err = ardb.IsPathAllowed(user, snap, app, path2, common.PermissionRead)
	c.Assert(err, Equals, accessrules.ErrNoMatchingRule)
	c.Assert(allowed, Equals, false)

	allowed, err = ardb.IsPathAllowed(user, snap, app, path1, common.PermissionRead)
	c.Assert(err, Equals, accessrules.ErrNoMatchingRule)
	c.Assert(allowed, Equals, false)
	allowed, err = ardb.IsPathAllowed(user, snap, app, path2, common.PermissionRead)
	c.Assert(err, Equals, accessrules.ErrNoMatchingRule)
	c.Assert(allowed, Equals, false)
}

func (s *accessruleSuite) TestRulesLookup(c *C) {
	ardb, _ := accessrules.New()

	var origUser uint32 = 1000
	snap := "lxd"
	app := "lxc"
	pathPattern := "/home/test/Documents/**"
	outcome := common.OutcomeAllow
	lifespan := common.LifespanForever
	duration := ""
	permissions := []common.PermissionType{
		common.PermissionRead,
		common.PermissionWrite,
		common.PermissionExecute,
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
