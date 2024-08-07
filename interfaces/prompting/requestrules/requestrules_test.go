package requestrules_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/interfaces/prompting"
	"github.com/snapcore/snapd/interfaces/prompting/patterns"
	"github.com/snapcore/snapd/interfaces/prompting/requestrules"
	"github.com/snapcore/snapd/logger"
)

func Test(t *testing.T) { TestingT(t) }

type noticeInfo struct {
	ruleID prompting.IDType
	data   map[string]string
}

func (ni *noticeInfo) String() string {
	return fmt.Sprintf("{\n\truleID: %s\n\tdata:   %#v\n}", ni.ruleID, ni.data)
}

type requestrulesSuite struct {
	defaultNotifyRule func(userID uint32, ruleID prompting.IDType, data map[string]string) error
	defaultUser       uint32
	ruleNotices       []*noticeInfo

	tmpdir string
}

var _ = Suite(&requestrulesSuite{})

func (s *requestrulesSuite) SetUpTest(c *C) {
	s.defaultUser = 1000
	s.defaultNotifyRule = func(userID uint32, ruleID prompting.IDType, data map[string]string) error {
		c.Check(userID, Equals, s.defaultUser)
		info := &noticeInfo{
			ruleID: ruleID,
			data:   data,
		}
		s.ruleNotices = append(s.ruleNotices, info)
		return nil
	}
	s.ruleNotices = make([]*noticeInfo, 0)
	s.tmpdir = c.MkDir()
	dirs.SetRootDir(s.tmpdir)
	c.Assert(os.MkdirAll(dirs.SnapdStateDir(dirs.GlobalRootDir), 0700), IsNil)
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
	notifyRule := func(userID uint32, ruleID prompting.IDType, data map[string]string) error {
		c.Errorf("unexpected rule notice with user %d and ID %s", userID, ruleID)
		return nil
	}
	rdb, _ := requestrules.New(notifyRule)

	snap := "lxd"
	iface := "home"
	outcome := prompting.OutcomeAllow
	lifespan := prompting.LifespanForever
	duration := ""
	permissions := []string{"read", "write", "execute"}

	for _, pattern := range []*patterns.PathPattern{
		mustParsePathPattern(c, "/home/test/Documents/**"),
		mustParsePathPattern(c, "/home/test/**/*.pdf"),
		mustParsePathPattern(c, "/home/test/*"),
	} {
		constraints := &prompting.Constraints{
			PathPattern: pattern,
			Permissions: permissions,
		}
		rule, err := rdb.PopulateNewRule(s.defaultUser, snap, iface, constraints, outcome, lifespan, duration)
		c.Check(err, IsNil)
		c.Check(rule.User, Equals, s.defaultUser)
		c.Check(rule.Snap, Equals, snap)
		c.Check(rule.Constraints, Equals, constraints)
		c.Check(rule.Outcome, Equals, outcome)
		c.Check(rule.Lifespan, Equals, lifespan)
		c.Check(rule.Expiration.IsZero(), Equals, true)
	}

	pathPattern := mustParsePathPattern(c, "/home/test/Pictures/**/*.{jpg,png,svg,tiff}")
	for _, perms := range [][]string{
		{"read", "fly", "write"},
		{"foo"},
	} {
		constraints := &prompting.Constraints{
			PathPattern: pathPattern,
			Permissions: perms,
		}
		rule, err := rdb.PopulateNewRule(s.defaultUser, snap, iface, constraints, outcome, lifespan, duration)
		c.Check(err, ErrorMatches, "invalid constraints: unsupported permission for home interface:.*")
		c.Check(rule, IsNil)
	}
}

func mustParsePathPattern(c *C, patternStr string) *patterns.PathPattern {
	pattern, err := patterns.ParsePathPattern(patternStr)
	c.Assert(err, IsNil)
	return pattern
}

func (s *requestrulesSuite) TestAddRemoveRuleSimple(c *C) {
	rdb, _ := requestrules.New(s.defaultNotifyRule)

	snap := "lxd"
	iface := "home"
	pathPattern := mustParsePathPattern(c, "/home/test/Documents/**")
	outcome := prompting.OutcomeAllow
	lifespan := prompting.LifespanForever
	duration := ""
	permissions := []string{"read", "write", "execute"}
	constraints := &prompting.Constraints{
		PathPattern: pathPattern,
		Permissions: permissions,
	}

	rule, err := rdb.AddRule(s.defaultUser, snap, iface, constraints, outcome, lifespan, duration)
	c.Assert(err, IsNil)

	s.checkNewNoticesSimple(c, []prompting.IDType{rule.ID}, nil)

	storedRule, err := rdb.RuleWithID(s.defaultUser, rule.ID)
	c.Assert(err, IsNil)
	c.Assert(storedRule, DeepEquals, rule)

	removedRule, err := rdb.RemoveRule(s.defaultUser, rule.ID)
	c.Assert(err, IsNil)
	c.Assert(removedRule, DeepEquals, rule)

	expectedData := map[string]string{"removed": "removed"}
	s.checkNewNoticesSimple(c, []prompting.IDType{removedRule.ID}, expectedData)

	c.Assert(rdb.Rules(s.defaultUser), HasLen, 0)
}

func (s *requestrulesSuite) checkNewNoticesSimple(c *C, expectedRuleIDs []prompting.IDType, expectedData map[string]string) {
	s.checkNewNotices(c, applyNotices(expectedRuleIDs, expectedData))
}

func applyNotices(expectedRuleIDs []prompting.IDType, expectedData map[string]string) []*noticeInfo {
	expectedNotices := make([]*noticeInfo, len(expectedRuleIDs))
	for i, id := range expectedRuleIDs {
		info := &noticeInfo{
			ruleID: id,
			data:   expectedData,
		}
		expectedNotices[i] = info
	}
	return expectedNotices
}

