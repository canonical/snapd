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
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/strutil"
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

func (s *accessruleSuite) TestPopulateNewRule(c *C) {
	notifyRule := func(userID uint32, ruleID string, options *state.AddNoticeOptions) error {
		c.Errorf("unexpected rule notice with user %d and ID %s", userID, ruleID)
		return nil
	}
	rdb, _ := accessrules.New(notifyRule)

	var user uint32 = 1000
	snap := "lxd"
	app := "lxc"
	iface := "home"
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
		rule, err := rdb.PopulateNewRule(user, snap, app, iface, pattern, outcome, lifespan, duration, permissions)
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
		rule, err := rdb.PopulateNewRule(user, snap, app, iface, pattern, outcome, lifespan, duration, permissions)
		c.Assert(err, Equals, common.ErrInvalidPathPattern)
		c.Assert(rule, IsNil)
	}
}

func (s *accessruleSuite) TestCreateRemoveRuleSimple(c *C) {
	var user uint32 = 1000
	ruleNoticeIDs := make([]string, 0, 2)
	notifyRule := func(userID uint32, ruleID string, options *state.AddNoticeOptions) error {
		c.Check(userID, Equals, user)
		ruleNoticeIDs = append(ruleNoticeIDs, ruleID)
		return nil
	}
	rdb, _ := accessrules.New(notifyRule)

	snap := "lxd"
	app := "lxc"
	iface := "system-files"
	pathPattern := "/home/test/Documents/**"
	outcome := common.OutcomeAllow
	lifespan := common.LifespanForever
	duration := ""
	permissions := []common.PermissionType{
		common.PermissionRead,
		common.PermissionWrite,
		common.PermissionExecute,
	}

	rule, err := rdb.CreateRule(user, snap, app, iface, pathPattern, outcome, lifespan, duration, permissions)
	c.Assert(err, IsNil)

	c.Assert(ruleNoticeIDs, HasLen, 1, Commentf("ruleNoticeIDs: %v; rdb.ByID: %+v", ruleNoticeIDs, rdb.ByID))
	c.Check(ruleNoticeIDs[0], Equals, rule.ID)
	ruleNoticeIDs = ruleNoticeIDs[1:]

	c.Assert(rdb.ByID, HasLen, 1)
	storedRule, exists := rdb.ByID[rule.ID]
	c.Assert(exists, Equals, true)
	c.Assert(storedRule, DeepEquals, rule)

	c.Assert(rdb.PerUser, HasLen, 1)

	userEntry, exists := rdb.PerUser[user]
	c.Assert(exists, Equals, true)
	c.Assert(userEntry.PerSnap, HasLen, 1)

	snapEntry, exists := userEntry.PerSnap[snap]
	c.Assert(exists, Equals, true)
	c.Assert(snapEntry.PerApp, HasLen, 1)

	appEntry, exists := snapEntry.PerApp[app]
	c.Assert(exists, Equals, true)
	c.Assert(appEntry.PerInterface, HasLen, 1)

	interfaceEntry, exists := appEntry.PerInterface[iface]
	c.Assert(exists, Equals, true)
	c.Assert(interfaceEntry.PerPermission, HasLen, 3)

	for _, permission := range permissions {
		permissionEntry, exists := interfaceEntry.PerPermission[permission]
		c.Assert(exists, Equals, true)
		c.Assert(permissionEntry.PathRules, HasLen, 1)

		pathID, exists := permissionEntry.PathRules[pathPattern]
		c.Assert(exists, Equals, true)
		c.Assert(pathID, Equals, rule.ID)
	}

	removedRule, err := rdb.RemoveRule(user, rule.ID)
	c.Assert(err, IsNil)
	c.Assert(removedRule, DeepEquals, rule)

	c.Assert(ruleNoticeIDs, HasLen, 1, Commentf("ruleNoticeIDs: %v; rdb.ByID: %+v", ruleNoticeIDs, rdb.ByID))
	c.Check(ruleNoticeIDs[0], Equals, removedRule.ID)
	ruleNoticeIDs = ruleNoticeIDs[1:]

	c.Assert(rdb.ByID, HasLen, 0)
	c.Assert(rdb.PerUser, HasLen, 1)

	userEntry, exists = rdb.PerUser[user]
	c.Assert(exists, Equals, true)
	c.Assert(userEntry.PerSnap, HasLen, 1)

	snapEntry, exists = userEntry.PerSnap[snap]
	c.Assert(exists, Equals, true)
	c.Assert(snapEntry.PerApp, HasLen, 1)

	appEntry, exists = snapEntry.PerApp[app]
	c.Assert(exists, Equals, true)
	c.Assert(appEntry.PerInterface, HasLen, 1)

	interfaceEntry, exists = appEntry.PerInterface[iface]
	c.Assert(exists, Equals, true)
	c.Assert(interfaceEntry.PerPermission, HasLen, 3)

	for _, permission := range permissions {
		permissionEntry, exists := interfaceEntry.PerPermission[permission]
		c.Assert(exists, Equals, true)
		c.Assert(permissionEntry.PathRules, HasLen, 0)
	}
}

