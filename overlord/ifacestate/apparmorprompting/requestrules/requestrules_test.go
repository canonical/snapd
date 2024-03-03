package requestrules_test

import (
	"fmt"
	"reflect"
	"testing"
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/overlord/ifacestate/apparmorprompting/common"
	"github.com/snapcore/snapd/overlord/ifacestate/apparmorprompting/requestrules"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/strutil"
)

func Test(t *testing.T) { TestingT(t) }

type requestrulesSuite struct {
	tmpdir string
}

var _ = Suite(&requestrulesSuite{})

func (s *requestrulesSuite) SetUpTest(c *C) {
	s.tmpdir = c.MkDir()
	dirs.SetRootDir(s.tmpdir)
}

func (s *requestrulesSuite) TestPopulateNewRule(c *C) {
	notifyRule := func(userID uint32, ruleID string, options *state.AddNoticeOptions) error {
		c.Errorf("unexpected rule notice with user %d and ID %s", userID, ruleID)
		return nil
	}
	rdb, _ := requestrules.New(notifyRule)

	var user uint32 = 1000
	snap := "lxd"
	iface := "home"
	outcome := common.OutcomeAllow
	lifespan := common.LifespanSession
	duration := ""
	permissions := []string{"read", "write", "execute"}

	for _, pattern := range []string{
		"/home/test/Documents/**",
		"/home/test/**/*.pdf",
		"/home/test/*",
	} {
		constraints := &common.Constraints{
			PathPattern: pattern,
			Permissions: permissions,
		}
		rule, err := rdb.PopulateNewRule(user, snap, iface, constraints, outcome, lifespan, duration)
		c.Check(err, IsNil)
		c.Check(rule.User, Equals, user)
		c.Check(rule.Snap, Equals, snap)
		c.Check(rule.Constraints, Equals, constraints)
		c.Check(rule.Outcome, Equals, outcome)
		c.Check(rule.Lifespan, Equals, lifespan)
		c.Check(rule.Expiration, Equals, "")
	}

	for _, pattern := range []string{
		`/home/test/**/foo.[abc]onf`,
		`/home/test/*/**\`,
		`/home/test/*{/*.txt`,
	} {
		constraints := &common.Constraints{
			PathPattern: pattern,
			Permissions: permissions,
		}
		rule, err := rdb.PopulateNewRule(user, snap, iface, constraints, outcome, lifespan, duration)
		c.Assert(err, ErrorMatches, "invalid path pattern.*")
		c.Assert(rule, IsNil)
	}
}

func (s *requestrulesSuite) TestAddRemoveRuleSimple(c *C) {
	var user uint32 = 1000
	ruleNoticeIDs := make([]string, 0, 2)
	notifyRule := func(userID uint32, ruleID string, options *state.AddNoticeOptions) error {
		c.Check(userID, Equals, user)
		ruleNoticeIDs = append(ruleNoticeIDs, ruleID)
		return nil
	}
	rdb, _ := requestrules.New(notifyRule)

	snap := "lxd"
	iface := "home"
	pathPattern := "/home/test/Documents/**"
	outcome := common.OutcomeAllow
	lifespan := common.LifespanForever
	duration := ""
	permissions := []string{"read", "write", "execute"}
	constraints := &common.Constraints{
		PathPattern: pathPattern,
		Permissions: permissions,
	}

	rule, err := rdb.AddRule(user, snap, iface, constraints, outcome, lifespan, duration)
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
	c.Assert(snapEntry.PerInterface, HasLen, 1)

	interfaceEntry, exists := snapEntry.PerInterface[iface]
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
	c.Assert(snapEntry.PerInterface, HasLen, 1)

	interfaceEntry, exists = snapEntry.PerInterface[iface]
	c.Assert(exists, Equals, true)
	c.Assert(interfaceEntry.PerPermission, HasLen, 3)

	for _, permission := range permissions {
		permissionEntry, exists := interfaceEntry.PerPermission[permission]
		c.Assert(exists, Equals, true)
		c.Assert(permissionEntry.PathRules, HasLen, 0)
	}
}

func (s *requestrulesSuite) TestAddRuleUnhappy(c *C) {
	var user uint32 = 1000
	ruleNoticeIDs := make([]string, 0, 1)
	notifyRule := func(userID uint32, ruleID string, options *state.AddNoticeOptions) error {
		c.Check(userID, Equals, user)
		ruleNoticeIDs = append(ruleNoticeIDs, ruleID)
		return nil
	}
	rdb, _ := requestrules.New(notifyRule)

	snap := "lxd"
	iface := "home"
	pathPattern := "/home/test/Documents/**"
	outcome := common.OutcomeAllow
	lifespan := common.LifespanForever
	duration := ""
	permissions := []string{"read", "write", "execute"}
	constraints := &common.Constraints{
		PathPattern: pathPattern,
		Permissions: permissions,
	}

	storedRule, err := rdb.AddRule(user, snap, iface, constraints, outcome, lifespan, duration)
	c.Assert(err, IsNil)

	c.Assert(ruleNoticeIDs, HasLen, 1, Commentf("ruleNoticeIDs: %v; rdb.ByID: %+v", ruleNoticeIDs, rdb.ByID))
	c.Check(ruleNoticeIDs[0], Equals, storedRule.ID)
	ruleNoticeIDs = ruleNoticeIDs[1:]

	conflictingPermissions := permissions[:1]
	conflictingConstraints := &common.Constraints{
		PathPattern: pathPattern,
		Permissions: conflictingPermissions,
	}
	_, err = rdb.AddRule(user, snap, iface, conflictingConstraints, outcome, lifespan, duration)
	c.Assert(err, ErrorMatches, fmt.Sprintf("^%s.*%s.*%s.*", requestrules.ErrPathPatternConflict, storedRule.ID, conflictingPermissions[0]))

	// Error while adding rule should cause no notice to be issued
	c.Assert(ruleNoticeIDs, HasLen, 0, Commentf("ruleNoticeIDs: %v; rdb.ByID: %+v", ruleNoticeIDs, rdb.ByID))

	badPattern := "bad/pattern"
	badConstraints := &common.Constraints{
		PathPattern: badPattern,
		Permissions: permissions,
	}
	_, err = rdb.AddRule(user, snap, iface, badConstraints, outcome, lifespan, duration)
	c.Assert(err, ErrorMatches, "invalid path pattern.*")

	// Error while adding rule should cause no notice to be issued
	c.Assert(ruleNoticeIDs, HasLen, 0, Commentf("ruleNoticeIDs: %v; rdb.ByID: %+v", ruleNoticeIDs, rdb.ByID))

	badPermissions := []string{"foo"}
	badConstraints = &common.Constraints{
		PathPattern: pathPattern,
		Permissions: badPermissions,
	}
	_, err = rdb.AddRule(user, snap, iface, badConstraints, outcome, lifespan, duration)
	c.Assert(err, ErrorMatches, "unsupported permission.*")

	// Error while adding rule should cause no notice to be issued
	c.Assert(ruleNoticeIDs, HasLen, 0, Commentf("ruleNoticeIDs: %v; rdb.ByID: %+v", ruleNoticeIDs, rdb.ByID))

	badOutcome := common.OutcomeType("secret third thing")
	_, err = rdb.AddRule(user, snap, iface, constraints, badOutcome, lifespan, duration)
	c.Assert(err, Equals, common.ErrInvalidOutcome)

	// Error while adding rule should cause no notice to be issued
	c.Assert(ruleNoticeIDs, HasLen, 0, Commentf("ruleNoticeIDs: %v; rdb.ByID: %+v", ruleNoticeIDs, rdb.ByID))
}

func (s *requestrulesSuite) TestPatchRule(c *C) {
	var user uint32 = 1000
	ruleNoticeIDs := make([]string, 0, 5)
	notifyRule := func(userID uint32, ruleID string, options *state.AddNoticeOptions) error {
		c.Check(userID, Equals, user)
		ruleNoticeIDs = append(ruleNoticeIDs, ruleID)
		return nil
	}
	rdb, _ := requestrules.New(notifyRule)

	snap := "lxd"
	iface := "home"
	pathPattern := "/home/test/Documents/**"
	outcome := common.OutcomeAllow
	lifespan := common.LifespanForever
	duration := ""
	permissions := []string{"read"}
	constraints := &common.Constraints{
		PathPattern: pathPattern,
		Permissions: permissions,
	}

	storedRule, err := rdb.AddRule(user, snap, iface, constraints, outcome, lifespan, duration)
	c.Assert(err, IsNil)
	c.Assert(rdb.ByID, HasLen, 1)

	c.Assert(ruleNoticeIDs, HasLen, 1, Commentf("ruleNoticeIDs: %v; rdb.ByID: %+v", ruleNoticeIDs, rdb.ByID))
	c.Check(ruleNoticeIDs[0], Equals, storedRule.ID)
	ruleNoticeIDs = ruleNoticeIDs[1:]

	conflictingPermission := "write"

	otherPathPattern := "/home/test/Pictures/**/*.png"
	otherPermissions := []string{
		conflictingPermission,
	}
	otherConstraints := &common.Constraints{
		PathPattern: otherPathPattern,
		Permissions: otherPermissions,
	}
	otherRule, err := rdb.AddRule(user, snap, iface, otherConstraints, outcome, lifespan, duration)
	c.Assert(err, IsNil)
	c.Assert(rdb.ByID, HasLen, 2)

	c.Assert(ruleNoticeIDs, HasLen, 1, Commentf("ruleNoticeIDs: %v; rdb.ByID: %+v", ruleNoticeIDs, rdb.ByID))
	c.Check(ruleNoticeIDs[0], Equals, otherRule.ID)
	ruleNoticeIDs = ruleNoticeIDs[1:]

	// Check that patching with the original values results in an identical rule
	unchangedRule1, err := rdb.PatchRule(user, storedRule.ID, constraints, outcome, lifespan, duration)
	c.Assert(err, IsNil)
	// Timestamp should be different, the rest should be the same
	unchangedRule1.Timestamp = storedRule.Timestamp
	c.Assert(unchangedRule1, DeepEquals, storedRule)
	c.Assert(rdb.ByID, HasLen, 2)

	// Though rule was patched with the same values as it originally had, the
	// timestamp was changed, and thus a notice should be issued for it.
	c.Assert(ruleNoticeIDs, HasLen, 1, Commentf("ruleNoticeIDs: %v; rdb.ByID: %+v", ruleNoticeIDs, rdb.ByID))
	c.Check(ruleNoticeIDs[0], Equals, unchangedRule1.ID)
	ruleNoticeIDs = ruleNoticeIDs[1:]

	// Check that patching with unset values results in an identical rule
	unchangedRule2, err := rdb.PatchRule(user, storedRule.ID, nil, common.OutcomeUnset, common.LifespanUnset, "")
	c.Assert(err, IsNil)
	// Timestamp should be different, the rest should be the same
	unchangedRule2.Timestamp = storedRule.Timestamp
	c.Assert(unchangedRule2, DeepEquals, storedRule)
	c.Assert(rdb.ByID, HasLen, 2)

	// Though rule was patched with unset values, and thus was unchanged aside
	// from the timestamp, a notice should be issued for it.
	c.Assert(ruleNoticeIDs, HasLen, 1, Commentf("ruleNoticeIDs: %v; rdb.ByID: %+v", ruleNoticeIDs, rdb.ByID))
	c.Check(ruleNoticeIDs[0], Equals, unchangedRule2.ID)
	ruleNoticeIDs = ruleNoticeIDs[1:]

	newPathPattern := otherPathPattern
	newOutcome := common.OutcomeDeny
	newLifespan := common.LifespanTimespan
	newDuration := "1s"
	newPermissions := []string{"execute"}
	newConstraints := &common.Constraints{
		PathPattern: newPathPattern,
		Permissions: newPermissions,
	}
	patchedRule, err := rdb.PatchRule(user, storedRule.ID, newConstraints, newOutcome, newLifespan, newDuration)
	c.Assert(err, IsNil)
	c.Assert(rdb.ByID, HasLen, 2)

	c.Assert(ruleNoticeIDs, HasLen, 1, Commentf("ruleNoticeIDs: %v; rdb.ByID: %+v", ruleNoticeIDs, rdb.ByID))
	c.Check(ruleNoticeIDs[0], Equals, patchedRule.ID)
	ruleNoticeIDs = ruleNoticeIDs[1:]

	badPathPattern := "bad/pattern"
	badConstraints := &common.Constraints{
		PathPattern: badPathPattern,
		Permissions: permissions,
	}
	output, err := rdb.PatchRule(user, storedRule.ID, badConstraints, outcome, lifespan, duration)
	c.Assert(err, ErrorMatches, "invalid path pattern.*")
	c.Assert(output, IsNil)
	c.Assert(rdb.ByID, HasLen, 2)

	// Error while patching rule should cause no notice to be issued
	c.Assert(ruleNoticeIDs, HasLen, 0, Commentf("ruleNoticeIDs: %v; rdb.ByID: %+v", ruleNoticeIDs, rdb.ByID))

	currentRule, exists := rdb.ByID[storedRule.ID]
	c.Assert(exists, Equals, true)
	c.Assert(currentRule, DeepEquals, patchedRule)

	conflictingPermissions := append(newPermissions, conflictingPermission)
	conflictingConstraints := &common.Constraints{
		PathPattern: newPathPattern,
		Permissions: conflictingPermissions,
	}
	output, err = rdb.PatchRule(user, storedRule.ID, conflictingConstraints, newOutcome, newLifespan, newDuration)
	c.Assert(err, ErrorMatches, fmt.Sprintf("^%s.*%s.*%s.*", requestrules.ErrPathPatternConflict, otherRule.ID, conflictingPermission))
	c.Assert(output, IsNil)
	c.Assert(rdb.ByID, HasLen, 2)

	// Permission conflicts while patching rule should cause no notice to be issued
	c.Assert(ruleNoticeIDs, HasLen, 0, Commentf("ruleNoticeIDs: %v; rdb.ByID: %+v", ruleNoticeIDs, rdb.ByID))

	currentRule, exists = rdb.ByID[storedRule.ID]
	c.Assert(exists, Equals, true)
	c.Assert(currentRule, DeepEquals, patchedRule)
}

func (s *requestrulesSuite) TestRuleWithID(c *C) {
	var user uint32 = 1000
	ruleNoticeIDs := make([]string, 0, 1)
	notifyRule := func(userID uint32, ruleID string, options *state.AddNoticeOptions) error {
		c.Check(userID, Equals, user)
		ruleNoticeIDs = append(ruleNoticeIDs, ruleID)
		return nil
	}
	rdb, _ := requestrules.New(notifyRule)

	snap := "lxd"
	iface := "home"
	pathPattern := "/home/test/Documents/**"
	outcome := common.OutcomeAllow
	lifespan := common.LifespanForever
	duration := ""
	permissions := []string{"read", "write", "execute"}
	constraints := &common.Constraints{
		PathPattern: pathPattern,
		Permissions: permissions,
	}

	newRule, err := rdb.AddRule(user, snap, iface, constraints, outcome, lifespan, duration)
	c.Assert(err, IsNil)
	c.Assert(newRule, NotNil)

	c.Assert(ruleNoticeIDs, HasLen, 1, Commentf("ruleNoticeIDs: %v; rdb.ByID: %+v", ruleNoticeIDs, rdb.ByID))
	c.Check(ruleNoticeIDs[0], Equals, newRule.ID)
	ruleNoticeIDs = ruleNoticeIDs[1:]

	accessedRule, err := rdb.RuleWithID(user, newRule.ID)
	c.Check(err, IsNil)
	c.Check(accessedRule, DeepEquals, newRule)

	accessedRule, err = rdb.RuleWithID(user, "nonexistent")
	c.Check(err, Equals, requestrules.ErrRuleIDNotFound)
	c.Check(accessedRule, IsNil)

	accessedRule, err = rdb.RuleWithID(user+1, newRule.ID)
	c.Check(err, Equals, requestrules.ErrUserNotAllowed)
	c.Check(accessedRule, IsNil)

	// Reading (or failing to read) a notice should not trigger a notice
	c.Assert(ruleNoticeIDs, HasLen, 0, Commentf("ruleNoticeIDs: %v; rdb.ByID: %+v", ruleNoticeIDs, rdb.ByID))
}

func (s *requestrulesSuite) TestRefreshTreeEnforceConsistencySimple(c *C) {
	var user uint32 = 1000
	ruleNoticeIDs := make([]string, 0, 1)
	notifyRule := func(userID uint32, ruleID string, options *state.AddNoticeOptions) error {
		c.Check(userID, Equals, user)
		ruleNoticeIDs = append(ruleNoticeIDs, ruleID)
		return nil
	}
	rdb, _ := requestrules.New(notifyRule)

	snap := "lxd"
	iface := "home"
	pathPattern := "/home/test/Documents/**"
	outcome := common.OutcomeAllow
	lifespan := common.LifespanForever
	duration := ""
	permissions := []string{"read", "write", "execute"}
	constraints := &common.Constraints{
		PathPattern: pathPattern,
		Permissions: permissions,
	}

	rule, err := rdb.PopulateNewRule(user, snap, iface, constraints, outcome, lifespan, duration)
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
	c.Assert(snapEntry.PerInterface, HasLen, 1)

	interfaceEntry, exists := snapEntry.PerInterface[iface]
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

func (s *requestrulesSuite) TestRefreshTreeEnforceConsistencyComplex(c *C) {
	var user uint32 = 1000
	ruleNoticeIDs := make([]string, 0, 4)
	notifyRule := func(userID uint32, ruleID string, options *state.AddNoticeOptions) error {
		c.Check(userID, Equals, user)
		ruleNoticeIDs = append(ruleNoticeIDs, ruleID)
		return nil
	}
	rdb, _ := requestrules.New(notifyRule)

	snap := "lxd"
	iface := "home"
	pathPattern := "/home/test/Documents/**"
	outcome := common.OutcomeAllow
	lifespan := common.LifespanForever
	duration := ""
	permissions := []string{"read", "write", "execute"}

	// Create two rules with bad timestamps
	constraints1 := &common.Constraints{
		PathPattern: pathPattern,
		Permissions: copyPermissions(permissions[:1]),
	}
	badTsRule1, err := rdb.PopulateNewRule(user, snap, iface, constraints1, outcome, lifespan, duration)
	c.Assert(err, IsNil)
	badTsRule1.Timestamp = "bar"
	time.Sleep(time.Millisecond)
	constraints2 := &common.Constraints{
		PathPattern: pathPattern,
		Permissions: copyPermissions(permissions[:1]),
	}
	badTsRule2, err := rdb.PopulateNewRule(user, snap, iface, constraints2, outcome, lifespan, duration)
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
	earliestConstraints := &common.Constraints{
		PathPattern: pathPattern,
		Permissions: copyPermissions(permissions),
	}
	earliestRule, err := rdb.PopulateNewRule(user, snap, iface, earliestConstraints, outcome, lifespan, duration)
	c.Assert(err, IsNil)

	// Create and add a rule which will overwrite the rule with bad timestamp
	initialConstraints := &common.Constraints{
		PathPattern: pathPattern,
		Permissions: copyPermissions(permissions),
	}
	initialRule, err := rdb.PopulateNewRule(user, snap, iface, initialConstraints, outcome, lifespan, duration)
	c.Assert(err, IsNil)
	rdb.ByID[initialRule.ID] = initialRule
	rdb.RefreshTreeEnforceConsistency(notifyEveryRule)

	// Expect notice for rule with bad timestamp, not initialRule
	c.Assert(ruleNoticeIDs, HasLen, 1, Commentf("ruleNoticeIDs: %v; rdb.ByID: %+v", ruleNoticeIDs, rdb.ByID))
	c.Check(ruleNoticeIDs[0], Equals, badTsRule2.ID)
	ruleNoticeIDs = ruleNoticeIDs[1:]

	// Check that rule with bad timestamp was overwritten
	c.Assert(rdb.ByID, HasLen, 1, Commentf("rdb.ByID: %+v", rdb.ByID))
	initialRuleRet, exists := rdb.ByID[initialRule.ID]
	c.Assert(exists, Equals, true)
	c.Assert(initialRuleRet.Constraints.Permissions, DeepEquals, permissions)

	// Create newer rule which will overwrite all but the first permission of initialRule
	newRulePermissions := permissions[1:]
	newConstraints := &common.Constraints{
		PathPattern: pathPattern,
		Permissions: copyPermissions(newRulePermissions),
	}
	newRule, err := rdb.PopulateNewRule(user, snap, iface, newConstraints, outcome, lifespan, duration)
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
	c.Assert(initialRuleRet.Constraints.Permissions, DeepEquals, permissions[:1])

	newRuleRet, exists := rdb.ByID[newRule.ID]
	c.Assert(exists, Equals, true)
	c.Assert(newRuleRet.Constraints.Permissions, DeepEquals, newRulePermissions, Commentf("newRulePermissions: %+v", newRulePermissions))

	c.Assert(rdb.PerUser, HasLen, 1)

	userEntry, exists := rdb.PerUser[user]
	c.Assert(exists, Equals, true)
	c.Assert(userEntry.PerSnap, HasLen, 1)

	snapEntry, exists := userEntry.PerSnap[snap]
	c.Assert(exists, Equals, true)
	c.Assert(snapEntry.PerInterface, HasLen, 1)

	interfaceEntry, exists := snapEntry.PerInterface[iface]
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

func copyPermissions(permissions []string) []string {
	newPermissions := make([]string, len(permissions))
	copy(newPermissions, permissions)
	return newPermissions
}

func (s *requestrulesSuite) TestNewSaveLoad(c *C) {
	doNotNotifyRule := func(userID uint32, ruleID string, options *state.AddNoticeOptions) error {
		return nil
	}
	rdb, _ := requestrules.New(doNotNotifyRule)

	var user uint32 = 1000
	snap := "lxd"
	iface := "home"
	pathPattern := "/home/test/Documents/**"
	outcome := common.OutcomeAllow
	lifespan := common.LifespanForever
	duration := ""
	permissions := []string{"read", "write", "execute"}

	constraints1 := &common.Constraints{
		PathPattern: pathPattern,
		Permissions: permissions[:1],
	}
	rule1, err := rdb.AddRule(user, snap, iface, constraints1, outcome, lifespan, duration)
	c.Assert(err, IsNil)
	constraints2 := &common.Constraints{
		PathPattern: pathPattern,
		Permissions: permissions[1:2],
	}
	rule2, err := rdb.AddRule(user, snap, iface, constraints2, outcome, lifespan, duration)
	c.Assert(err, IsNil)
	constraints3 := &common.Constraints{
		PathPattern: pathPattern,
		Permissions: permissions[2:],
	}
	rule3, err := rdb.AddRule(user, snap, iface, constraints3, outcome, lifespan, duration)
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
	loadedArdb, err := requestrules.New(notifyRule)
	c.Assert(err, IsNil)
	c.Assert(rdb.ByID, DeepEquals, loadedArdb.ByID)
	c.Assert(rdb.PerUser, DeepEquals, loadedArdb.PerUser)
}

func (s *requestrulesSuite) TestIsPathAllowed(c *C) {
	var user uint32 = 1000
	patterns := make(map[string]common.OutcomeType)
	patterns["/home/test/Documents/**"] = common.OutcomeAllow
	patterns["/home/test/Documents/foo/**"] = common.OutcomeDeny
	patterns["/home/test/Documents/foo/bar/**"] = common.OutcomeAllow
	patterns["/home/test/Documents/foo/bar/baz/**"] = common.OutcomeDeny
	patterns["/home/test/Documents/foo/bar/baz/**/{fizz,buzz}"] = common.OutcomeAllow
	patterns["/home/test/**/*.png"] = common.OutcomeAllow
	patterns["/home/test/**/*.jpg"] = common.OutcomeDeny

	ruleNoticeIDs := make([]string, 0, len(patterns))
	notifyRule := func(userID uint32, ruleID string, options *state.AddNoticeOptions) error {
		c.Check(userID, Equals, user)
		ruleNoticeIDs = append(ruleNoticeIDs, ruleID)
		return nil
	}
	rdb, _ := requestrules.New(notifyRule)

	snap := "lxd"
	iface := "home"
	lifespan := common.LifespanForever
	duration := ""
	permissions := []string{"read", "write", "execute"}

	for pattern, outcome := range patterns {
		newConstraints := &common.Constraints{
			PathPattern: pattern,
			Permissions: permissions,
		}
		newRule, err := rdb.AddRule(user, snap, iface, newConstraints, outcome, lifespan, duration)
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
	cases["/home/test/Documents/file.jpg"] = true
	cases["/home/test/Documents/foo/file.png"] = false
	cases["/home/test/Documents/foo/file.jpg"] = false
	cases["/home/test/Documents/foo/bar/file.png"] = true
	cases["/home/test/Documents/foo/bar/file.jpg"] = true
	cases["/home/test/Documents/foo/bar/baz/file.png"] = false
	cases["/home/test/Documents/foo/bar/baz/file.jpg"] = false
	cases["/home/test/Documents.png"] = true
	cases["/home/test/Documents.jpg"] = false
	cases["/home/test/Documents/foo.png"] = true
	cases["/home/test/Documents/foo.jpg"] = true
	cases["/home/test/Documents/foo.txt"] = true
	cases["/home/test/Documents/foo/bar.png"] = false
	cases["/home/test/Documents/foo/bar.jpg"] = false
	cases["/home/test/Documents/foo/bar.txt"] = false
	cases["/home/test/Documents/foo/bar/baz.png"] = true
	cases["/home/test/Documents/foo/bar/baz.jpg"] = true
	cases["/home/test/Documents/foo/bar/baz.png"] = true
	cases["/home/test/Documents/foo/bar/baz/file.png"] = false
	cases["/home/test/Documents/foo/bar/baz/file.jpg"] = false
	cases["/home/test/Documents/foo/bar/baz/file.txt"] = false
	cases["/home/test/Documents/foo/bar/baz/fizz"] = true
	cases["/home/test/Documents/foo/bar/baz/buzz"] = true
	cases["/home/test/file.jpg.png"] = true
	cases["/home/test/file.png.jpg"] = false

	for path, expected := range cases {
		result, err := rdb.IsPathAllowed(user, snap, iface, path, permissions[0])
		c.Assert(err, IsNil, Commentf("path: %s: error: %v\nrdb.ByID: %+v", path, err, rdb.ByID))
		c.Assert(result, Equals, expected, Commentf("path: %s: expected %b but got %b\nrdb.ByID: %+v", path, expected, result, rdb.ByID))

		// Matching against rules should not cause any notices
		c.Assert(ruleNoticeIDs, HasLen, 0, Commentf("ruleNoticeIDs: %v; rdb.ByID: %+v", ruleNoticeIDs, rdb.ByID))
	}
}

func (s *requestrulesSuite) TestRuleExpiration(c *C) {
	var user uint32 = 1000
	ruleNoticeIDs := make([]string, 0, 6)
	notifyRule := func(userID uint32, ruleID string, options *state.AddNoticeOptions) error {
		c.Check(userID, Equals, user)
		ruleNoticeIDs = append(ruleNoticeIDs, ruleID)
		return nil
	}
	rdb, _ := requestrules.New(notifyRule)

	snap := "lxd"
	iface := "home"
	permissions := []string{"read", "write", "execute"}

	pathPattern := "/home/test/**"
	outcome := common.OutcomeAllow
	lifespan := common.LifespanSingle
	duration := ""
	constraints1 := &common.Constraints{
		PathPattern: pathPattern,
		Permissions: permissions,
	}
	rule1, err := rdb.AddRule(user, snap, iface, constraints1, outcome, lifespan, duration)
	c.Assert(err, IsNil)

	c.Assert(ruleNoticeIDs, HasLen, 1, Commentf("ruleNoticeIDs: %v; rdb.ByID: %+v", ruleNoticeIDs, rdb.ByID))
	c.Check(ruleNoticeIDs[0], Equals, rule1.ID)
	ruleNoticeIDs = ruleNoticeIDs[1:]

	pathPattern = "/home/test/Pictures/**"
	outcome = common.OutcomeDeny
	lifespan = common.LifespanTimespan
	duration = "2s"
	constraints2 := &common.Constraints{
		PathPattern: pathPattern,
		Permissions: permissions,
	}
	rule2, err := rdb.AddRule(user, snap, iface, constraints2, outcome, lifespan, duration)
	c.Assert(err, IsNil)

	c.Assert(ruleNoticeIDs, HasLen, 1, Commentf("ruleNoticeIDs: %v; rdb.ByID: %+v", ruleNoticeIDs, rdb.ByID))
	c.Check(ruleNoticeIDs[0], Equals, rule2.ID)
	ruleNoticeIDs = ruleNoticeIDs[1:]

	pathPattern = "/home/test/Pictures/**/*.png"
	outcome = common.OutcomeAllow
	lifespan = common.LifespanTimespan
	duration = "1s"
	constraints3 := &common.Constraints{
		PathPattern: pathPattern,
		Permissions: permissions,
	}
	rule3, err := rdb.AddRule(user, snap, iface, constraints3, outcome, lifespan, duration)
	c.Assert(err, IsNil)

	c.Assert(ruleNoticeIDs, HasLen, 1, Commentf("ruleNoticeIDs: %v; rdb.ByID: %+v", ruleNoticeIDs, rdb.ByID))
	c.Check(ruleNoticeIDs[0], Equals, rule3.ID)
	ruleNoticeIDs = ruleNoticeIDs[1:]

	path1 := "/home/test/Pictures/img.png"
	path2 := "/home/test/Pictures/img.jpg"

	allowed, err := rdb.IsPathAllowed(user, snap, iface, path1, "read")
	c.Assert(err, IsNil)
	c.Assert(allowed, Equals, true, Commentf("rdb.ByID: %+v", rdb.ByID))
	allowed, err = rdb.IsPathAllowed(user, snap, iface, path2, "read")
	c.Assert(err, IsNil)
	c.Assert(allowed, Equals, false)

	// No rules expired, so should not cause a notice
	c.Assert(ruleNoticeIDs, HasLen, 0, Commentf("ruleNoticeIDs: %v; rdb.ByID: %+v", ruleNoticeIDs, rdb.ByID))

	time.Sleep(time.Second)

	allowed, err = rdb.IsPathAllowed(user, snap, iface, path1, "read")
	c.Assert(err, IsNil)
	c.Assert(allowed, Equals, false)

	// rule3 should have expired and triggered a notice
	c.Assert(ruleNoticeIDs, HasLen, 1, Commentf("ruleNoticeIDs: %v; rdb.ByID: %+v", ruleNoticeIDs, rdb.ByID))
	c.Check(ruleNoticeIDs[0], Equals, rule3.ID)
	ruleNoticeIDs = ruleNoticeIDs[1:]

	allowed, err = rdb.IsPathAllowed(user, snap, iface, path2, "read")
	c.Assert(err, IsNil)
	c.Assert(allowed, Equals, false)

	// No rules newly expired, so should not cause a notice
	c.Assert(ruleNoticeIDs, HasLen, 0, Commentf("ruleNoticeIDs: %v; rdb.ByID: %+v", ruleNoticeIDs, rdb.ByID))

	time.Sleep(time.Second)

	// Matches rule1, which has lifetime single, which thus expires.
	// Meanwhile, rule2 also expires.
	allowed, err = rdb.IsPathAllowed(user, snap, iface, path1, "read")
	c.Assert(err, Equals, nil)
	c.Assert(allowed, Equals, true)

	c.Assert(ruleNoticeIDs, HasLen, 2, Commentf("ruleNoticeIDs: %v; rdb.ByID: %+v", ruleNoticeIDs, rdb.ByID))
	c.Check(strutil.ListContains(ruleNoticeIDs, rule1.ID), Equals, true)
	c.Check(strutil.ListContains(ruleNoticeIDs, rule2.ID), Equals, true)
	ruleNoticeIDs = ruleNoticeIDs[2:]

	allowed, err = rdb.IsPathAllowed(user, snap, iface, path2, "read")
	c.Assert(err, Equals, requestrules.ErrNoMatchingRule)
	c.Assert(allowed, Equals, false)

	allowed, err = rdb.IsPathAllowed(user, snap, iface, path1, "read")
	c.Assert(err, Equals, requestrules.ErrNoMatchingRule)
	c.Assert(allowed, Equals, false)
	allowed, err = rdb.IsPathAllowed(user, snap, iface, path2, "read")
	c.Assert(err, Equals, requestrules.ErrNoMatchingRule)
	c.Assert(allowed, Equals, false)

	// No rules newly expired, so should not cause a notice
	c.Assert(ruleNoticeIDs, HasLen, 0, Commentf("ruleNoticeIDs: %v; rdb.ByID: %+v", ruleNoticeIDs, rdb.ByID))
}

type userAndID struct {
	userID uint32
	ruleID string
}

func (s *requestrulesSuite) TestRulesLookup(c *C) {
	ruleNotices := make([]*userAndID, 0, 4)
	notifyRule := func(userID uint32, ruleID string, options *state.AddNoticeOptions) error {
		newUserAndID := &userAndID{
			userID: userID,
			ruleID: ruleID,
		}
		ruleNotices = append(ruleNotices, newUserAndID)
		return nil
	}
	rdb, _ := requestrules.New(notifyRule)

	var origUser uint32 = 1000
	snap := "lxd"
	iface := "home"
	pathPattern := "/home/test/Documents/**"
	outcome := common.OutcomeAllow
	lifespan := common.LifespanForever
	duration := ""
	permissions := []string{"read", "write", "execute"}
	constraints := &common.Constraints{
		PathPattern: pathPattern,
		Permissions: permissions,
	}

	rule1, err := rdb.AddRule(origUser, snap, iface, constraints, outcome, lifespan, duration)
	c.Assert(err, IsNil)

	c.Assert(ruleNotices, HasLen, 1, Commentf("ruleNoticeIDs: %+v; rdb.ByID: %+v", ruleNotices, rdb.ByID))
	c.Check(ruleNotices[0].userID, Equals, origUser)
	c.Check(ruleNotices[0].ruleID, Equals, rule1.ID)
	ruleNotices = ruleNotices[1:]

	user := origUser + 1
	rule2, err := rdb.AddRule(user, snap, iface, constraints, outcome, lifespan, duration)
	c.Assert(err, IsNil)

	c.Assert(ruleNotices, HasLen, 1, Commentf("ruleNoticeIDs: %+v; rdb.ByID: %+v", ruleNotices, rdb.ByID))
	c.Check(ruleNotices[0].userID, Equals, user)
	c.Check(ruleNotices[0].ruleID, Equals, rule2.ID)
	ruleNotices = ruleNotices[1:]

	snap = "nextcloud"
	rule3, err := rdb.AddRule(user, snap, iface, constraints, outcome, lifespan, duration)
	c.Assert(err, IsNil)

	c.Assert(ruleNotices, HasLen, 1, Commentf("ruleNoticeIDs: %+v; rdb.ByID: %+v", ruleNotices, rdb.ByID))
	c.Check(ruleNotices[0].userID, Equals, user)
	c.Check(ruleNotices[0].ruleID, Equals, rule3.ID)
	ruleNotices = ruleNotices[1:]

	iface = "camera"
	constraints.Permissions = []string{"access"}
	rule4, err := rdb.AddRule(user, snap, iface, constraints, outcome, lifespan, duration)
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
	for _, rule := range []*requestrules.Rule{rule2, rule3, rule4} {
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
	for _, rule := range []*requestrules.Rule{rule3, rule4} {
		for _, userRule := range userRules {
			if reflect.DeepEqual(rule, userRule) {
				continue OUTER_LOOP_USER_SNAP
			}
		}
		c.Assert(rule, DeepEquals, userRules[1])
	}

	userSnapInterfaceRules := rdb.RulesForSnapInterface(user, snap, iface)
	c.Assert(userSnapInterfaceRules, HasLen, 1)
	c.Assert(userSnapInterfaceRules[0], DeepEquals, rule4)

	// Looking up these rules should not cause any notices
	c.Assert(ruleNotices, HasLen, 0, Commentf("ruleNoticeIDs: %+v; rdb.ByID: %+v", ruleNotices, rdb.ByID))
}