func (s *requestrulesSuite) checkNewNotices(c *C, expectedNotices []*noticeInfo) {
	c.Check(s.ruleNotices, DeepEquals, expectedNotices, Commentf("\nReceived: %s\nExpected: %s", s.ruleNotices, expectedNotices))
	s.ruleNotices = s.ruleNotices[:0]
}

func (s *requestrulesSuite) checkNewNoticesUnorderedSimple(c *C, expectedRuleIDs []prompting.IDType, expectedData map[string]string) {
	s.checkNewNoticesUnordered(c, applyNotices(expectedRuleIDs, expectedData))
}

func (s *requestrulesSuite) checkNewNoticesUnordered(c *C, expectedNotices []*noticeInfo) {
	sort.Slice(sortSliceParams(s.ruleNotices))
	sort.Slice(sortSliceParams(expectedNotices))
	s.checkNewNotices(c, expectedNotices)
}

func sortSliceParams(list []*noticeInfo) ([]*noticeInfo, func(i, j int) bool) {
	less := func(i, j int) bool {
		return list[i].ruleID < list[j].ruleID
	}
	return list, less
}

func (s *requestrulesSuite) TestAddRuleUnhappy(c *C) {
	rdb, _ := requestrules.New(s.defaultNotifyRule)

	snap := "lxd"
	iface := "home"
	pathPattern := mustParsePathPattern(c, "/home/test/Documents/**")
	outcome := prompting.OutcomeAllow
	lifespan := prompting.LifespanForever
	duration := ""
	permissions := []string{"read", "write", "execute"}
	constraints := &prompting.Constraints{
		PathPattern: pathPattern,
		Permissions: permissions,
	}

	storedRule, err := rdb.AddRule(s.defaultUser, snap, iface, constraints, outcome, lifespan, duration)
	c.Assert(err, IsNil)

	s.checkNewNoticesSimple(c, []prompting.IDType{storedRule.ID}, nil)

	conflictingPermissions := permissions[:1]
	conflictingConstraints := &prompting.Constraints{
		PathPattern: pathPattern,
		Permissions: conflictingPermissions,
	}
	_, err = rdb.AddRule(s.defaultUser, snap, iface, conflictingConstraints, outcome, lifespan, duration)
	c.Assert(err, ErrorMatches, fmt.Sprintf("^%s.*%s.*%s.*", requestrules.ErrPathPatternConflict, storedRule.ID.String(), conflictingPermissions[0]))

	// Error while adding rule should cause no notice to be issued
	s.checkNewNoticesSimple(c, []prompting.IDType{}, nil)

	badPermissions := []string{"foo"}
	badConstraints := &prompting.Constraints{
		PathPattern: pathPattern,
		Permissions: badPermissions,
	}
	_, err = rdb.AddRule(s.defaultUser, snap, iface, badConstraints, outcome, lifespan, duration)
	c.Assert(err, ErrorMatches, "invalid constraints: unsupported permission for home interface:.*")

	// Error while adding rule should cause no notice to be issued
	s.checkNewNoticesSimple(c, []prompting.IDType{}, nil)

	badOutcome := prompting.OutcomeType("secret third thing")
	_, err = rdb.AddRule(s.defaultUser, snap, iface, constraints, badOutcome, lifespan, duration)
	c.Assert(err, ErrorMatches, `internal error: invalid outcome.*`, Commentf("rdb.Rules(): %+v", s.marshalRules(c, rdb)))

	// Error while adding rule should cause no notice to be issued
	s.checkNewNoticesSimple(c, []prompting.IDType{}, nil)

	badLifespan := prompting.LifespanSingle
	_, err = rdb.AddRule(s.defaultUser, snap, iface, constraints, outcome, badLifespan, duration)
	c.Assert(err, Equals, requestrules.ErrLifespanSingle, Commentf("rdb.Rules(): %+v", s.marshalRules(c, rdb)))
}

func (s *requestrulesSuite) marshalRules(c *C, rdb *requestrules.RuleDB) string {
	rules := rdb.Rules(s.defaultUser)
	marshalled, err := json.Marshal(rules)
	c.Assert(err, IsNil)
	return string(marshalled)
}