func (s *accessruleSuite) TestCreateRuleUnhappy(c *C) {
	var user uint32 = 1000
	ruleNoticeIDs := make([]string, 0, 1)
	notifyRule := func(userID uint32, ruleID string, options *state.AddNoticeOptions) error {
		c.Check(userID, Equals, user)
		ruleNoticeIDs = append(ruleNoticeIDs, ruleID)
		return nil
	}
	rdb, _ := accessrules.New(notifyRule)

	snap := "lxd"
	app := "lxc"
	iface := "home"
	pathPattern := "/home/test/Documents/**"
	outcome := common.OutcomeAllow
	lifespan := common.LifespanForever
	duration := ""
	permissions := []common.PermissionType{
		common.PermissionRead,
		common.PermissionWrite,
		common.PermissionExecute,
	}

	storedRule, err := rdb.CreateRule(user, snap, app, iface, pathPattern, outcome, lifespan, duration, permissions)
	c.Assert(err, IsNil)

	c.Assert(ruleNoticeIDs, HasLen, 1, Commentf("ruleNoticeIDs: %v; rdb.ByID: %+v", ruleNoticeIDs, rdb.ByID))
	c.Check(ruleNoticeIDs[0], Equals, storedRule.ID)
	ruleNoticeIDs = ruleNoticeIDs[1:]

	conflictingPermissions := permissions[:1]
	_, err = rdb.CreateRule(user, snap, app, iface, pathPattern, outcome, lifespan, duration, conflictingPermissions)
	c.Assert(err, ErrorMatches, fmt.Sprintf("^%s.*%s.*%s.*", accessrules.ErrPathPatternConflict, storedRule.ID, conflictingPermissions[0]))

	// Error while adding rule should cause no notice to be issued
	c.Assert(ruleNoticeIDs, HasLen, 0, Commentf("ruleNoticeIDs: %v; rdb.ByID: %+v", ruleNoticeIDs, rdb.ByID))

	badPattern := "bad/pattern"
	_, err = rdb.CreateRule(user, snap, app, iface, badPattern, outcome, lifespan, duration, permissions)
	c.Assert(err, Equals, common.ErrInvalidPathPattern)

	// Error while adding rule should cause no notice to be issued
	c.Assert(ruleNoticeIDs, HasLen, 0, Commentf("ruleNoticeIDs: %v; rdb.ByID: %+v", ruleNoticeIDs, rdb.ByID))

	badOutcome := common.OutcomeType("secret third thing")
	_, err = rdb.CreateRule(user, snap, app, iface, pathPattern, badOutcome, lifespan, duration, permissions)
	c.Assert(err, Equals, common.ErrInvalidOutcome)

	// Error while adding rule should cause no notice to be issued
	c.Assert(ruleNoticeIDs, HasLen, 0, Commentf("ruleNoticeIDs: %v; rdb.ByID: %+v", ruleNoticeIDs, rdb.ByID))
}

