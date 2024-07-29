package requestrules_test

import (
	"errors"
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

func (s *requestrulesSuite) TestJoinInternalErrors(c *C) {
	ErrFoo := errors.New("foo")
	ErrBar := errors.New("bar")
	ErrBaz := errors.New("baz")

	// Check that empty list or list of nil error(s) result in nil error.
	for _, errs := range [][]error{
		{},
		{nil},
		{nil, nil, nil},
	} {
		err := requestrules.JoinInternalErrors(errs)
		c.Check(err, IsNil)
	}

	errs := []error{nil, ErrFoo, nil}
	err := requestrules.JoinInternalErrors(errs)
	c.Check(errors.Is(err, requestrules.ErrInternalInconsistency), Equals, true)
	// XXX: check the following when we're on golang v1.20+
	// c.Check(errors.Is(err, ErrFoo), Equals, true)
	c.Check(errors.Is(err, ErrBar), Equals, false)
	c.Check(fmt.Sprintf("%v", err), Equals, fmt.Sprintf("%v\n%v", requestrules.ErrInternalInconsistency, ErrFoo))

	errs = append(errs, ErrBar, ErrBaz)
	err = requestrules.JoinInternalErrors(errs)
	c.Check(errors.Is(err, requestrules.ErrInternalInconsistency), Equals, true)
	// XXX: check the following when we're on golang v1.20+
	// c.Check(errors.Is(err, ErrFoo), Equals, true)
	// c.Check(errors.Is(err, ErrBar), Equals, true)
	// c.Check(errors.Is(err, ErrBaz), Equals, true)
	c.Check(fmt.Sprintf("%v", err), Equals, fmt.Sprintf("%v\n%v\n%v\n%v", requestrules.ErrInternalInconsistency, ErrFoo, ErrBar, ErrBaz))
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
		c.Check(rule.Expiration.IsZero(), Equals, true)
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
	c.Assert(err, ErrorMatches, `invalid outcome.*`)

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

func (s *requestrulesSuite) TestRemoveRulesForSnap(c *C) {
	var user uint32 = 1000
	ruleNoticeIDs := make([]string, 0, 5)
	notifyRule := func(userID uint32, ruleID string, options *state.AddNoticeOptions) error {
		c.Check(userID, Equals, user)
		ruleNoticeIDs = append(ruleNoticeIDs, ruleID)
		return nil
	}
	rdb, _ := requestrules.New(notifyRule)

	snap := "lxd"
	otherSnap := "nextcloud"
	iface := "home"
	pathPattern := "/home/test/Documents/**"
	outcome := common.OutcomeAllow
	lifespan := common.LifespanForever
	duration := ""
	permissions := []string{"read", "write", "execute"}
	constraints := &common.Constraints{
		PathPattern: pathPattern,
		Permissions: permissions[:1],
	}
	otherConstraints := &common.Constraints{
		PathPattern: pathPattern,
		Permissions: permissions[1:],
	}

	rule1, err := rdb.AddRule(user, snap, iface, constraints, outcome, lifespan, duration)
	c.Assert(err, IsNil)
	c.Assert(rule1, NotNil)

	rule2, err := rdb.AddRule(user, otherSnap, iface, constraints, outcome, lifespan, duration)
	c.Assert(err, IsNil)
	c.Assert(rule2, NotNil)

	rule3, err := rdb.AddRule(user, snap, iface, otherConstraints, outcome, lifespan, duration)
	c.Assert(err, IsNil)
	c.Assert(rule3, NotNil)

	c.Assert(ruleNoticeIDs, HasLen, 3, Commentf("ruleNoticeIDs: %v; rdb.ByID: %v", ruleNoticeIDs, rdb.ByID))
	c.Check(ruleNoticeIDs[0], Equals, rule1.ID)
	c.Check(ruleNoticeIDs[1], Equals, rule2.ID)
	c.Check(ruleNoticeIDs[2], Equals, rule3.ID)
	ruleNoticeIDs = ruleNoticeIDs[3:]

	removed := rdb.RemoveRulesForSnap(user, snap)
	c.Check(removed, HasLen, 2, Commentf("expected to remove 2 rules but instead removed: %+v", removed))
	c.Check(removed[0] == rule1 || removed[0] == rule3, Equals, true, Commentf("unexpected rule: %+v", removed[0]))
	c.Check(removed[1] == rule1 || removed[1] == rule3, Equals, true, Commentf("unexpected rule: %+v", removed[1]))
	c.Check(removed[0] != removed[1], Equals, true, Commentf("removed duplicate rules: %+v", removed))

	c.Assert(ruleNoticeIDs, HasLen, 2, Commentf("ruleNoticeIDs: %v; rdb.ByID: %v", ruleNoticeIDs, rdb.ByID))
	c.Check(strutil.ListContains(ruleNoticeIDs, rule1.ID), Equals, true)
	c.Check(strutil.ListContains(ruleNoticeIDs, rule3.ID), Equals, true)
	ruleNoticeIDs = ruleNoticeIDs[2:]
}

func (s *requestrulesSuite) TestRemoveRulesForSnapInterface(c *C) {
	var user uint32 = 1000
	ruleNoticeIDs := make([]string, 0, 5)
	notifyRule := func(userID uint32, ruleID string, options *state.AddNoticeOptions) error {
		c.Check(userID, Equals, user)
		ruleNoticeIDs = append(ruleNoticeIDs, ruleID)
		return nil
	}
	rdb, _ := requestrules.New(notifyRule)

	snap := "lxd"
	otherSnap := "nextcloud"
	iface := "home"
	otherIface := "camera"
	pathPattern := "/home/test/Documents/**"
	outcome := common.OutcomeAllow
	lifespan := common.LifespanForever
	duration := ""
	permissions := []string{"read", "write", "execute"}
	constraints := &common.Constraints{
		PathPattern: pathPattern,
		Permissions: permissions[:1],
	}
	otherConstraints := &common.Constraints{
		PathPattern: pathPattern,
		Permissions: permissions[1:],
	}

	rule1, err := rdb.AddRule(user, snap, iface, constraints, outcome, lifespan, duration)
	c.Assert(err, IsNil)
	c.Assert(rule1, NotNil)

	rule2, err := rdb.AddRule(user, otherSnap, iface, constraints, outcome, lifespan, duration)
	c.Assert(err, IsNil)
	c.Assert(rule2, NotNil)

	rule3, err := rdb.AddRule(user, snap, iface, otherConstraints, outcome, lifespan, duration)
	c.Assert(err, IsNil)
	c.Assert(rule3, NotNil)

	// For now, impossible to add a rule for an interface other than "home", so
	// must adjust it after it's been added.
	rule3.Interface = otherIface

	c.Assert(ruleNoticeIDs, HasLen, 3, Commentf("ruleNoticeIDs: %v; rdb.ByID: %v", ruleNoticeIDs, rdb.ByID))
	c.Check(ruleNoticeIDs[0], Equals, rule1.ID)
	c.Check(ruleNoticeIDs[1], Equals, rule2.ID)
	c.Check(ruleNoticeIDs[2], Equals, rule3.ID)
	ruleNoticeIDs = ruleNoticeIDs[3:]

	removed := rdb.RemoveRulesForSnapInterface(user, snap, iface)
	c.Check(removed, HasLen, 1, Commentf("expected to remove 2 rules but instead removed: %+v", removed))
	c.Check(removed[0] == rule1, Equals, true, Commentf("unexpected rule: %+v", removed[0]))

	c.Assert(ruleNoticeIDs, HasLen, 1, Commentf("ruleNoticeIDs: %v; rdb.ByID: %v", ruleNoticeIDs, rdb.ByID))
	c.Check(ruleNoticeIDs[0], Equals, rule1.ID)
	ruleNoticeIDs = ruleNoticeIDs[1:]
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

	// Reading (or failing to read) a notice should not record a notice
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
	pathPattern := "/home/test/{Documents,Downloads}/**"
	expandedPatterns, err := common.ExpandPathPattern(pathPattern)
	c.Assert(err, IsNil)
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
		c.Assert(permissionEntry.PathRules, HasLen, 2)

		for _, pattern := range expandedPatterns {
			pathID, exists := permissionEntry.PathRules[pattern]
			c.Check(exists, Equals, true)
			c.Check(pathID, Equals, rule.ID)
		}
	}
}

func (s *requestrulesSuite) TestRefreshTreeEnforceConsistencyComplex(c *C) {
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
	// Make sure all expanded path patterns include "/home/test/Documents/**/foo.txt"
	outcome := common.OutcomeAllow
	lifespan := common.LifespanForever
	duration := ""
	permissions := []string{"read", "write", "execute"}

	// Create two rules with early
	constraints1 := &common.Constraints{
		PathPattern: "/home/test/{Documents,Downloads}/**/foo.txt",
		Permissions: copyPermissions(permissions[:1]),
	}
	badTsRule1, err := rdb.PopulateNewRule(user, snap, iface, constraints1, outcome, lifespan, duration)
	c.Assert(err, IsNil)
	var timeZero time.Time
	badTsRule1.Timestamp = timeZero
	time.Sleep(time.Millisecond)
	constraints2 := &common.Constraints{
		PathPattern: "/home/test/{Downloads,Documents/**}/foo.txt",
		Permissions: copyPermissions(permissions[:1]),
	}
	badTsRule2, err := rdb.PopulateNewRule(user, snap, iface, constraints2, outcome, lifespan, duration)
	c.Assert(err, IsNil)
	badTsRule2.Timestamp = timeZero.Add(time.Second)
	rdb.ByID[badTsRule1.ID] = badTsRule1
	rdb.ByID[badTsRule2.ID] = badTsRule2

	c.Assert(badTsRule1, Not(Equals), badTsRule2)

	// The former should be overwritten by RefreshTreeEnforceConsistency
	notifyEveryRule := false
	rdb.RefreshTreeEnforceConsistency(notifyEveryRule)
	c.Assert(rdb.ByID, HasLen, 1, Commentf("rdb.ByID: %+v", rdb.ByID))
	_, exists := rdb.ByID[badTsRule2.ID]
	c.Assert(exists, Equals, true)
	// The latter should be overwritten by any conflicting rule which has a later timestamp

	// Since notifyEveryRule = false, only one notice should be issued, for the overwritten rule
	c.Assert(ruleNoticeIDs, HasLen, 1, Commentf("ruleNoticeIDs: %v; rdb.ByID: %+v", ruleNoticeIDs, rdb.ByID))
	c.Check(ruleNoticeIDs[0], Equals, badTsRule1.ID)
	ruleNoticeIDs = ruleNoticeIDs[1:]

	// Create a rule with the earliest timestamp, which will be totally overwritten when attempting to add later
	earliestConstraints := &common.Constraints{
		PathPattern: "/home/test/Documents/**/{foo,bar,baz}.txt",
		Permissions: copyPermissions(permissions),
	}
	earliestRule, err := rdb.PopulateNewRule(user, snap, iface, earliestConstraints, outcome, lifespan, duration)
	c.Assert(err, IsNil)

	// Create and add a rule which will overwrite the rule with bad timestamp
	initialPathPattern := "/home/test/{Music,Pictures,Videos,Documents}/**/foo.txt"
	initialConstraints := &common.Constraints{
		PathPattern: initialPathPattern,
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

	// Create rule with expiration in the past, which will be immediately be discarded without conflicting with other rules
	expiredConstraints := &common.Constraints{
		PathPattern: "/home/test/Documents/**/foo.txt",
		Permissions: copyPermissions(permissions),
	}
	expiredRule, err := rdb.PopulateNewRule(user, snap, iface, expiredConstraints, outcome, common.LifespanTimespan, "1s")
	c.Assert(err, IsNil)
	expiredRule.Expiration = time.Now().Add(-10 * time.Second)

	rdb.ByID[expiredRule.ID] = expiredRule
	rdb.RefreshTreeEnforceConsistency(notifyEveryRule)

	// Expect notice for only expiredRule
	c.Assert(ruleNoticeIDs, HasLen, 1, Commentf("ruleNoticeIDs: %v; rdb.ByID: %+v", ruleNoticeIDs, rdb.ByID))
	c.Check(strutil.ListContains(ruleNoticeIDs, expiredRule.ID), Equals, true)
	ruleNoticeIDs = ruleNoticeIDs[1:]

	c.Assert(rdb.ByID, HasLen, 1, Commentf("rdb.ByID: %+v", rdb.ByID))

	_, exists = rdb.ByID[expiredRule.ID]
	c.Assert(exists, Equals, false)

	initialRuleRet, exists = rdb.ByID[initialRule.ID]
	c.Assert(exists, Equals, true)
	c.Assert(initialRuleRet.Constraints.Permissions, DeepEquals, permissions)

	// Create rule with invalid path pattern, which will be immediately be discarded without conflicting with other rules
	invalidConstraints := &common.Constraints{
		PathPattern: "/home/test/Documents/**/foo.txt",
		Permissions: copyPermissions(permissions),
	}
	invalidRule, err := rdb.PopulateNewRule(user, snap, iface, invalidConstraints, outcome, common.LifespanTimespan, "1s")
	c.Assert(err, IsNil)
	invalidRule.Constraints.PathPattern = "/home/test/Documents/**/foo.txt{"

	rdb.ByID[invalidRule.ID] = invalidRule
	rdb.RefreshTreeEnforceConsistency(notifyEveryRule)

	// Expect notice for only invalidRule
	c.Assert(ruleNoticeIDs, HasLen, 1, Commentf("ruleNoticeIDs: %v; rdb.ByID: %+v", ruleNoticeIDs, rdb.ByID))
	c.Check(strutil.ListContains(ruleNoticeIDs, invalidRule.ID), Equals, true)
	ruleNoticeIDs = ruleNoticeIDs[1:]

	c.Assert(rdb.ByID, HasLen, 1, Commentf("rdb.ByID: %+v", rdb.ByID))

	_, exists = rdb.ByID[invalidRule.ID]
	c.Assert(exists, Equals, false)

	initialRuleRet, exists = rdb.ByID[initialRule.ID]
	c.Assert(exists, Equals, true)
	c.Assert(initialRuleRet.Constraints.Permissions, DeepEquals, permissions)

	// Create newer rule which will overwrite all but the first permission of initialRule
	newPathPattern := "/home/test/Documents{,/,/**/foo.{txt,md,pdf}}"
	newRulePermissions := permissions[1:]
	newConstraints := &common.Constraints{
		PathPattern: newPathPattern,
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

	expandedInitialPathPatterns, err := common.ExpandPathPattern(initialPathPattern)
	c.Assert(err, IsNil)
	expandedNewPathPatterns, err := common.ExpandPathPattern(newPathPattern)
	c.Assert(err, IsNil)
	for i, permission := range permissions {
		permissionEntry, exists := interfaceEntry.PerPermission[permission]
		c.Assert(exists, Equals, true)
		var expandedPatterns []string
		var ruleID string
		if i == 0 {
			expandedPatterns = expandedInitialPathPatterns
			ruleID = initialRule.ID
		} else {
			expandedPatterns = expandedNewPathPatterns
			ruleID = newRule.ID
		}
		c.Assert(permissionEntry.PathRules, HasLen, len(expandedPatterns))
		for _, expanded := range expandedPatterns {
			pathID, exists := permissionEntry.PathRules[expanded]
			c.Check(exists, Equals, true)
			c.Check(pathID, Equals, ruleID)
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
	loadedRdb, err := requestrules.New(notifyRule)
	c.Assert(err, IsNil)
	// DeepEquals does not treat time.Time well, so manually validate them and
	// set them to be explicitly equal so the DeepEquals check succeeds.
	c.Check(loadedRdb.ByID, HasLen, len(rdb.ByID))
	for id, rule := range rdb.ByID {
		loadedRule, exists := loadedRdb.ByID[id]
		c.Assert(exists, Equals, true, Commentf("missing rule after loading: %+v", rule))
		c.Check(rule.Timestamp.Equal(loadedRule.Timestamp), Equals, true, Commentf("%s != %s", rule.Timestamp, loadedRule.Timestamp))
		rule.Timestamp = loadedRule.Timestamp
	}
	c.Check(rdb.ByID, DeepEquals, loadedRdb.ByID)
	c.Check(rdb.PerUser, DeepEquals, loadedRdb.PerUser)
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

	// rule 3 should have expired, check that it's not included when getting rules
	rules := rdb.Rules(user)
	c.Check(rules, HasLen, 2, Commentf("rules: %+v", rules))
	c.Check(rules[0] == rule1 || rules[0] == rule2, Equals, true, Commentf("unexpected rule: %+v", rules[0]))
	c.Check(rules[1] == rule1 || rules[1] == rule2, Equals, true, Commentf("unexpected rule: %+v", rules[1]))
	c.Check(rules[0] != rules[1], Equals, true, Commentf("Rules returned duplicate rules: %+v", rules))

	allowed, err = rdb.IsPathAllowed(user, snap, iface, path1, "read")
	c.Assert(err, IsNil)
	c.Assert(allowed, Equals, false)

	// rule3 expiration should have recorded a notice
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

	origIface := iface

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
	c.Check(userRules, HasLen, 3)
OUTER_LOOP_USER:
	for _, rule := range []*requestrules.Rule{rule2, rule3, rule4} {
		for _, userRule := range userRules {
			if reflect.DeepEqual(rule, userRule) {
				continue OUTER_LOOP_USER
			}
		}
		c.Errorf("rule not found in userRules:\nrule: %+v\nuserRules: %+v", rule, userRules)
	}

	userSnapRules := rdb.RulesForSnap(user, snap)
	c.Check(userSnapRules, HasLen, 2)
OUTER_LOOP_USER_SNAP:
	for _, rule := range []*requestrules.Rule{rule3, rule4} {
		for _, userRule := range userSnapRules {
			if reflect.DeepEqual(rule, userRule) {
				continue OUTER_LOOP_USER_SNAP
			}
		}
		c.Errorf("rule not found in userRules:\nrule: %+v\nuserSnapRules: %+v", rule, userRules)
	}

	userInterfaceRules := rdb.RulesForInterface(user, origIface)
	c.Check(userInterfaceRules, HasLen, 2)
OUTER_LOOP_USER_INTERFACE:
	for _, rule := range []*requestrules.Rule{rule2, rule3} {
		for _, userRule := range userInterfaceRules {
			if reflect.DeepEqual(rule, userRule) {
				continue OUTER_LOOP_USER_INTERFACE
			}
		}
		c.Errorf("rule not found in userRules:\nrule: %+v\nuserInterfaceRules: %+v", rule, userRules)
	}

	userInterfaceRules = rdb.RulesForInterface(user, iface)
	c.Check(userInterfaceRules, HasLen, 1)
	c.Check(userInterfaceRules[0], DeepEquals, rule4)

	userSnapInterfaceRules := rdb.RulesForSnapInterface(user, snap, iface)
	c.Check(userSnapInterfaceRules, HasLen, 1)
	c.Check(userSnapInterfaceRules[0], DeepEquals, rule4)

	// Looking up these rules should not cause any notices
	c.Check(ruleNotices, HasLen, 0, Commentf("ruleNoticeIDs: %+v; rdb.ByID: %+v", ruleNotices, rdb.ByID))
}
