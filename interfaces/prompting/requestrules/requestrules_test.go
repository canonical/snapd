package requestrules_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/interfaces/prompting"
	"github.com/snapcore/snapd/interfaces/prompting/patterns"
	"github.com/snapcore/snapd/interfaces/prompting/requestrules"
	"github.com/snapcore/snapd/testutil"
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
	c.Check(filepath.Join(prompting.StateDir(), "request-rule-max-id"), testutil.FileAbsent)

	rdb, _ := requestrules.New(s.defaultNotifyRule)

	// ID file initialized
	c.Check(filepath.Join(prompting.StateDir(), "request-rule-max-id"), testutil.FileEquals,
		[]byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00})
	// no rules yet
	c.Check(filepath.Join(prompting.StateDir(), "request-rules.json"), testutil.FileAbsent)

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

	c.Check(filepath.Join(prompting.StateDir(), "request-rules.json"), testutil.FilePresent)

	storedRule, err := rdb.RuleWithID(s.defaultUser, rule.ID)
	c.Assert(err, IsNil)
	c.Assert(storedRule, DeepEquals, rule)

	removedRule, err := rdb.RemoveRule(s.defaultUser, rule.ID)
	c.Assert(err, IsNil)
	c.Assert(removedRule, DeepEquals, rule)

	// no rules, so only a top level JSON serialization artifact
	c.Check(filepath.Join(prompting.StateDir(), "request-rules.json"), testutil.FileEquals, `{"rules":[]}`)

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
	c.Assert(err, ErrorMatches, fmt.Sprintf("^cannot add rule: %s.*%s.*%s.*", requestrules.ErrPathPatternConflict, storedRule.ID.String(), conflictingPermissions[0]))

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
	c.Assert(err, ErrorMatches, `internal error: invalid outcome.*`, Commentf("rdb.Rules(): %s", s.marshalRules(c, rdb)))

	// Error while adding rule should cause no notice to be issued
	s.checkNewNoticesSimple(c, []prompting.IDType{}, nil)

	badLifespan := prompting.LifespanSingle
	_, err = rdb.AddRule(s.defaultUser, snap, iface, constraints, outcome, badLifespan, duration)
	c.Assert(err, Equals, requestrules.ErrLifespanSingle, Commentf("rdb.Rules(): %s", s.marshalRules(c, rdb)))
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
	c.Assert(rdb.Rules(s.defaultUser), HasLen, 2, Commentf("rdb.Rules(): %s", s.marshalRules(c, rdb)))

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
	c.Assert(rdb.Rules(s.defaultUser), HasLen, 2, Commentf("rdb.Rules(): %s", s.marshalRules(c, rdb)))

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
	c.Assert(err, ErrorMatches, fmt.Sprintf("^cannot patch rule: %s.*%s.*%s.*", requestrules.ErrPathPatternConflict, otherRule.ID.String(), conflictingPermission))
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

	removed, err := rdb.RemoveRulesForSnapInterface(s.defaultUser, snap, iface)
	c.Assert(err, IsNil)
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