func (s *accessruleSuite) TestModifyRule(c *C) {
	var user uint32 = 1000
	ruleNoticeIDs := make([]string, 0, 5)
	notifyRule := func(userID uint32, ruleID string, options *state.AddNoticeOptions) error {
		c.Check(userID, Equals, user)
		ruleNoticeIDs = append(ruleNoticeIDs, ruleID)
		return nil
	}
	rdb, _ := accessrules.New(notifyRule)

	snap := "lxd"
	app := "lxc"
	iface := "home"
	pathPattern := "/home/test/Documents/**"
	outcome := common.OutcomeAllow
	lifespan := common.LifespanForever
	duration := ""
	permissions := []common.PermissionType{
		common.PermissionRead,
		common.PermissionWrite,
		common.PermissionExecute,
	}

	storedRule, err := rdb.CreateRule(user, snap, app, iface, pathPattern, outcome, lifespan, duration, permissions)
	c.Assert(err, IsNil)
	c.Assert(rdb.ByID, HasLen, 1)

	c.Assert(ruleNoticeIDs, HasLen, 1, Commentf("ruleNoticeIDs: %v; rdb.ByID: %+v", ruleNoticeIDs, rdb.ByID))
	c.Check(ruleNoticeIDs[0], Equals, storedRule.ID)
	ruleNoticeIDs = ruleNoticeIDs[1:]

	conflictingPermission := common.PermissionRename

	otherPathPattern := "/home/test/Pictures/**/*.png"
	otherPermissions := []common.PermissionType{
		conflictingPermission,
	}
	otherRule, err := rdb.CreateRule(user, snap, app, iface, otherPathPattern, outcome, lifespan, duration, otherPermissions)
	c.Assert(err, IsNil)
	c.Assert(rdb.ByID, HasLen, 2)

	c.Assert(ruleNoticeIDs, HasLen, 1, Commentf("ruleNoticeIDs: %v; rdb.ByID: %+v", ruleNoticeIDs, rdb.ByID))
	c.Check(ruleNoticeIDs[0], Equals, otherRule.ID)
	ruleNoticeIDs = ruleNoticeIDs[1:]

	// Check that modifying with the original values results in an identical rule
	unchangedRule1, err := rdb.ModifyRule(user, storedRule.ID, pathPattern, outcome, lifespan, duration, permissions)
	c.Assert(err, IsNil)
	// Timestamp should be different, the rest should be the same
	unchangedRule1.Timestamp = storedRule.Timestamp
	c.Assert(unchangedRule1, DeepEquals, storedRule)
	c.Assert(rdb.ByID, HasLen, 2)

	// Though rule was modified with the same values as it originally had, the
	// timestamp was changed, and thus a notice should be issued for it.
	c.Assert(ruleNoticeIDs, HasLen, 1, Commentf("ruleNoticeIDs: %v; rdb.ByID: %+v", ruleNoticeIDs, rdb.ByID))
	c.Check(ruleNoticeIDs[0], Equals, unchangedRule1.ID)
	ruleNoticeIDs = ruleNoticeIDs[1:]

	// Check that modifying with unset values results in an identical rule
	unchangedRule2, err := rdb.ModifyRule(user, storedRule.ID, "", common.OutcomeUnset, common.LifespanUnset, "", nil)
	c.Assert(err, IsNil)
	// Timestamp should be different, the rest should be the same
	unchangedRule2.Timestamp = storedRule.Timestamp
	c.Assert(unchangedRule2, DeepEquals, storedRule)
	c.Assert(rdb.ByID, HasLen, 2)

	// Though rule was modified with unset values, and thus was unchanged aside
	// from the timestamp, a notice should be issued for it.
	c.Assert(ruleNoticeIDs, HasLen, 1, Commentf("ruleNoticeIDs: %v; rdb.ByID: %+v", ruleNoticeIDs, rdb.ByID))
	c.Check(ruleNoticeIDs[0], Equals, unchangedRule2.ID)
	ruleNoticeIDs = ruleNoticeIDs[1:]

	newPathPattern := otherPathPattern
	newOutcome := common.OutcomeDeny
	newLifespan := common.LifespanTimespan
	newDuration := "1s"
	newPermissions := []common.PermissionType{common.PermissionAppend}
	modifiedRule, err := rdb.ModifyRule(user, storedRule.ID, newPathPattern, newOutcome, newLifespan, newDuration, newPermissions)
	c.Assert(err, IsNil)
	c.Assert(rdb.ByID, HasLen, 2)

	c.Assert(ruleNoticeIDs, HasLen, 1, Commentf("ruleNoticeIDs: %v; rdb.ByID: %+v", ruleNoticeIDs, rdb.ByID))
	c.Check(ruleNoticeIDs[0], Equals, modifiedRule.ID)
	ruleNoticeIDs = ruleNoticeIDs[1:]

	badPathPattern := "bad/pattern"
	output, err := rdb.ModifyRule(user, storedRule.ID, badPathPattern, outcome, lifespan, duration, permissions)
	c.Assert(err, Equals, common.ErrInvalidPathPattern)
	c.Assert(output, IsNil)
	c.Assert(rdb.ByID, HasLen, 2)

	// Error while modifying rule should cause no notice to be issued
	c.Assert(ruleNoticeIDs, HasLen, 0, Commentf("ruleNoticeIDs: %v; rdb.ByID: %+v", ruleNoticeIDs, rdb.ByID))

	currentRule, exists := rdb.ByID[storedRule.ID]
	c.Assert(exists, Equals, true)
	c.Assert(currentRule, DeepEquals, modifiedRule)

	conflictingPermissions := append(newPermissions, conflictingPermission)
	output, err = rdb.ModifyRule(user, storedRule.ID, newPathPattern, newOutcome, newLifespan, newDuration, conflictingPermissions)
	c.Assert(err, ErrorMatches, fmt.Sprintf("^%s.*%s.*%s.*", accessrules.ErrPathPatternConflict, otherRule.ID, conflictingPermission))
	c.Assert(output, IsNil)
	c.Assert(rdb.ByID, HasLen, 2)

	// Permission conflicts while modifying rule should cause no notice to be issued
	c.Assert(ruleNoticeIDs, HasLen, 0, Commentf("ruleNoticeIDs: %v; rdb.ByID: %+v", ruleNoticeIDs, rdb.ByID))

	currentRule, exists = rdb.ByID[storedRule.ID]
	c.Assert(exists, Equals, true)
	c.Assert(currentRule, DeepEquals, modifiedRule)
}