func (s *requestrulesSuite) TestPatchRule(c *C) {
	rdb, _ := requestrules.New(s.defaultNotifyRule)

	snap := "lxd"
	iface := "home"
	pathPattern := mustParsePathPattern(c, "/home/test/Documents/**")
	outcome := prompting.OutcomeAllow
	lifespan := prompting.LifespanForever
	duration := ""
	permissions := []string{"read"}
	constraints := &prompting.Constraints{
		PathPattern: pathPattern,
		Permissions: permissions,
	}

	storedRule, err := rdb.AddRule(s.defaultUser, snap, iface, constraints, outcome, lifespan, duration)
	c.Assert(err, IsNil)
	c.Assert(rdb.Rules(s.defaultUser), HasLen, 1)

	s.checkNewNoticesSimple(c, []prompting.IDType{storedRule.ID}, nil)

	conflictingPermission := "write"

	otherPathPattern := mustParsePathPattern(c, "/home/test/Pictures/**/*.png")
	otherPermissions := []string{
		conflictingPermission,
	}
	otherConstraints := &prompting.Constraints{
		PathPattern: otherPathPattern,
		Permissions: otherPermissions,
	}
	otherRule, err := rdb.AddRule(s.defaultUser, snap, iface, otherConstraints, outcome, lifespan, duration)
	c.Assert(err, IsNil)
	c.Assert(rdb.Rules(s.defaultUser), HasLen, 2)

	s.checkNewNoticesSimple(c, []prompting.IDType{otherRule.ID}, nil)

	// Check that patching with the original values results in an identical rule
	unchangedRule1, err := rdb.PatchRule(s.defaultUser, storedRule.ID, constraints, outcome, lifespan, duration)
	c.Assert(err, IsNil)
	// Timestamp should be different, the rest should be the same
	unchangedRule1.Timestamp = storedRule.Timestamp
	c.Assert(unchangedRule1, DeepEquals, storedRule)
	c.Assert(rdb.Rules(s.defaultUser), HasLen, 2, Commentf("rdb.Rules(): %+v", s.marshalRules(c, rdb)))

	// Though rule was patched with the same values as it originally had, the
	// timestamp was changed, and thus a notice should be issued for it.
	s.checkNewNoticesSimple(c, []prompting.IDType{unchangedRule1.ID}, nil)

	// Check that patching with unset values results in an identical rule
	unchangedRule2, err := rdb.PatchRule(s.defaultUser, storedRule.ID, nil, prompting.OutcomeUnset, prompting.LifespanUnset, "")
	c.Assert(err, IsNil)
	// Timestamp should be different, the rest should be the same
	unchangedRule2.Timestamp = storedRule.Timestamp
	c.Assert(unchangedRule2, DeepEquals, storedRule)
	c.Assert(rdb.Rules(s.defaultUser), HasLen, 2)

	// Though rule was patched with unset values, and thus was unchanged aside
	// from the timestamp, a notice should be issued for it.
	s.checkNewNoticesSimple(c, []prompting.IDType{unchangedRule2.ID}, nil)

	newPathPattern := otherPathPattern
	newOutcome := prompting.OutcomeDeny
	newLifespan := prompting.LifespanTimespan
	newDuration := "1s"
	newPermissions := []string{"execute"}
	newConstraints := &prompting.Constraints{
		PathPattern: newPathPattern,
		Permissions: newPermissions,
	}
	patchedRule, err := rdb.PatchRule(s.defaultUser, storedRule.ID, newConstraints, newOutcome, newLifespan, newDuration)
	c.Assert(err, IsNil)
	c.Assert(rdb.Rules(s.defaultUser), HasLen, 2)

	s.checkNewNoticesSimple(c, []prompting.IDType{patchedRule.ID}, nil)

	badConstraints := &prompting.Constraints{
		PathPattern: pathPattern,
		Permissions: []string{"fly"},
	}
	output, err := rdb.PatchRule(s.defaultUser, storedRule.ID, badConstraints, outcome, lifespan, duration)
	c.Assert(err, ErrorMatches, "invalid constraints: unsupported permission.*")
	c.Assert(output, IsNil)
	c.Assert(rdb.Rules(s.defaultUser), HasLen, 2, Commentf("rdb.Rules(): %+v", s.marshalRules(c, rdb)))

	// Error while patching rule should cause no notice to be issued
	s.checkNewNoticesSimple(c, []prompting.IDType{}, nil)

	currentRule, err := rdb.RuleWithID(s.defaultUser, storedRule.ID)
	c.Assert(err, IsNil)
	c.Assert(currentRule, DeepEquals, patchedRule)

	conflictingPermissions := append(newPermissions, conflictingPermission)
	conflictingConstraints := &prompting.Constraints{
		PathPattern: newPathPattern,
		Permissions: conflictingPermissions,
	}
	output, err = rdb.PatchRule(s.defaultUser, storedRule.ID, conflictingConstraints, newOutcome, newLifespan, newDuration)
	c.Assert(err, ErrorMatches, fmt.Sprintf("^%s.*%s.*%s.*", requestrules.ErrPathPatternConflict, otherRule.ID.String(), conflictingPermission))
	c.Assert(output, IsNil)
	c.Assert(rdb.Rules(s.defaultUser), HasLen, 2)

	// Permission conflicts while patching rule should cause no notice to be issued
	s.checkNewNoticesSimple(c, []prompting.IDType{}, nil)

	currentRule, err = rdb.RuleWithID(s.defaultUser, storedRule.ID)
	c.Assert(err, IsNil)
	c.Assert(currentRule, DeepEquals, patchedRule)
}

func (s *requestrulesSuite) TestRemoveRulesForSnap(c *C) {
	rdb, _ := requestrules.New(s.defaultNotifyRule)

	snap := "lxd"
	otherSnap := "nextcloud"
	iface := "home"
	pathPattern := mustParsePathPattern(c, "/home/test/Documents/**")
	outcome := prompting.OutcomeAllow
	lifespan := prompting.LifespanForever
	duration := ""
	permissions := []string{"read", "write", "execute"}
	constraints := &prompting.Constraints{
		PathPattern: pathPattern,
		Permissions: permissions[:1],
	}
	otherConstraints := &prompting.Constraints{
		PathPattern: pathPattern,
		Permissions: permissions[1:],
	}

	rule1, err := rdb.AddRule(s.defaultUser, snap, iface, constraints, outcome, lifespan, duration)
	c.Assert(err, IsNil)
	c.Assert(rule1, NotNil)

	rule2, err := rdb.AddRule(s.defaultUser, otherSnap, iface, constraints, outcome, lifespan, duration)
	c.Assert(err, IsNil)
	c.Assert(rule2, NotNil)

	rule3, err := rdb.AddRule(s.defaultUser, snap, iface, otherConstraints, outcome, lifespan, duration)
	c.Assert(err, IsNil)
	c.Assert(rule3, NotNil)

	s.checkNewNoticesSimple(c, []prompting.IDType{rule1.ID, rule2.ID, rule3.ID}, nil)

	removed := rdb.RemoveRulesForSnap(s.defaultUser, snap)
	c.Check(removed, HasLen, 2, Commentf("expected to remove 2 rules but instead removed: %+v", removed))
	c.Check(removed[0] == rule1 || removed[0] == rule3, Equals, true, Commentf("unexpected rule: %+v", removed[0]))
	c.Check(removed[1] == rule1 || removed[1] == rule3, Equals, true, Commentf("unexpected rule: %+v", removed[1]))
	c.Check(removed[0] != removed[1], Equals, true, Commentf("removed duplicate rules: %+v", removed))

	expectedData := map[string]string{"removed": "removed"}
	s.checkNewNoticesUnorderedSimple(c, []prompting.IDType{rule1.ID, rule3.ID}, expectedData)
}