func (s *requestrulesSuite) TestNewSaveLoad(c *C) {
	doNotNotifyRule := func(userID uint32, ruleID prompting.IDType, data map[string]string) error {
		return nil
	}
	rdb, err := requestrules.New(doNotNotifyRule)
	c.Assert(err, IsNil)

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
		c.Assert(err, IsNil, Commentf("path: %s: error: %v\nrdb.Rules(): %s", path, err, s.marshalRules(c, rdb)))
		c.Assert(result, Equals, expected, Commentf("path: %s: expected %b but got %b\nrdb.Rules(): %s", path, expected, result, s.marshalRules(c, rdb)))

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
	duration = "200ms"
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
	duration = "100ms"
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
	c.Check(allowed, Equals, true, Commentf("rdb.Rules(): %s", s.marshalRules(c, rdb)))
	allowed, err = rdb.IsPathAllowed(s.defaultUser, snap, iface, path2, "read")
	c.Check(err, IsNil)
	c.Check(allowed, Equals, false)

	// No rules expired, so should not cause a notice
	s.checkNewNoticesSimple(c, []prompting.IDType{}, nil)

	time.Sleep(100 * time.Millisecond)

	// rule 3 should have expired, check that it's not included when getting rules
	rules := rdb.Rules(s.defaultUser)
	c.Check(rules, HasLen, 2, Commentf("rules: %+v", rules))
	c.Check(rules[0] == rule1 || rules[0] == rule2, Equals, true, Commentf("unexpected rule: %+v", rules[0]))
	c.Check(rules[1] == rule1 || rules[1] == rule2, Equals, true, Commentf("unexpected rule: %+v", rules[1]))
	c.Check(rules[0] != rules[1], Equals, true, Commentf("Rules returned duplicate rules: %+v", rules))

	allowed, err = rdb.IsPathAllowed(s.defaultUser, snap, iface, path1, "read")
	c.Check(err, IsNil)
	c.Check(allowed, Equals, false)

	// Add rule which conflicts with rule3 in order to force it to be pruned.
	// New rule should expire almost immediately as well, but won't trigger
	// notice until refreshTreeEnforceConsistency or a conflict occurs.
	// By waiting to get current rules until after it expires, and calling
	// refreshTreeEnforceConsistency with that list, we won't get a notice for
	// it ever.
	rule4, err := rdb.AddRule(s.defaultUser, snap, iface, constraints3, prompting.OutcomeDeny, lifespan, "1ms")
	// Despite conflict, error should be nil, because conflicted permission is expired
	c.Assert(err, IsNil)

	time.Sleep(time.Millisecond)

	// rule3 expiration should have recorded a notice
	expectedNotices := []*noticeInfo{
		{
			ruleID: rule3.ID,
			data:   map[string]string{"removed": "expired"},
		},
		{
			ruleID: rule4.ID,
			data:   nil,
		},
	}
	s.checkNewNoticesUnordered(c, expectedNotices)

	// Re-load DB from disk to force expired rule4 to be pruned
	err = rdb.Load()
	c.Assert(err, IsNil)

	expectedNotices = []*noticeInfo{
		{
			ruleID: rule1.ID,
			data:   nil,
		},
		{
			ruleID: rule2.ID,
			data:   nil,
		},
		{
			ruleID: rule4.ID,
			data:   map[string]string{"removed": "expired"},
		},
	}
	s.checkNewNoticesUnordered(c, expectedNotices)

	allowed, err = rdb.IsPathAllowed(s.defaultUser, snap, iface, path2, "read")
	c.Check(err, IsNil)
	c.Check(allowed, Equals, false)

	// No rules newly expired, so should not cause a notice
	s.checkNewNoticesSimple(c, []prompting.IDType{}, nil)

	time.Sleep(100 * time.Millisecond)

	// Matches rule1.
	// Meanwhile, rule2 expires.
	allowed, err = rdb.IsPathAllowed(s.defaultUser, snap, iface, path1, "read")
	c.Check(err, Equals, nil)
	c.Check(allowed, Equals, true)

	// Re-load DB from disk to force expired rule2 to be pruned
	err = rdb.Load()
	c.Assert(err, IsNil)

	// As a result, get notices for rule1 and rule4 again, plus rule2 expiration
	expectedNotices = []*noticeInfo{
		{
			ruleID: rule1.ID,
			data:   nil,
		},
		{
			ruleID: rule2.ID,
			data:   map[string]string{"removed": "expired"},
		},
	}
	s.checkNewNotices(c, expectedNotices)

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
		for _, expected := range userRules {
			if reflect.DeepEqual(rule, expected) {
				continue OUTER_LOOP_USER
			}
		}
		c.Errorf("rule not found in userRules:\nrule: %+v\nuserRules: %+v", rule, userRules)
	}

	userSnapRules := rdb.RulesForSnap(user, snap)
	c.Check(userSnapRules, HasLen, 2)
OUTER_LOOP_USER_SNAP:
	for _, rule := range []*requestrules.Rule{rule3, rule4} {
		for _, expected := range userSnapRules {
			if reflect.DeepEqual(rule, expected) {
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
		for _, expected := range userInterfaceRules {
			if reflect.DeepEqual(rule, expected) {
				continue OUTER_LOOP_USER_INTERFACE
			}
		}
		c.Errorf("rule not found in userRules:\nrule: %+v\nuserInterfaceRules: %+v", rule, userRules)
	}

	// TODO: check these once rule4 is for another interface
	// userInterfaceRules = rdb.RulesForInterface(user, iface)
	// c.Check(userInterfaceRules, HasLen, 1)
	// c.Check(userInterfaceRules[0], DeepEquals, rule4)

	userSnapInterfaceRules := rdb.RulesForSnapInterface(user, snap, origIface)
	// TODO: make this 1 when rule4 is for another interface
	c.Check(userSnapRules, HasLen, 2)
OUTER_LOOP_USER_SNAP_INTERFACE:
	for _, rule := range []*requestrules.Rule{rule3, rule4} {
		for _, expected := range userSnapInterfaceRules {
			if reflect.DeepEqual(rule, expected) {
				continue OUTER_LOOP_USER_SNAP_INTERFACE
			}
		}
		c.Errorf("rule not found in userRules:\nrule: %+v\nuserSnapRules: %+v", rule, userRules)
	}

	// userSnapInterfaceRules := rdb.RulesForSnapInterface(user, snap, iface)
	// c.Check(userSnapInterfaceRules, HasLen, 1)
	// c.Check(userSnapInterfaceRules[0], DeepEquals, rule4)

	// Looking up these rules should not cause any notices
	s.checkNewNoticesSimple(c, []prompting.IDType{}, nil)
}