func (s *accessruleSuite) TestRuleWithID(c *C) {
	var user uint32 = 1000
	ruleNoticeIDs := make([]string, 0, 1)
	notifyRule := func(userID uint32, ruleID string, options *state.AddNoticeOptions) error {
		c.Check(userID, Equals, user)
		ruleNoticeIDs = append(ruleNoticeIDs, ruleID)
		return nil
	}
	rdb, _ := accessrules.New(notifyRule)

	snap := "lxd"
	app := "lxc"
	iface := "system-files"
	pathPattern := "/home/test/Documents/**"
	outcome := common.OutcomeAllow
	lifespan := common.LifespanForever
	duration := ""
	permissions := []common.PermissionType{
		common.PermissionRead,
		common.PermissionWrite,
		common.PermissionExecute,
	}

	newRule, err := rdb.CreateRule(user, snap, app, iface, pathPattern, outcome, lifespan, duration, permissions)
	c.Assert(err, IsNil)
	c.Assert(newRule, NotNil)

	c.Assert(ruleNoticeIDs, HasLen, 1, Commentf("ruleNoticeIDs: %v; rdb.ByID: %+v", ruleNoticeIDs, rdb.ByID))
	c.Check(ruleNoticeIDs[0], Equals, newRule.ID)
	ruleNoticeIDs = ruleNoticeIDs[1:]

	accessedRule, err := rdb.RuleWithID(user, newRule.ID)
	c.Check(err, IsNil)
	c.Check(accessedRule, DeepEquals, newRule)

	accessedRule, err = rdb.RuleWithID(user, "nonexistent")
	c.Check(err, Equals, accessrules.ErrRuleIDNotFound)
	c.Check(accessedRule, IsNil)

	accessedRule, err = rdb.RuleWithID(user+1, newRule.ID)
	c.Check(err, Equals, accessrules.ErrUserNotAllowed)
	c.Check(accessedRule, IsNil)

	// Reading (or failing to read) a notice should not trigger a notice
	c.Assert(ruleNoticeIDs, HasLen, 0, Commentf("ruleNoticeIDs: %v; rdb.ByID: %+v", ruleNoticeIDs, rdb.ByID))
}

func (s *accessruleSuite) TestRefreshTreeEnforceConsistencySimple(c *C) {
	var user uint32 = 1000
	ruleNoticeIDs := make([]string, 0, 1)
	notifyRule := func(userID uint32, ruleID string, options *state.AddNoticeOptions) error {
		c.Check(userID, Equals, user)
		ruleNoticeIDs = append(ruleNoticeIDs, ruleID)
		return nil
	}
	rdb, _ := accessrules.New(notifyRule)

	snap := "lxd"
	app := "lxc"
	iface := "system-backup"
	pathPattern := "/home/test/Documents/**"
	outcome := common.OutcomeAllow
	lifespan := common.LifespanForever
	duration := ""
	permissions := []common.PermissionType{
		common.PermissionRead,
		common.PermissionWrite,
		common.PermissionExecute,
	}

	rule, err := rdb.PopulateNewRule(user, snap, app, iface, pathPattern, outcome, lifespan, duration, permissions)
	c.Assert(err, IsNil)
	rdb.ByID[rule.ID] = rule
	notifyEveryRule := true
	rdb.RefreshTreeEnforceConsistency(notifyEveryRule)

	// Test that RefreshTreeEnforceConsistency results in a single notice
	c.Assert(ruleNoticeIDs, HasLen, 1, Commentf("ruleNoticeIDs: %v; rdb.ByID: %+v", ruleNoticeIDs, rdb.ByID))
	c.Check(ruleNoticeIDs[0], Equals, rule.ID)
	ruleNoticeIDs = ruleNoticeIDs[1:]

	c.Assert(rdb.PerUser, HasLen, 1)

	userEntry, exists := rdb.PerUser[user]
	c.Assert(exists, Equals, true)
	c.Assert(userEntry.PerSnap, HasLen, 1)

	snapEntry, exists := userEntry.PerSnap[snap]
	c.Assert(exists, Equals, true)
	c.Assert(snapEntry.PerApp, HasLen, 1)

	appEntry, exists := snapEntry.PerApp[app]
	c.Assert(exists, Equals, true)
	c.Assert(appEntry.PerInterface, HasLen, 1)

	interfaceEntry, exists := appEntry.PerInterface[iface]
	c.Assert(exists, Equals, true)
	c.Assert(interfaceEntry.PerPermission, HasLen, 3)

	for _, permission := range permissions {
		permissionEntry, exists := interfaceEntry.PerPermission[permission]
		c.Assert(exists, Equals, true)
		c.Assert(permissionEntry.PathRules, HasLen, 1)

		pathID, exists := permissionEntry.PathRules[pathPattern]
		c.Assert(exists, Equals, true)
		c.Assert(pathID, Equals, rule.ID)
	}
}