func (s *requestrulesSuite) TestRemoveRulesForSnapInterface(c *C) {
	rdb, _ := requestrules.New(s.defaultNotifyRule)

	snap := "lxd"
	otherSnap := "nextcloud"
	iface := "home"
	otherIface := "camera"
	pathPattern := mustParsePathPattern(c, "/home/test/Documents/**")
	outcome := prompting.OutcomeAllow
	lifespan := prompting.LifespanForever
	duration := ""
	permissions := []string{"read", "write", "execute"}
	constraints := &prompting.Constraints{
		PathPattern: pathPattern,
		Permissions: permissions[:1],
	}
	otherConstraints := &prompting.Constraints{
		PathPattern: pathPattern,
		Permissions: permissions[1:],
	}

	rule1, err := rdb.AddRule(s.defaultUser, snap, iface, constraints, outcome, lifespan, duration)
	c.Assert(err, IsNil)
	c.Assert(rule1, NotNil)

	rule2, err := rdb.AddRule(s.defaultUser, otherSnap, iface, constraints, outcome, lifespan, duration)
	c.Assert(err, IsNil)
	c.Assert(rule2, NotNil)

	rule3, err := rdb.AddRule(s.defaultUser, snap, iface, otherConstraints, outcome, lifespan, duration)
	c.Assert(err, IsNil)
	c.Assert(rule3, NotNil)

	// For now, impossible to add a rule for an interface other than "home", so
	// must adjust it after it's been added.
	rule3.Interface = otherIface

	s.checkNewNoticesSimple(c, []prompting.IDType{rule1.ID, rule2.ID, rule3.ID}, nil)

	removed := rdb.RemoveRulesForSnapInterface(s.defaultUser, snap, iface)
	c.Check(removed, HasLen, 1, Commentf("expected to remove 2 rules but instead removed: %+v", removed))
	c.Check(removed[0] == rule1, Equals, true, Commentf("unexpected rule: %+v", removed[0]))

	expectedData := map[string]string{"removed": "removed"}
	s.checkNewNoticesSimple(c, []prompting.IDType{rule1.ID}, expectedData)
}

func (s *requestrulesSuite) TestRuleWithID(c *C) {
	rdb, _ := requestrules.New(s.defaultNotifyRule)

	snap := "lxd"
	iface := "home"
	pathPattern := mustParsePathPattern(c, "/home/test/Documents/**")
	outcome := prompting.OutcomeAllow
	lifespan := prompting.LifespanForever
	duration := ""
	permissions := []string{"read", "write", "execute"}
	constraints := &prompting.Constraints{
		PathPattern: pathPattern,
		Permissions: permissions,
	}

	newRule, err := rdb.AddRule(s.defaultUser, snap, iface, constraints, outcome, lifespan, duration)
	c.Assert(err, IsNil)
	c.Assert(newRule, NotNil)

	s.checkNewNoticesSimple(c, []prompting.IDType{newRule.ID}, nil)

	accessedRule, err := rdb.RuleWithID(s.defaultUser, newRule.ID)
	c.Check(err, IsNil)
	c.Check(accessedRule, DeepEquals, newRule)

	accessedRule, err = rdb.RuleWithID(s.defaultUser, prompting.IDType(1234567890))
	c.Check(err, Equals, requestrules.ErrRuleIDNotFound)
	c.Check(accessedRule, IsNil)

	accessedRule, err = rdb.RuleWithID(s.defaultUser+1, newRule.ID)
	c.Check(err, Equals, requestrules.ErrUserNotAllowed)
	c.Check(accessedRule, IsNil)

	// Reading (or failing to read) a notice should not record a notice
	s.checkNewNoticesSimple(c, []prompting.IDType{}, nil)
}

func (s *requestrulesSuite) TestRefreshTreeEnforceConsistencySimple(c *C) {
	rdb, _ := requestrules.New(s.defaultNotifyRule)

	snap := "lxd"
	iface := "home"
	pathPattern := mustParsePathPattern(c, "/home/test/{Documents,Downloads}/**")
	outcome := prompting.OutcomeAllow
	lifespan := prompting.LifespanForever
	duration := ""
	permissions := []string{"read", "write", "execute"}
	constraints := &prompting.Constraints{
		PathPattern: pathPattern,
		Permissions: permissions,
	}

	rule, err := rdb.PopulateNewRule(s.defaultUser, snap, iface, constraints, outcome, lifespan, duration)
	c.Assert(err, IsNil)
	rdb.InjectRule(rule)
	notifyEveryRule := true
	rdb.RefreshTreeEnforceConsistency(notifyEveryRule)

	// Test that RefreshTreeEnforceConsistency results in a single notice
	s.checkNewNoticesSimple(c, []prompting.IDType{rule.ID}, nil)

	userEntry, exists := rdb.PerUser()[s.defaultUser]
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
		c.Assert(permissionEntry.VariantEntries, HasLen, 2)

		checkVariants := func(index int, variant patterns.PatternVariant) {
			entry, exists := permissionEntry.VariantEntries[variant.String()]
			c.Check(exists, Equals, true)
			c.Check(entry.RuleID, Equals, rule.ID)
		}
		pathPattern.RenderAllVariants(checkVariants)
	}
}

func (s *requestrulesSuite) TestRefreshTreeEnforceConsistencyComplex(c *C) {
	rdb, _ := requestrules.New(s.defaultNotifyRule)

	snap := "lxd"
	iface := "home"
	// Make sure all pattern variants include "/home/test/Documents/**/foo.txt"
	outcome := prompting.OutcomeAllow
	lifespan := prompting.LifespanForever
	duration := ""
	permissions := []string{"read", "write", "execute"}

	// Create two rules with early timestamps
	constraints1 := &prompting.Constraints{
		PathPattern: mustParsePathPattern(c, "/home/test/{Documents,Downloads}/**/foo.txt"),
		Permissions: copyPermissions(permissions[:1]),
	}
	earlyTsRule1, err := rdb.PopulateNewRule(s.defaultUser, snap, iface, constraints1, outcome, lifespan, duration)
	c.Assert(err, IsNil)
	var timeZero time.Time
	earlyTsRule1.Timestamp = timeZero
	time.Sleep(time.Millisecond)
	constraints2 := &prompting.Constraints{
		PathPattern: mustParsePathPattern(c, "/home/test/{Downloads,Documents/**}/foo.txt"),
		Permissions: copyPermissions(permissions[:1]),
	}
	earlyTsRule2, err := rdb.PopulateNewRule(s.defaultUser, snap, iface, constraints2, outcome, lifespan, duration)
	c.Assert(err, IsNil)
	earlyTsRule2.Timestamp = timeZero.Add(time.Second)
	rdb.InjectRule(earlyTsRule1)
	rdb.InjectRule(earlyTsRule2)

	c.Assert(earlyTsRule1, Not(Equals), earlyTsRule2)

	// The former should be overwritten by the latter during RefreshTreeEnforceConsistency
	notifyEveryRule := false
	rdb.RefreshTreeEnforceConsistency(notifyEveryRule)
	c.Assert(rdb.Rules(s.defaultUser), HasLen, 1, Commentf("rdb.Rules(): %+v", rdb.Rules(s.defaultUser)))
	_, err = rdb.RuleWithID(s.defaultUser, earlyTsRule2.ID)
	c.Assert(err, IsNil)
	// The latter should be overwritten by any conflicting rule which has a later timestamp

	// Since notifyEveryRule = false, only one notice should be issued, for the overwritten rule
	expectedData := map[string]string{"removed": "conflict"}
	s.checkNewNoticesSimple(c, []prompting.IDType{earlyTsRule1.ID}, expectedData)

	// Create a rule with the earliest timestamp (aside from earlyTsRule[12]),
	// which will be totally overwritten when attempting to add later
	earliestConstraints := &prompting.Constraints{
		PathPattern: mustParsePathPattern(c, "/home/test/Documents/**/{foo,bar,baz}.txt"),
		Permissions: copyPermissions(permissions),
	}
	earliestRule, err := rdb.PopulateNewRule(s.defaultUser, snap, iface, earliestConstraints, outcome, lifespan, duration)
	c.Assert(err, IsNil)

	// Create and add a rule which will overwrite the rule with the timestamp 1s after zero
	initialPathPattern := mustParsePathPattern(c, "/home/test/{Music,Pictures,Videos,Documents}/**/foo.txt")
	initialConstraints := &prompting.Constraints{
		PathPattern: initialPathPattern,
		Permissions: copyPermissions(permissions),
	}
	initialRule, err := rdb.PopulateNewRule(s.defaultUser, snap, iface, initialConstraints, outcome, lifespan, duration)
	c.Assert(err, IsNil)
	rdb.InjectRule(initialRule)
	rdb.RefreshTreeEnforceConsistency(notifyEveryRule)

	// Expect notice for rule with timestamp of 1s after zero, not initialRule
	expectedData = map[string]string{"removed": "conflict"}
	s.checkNewNoticesSimple(c, []prompting.IDType{earlyTsRule2.ID}, expectedData)

	// Check that rule with bad timestamp was overwritten
	c.Assert(rdb.Rules(s.defaultUser), HasLen, 1, Commentf("rdb.Rules(): %+v", rdb.Rules(s.defaultUser)))
	initialRuleRet, err := rdb.RuleWithID(s.defaultUser, initialRule.ID)
	c.Assert(err, IsNil)
	c.Assert(initialRuleRet.Constraints.Permissions, DeepEquals, permissions)

	// Create rule with expiration in the past, which will be immediately be discarded without conflicting with other rules
	expiredConstraints := &prompting.Constraints{
		PathPattern: mustParsePathPattern(c, "/home/test/Documents/**/foo.txt"),
		Permissions: copyPermissions(permissions),
	}
	expiredRule, err := rdb.PopulateNewRule(s.defaultUser, snap, iface, expiredConstraints, outcome, prompting.LifespanTimespan, "1s")
	c.Assert(err, IsNil)
	expiredRule.Expiration = time.Now().Add(-10 * time.Second)

	rdb.InjectRule(expiredRule)
	rdb.RefreshTreeEnforceConsistency(notifyEveryRule)

	// Expect notice for only expiredRule
	expectedData = map[string]string{"removed": "expired"}
	s.checkNewNoticesSimple(c, []prompting.IDType{expiredRule.ID}, expectedData)

	c.Assert(rdb.Rules(s.defaultUser), HasLen, 1, Commentf("rdb.Rules(): %+v", rdb.Rules(s.defaultUser)))

	_, err = rdb.RuleWithID(s.defaultUser, expiredRule.ID)
	c.Assert(err, Equals, requestrules.ErrRuleIDNotFound)

	initialRuleRet, err = rdb.RuleWithID(s.defaultUser, initialRule.ID)
	c.Assert(err, IsNil)
	c.Assert(initialRuleRet.Constraints.Permissions, DeepEquals, permissions)

	// Create rule with invalid permissions, which will be immediately be discarded without conflicting with other rules
	invalidConstraints := &prompting.Constraints{
		PathPattern: mustParsePathPattern(c, "/home/test/Documents/**/foo.txt"),
		Permissions: copyPermissions(permissions),
	}
	invalidRule, err := rdb.PopulateNewRule(s.defaultUser, snap, iface, invalidConstraints, outcome, prompting.LifespanTimespan, "1s")
	c.Assert(err, IsNil)
	invalidRule.Constraints.Permissions = []string{"fly"}

	rdb.InjectRule(invalidRule)
	rdb.RefreshTreeEnforceConsistency(notifyEveryRule)

	// Expect notice for only invalidRule
	expectedData = map[string]string{"removed": "invalid"}
	s.checkNewNoticesSimple(c, []prompting.IDType{invalidRule.ID}, expectedData)

	c.Assert(rdb.Rules(s.defaultUser), HasLen, 1, Commentf("rdb.Rules(): %+v", rdb.Rules(s.defaultUser)))

	_, err = rdb.RuleWithID(s.defaultUser, invalidRule.ID)
	c.Assert(err, Equals, requestrules.ErrRuleIDNotFound)

	initialRuleRet, err = rdb.RuleWithID(s.defaultUser, initialRule.ID)
	c.Assert(err, IsNil)
	c.Assert(initialRuleRet.Constraints.Permissions, DeepEquals, permissions)

	// Create newer rule which will overwrite all but the first permission of initialRule
	newPathPattern := mustParsePathPattern(c, "/home/test/Documents{,/,/**/foo.{txt,md,pdf}}")
	newRulePermissions := permissions[1:]
	newConstraints := &prompting.Constraints{
		PathPattern: newPathPattern,
		Permissions: copyPermissions(newRulePermissions),
	}
	newRule, err := rdb.PopulateNewRule(s.defaultUser, snap, iface, newConstraints, outcome, lifespan, duration)
	c.Assert(err, IsNil)

	rdb.InjectRule(newRule)
	rdb.InjectRule(earliestRule)
	rdb.RefreshTreeEnforceConsistency(notifyEveryRule)

	// Expect notice for initialRule and earliestRule, not newRule
	expectedNotices := []*noticeInfo{
		{
			ruleID: initialRule.ID,
			data:   nil,
		},
		{
			ruleID: earliestRule.ID,
			data:   map[string]string{"removed": "conflict"},
		},
	}
	s.checkNewNoticesUnordered(c, expectedNotices)

	c.Assert(rdb.Rules(s.defaultUser), HasLen, 2, Commentf("rdb.Rules(): %+v", rdb.Rules(s.defaultUser)))

	_, err = rdb.RuleWithID(s.defaultUser, earliestRule.ID)
	c.Assert(err, Equals, requestrules.ErrRuleIDNotFound)

	initialRuleRet, err = rdb.RuleWithID(s.defaultUser, initialRule.ID)
	c.Assert(err, IsNil)
	c.Assert(initialRuleRet.Constraints.Permissions, DeepEquals, permissions[:1])

	newRuleRet, err := rdb.RuleWithID(s.defaultUser, newRule.ID)
	c.Assert(err, IsNil)
	c.Assert(newRuleRet.Constraints.Permissions, DeepEquals, newRulePermissions, Commentf("newRulePermissions: %+v", newRulePermissions))

	c.Assert(rdb.PerUser(), HasLen, 1)

	userEntry, exists := rdb.PerUser()[s.defaultUser]
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
		var pathPattern *patterns.PathPattern
		var ruleID prompting.IDType
		if i == 0 {
			ruleID = initialRule.ID
			pathPattern = initialPathPattern
		} else {
			ruleID = newRule.ID
			pathPattern = newPathPattern
		}
		c.Assert(permissionEntry.VariantEntries, HasLen, pathPattern.NumVariants())
		checkVariants := func(index int, variant patterns.PatternVariant) {
			variantEntry, exists := permissionEntry.VariantEntries[variant.String()]
			c.Check(exists, Equals, true)
			c.Check(variantEntry.RuleID, Equals, ruleID)
		}
		pathPattern.RenderAllVariants(checkVariants)
	}
}