func (s *accessruleSuite) TestRefreshTreeEnforceConsistencyComplex(c *C) {
	var user uint32 = 1000
	ruleNoticeIDs := make([]string, 0, 4)
	notifyRule := func(userID uint32, ruleID string, options *state.AddNoticeOptions) error {
		c.Check(userID, Equals, user)
		ruleNoticeIDs = append(ruleNoticeIDs, ruleID)
		return nil
	}
	rdb, _ := accessrules.New(notifyRule)

	snap := "lxd"
	app := "lxc"
	iface := "home"
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
	badTsRule1, err := rdb.PopulateNewRule(user, snap, app, iface, pathPattern, outcome, lifespan, duration, permissions[:1])
	c.Assert(err, IsNil)
	badTsRule1.Timestamp = "bar"
	time.Sleep(time.Millisecond)
	badTsRule2, err := rdb.PopulateNewRule(user, snap, app, iface, pathPattern, outcome, lifespan, duration, permissions[:1])
	c.Assert(err, IsNil)
	badTsRule2.Timestamp = "baz"
	rdb.ByID[badTsRule1.ID] = badTsRule1
	rdb.ByID[badTsRule2.ID] = badTsRule2

	c.Assert(badTsRule1, Not(Equals), badTsRule2)

	// The former should be overwritten by RefreshTreeEnforceConsistency
	notifyEveryRule := false
	rdb.RefreshTreeEnforceConsistency(notifyEveryRule)
	c.Assert(rdb.ByID, HasLen, 1, Commentf("rdb.ByID: %+v", rdb.ByID))
	_, exists := rdb.ByID[badTsRule2.ID]
	c.Assert(exists, Equals, true)
	// The latter should be overwritten by any conflicting rule which has a valid timestamp

	// Since notifyEveryRule = false, only one notice should be issued, for the overwritten rule
	c.Assert(ruleNoticeIDs, HasLen, 1, Commentf("ruleNoticeIDs: %v; rdb.ByID: %+v", ruleNoticeIDs, rdb.ByID))
	c.Check(ruleNoticeIDs[0], Equals, badTsRule1.ID)
	ruleNoticeIDs = ruleNoticeIDs[1:]

	// Create a rule with the earliest timestamp, which will be totally overwritten when attempting to add later
	earliestRule, err := rdb.PopulateNewRule(user, snap, app, iface, pathPattern, outcome, lifespan, duration, permissions)
	c.Assert(err, IsNil)

	// Create and add a rule which will overwrite the rule with bad timestamp
	initialRule, err := rdb.PopulateNewRule(user, snap, app, iface, pathPattern, outcome, lifespan, duration, permissions)
	c.Assert(err, IsNil)
	rdb.ByID[initialRule.ID] = initialRule
	rdb.RefreshTreeEnforceConsistency(notifyEveryRule)

	// Expect notice for rule with bad timestamp, not initialRule
	c.Assert(ruleNoticeIDs, HasLen, 1, Commentf("ruleNoticeIDs: %v; rdb.ByID: %+v", ruleNoticeIDs, rdb.ByID))
	c.Check(ruleNoticeIDs[0], Equals, badTsRule2.ID)
	ruleNoticeIDs = ruleNoticeIDs[1:]

	// Check that rule with bad timestamp was overwritten
	c.Assert(initialRule.Permissions, HasLen, len(permissions))
	c.Assert(rdb.ByID, HasLen, 1, Commentf("rdb.ByID: %+v", rdb.ByID))
	initialRuleRet, exists := rdb.ByID[initialRule.ID]
	c.Assert(exists, Equals, true)
	c.Assert(initialRuleRet.Permissions, DeepEquals, permissions)

	// Create newer rule which will overwrite all but the first permission of initialRule
	newRulePermissions := permissions[1:]
	newRule, err := rdb.PopulateNewRule(user, snap, app, iface, pathPattern, outcome, lifespan, duration, newRulePermissions)
	c.Assert(err, IsNil)

	rdb.ByID[newRule.ID] = newRule
	rdb.ByID[earliestRule.ID] = earliestRule
	rdb.RefreshTreeEnforceConsistency(notifyEveryRule)

	// Expect notice for initialRule and earliestRule, not newRule
	c.Assert(ruleNoticeIDs, HasLen, 2, Commentf("ruleNoticeIDs: %v; rdb.ByID: %+v", ruleNoticeIDs, rdb.ByID))
	c.Check(strutil.ListContains(ruleNoticeIDs, initialRule.ID), Equals, true)
	c.Check(strutil.ListContains(ruleNoticeIDs, earliestRule.ID), Equals, true)
	ruleNoticeIDs = ruleNoticeIDs[2:]

	c.Assert(rdb.ByID, HasLen, 2, Commentf("rdb.ByID: %+v", rdb.ByID))

	_, exists = rdb.ByID[earliestRule.ID]
	c.Assert(exists, Equals, false)

	initialRuleRet, exists = rdb.ByID[initialRule.ID]
	c.Assert(exists, Equals, true)
	c.Assert(initialRuleRet.Permissions, DeepEquals, permissions[:1])

	newRuleRet, exists := rdb.ByID[newRule.ID]
	c.Assert(exists, Equals, true)
	c.Assert(newRuleRet.Permissions, DeepEquals, newRulePermissions, Commentf("newRulePermissions: %+v", newRulePermissions))

	c.Assert(rdb.PerUser, HasLen, 1)

	userEntry, exists := rdb.PerUser[user]
	c.Assert(exists, Equals, true)
	c.Assert(userEntry.PerSnap, HasLen, 1)

	snapEntry, exists := userEntry.PerSnap[snap]
	c.Assert(exists, Equals, true)
	c.Assert(snapEntry.PerApp, HasLen, 1)

	appEntry, exists := snapEntry.PerApp[app]
	c.Assert(exists, Equals, true)
	c.Assert(appEntry.PerInterface, HasLen, 1)

	interfaceEntry, exists := appEntry.PerInterface[iface]
	c.Assert(exists, Equals, true)
	c.Assert(interfaceEntry.PerPermission, HasLen, 3)

	for i, permission := range permissions {
		permissionEntry, exists := interfaceEntry.PerPermission[permission]
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
	doNotNotifyRule := func(userID uint32, ruleID string, options *state.AddNoticeOptions) error {
		return nil
	}
	rdb, _ := accessrules.New(doNotNotifyRule)

	var user uint32 = 1000
	snap := "lxd"
	app := "lxc"
	iface := "home"
	pathPattern := "/home/test/Documents/**"
	outcome := common.OutcomeAllow
	lifespan := common.LifespanForever
	duration := ""
	permissions := []common.PermissionType{
		common.PermissionRead,
		common.PermissionWrite,
		common.PermissionExecute,
	}

	rule1, err := rdb.CreateRule(user, snap, app, iface, pathPattern, outcome, lifespan, duration, permissions[:1])
	c.Assert(err, IsNil)
	rule2, err := rdb.CreateRule(user, snap, app, iface, pathPattern, outcome, lifespan, duration, permissions[1:2])
	c.Assert(err, IsNil)
	rule3, err := rdb.CreateRule(user, snap, app, iface, pathPattern, outcome, lifespan, duration, permissions[2:])
	c.Assert(err, IsNil)

	ruleIDs := []string{rule1.ID, rule2.ID, rule3.ID}
	previous := make([]string, 0, 3)

	notifyRule := func(userID uint32, ruleID string, options *state.AddNoticeOptions) error {
		c.Check(userID, Equals, user)
		c.Check(strutil.ListContains(ruleIDs, ruleID), Equals, true, Commentf("unexpected rule ID: %s", ruleID))
		c.Check(strutil.ListContains(previous, ruleID), Equals, false, Commentf("repeated rule ID: %s", ruleID))
		previous = append(previous, ruleID)
		return nil
	}
	loadedArdb, err := accessrules.New(notifyRule)
	c.Assert(err, IsNil)
	c.Assert(rdb.ByID, DeepEquals, loadedArdb.ByID)
	c.Assert(rdb.PerUser, DeepEquals, loadedArdb.PerUser)
}

func (s *accessruleSuite) TestIsPathAllowed(c *C) {
	var user uint32 = 1000
	patterns := make(map[string]common.OutcomeType)
	patterns["/home/test/Documents/**"] = common.OutcomeAllow
	patterns["/home/test/Documents/foo/**"] = common.OutcomeDeny
	patterns["/home/test/Documents/foo/bar/**"] = common.OutcomeAllow
	patterns["/home/test/Documents/foo/bar/baz/**"] = common.OutcomeDeny
	patterns["/home/test/**/*.png"] = common.OutcomeAllow
	patterns["/home/test/**/*.jpg"] = common.OutcomeDeny

	ruleNoticeIDs := make([]string, 0, len(patterns))
	notifyRule := func(userID uint32, ruleID string, options *state.AddNoticeOptions) error {
		c.Check(userID, Equals, user)
		ruleNoticeIDs = append(ruleNoticeIDs, ruleID)
		return nil
	}
	rdb, _ := accessrules.New(notifyRule)

	snap := "lxd"
	app := "lxc"
	iface := "home"
	lifespan := common.LifespanForever
	duration := ""
	permissions := []common.PermissionType{
		common.PermissionRead,
		common.PermissionWrite,
		common.PermissionExecute,
	}

	for pattern, outcome := range patterns {
		newRule, err := rdb.CreateRule(user, snap, app, iface, pattern, outcome, lifespan, duration, permissions)
		c.Assert(err, IsNil)

		c.Assert(ruleNoticeIDs, HasLen, 1, Commentf("ruleNoticeIDs: %v; rdb.ByID: %+v", ruleNoticeIDs, rdb.ByID))
		c.Check(ruleNoticeIDs[0], Equals, newRule.ID)
		ruleNoticeIDs = ruleNoticeIDs[1:]
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
		result, err := rdb.IsPathAllowed(user, snap, app, iface, path, permissions[0])
		c.Assert(err, IsNil, Commentf("path: %s: error: %v\nrdb.ByID: %+v", path, err, rdb.ByID))
		c.Assert(result, Equals, expected, Commentf("path: %s: expected %b but got %b\nrdb.ByID: %+v", path, expected, result, rdb.ByID))

		// Matching against rules should not cause any notices
		c.Assert(ruleNoticeIDs, HasLen, 0, Commentf("ruleNoticeIDs: %v; rdb.ByID: %+v", ruleNoticeIDs, rdb.ByID))
	}
}

func (s *accessruleSuite) TestRuleExpiration(c *C) {
	var user uint32 = 1000
	ruleNoticeIDs := make([]string, 0, 6)
	notifyRule := func(userID uint32, ruleID string, options *state.AddNoticeOptions) error {
		c.Check(userID, Equals, user)
		ruleNoticeIDs = append(ruleNoticeIDs, ruleID)
		return nil
	}
	rdb, _ := accessrules.New(notifyRule)

	snap := "lxd"
	app := "lxc"
	iface := "home"
	permissions := []common.PermissionType{
		common.PermissionRead,
		common.PermissionWrite,
		common.PermissionExecute,
	}

	pathPattern := "/home/test/**"
	outcome := common.OutcomeAllow
	lifespan := common.LifespanSingle
	duration := ""
	rule1, err := rdb.CreateRule(user, snap, app, iface, pathPattern, outcome, lifespan, duration, permissions)
	c.Assert(err, IsNil)

	c.Assert(ruleNoticeIDs, HasLen, 1, Commentf("ruleNoticeIDs: %v; rdb.ByID: %+v", ruleNoticeIDs, rdb.ByID))
	c.Check(ruleNoticeIDs[0], Equals, rule1.ID)
	ruleNoticeIDs = ruleNoticeIDs[1:]

	pathPattern = "/home/test/Pictures/**"
	outcome = common.OutcomeDeny
	lifespan = common.LifespanTimespan
	duration = "2s"
	rule2, err := rdb.CreateRule(user, snap, app, iface, pathPattern, outcome, lifespan, duration, permissions)
	c.Assert(err, IsNil)

	c.Assert(ruleNoticeIDs, HasLen, 1, Commentf("ruleNoticeIDs: %v; rdb.ByID: %+v", ruleNoticeIDs, rdb.ByID))
	c.Check(ruleNoticeIDs[0], Equals, rule2.ID)
	ruleNoticeIDs = ruleNoticeIDs[1:]

	pathPattern = "/home/test/Pictures/**/*.png"
	outcome = common.OutcomeAllow
	lifespan = common.LifespanTimespan
	duration = "1s"
	rule3, err := rdb.CreateRule(user, snap, app, iface, pathPattern, outcome, lifespan, duration, permissions)
	c.Assert(err, IsNil)

	c.Assert(ruleNoticeIDs, HasLen, 1, Commentf("ruleNoticeIDs: %v; rdb.ByID: %+v", ruleNoticeIDs, rdb.ByID))
	c.Check(ruleNoticeIDs[0], Equals, rule3.ID)
	ruleNoticeIDs = ruleNoticeIDs[1:]

	path1 := "/home/test/Pictures/img.png"
	path2 := "/home/test/Pictures/img.jpg"

	allowed, err := rdb.IsPathAllowed(user, snap, app, iface, path1, common.PermissionRead)
	c.Assert(err, IsNil)
	c.Assert(allowed, Equals, true)
	allowed, err = rdb.IsPathAllowed(user, snap, app, iface, path2, common.PermissionRead)
	c.Assert(err, IsNil)
	c.Assert(allowed, Equals, false)

	// No rules expired, so should not cause a notice
	c.Assert(ruleNoticeIDs, HasLen, 0, Commentf("ruleNoticeIDs: %v; rdb.ByID: %+v", ruleNoticeIDs, rdb.ByID))

	time.Sleep(time.Second)

	allowed, err = rdb.IsPathAllowed(user, snap, app, iface, path1, common.PermissionRead)
	c.Assert(err, IsNil)
	c.Assert(allowed, Equals, false)

	// rule3 should have expired and triggered a notice
	c.Assert(ruleNoticeIDs, HasLen, 1, Commentf("ruleNoticeIDs: %v; rdb.ByID: %+v", ruleNoticeIDs, rdb.ByID))
	c.Check(ruleNoticeIDs[0], Equals, rule3.ID)
	ruleNoticeIDs = ruleNoticeIDs[1:]

	allowed, err = rdb.IsPathAllowed(user, snap, app, iface, path2, common.PermissionRead)
	c.Assert(err, IsNil)
	c.Assert(allowed, Equals, false)

	// No rules newly expired, so should not cause a notice
	c.Assert(ruleNoticeIDs, HasLen, 0, Commentf("ruleNoticeIDs: %v; rdb.ByID: %+v", ruleNoticeIDs, rdb.ByID))

	time.Sleep(time.Second)

	// Matches rule1, which has lifetime single, which thus expires.
	// Meanwhile, rule2 also expires.
	allowed, err = rdb.IsPathAllowed(user, snap, app, iface, path1, common.PermissionRead)
	c.Assert(err, Equals, nil)
	c.Assert(allowed, Equals, true)

	c.Assert(ruleNoticeIDs, HasLen, 2, Commentf("ruleNoticeIDs: %v; rdb.ByID: %+v", ruleNoticeIDs, rdb.ByID))
	c.Check(strutil.ListContains(ruleNoticeIDs, rule1.ID), Equals, true)
	c.Check(strutil.ListContains(ruleNoticeIDs, rule2.ID), Equals, true)
	ruleNoticeIDs = ruleNoticeIDs[2:]

	allowed, err = rdb.IsPathAllowed(user, snap, app, iface, path2, common.PermissionRead)
	c.Assert(err, Equals, accessrules.ErrNoMatchingRule)
	c.Assert(allowed, Equals, false)

	allowed, err = rdb.IsPathAllowed(user, snap, app, iface, path1, common.PermissionRead)
	c.Assert(err, Equals, accessrules.ErrNoMatchingRule)
	c.Assert(allowed, Equals, false)
	allowed, err = rdb.IsPathAllowed(user, snap, app, iface, path2, common.PermissionRead)
	c.Assert(err, Equals, accessrules.ErrNoMatchingRule)
	c.Assert(allowed, Equals, false)

	// No rules newly expired, so should not cause a notice
	c.Assert(ruleNoticeIDs, HasLen, 0, Commentf("ruleNoticeIDs: %v; rdb.ByID: %+v", ruleNoticeIDs, rdb.ByID))
}

type userAndID struct {
	userID uint32
	ruleID string
}

func (s *accessruleSuite) TestRulesLookup(c *C) {
	ruleNotices := make([]*userAndID, 0, 4)
	notifyRule := func(userID uint32, ruleID string, options *state.AddNoticeOptions) error {
		newUserAndID := &userAndID{
			userID: userID,
			ruleID: ruleID,
		}
		ruleNotices = append(ruleNotices, newUserAndID)
		return nil
	}
	rdb, _ := accessrules.New(notifyRule)

	var origUser uint32 = 1000
	snap := "lxd"
	app := "lxc"
	iface := "home"
	pathPattern := "/home/test/Documents/**"
	outcome := common.OutcomeAllow
	lifespan := common.LifespanForever
	duration := ""
	permissions := []common.PermissionType{
		common.PermissionRead,
		common.PermissionWrite,
		common.PermissionExecute,
	}

	rule1, err := rdb.CreateRule(origUser, snap, app, iface, pathPattern, outcome, lifespan, duration, permissions)
	c.Assert(err, IsNil)

	c.Assert(ruleNotices, HasLen, 1, Commentf("ruleNoticeIDs: %+v; rdb.ByID: %+v", ruleNotices, rdb.ByID))
	c.Check(ruleNotices[0].userID, Equals, origUser)
	c.Check(ruleNotices[0].ruleID, Equals, rule1.ID)
	ruleNotices = ruleNotices[1:]

	user := origUser + 1
	rule2, err := rdb.CreateRule(user, snap, app, iface, pathPattern, outcome, lifespan, duration, permissions)
	c.Assert(err, IsNil)

	c.Assert(ruleNotices, HasLen, 1, Commentf("ruleNoticeIDs: %+v; rdb.ByID: %+v", ruleNotices, rdb.ByID))
	c.Check(ruleNotices[0].userID, Equals, user)
	c.Check(ruleNotices[0].ruleID, Equals, rule2.ID)
	ruleNotices = ruleNotices[1:]

	snap = "nextcloud"
	app = "occ"
	rule3, err := rdb.CreateRule(user, snap, app, iface, pathPattern, outcome, lifespan, duration, permissions)
	c.Assert(err, IsNil)

	c.Assert(ruleNotices, HasLen, 1, Commentf("ruleNoticeIDs: %+v; rdb.ByID: %+v", ruleNotices, rdb.ByID))
	c.Check(ruleNotices[0].userID, Equals, user)
	c.Check(ruleNotices[0].ruleID, Equals, rule3.ID)
	ruleNotices = ruleNotices[1:]

	iface = "system-files"
	rule4, err := rdb.CreateRule(user, snap, app, iface, pathPattern, outcome, lifespan, duration, permissions)
	c.Assert(err, IsNil)

	c.Assert(ruleNotices, HasLen, 1, Commentf("ruleNoticeIDs: %+v; rdb.ByID: %+v", ruleNotices, rdb.ByID))
	c.Check(ruleNotices[0].userID, Equals, user)
	c.Check(ruleNotices[0].ruleID, Equals, rule4.ID)
	ruleNotices = ruleNotices[1:]

	origUserRules := rdb.Rules(origUser)
	c.Assert(origUserRules, HasLen, 1)
	c.Assert(origUserRules[0], DeepEquals, rule1)

	userRules := rdb.Rules(user)
	c.Assert(userRules, HasLen, 3)
OUTER_LOOP_USER:
	for _, rule := range []*accessrules.Rule{rule2, rule3, rule4} {
		for _, userRule := range userRules {
			if reflect.DeepEqual(rule, userRule) {
				continue OUTER_LOOP_USER
			}
		}
		c.Assert(rule, DeepEquals, userRules[2])
	}

	userSnapRules := rdb.RulesForSnap(user, snap)
	c.Assert(userSnapRules, HasLen, 2)
OUTER_LOOP_USER_SNAP:
	for _, rule := range []*accessrules.Rule{rule3, rule4} {
		for _, userRule := range userRules {
			if reflect.DeepEqual(rule, userRule) {
				continue OUTER_LOOP_USER_SNAP
			}
		}
		c.Assert(rule, DeepEquals, userRules[1])
	}

	userSnapAppRules := rdb.RulesForSnapAppInterface(user, snap, app, iface)
	c.Assert(userSnapAppRules, HasLen, 1)
	c.Assert(userSnapAppRules[0], DeepEquals, rule4)

	// Looking up these rules should not cause any notices
	c.Assert(ruleNotices, HasLen, 0, Commentf("ruleNoticeIDs: %+v; rdb.ByID: %+v", ruleNotices, rdb.ByID))
}