func copyPermissions(permissions []string) []string {
	newPermissions := make([]string, len(permissions))
	copy(newPermissions, permissions)
	return newPermissions
}

func (s *requestrulesSuite) TestNewSaveLoad(c *C) {
	logbuf, restore := logger.MockLogger()
	defer restore()

	doNotNotifyRule := func(userID uint32, ruleID prompting.IDType, data map[string]string) error {
		return nil
	}
	rdb, err := requestrules.New(doNotNotifyRule)
	c.Check(err, IsNil)
	c.Check(fmt.Errorf("%s", strings.TrimSpace(logbuf.String())), ErrorMatches, ".*cannot open rules database file:.*")

	snap := "lxd"
	iface := "home"
	pathPattern := mustParsePathPattern(c, "/home/test/Documents/**")
	outcome := prompting.OutcomeAllow
	lifespan := prompting.LifespanForever
	duration := ""
	permissions := []string{"read", "write", "execute"}

	constraints1 := &prompting.Constraints{
		PathPattern: pathPattern,
		Permissions: permissions[:1],
	}
	rule1, err := rdb.AddRule(s.defaultUser, snap, iface, constraints1, outcome, lifespan, duration)
	c.Assert(err, IsNil)
	constraints2 := &prompting.Constraints{
		PathPattern: pathPattern,
		Permissions: permissions[1:2],
	}
	rule2, err := rdb.AddRule(s.defaultUser, snap, iface, constraints2, outcome, lifespan, duration)
	c.Assert(err, IsNil)
	constraints3 := &prompting.Constraints{
		PathPattern: pathPattern,
		Permissions: permissions[2:],
	}
	rule3, err := rdb.AddRule(s.defaultUser, snap, iface, constraints3, outcome, lifespan, duration)
	c.Assert(err, IsNil)

	loadedRdb, err := requestrules.New(s.defaultNotifyRule)
	c.Assert(err, IsNil)

	s.checkNewNoticesUnorderedSimple(c, []prompting.IDType{rule1.ID, rule2.ID, rule3.ID}, nil)

	// DeepEquals does not treat time.Time well, so manually validate them and
	// set them to be explicitly equal so the DeepEquals check succeeds.
	c.Check(loadedRdb.Rules(s.defaultUser), HasLen, len(rdb.Rules(s.defaultUser)))
	for _, rule := range rdb.Rules(s.defaultUser) {
		loadedRule, err := loadedRdb.RuleWithID(s.defaultUser, rule.ID)
		c.Assert(err, IsNil, Commentf("missing rule after loading: %+v", rule))
		c.Check(rule.Timestamp.Equal(loadedRule.Timestamp), Equals, true, Commentf("%s != %s", rule.Timestamp, loadedRule.Timestamp))
		rule.Timestamp = loadedRule.Timestamp
	}
	c.Check(rdb.Rules(s.defaultUser), DeepEquals, loadedRdb.Rules(s.defaultUser))
	c.Check(rdb.PerUser(), DeepEquals, loadedRdb.PerUser())
}

func (s *requestrulesSuite) TestIsPathAllowed(c *C) {
	patterns := make(map[string]prompting.OutcomeType)
	patterns["/home/test/Documents/**"] = prompting.OutcomeAllow
	patterns["/home/test/Documents/foo/**"] = prompting.OutcomeDeny
	patterns["/home/test/Documents/foo/bar/**"] = prompting.OutcomeAllow
	patterns["/home/test/Documents/foo/bar/baz/**"] = prompting.OutcomeDeny
	patterns["/home/test/Documents/foo/bar/baz/**/{fizz,buzz}"] = prompting.OutcomeAllow
	patterns["/home/test/**/*.png"] = prompting.OutcomeAllow
	patterns["/home/test/**/*.jpg"] = prompting.OutcomeDeny

	rdb, _ := requestrules.New(s.defaultNotifyRule)

	snap := "lxd"
	iface := "home"
	lifespan := prompting.LifespanForever
	duration := ""
	permissions := []string{"read", "write", "execute"}

	for pattern, outcome := range patterns {
		newConstraints := &prompting.Constraints{
			PathPattern: mustParsePathPattern(c, pattern),
			Permissions: permissions,
		}
		newRule, err := rdb.AddRule(s.defaultUser, snap, iface, newConstraints, outcome, lifespan, duration)
		c.Assert(err, IsNil)

		s.checkNewNoticesSimple(c, []prompting.IDType{newRule.ID}, nil)
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
		result, err := rdb.IsPathAllowed(s.defaultUser, snap, iface, path, permissions[0])
		c.Assert(err, IsNil, Commentf("path: %s: error: %v\nrdb.Rules(): %+v", path, err, rdb.Rules(s.defaultUser)))
		c.Assert(result, Equals, expected, Commentf("path: %s: expected %b but got %b\nrdb.Rules(): %+v", path, expected, result, rdb.Rules(s.defaultUser)))

		// Matching against rules should not cause any notices
		s.checkNewNoticesSimple(c, []prompting.IDType{}, nil)
	}
}

func (s *requestrulesSuite) TestRuleExpiration(c *C) {
	rdb, _ := requestrules.New(s.defaultNotifyRule)

	snap := "lxd"
	iface := "home"
	permissions := []string{"read", "write", "execute"}

	pathPattern := mustParsePathPattern(c, "/home/test/**")
	outcome := prompting.OutcomeAllow
	lifespan := prompting.LifespanForever
	duration := ""
	constraints1 := &prompting.Constraints{
		PathPattern: pathPattern,
		Permissions: permissions,
	}
	rule1, err := rdb.AddRule(s.defaultUser, snap, iface, constraints1, outcome, lifespan, duration)
	c.Assert(err, IsNil)

	s.checkNewNoticesSimple(c, []prompting.IDType{rule1.ID}, nil)

	pathPattern = mustParsePathPattern(c, "/home/test/Pictures/**")
	outcome = prompting.OutcomeDeny
	lifespan = prompting.LifespanTimespan
	duration = "2s"
	constraints2 := &prompting.Constraints{
		PathPattern: pathPattern,
		Permissions: permissions,
	}
	rule2, err := rdb.AddRule(s.defaultUser, snap, iface, constraints2, outcome, lifespan, duration)
	c.Assert(err, IsNil)

	s.checkNewNoticesSimple(c, []prompting.IDType{rule2.ID}, nil)

	pathPattern = mustParsePathPattern(c, "/home/test/Pictures/**/*.png")
	outcome = prompting.OutcomeAllow
	lifespan = prompting.LifespanTimespan
	duration = "1s"
	constraints3 := &prompting.Constraints{
		PathPattern: pathPattern,
		Permissions: permissions,
	}
	rule3, err := rdb.AddRule(s.defaultUser, snap, iface, constraints3, outcome, lifespan, duration)
	c.Assert(err, IsNil)

	s.checkNewNoticesSimple(c, []prompting.IDType{rule3.ID}, nil)

	path1 := "/home/test/Pictures/img.png"
	path2 := "/home/test/Pictures/img.jpg"

	allowed, err := rdb.IsPathAllowed(s.defaultUser, snap, iface, path1, "read")
	c.Check(err, IsNil)
	c.Check(allowed, Equals, true, Commentf("rdb.Rules(): %+v", rdb.Rules(s.defaultUser)))
	allowed, err = rdb.IsPathAllowed(s.defaultUser, snap, iface, path2, "read")
	c.Check(err, IsNil)
	c.Check(allowed, Equals, false)

	// No rules expired, so should not cause a notice
	s.checkNewNoticesSimple(c, []prompting.IDType{}, nil)

	time.Sleep(time.Second)

	// rule 3 should have expired, check that it's not included when getting rules
	rules := rdb.Rules(s.defaultUser)
	c.Check(rules, HasLen, 2, Commentf("rules: %+v", rules))
	c.Check(rules[0] == rule1 || rules[0] == rule2, Equals, true, Commentf("unexpected rule: %+v", rules[0]))
	c.Check(rules[1] == rule1 || rules[1] == rule2, Equals, true, Commentf("unexpected rule: %+v", rules[1]))
	c.Check(rules[0] != rules[1], Equals, true, Commentf("Rules returned duplicate rules: %+v", rules))

	allowed, err = rdb.IsPathAllowed(s.defaultUser, snap, iface, path1, "read")
	c.Check(err, IsNil)
	c.Check(allowed, Equals, false)

	// rule3 expiration should have recorded a notice
	expectedData := map[string]string{"removed": "expired"}
	s.checkNewNoticesSimple(c, []prompting.IDType{rule3.ID}, expectedData)

	allowed, err = rdb.IsPathAllowed(s.defaultUser, snap, iface, path2, "read")
	c.Check(err, IsNil)
	c.Check(allowed, Equals, false)

	// No rules newly expired, so should not cause a notice
	s.checkNewNoticesSimple(c, []prompting.IDType{}, nil)

	time.Sleep(time.Second)

	// Matches rule1.
	// Meanwhile, rule2 expires.
	allowed, err = rdb.IsPathAllowed(s.defaultUser, snap, iface, path1, "read")
	c.Check(err, Equals, nil)
	c.Check(allowed, Equals, true)

	expectedData = map[string]string{"removed": "expired"}
	s.checkNewNoticesSimple(c, []prompting.IDType{rule2.ID}, expectedData)

	allowed, err = rdb.IsPathAllowed(s.defaultUser, snap, iface, path2, "read")
	c.Check(err, IsNil)
	c.Check(allowed, Equals, true)

	allowed, err = rdb.IsPathAllowed(s.defaultUser, snap, iface, path1, "read")
	c.Check(err, IsNil)
	c.Check(allowed, Equals, true)

	// No rules newly expired, so should not cause a notice
	s.checkNewNoticesSimple(c, []prompting.IDType{}, nil)
}

func (s *requestrulesSuite) TestRulesLookup(c *C) {
	rdb, _ := requestrules.New(s.defaultNotifyRule)

	var origUser uint32 = s.defaultUser
	snap := "lxd"
	iface := "home"
	pathPattern := mustParsePathPattern(c, "/home/test/Documents/**")
	outcome := prompting.OutcomeAllow
	lifespan := prompting.LifespanForever
	duration := ""
	permissions := []string{"read", "write", "execute"}
	constraints := &prompting.Constraints{
		PathPattern: pathPattern,
		Permissions: permissions,
	}

	origIface := iface

	rule1, err := rdb.AddRule(origUser, snap, iface, constraints, outcome, lifespan, duration)
	c.Assert(err, IsNil)

	s.checkNewNoticesSimple(c, []prompting.IDType{rule1.ID}, nil)

	user := origUser + 1
	rule2, err := rdb.AddRule(user, snap, iface, constraints, outcome, lifespan, duration)
	c.Assert(err, IsNil)

	s.checkNewNoticesSimple(c, []prompting.IDType{rule2.ID}, nil)

	snap = "nextcloud"
	rule3, err := rdb.AddRule(user, snap, iface, constraints, outcome, lifespan, duration)
	c.Assert(err, IsNil)

	s.checkNewNoticesSimple(c, []prompting.IDType{rule3.ID}, nil)

	// TODO: add rule for other interface once other interfaces are supported
	// iface = "camera"
	// constraints.Permissions = []string{"access"}
	iface = "home"
	constraints.PathPattern = mustParsePathPattern(c, "/home/test/foo")
	rule4, err := rdb.AddRule(user, snap, iface, constraints, outcome, lifespan, duration)
	c.Assert(err, IsNil)

	s.checkNewNoticesSimple(c, []prompting.IDType{rule4.ID}, nil)

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
	// TODO: make this 2 when rule4 is for another interface
	// c.Check(userInterfaceRules, HasLen, 2)
	c.Check(userInterfaceRules, HasLen, 3)
OUTER_LOOP_USER_INTERFACE:
	// TODO: remove rule4 when rule4 is for another interface
	// for _, rule := range []*requestrules.Rule{rule2, rule3} {
	for _, rule := range []*requestrules.Rule{rule2, rule3, rule4} {
		for _, userRule := range userInterfaceRules {
			if reflect.DeepEqual(rule, userRule) {
				continue OUTER_LOOP_USER_INTERFACE
			}
		}
		c.Errorf("rule not found in userRules:\nrule: %+v\nuserInterfaceRules: %+v", rule, userRules)
	}

	// TODO: check these once rule4 is for another interface
	// userInterfaceRules = rdb.RulesForInterface(user, iface)
	// c.Check(userInterfaceRules, HasLen, 1)
	// c.Check(userInterfaceRules[0], DeepEquals, rule4)

	// userSnapInterfaceRules := rdb.RulesForSnapInterface(user, snap, iface)
	// c.Check(userSnapInterfaceRules, HasLen, 1)
	// c.Check(userSnapInterfaceRules[0], DeepEquals, rule4)

	// Looking up these rules should not cause any notices
	s.checkNewNoticesSimple(c, []prompting.IDType{}, nil)
}
