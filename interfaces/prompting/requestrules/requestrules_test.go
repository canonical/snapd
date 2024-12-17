// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2024 Canonical Ltd
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License version 3 as
 * published by the Free Software Foundation.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with this program.  If not, see <http://www.gnu.org/licenses/>.
 *
 */

package requestrules_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/interfaces/prompting"
	prompting_errors "github.com/snapcore/snapd/interfaces/prompting/errors"
	"github.com/snapcore/snapd/interfaces/prompting/patterns"
	"github.com/snapcore/snapd/interfaces/prompting/requestrules"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/testutil"
)

func Test(t *testing.T) { TestingT(t) }

type noticeInfo struct {
	userID uint32
	ruleID prompting.IDType
	data   map[string]string
}

func (ni *noticeInfo) String() string {
	return fmt.Sprintf("{\n\tuserID: %x\n\truleID: %s\n\tdata:   %#v\n}", ni.userID, ni.ruleID, ni.data)
}

type requestrulesSuite struct {
	testutil.BaseTest

	defaultNotifyRule func(userID uint32, ruleID prompting.IDType, data map[string]string) error
	defaultUser       uint32
	ruleNotices       []*noticeInfo
}

var _ = Suite(&requestrulesSuite{})

func (s *requestrulesSuite) SetUpTest(c *C) {
	s.defaultUser = 1000
	s.defaultNotifyRule = func(userID uint32, ruleID prompting.IDType, data map[string]string) error {
		info := &noticeInfo{
			userID: userID,
			ruleID: ruleID,
			data:   data,
		}
		s.ruleNotices = append(s.ruleNotices, info)
		return nil
	}
	s.ruleNotices = make([]*noticeInfo, 0)
	dirs.SetRootDir(c.MkDir())
	s.AddCleanup(func() { dirs.SetRootDir("") })
	c.Assert(os.MkdirAll(dirs.SnapdStateDir(dirs.GlobalRootDir), 0700), IsNil)
}

func mustParsePathPattern(c *C, patternStr string) *patterns.PathPattern {
	pattern, err := patterns.ParsePathPattern(patternStr)
	c.Assert(err, IsNil)
	return pattern
}

func (s *requestrulesSuite) TestNew(c *C) {
	rdb, err := requestrules.New(s.defaultNotifyRule)
	c.Assert(err, IsNil)
	c.Assert(rdb, NotNil)
}

func (s *requestrulesSuite) TestNewErrors(c *C) {
	// Create file at prompting.StateDir so that EnsureStateDir fails
	stateDir := prompting.StateDir()
	f, err := os.Create(stateDir)
	c.Assert(err, IsNil)
	f.Close() // No need to keep the file open

	rdb, err := requestrules.New(s.defaultNotifyRule)
	c.Assert(err, ErrorMatches, "cannot create interfaces requests state directory.*") // Error from os.MkdirAll
	c.Assert(rdb, IsNil)

	// Remove the state dir file so we can continue
	c.Assert(os.Remove(stateDir), IsNil)

	// Create directory to conflict with max ID mmap
	maxIDFilepath := filepath.Join(prompting.StateDir(), "request-rule-max-id")
	c.Assert(os.MkdirAll(maxIDFilepath, 0o700), IsNil)

	rdb, err = requestrules.New(s.defaultNotifyRule)
	c.Assert(err, ErrorMatches, "cannot open max ID file:.*")
	c.Assert(rdb, IsNil)

	// Remove the conflicting directory so we can continue
	c.Assert(os.Remove(maxIDFilepath), IsNil)
}

// prepDBPath creates the the prompting state dir and returns the path of the
// rule DB.
func (s *requestrulesSuite) prepDBPath(c *C) string {
	dbPath := filepath.Join(prompting.StateDir(), "request-rules.json")
	parent := filepath.Dir(dbPath)
	c.Assert(os.MkdirAll(parent, 0o700), IsNil)
	return dbPath
}

func (s *requestrulesSuite) testLoadError(c *C, expectedErr string, rules []*requestrules.Rule, checkWritten bool) {
	logbuf, restore := logger.MockLogger()
	defer restore()
	rdb, err := requestrules.New(s.defaultNotifyRule)
	c.Check(err, IsNil)
	c.Check(rdb, NotNil)
	logErr := fmt.Errorf("%s", strings.TrimSpace(logbuf.String()))
	c.Check(logErr, ErrorMatches, fmt.Sprintf(".*cannot load rule database: %s; using new empty rule database", expectedErr))
	if checkWritten {
		s.checkWrittenRuleDB(c, nil)
	}
	s.checkNewNoticesSimple(c, map[string]string{"removed": "dropped"}, rules...)
}

func (s *requestrulesSuite) checkWrittenRuleDB(c *C, expectedRules []*requestrules.Rule) {
	dbPath := filepath.Join(prompting.StateDir(), "request-rules.json")

	if expectedRules == nil {
		expectedRules = []*requestrules.Rule{}
	}
	wrapper := requestrules.RulesDBJSON{Rules: expectedRules}
	marshalled, err := json.Marshal(wrapper)
	c.Assert(err, IsNil)

	writtenData, err := os.ReadFile(dbPath)
	c.Check(err, IsNil)
	c.Check(string(writtenData), Equals, string(marshalled))
}

func (s *requestrulesSuite) checkNewNoticesSimple(c *C, data map[string]string, rules ...*requestrules.Rule) {
	expectedNotices := make([]*noticeInfo, len(rules))
	for i, rule := range rules {
		info := &noticeInfo{
			userID: rule.User,
			ruleID: rule.ID,
			data:   data,
		}
		expectedNotices[i] = info
	}
	s.checkNewNotices(c, expectedNotices)
}

func (s *requestrulesSuite) checkNewNotices(c *C, expectedNotices []*noticeInfo) {
	if len(expectedNotices) == 0 {
		expectedNotices = []*noticeInfo{}
	}
	c.Check(s.ruleNotices, DeepEquals, expectedNotices, Commentf("\nReceived: %s\nExpected: %s", s.ruleNotices, expectedNotices))
	s.ruleNotices = s.ruleNotices[:0]
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

func (s *requestrulesSuite) TestLoadErrorOpen(c *C) {
	dbPath := s.prepDBPath(c)
	// Create unreadable DB file to cause open failure
	f, err := os.Create(dbPath)
	c.Assert(err, IsNil)
	c.Assert(f.Chmod(0o000), IsNil)
	checkWritten := false
	s.testLoadError(c, "cannot open rule database file:.*", nil, checkWritten)
}

func (s *requestrulesSuite) TestLoadErrorUnmarshal(c *C) {
	dbPath := s.prepDBPath(c)
	// Create malformed file in place of DB to cause unmarshal failure
	c.Assert(os.WriteFile(dbPath, []byte("foo"), 0o700), IsNil)
	checkWritten := true
	s.testLoadError(c, "cannot read stored request rules:.*", nil, checkWritten)
}

func (s *requestrulesSuite) TestLoadErrorValidate(c *C) {
	dbPath := s.prepDBPath(c)
	good1 := s.ruleTemplateWithRead(c, prompting.IDType(1))
	bad := s.ruleTemplateWithRead(c, prompting.IDType(2))
	bad.Interface = "foo" // will cause validate() to fail with invalid constraints
	good2 := s.ruleTemplateWithRead(c, prompting.IDType(3))
	good2.Constraints.Permissions["read"].Outcome = prompting.OutcomeDeny
	// Doesn't matter that rules have conflicting patterns/permissions,
	// validate() should catch invalid rule and exit before attempting to add.

	rules := []*requestrules.Rule{good1, bad, good2}
	s.writeRules(c, dbPath, rules)

	checkWritten := true
	s.testLoadError(c, `internal error: invalid interface: "foo".*`, rules, checkWritten)
}

// ruleTemplateWithRead returns a rule with valid contents, intended to be customized.
func (s *requestrulesSuite) ruleTemplateWithRead(c *C, id prompting.IDType) *requestrules.Rule {
	rule := s.ruleTemplate(c, id)
	rule.Constraints.Permissions["read"] = &prompting.RulePermissionEntry{
		Outcome:  prompting.OutcomeAllow,
		Lifespan: prompting.LifespanForever,
		// No expiration for lifespan forever
	}
	return rule
}

// ruleTemplate returns a rule with valid contents, intended to be customized.
func (s *requestrulesSuite) ruleTemplate(c *C, id prompting.IDType) *requestrules.Rule {
	constraints := prompting.RuleConstraints{
		PathPattern: mustParsePathPattern(c, "/home/test/foo"),
		Permissions: make(prompting.RulePermissionMap),
	}
	rule := requestrules.Rule{
		ID:          id,
		Timestamp:   time.Now(),
		User:        s.defaultUser,
		Snap:        "firefox",
		Interface:   "home",
		Constraints: &constraints,
	}
	return &rule
}

func (s *requestrulesSuite) writeRules(c *C, dbPath string, rules []*requestrules.Rule) {
	if rules == nil {
		rules = []*requestrules.Rule{}
	}
	wrapper := requestrules.RulesDBJSON{Rules: rules}
	marshalled, err := json.Marshal(wrapper)
	c.Assert(err, IsNil)
	c.Assert(os.WriteFile(dbPath, marshalled, 0o600), IsNil)
}

func (s *requestrulesSuite) TestLoadErrorConflictingID(c *C) {
	dbPath := s.prepDBPath(c)
	currTime := time.Now()
	good := s.ruleTemplateWithRead(c, prompting.IDType(1))
	// Expired rules should still get a {"removed": "expired"} notice, even if they don't conflict
	expired := s.ruleTemplate(c, prompting.IDType(2))
	expired.Constraints.PathPattern = mustParsePathPattern(c, "/home/test/other")
	setPermissionsOutcomeLifespanExpiration(c, expired, []string{"read"}, prompting.OutcomeAllow, prompting.LifespanTimespan, currTime.Add(-10*time.Second))
	// Add rule which conflicts with IDs but doesn't otherwise conflict
	conflicting := s.ruleTemplateWithRead(c, prompting.IDType(1))
	conflicting.Constraints.PathPattern = mustParsePathPattern(c, "/home/test/another")

	rules := []*requestrules.Rule{good, expired, conflicting}
	s.writeRules(c, dbPath, rules)

	checkWritten := true
	s.testLoadError(c, fmt.Sprintf("cannot add rule: %v.*", prompting_errors.ErrRuleIDConflict), rules, checkWritten)
}

func setPermissionsOutcomeLifespanExpiration(c *C, rule *requestrules.Rule, permissions []string, outcome prompting.OutcomeType, lifespan prompting.LifespanType, expiration time.Time) {
	for _, perm := range permissions {
		rule.Constraints.Permissions[perm] = &prompting.RulePermissionEntry{
			Outcome:    outcome,
			Lifespan:   lifespan,
			Expiration: expiration,
		}
	}
}

func (s *requestrulesSuite) TestLoadErrorConflictingPattern(c *C) {
	dbPath := s.prepDBPath(c)
	currTime := time.Now()
	good := s.ruleTemplateWithRead(c, prompting.IDType(1))
	// Expired rules should still get a {"removed": "expired"} notice, even if they don't conflict
	expired := s.ruleTemplateWithRead(c, prompting.IDType(2))
	expired.Constraints.PathPattern = mustParsePathPattern(c, "/home/test/other")
	setPermissionsOutcomeLifespanExpiration(c, expired, []string{"read"}, prompting.OutcomeAllow, prompting.LifespanTimespan, currTime.Add(-10*time.Second))
	// Add rule with conflicting pattern and permissions but not conflicting ID.
	conflicting := s.ruleTemplateWithRead(c, prompting.IDType(3))
	// Even with not all permissions being in conflict, still error
	var timeZero time.Time
	setPermissionsOutcomeLifespanExpiration(c, conflicting, []string{"read", "write"}, prompting.OutcomeDeny, prompting.LifespanForever, timeZero)

	rules := []*requestrules.Rule{good, expired, conflicting}
	s.writeRules(c, dbPath, rules)

	checkWritten := true
	s.testLoadError(c, fmt.Sprintf("cannot add rule: %v.*", prompting_errors.ErrRuleConflict), rules, checkWritten)
}

func (s *requestrulesSuite) TestLoadExpiredRules(c *C) {
	dbPath := s.prepDBPath(c)
	currTime := time.Now()

	good1 := s.ruleTemplateWithRead(c, prompting.IDType(1))

	// At the moment, expired rules with conflicts are discarded without error,
	// but we don't want to test this as part of our contract

	expired1 := s.ruleTemplate(c, prompting.IDType(2))
	expired1.Constraints.PathPattern = mustParsePathPattern(c, "/home/test/other")
	setPermissionsOutcomeLifespanExpiration(c, expired1, []string{"read"}, prompting.OutcomeAllow, prompting.LifespanTimespan, currTime.Add(-10*time.Second))

	// Rules with same pattern but non-conflicting permissions do not conflict
	good2 := s.ruleTemplate(c, prompting.IDType(3))
	var timeZero time.Time
	setPermissionsOutcomeLifespanExpiration(c, good2, []string{"write"}, prompting.OutcomeDeny, prompting.LifespanForever, timeZero)

	expired2 := s.ruleTemplate(c, prompting.IDType(4))
	expired2.Constraints.PathPattern = mustParsePathPattern(c, "/home/test/another")
	setPermissionsOutcomeLifespanExpiration(c, expired2, []string{"read"}, prompting.OutcomeAllow, prompting.LifespanTimespan, currTime.Add(-time.Nanosecond))

	// Rules with different pattern and conflicting permissions do not conflict
	good3 := s.ruleTemplateWithRead(c, prompting.IDType(5))
	good3.Constraints.PathPattern = mustParsePathPattern(c, "/home/test/no-conflict")

	rules := []*requestrules.Rule{good1, expired1, good2, expired2, good3}
	s.writeRules(c, dbPath, rules)

	logbuf, restore := logger.MockLogger()
	defer restore()
	rdb, err := requestrules.New(s.defaultNotifyRule)
	c.Check(err, IsNil)
	c.Check(rdb, NotNil)
	// Check that no error was logged
	c.Check(logbuf.String(), HasLen, 0)

	expectedWrittenRules := []*requestrules.Rule{good1, good2, good3}
	s.checkWrittenRuleDB(c, expectedWrittenRules)

	expectedNoticeInfo := []*noticeInfo{
		{
			userID: good1.User,
			ruleID: good1.ID,
			data:   nil,
		},
		{
			userID: expired1.User,
			ruleID: expired1.ID,
			data:   map[string]string{"removed": "expired"},
		},
		{
			userID: good2.User,
			ruleID: good2.ID,
			data:   nil,
		},
		{
			userID: expired2.User,
			ruleID: expired2.ID,
			data:   map[string]string{"removed": "expired"},
		},
		{
			userID: good3.User,
			ruleID: good3.ID,
			data:   nil,
		},
	}
	s.checkNewNotices(c, expectedNoticeInfo)
}

func (s *requestrulesSuite) TestLoadHappy(c *C) {
	dbPath := s.prepDBPath(c)

	good1 := s.ruleTemplateWithRead(c, prompting.IDType(1))

	// Rules with different users don't conflict
	good2 := s.ruleTemplateWithRead(c, prompting.IDType(2))
	good2.User = s.defaultUser + 1

	// Rules with different snaps don't conflict
	good3 := s.ruleTemplateWithRead(c, prompting.IDType(3))
	good3.Snap = "thunderbird"

	rules := []*requestrules.Rule{good1, good2, good3}
	s.writeRules(c, dbPath, rules)

	logbuf, restore := logger.MockLogger()
	defer restore()
	rdb, err := requestrules.New(s.defaultNotifyRule)
	c.Check(err, IsNil)
	c.Check(rdb, NotNil)
	// Check that no error was logged
	c.Check(logbuf.String(), HasLen, 0)

	s.checkWrittenRuleDB(c, rules)
	s.checkNewNoticesSimple(c, nil, rules...)
}

func (s *requestrulesSuite) TestJoinInternalErrors(c *C) {
	// Check that empty list or list of nil error(s) result in nil error.
	for _, errs := range [][]error{
		{},
		{nil},
		{nil, nil, nil},
	} {
		err := requestrules.JoinInternalErrors(errs)
		c.Check(err, IsNil)
	}

	errFoo := errors.New("foo")
	errBar := errors.New("bar")
	errBaz := errors.New("baz")

	// Check that a list containing non-nil errors results in a joined error
	// which is prompting_errors.ErrRuleDBInconsistent
	errs := []error{nil, errFoo, nil}
	err := requestrules.JoinInternalErrors(errs)
	c.Check(errors.Is(err, prompting_errors.ErrRuleDBInconsistent), Equals, true)
	// XXX: check the following when we're on golang v1.20+
	// c.Check(errors.Is(err, errFoo), Equals, true)
	c.Check(errors.Is(err, errBar), Equals, false)
	c.Check(err.Error(), Equals, fmt.Sprintf("%v\n%v", prompting_errors.ErrRuleDBInconsistent, errFoo))

	errs = append(errs, errBar, errBaz)
	err = requestrules.JoinInternalErrors(errs)
	c.Check(errors.Is(err, prompting_errors.ErrRuleDBInconsistent), Equals, true)
	// XXX: check the following when we're on golang v1.20+
	// c.Check(errors.Is(err, errFoo), Equals, true)
	// c.Check(errors.Is(err, errBar), Equals, true)
	// c.Check(errors.Is(err, errBaz), Equals, true)
	c.Check(err.Error(), Equals, fmt.Sprintf("%v\n%v\n%v\n%v", prompting_errors.ErrRuleDBInconsistent, errFoo, errBar, errBaz))
}

func (s *requestrulesSuite) TestClose(c *C) {
	rdb, err := requestrules.New(s.defaultNotifyRule)
	c.Assert(err, IsNil)

	c.Check(rdb.Close(), IsNil)
}

func (s *requestrulesSuite) TestCloseSaves(c *C) {
	rdb, err := requestrules.New(s.defaultNotifyRule)
	c.Assert(err, IsNil)

	// Add a rule, then mutate it in memory, then check that it is saved to
	// disk when DB is closed.
	constraints := &prompting.Constraints{
		PathPattern: mustParsePathPattern(c, "/home/test/foo"),
		Permissions: prompting.PermissionMap{
			"read": &prompting.PermissionEntry{
				Outcome:  prompting.OutcomeAllow,
				Lifespan: prompting.LifespanForever,
			},
		},
	}
	rule, err := rdb.AddRule(s.defaultUser, "firefox", "home", constraints)
	c.Assert(err, IsNil)

	// Check that new rule is on disk
	s.checkWrittenRuleDB(c, []*requestrules.Rule{rule})

	// Mutate rule in memory
	rule.Constraints.Permissions["read"].Outcome = prompting.OutcomeDeny

	// Close DB
	c.Check(rdb.Close(), IsNil)

	// Check that modified rule was written to disk
	s.checkWrittenRuleDB(c, []*requestrules.Rule{rule})
}

func (s *requestrulesSuite) TestCloseRepeatedly(c *C) {
	rdb, err := requestrules.New(s.defaultNotifyRule)
	c.Assert(err, IsNil)

	c.Check(rdb.Close(), IsNil)

	// Check that closing repeatedly results in ErrClosed
	c.Check(rdb.Close(), Equals, prompting_errors.ErrRulesClosed)
	c.Check(rdb.Close(), Equals, prompting_errors.ErrRulesClosed)
	c.Check(rdb.Close(), Equals, prompting_errors.ErrRulesClosed)
}

func (s *requestrulesSuite) TestCloseErrors(c *C) {
	rdb, err := requestrules.New(s.defaultNotifyRule)
	c.Assert(err, IsNil)

	// Mark state dir as non-writeable so save fails
	c.Assert(os.Chmod(prompting.StateDir(), 0o500), IsNil)
	defer os.Chmod(prompting.StateDir(), 0o700)

	c.Check(rdb.Close(), NotNil)
}

type addRuleContents struct {
	User        uint32
	Snap        string
	Interface   string
	PathPattern string
	Permissions []string
	Outcome     prompting.OutcomeType
	Lifespan    prompting.LifespanType
	Duration    string
}

func (s *requestrulesSuite) TestAddRuleHappy(c *C) {
	rdb, err := requestrules.New(s.defaultNotifyRule)
	c.Assert(err, IsNil)

	template := &addRuleContents{
		User:        s.defaultUser,
		Snap:        "lxd",
		Interface:   "home",
		PathPattern: "/home/test/Pictures/**/*.{jpg,png,svg}",
		Permissions: []string{"write"},
		Outcome:     prompting.OutcomeAllow,
		Lifespan:    prompting.LifespanForever,
		Duration:    "",
	}

	var addedRules []*requestrules.Rule

	// Add one rule matching template, then a rule with one differing field for
	// each conflict-defining field, to check that all rules add without error.
	for _, ruleContents := range []*addRuleContents{
		{}, // use template
		{User: s.defaultUser + 1},
		{Snap: "thunderbird"},
		// Can't change interface, as only "home" is supported for now (TODO: update)
		{PathPattern: "/home/test/**/*.{jpg,png,svg}"}, // no /Pictures/
		{Permissions: []string{"read", "execute"}},
		// Differing Outcome, Lifespan or Duration does not prevent conflict
		{PathPattern: "/home/test/1", Outcome: prompting.OutcomeDeny},
		{PathPattern: "/home/test/2", Lifespan: prompting.LifespanTimespan, Duration: "10s"},
	} {
		rule, err := addRuleFromTemplate(c, rdb, template, ruleContents)
		c.Check(err, IsNil)
		c.Check(rule, NotNil)
		addedRules = append(addedRules, rule)
		s.checkWrittenRuleDB(c, addedRules)
		s.checkNewNoticesSimple(c, nil, rule)
	}
}

// addRuleFromTemplate takes a template contents and a partial contents and,
// for every empty field in the partial contents, fills it with the details
// from the template contents, and then calls rdb.AddRule with the fields from
// the filled-in contents, and returns the results.
func addRuleFromTemplate(c *C, rdb *requestrules.RuleDB, template *addRuleContents, partial *addRuleContents) (*requestrules.Rule, error) {
	if partial == nil {
		partial = &addRuleContents{}
	}
	if partial.User == 0 {
		partial.User = template.User
	}
	if partial.Snap == "" {
		partial.Snap = template.Snap
	}
	if partial.Interface == "" {
		partial.Interface = template.Interface
	}
	if partial.PathPattern == "" {
		partial.PathPattern = template.PathPattern
	}
	if partial.Permissions == nil {
		partial.Permissions = template.Permissions
	}
	if partial.Outcome == prompting.OutcomeUnset {
		partial.Outcome = template.Outcome
	}
	if partial.Lifespan == prompting.LifespanUnset {
		partial.Lifespan = template.Lifespan
	}
	// Duration default is empty string, so just use partial.Duration
	replyConstraints := &prompting.ReplyConstraints{
		PathPattern: mustParsePathPattern(c, partial.PathPattern),
		Permissions: partial.Permissions,
	}
	constraints, err := replyConstraints.ToConstraints(partial.Interface, partial.Outcome, partial.Lifespan, partial.Duration)
	if err != nil {
		return nil, err
	}
	return rdb.AddRule(partial.User, partial.Snap, partial.Interface, constraints)
}

func (s *requestrulesSuite) TestAddRuleRemoveRuleDuplicateVariants(c *C) {
	rdb, err := requestrules.New(s.defaultNotifyRule)
	c.Assert(err, IsNil)

	ruleContents := &addRuleContents{
		User:        s.defaultUser,
		Snap:        "nextcloud",
		Interface:   "home",
		PathPattern: "/home/test/{{foo/{bar,baz},123},{123,foo{/bar,/baz}}}",
		Permissions: []string{"read"},
		Outcome:     prompting.OutcomeAllow,
		Lifespan:    prompting.LifespanForever,
		Duration:    "",
	}

	// Test that rule with a pattern which renders into duplicate variants does
	// not conflict with itself while adding
	var addedRules []*requestrules.Rule
	rule, err := addRuleFromTemplate(c, rdb, ruleContents, ruleContents)
	c.Check(err, IsNil)
	c.Check(rule, NotNil)
	addedRules = append(addedRules, rule)
	s.checkWrittenRuleDB(c, addedRules)
	s.checkNewNoticesSimple(c, nil, rule)

	// Test that the rule exists
	found, err := rdb.RuleWithID(rule.User, rule.ID)
	c.Assert(err, IsNil)
	c.Check(found, DeepEquals, rule)
	// Test that the rule's path pattern really renders to duplicate variants
	variantList := make([]string, 0, found.Constraints.PathPattern.NumVariants())
	variantSet := make(map[string]int, found.Constraints.PathPattern.NumVariants())
	found.Constraints.PathPattern.RenderAllVariants(func(index int, variant patterns.PatternVariant) {
		variantStr := variant.String()
		variantList = append(variantList, variantStr)
		variantSet[variantStr] += 1
	})
	c.Check(variantSet, Not(HasLen), len(variantList), Commentf("variant list: %q\nvariant set: %q", variantList, variantSet))

	// Test that rule with a pattern which renders into duplicate variants does
	// not cause an error while removing by trying to remove the same variant
	// twice and finding it already removed the second time
	removed, err := rdb.RemoveRule(rule.User, rule.ID)
	c.Assert(err, IsNil)
	c.Check(removed, DeepEquals, rule)
}

func (s *requestrulesSuite) TestAddRuleErrors(c *C) {
	rdb, err := requestrules.New(s.defaultNotifyRule)
	c.Assert(err, IsNil)

	template := &addRuleContents{
		User:        s.defaultUser,
		Snap:        "lxd",
		Interface:   "home",
		PathPattern: "/home/test/Pictures/**/*.{jpg,png,svg}",
		Permissions: []string{"write"},
		Outcome:     prompting.OutcomeAllow,
		Lifespan:    prompting.LifespanForever,
		Duration:    "",
	}

	// Add one good rule
	good, err := addRuleFromTemplate(c, rdb, template, template)
	c.Assert(err, IsNil)
	c.Assert(good, NotNil)
	s.checkWrittenRuleDB(c, []*requestrules.Rule{good})
	s.checkNewNoticesSimple(c, nil, good)

	// Preserve final error so it can be checked later
	var finalErr error

	for _, testCase := range []struct {
		contents *addRuleContents
		errStr   string
	}{
		{ // Non-empty duration with lifespan Forever
			&addRuleContents{Duration: "10m"},
			"invalid duration: cannot have specified duration.*",
		},
		{ // Empty duration with lifespan Timespan
			&addRuleContents{Lifespan: prompting.LifespanTimespan},
			"invalid duration: cannot have unspecified duration.*",
		},
		{ // Invalid duration
			&addRuleContents{Lifespan: prompting.LifespanTimespan, Duration: "invalid"},
			"invalid duration: cannot parse duration:.*",
		},
		{ // Negative duration
			&addRuleContents{Lifespan: prompting.LifespanTimespan, Duration: "-10s"},
			"invalid duration: cannot have zero or negative duration:.*",
		},
		{ // Invalid lifespan
			&addRuleContents{Lifespan: prompting.LifespanType("invalid")},
			`invalid lifespan: "invalid"`,
		},
		{ // Invalid outcome
			&addRuleContents{Outcome: prompting.OutcomeType("invalid")},
			`invalid outcome: "invalid"`,
		},
		{ // Invalid lifespan (for rules)
			&addRuleContents{Lifespan: prompting.LifespanSingle},
			prompting_errors.NewRuleLifespanSingleError(prompting.SupportedRuleLifespans).Error(),
		},
		{ // Conflicting rule
			&addRuleContents{
				PathPattern: "/home/test/Pictures/**/*.{svg,jpg}",
				Permissions: []string{"read", "write"},
				Outcome:     prompting.OutcomeDeny,
			},
			fmt.Sprintf("cannot add rule: %v", prompting_errors.ErrRuleConflict),
		},
	} {
		result, err := addRuleFromTemplate(c, rdb, template, testCase.contents)
		c.Check(err, ErrorMatches, testCase.errStr)
		c.Check(result, IsNil)
		// Check that rule DB was unchanged and no notices were recorded
		s.checkWrittenRuleDB(c, []*requestrules.Rule{good})
		s.checkNewNoticesSimple(c, nil)
		finalErr = err
	}

	// Check that the conflicting rule error can be unwrapped as ErrRuleConflict
	c.Check(errors.Is(finalErr, prompting_errors.ErrRuleConflict), Equals, true)

	// Failure to save rule DB should roll-back adding the rule and leave the
	// DB unchanged. Set DB parent directory as read-only.
	c.Assert(os.Chmod(prompting.StateDir(), 0o500), IsNil)
	result, err := addRuleFromTemplate(c, rdb, template, &addRuleContents{Permissions: []string{"execute"}})
	c.Assert(os.Chmod(prompting.StateDir(), 0o700), IsNil)
	c.Check(err, NotNil)
	c.Check(result, IsNil)
	// Failure should result in no changes to written rules and no notices
	s.checkWrittenRuleDB(c, []*requestrules.Rule{good})
	s.checkNewNoticesSimple(c, nil)
	// Remove read-only so we can continue

	// Adding rule after the rule DB has been closed should return an error
	// immediately.
	c.Assert(rdb.Close(), IsNil)
	result, err = addRuleFromTemplate(c, rdb, template, &addRuleContents{Permissions: []string{"execute"}})
	c.Check(err, Equals, prompting_errors.ErrRulesClosed)
	c.Check(result, IsNil)
	// Failure should result in no changes to written rules and no notices
	s.checkWrittenRuleDB(c, []*requestrules.Rule{good})
	s.checkNewNoticesSimple(c, nil)
}

func (s *requestrulesSuite) TestAddRuleOverlapping(c *C) {
	rdb, err := requestrules.New(s.defaultNotifyRule)
	c.Assert(err, IsNil)

	template := &addRuleContents{
		User:        s.defaultUser,
		Snap:        "lxd",
		Interface:   "home",
		PathPattern: "/home/test/Pictures/**/*.png",
		Permissions: []string{"write"},
		Outcome:     prompting.OutcomeAllow,
		Lifespan:    prompting.LifespanForever,
		Duration:    "",
	}

	var addedRules []*requestrules.Rule

	// Add one rule matching template, then various overlapping rules, and
	// check that all rules add without error.
	for _, ruleContents := range []*addRuleContents{
		{}, // use template
		{PathPattern: "/home/test/Pictures/**/*.{jpg,png,svg}"},
		{Permissions: []string{"read", "write"}},
		{PathPattern: "/home/test/Pictures/**/*.{jp,pn,sv}g"},
		{PathPattern: "/home/test/{Documents,Pictures}/**/*.{jpg,png,svg}", Permissions: []string{"read", "write", "execute"}},
		{}, // template again, for good measure
	} {
		rule, err := addRuleFromTemplate(c, rdb, template, ruleContents)
		c.Check(err, IsNil)
		c.Check(rule, NotNil)
		addedRules = append(addedRules, rule)
		s.checkWrittenRuleDB(c, addedRules)
		s.checkNewNoticesSimple(c, nil, rule)
	}

	// Lastly, add a conflicting rule, and check that it conflicts with all
	// the prior rules
	rule, err := addRuleFromTemplate(c, rdb, template, &addRuleContents{
		Outcome: prompting.OutcomeDeny,
	})
	c.Check(err, NotNil)
	c.Check(rule, IsNil)
	var ruleConflictErr *prompting_errors.RuleConflictError
	if !errors.As(err, &ruleConflictErr) {
		c.Fatalf("cannot cast error as RuleConflictError: %v", err)
	}
	c.Check(ruleConflictErr.Conflicts, HasLen, len(addedRules))
outer:
	for _, existing := range addedRules {
		for _, conflict := range ruleConflictErr.Conflicts {
			if conflict.ConflictingID == existing.ID.String() {
				continue outer
			}
		}
		c.Errorf("conflict error does not include existing rule: %+v", existing)
	}
}

func (s *requestrulesSuite) TestAddRuleExpired(c *C) {
	rdb, err := requestrules.New(s.defaultNotifyRule)
	c.Assert(err, IsNil)

	template := &addRuleContents{
		User:        s.defaultUser,
		Snap:        "lxd",
		Interface:   "home",
		PathPattern: "/home/test/**/{private,secret}/**",
		Permissions: []string{"write"},
		Outcome:     prompting.OutcomeAllow,
		Lifespan:    prompting.LifespanForever,
		Duration:    "",
	}

	// Add unrelated rule which should stick around throughout the test.
	good, err := addRuleFromTemplate(c, rdb, template, &addRuleContents{Snap: "gimp"})
	c.Assert(err, IsNil)
	c.Assert(good, NotNil)

	// TODO: ADD test which tests behavior of rules which partially expire

	// Add initial rule which will expire quickly
	prev, err := addRuleFromTemplate(c, rdb, template, &addRuleContents{
		Lifespan: prompting.LifespanTimespan,
		Duration: "1ms",
	})
	c.Assert(err, IsNil)
	c.Assert(prev, NotNil)
	time.Sleep(time.Millisecond)

	// Both rules should be on disk and have notices
	s.checkWrittenRuleDB(c, []*requestrules.Rule{good, prev})
	s.checkNewNoticesSimple(c, nil, good, prev)

	// Add rules which all conflict but each expire before the next is added,
	// thus causing the prior one to be removed and not causing a conflict error.
	for _, ruleContents := range []*addRuleContents{
		{Outcome: prompting.OutcomeDeny},
		{Outcome: prompting.OutcomeAllow, PathPattern: "/home/test/{**/secret,**/private}/**"},
		{Outcome: prompting.OutcomeDeny, PathPattern: "/home/test/**/{sec,priv}{ret,ate}/**", Permissions: []string{"read", "write"}},
		{Outcome: prompting.OutcomeAllow, Permissions: []string{"write", "execute"}},
		{Outcome: prompting.OutcomeDeny, PathPattern: "/home/test/*{*/secret/*,*/private/*}*"},
	} {
		ruleContents.Lifespan = prompting.LifespanTimespan
		ruleContents.Duration = "1ms"
		newRule, err := addRuleFromTemplate(c, rdb, template, ruleContents)
		c.Check(err, IsNil)
		c.Assert(newRule, NotNil)
		s.checkWrittenRuleDB(c, []*requestrules.Rule{good, newRule})
		expectedNoticeInfo := []*noticeInfo{
			{
				userID: prev.User,
				ruleID: prev.ID,
				data:   map[string]string{"removed": "expired"},
			},
			{
				userID: newRule.User,
				ruleID: newRule.ID,
				data:   nil,
			},
		}
		s.checkNewNotices(c, expectedNoticeInfo)
		time.Sleep(time.Millisecond)
		// Store newRule as prev for next iteration
		prev = newRule
	}

	// Lastly, add a rule with a lifespan of forever which would also conflict
	// if not for the previous rules having expired.
	final, err := addRuleFromTemplate(c, rdb, template, template)
	c.Assert(err, IsNil)
	c.Assert(final, NotNil)
	s.checkWrittenRuleDB(c, []*requestrules.Rule{good, final})
	expectedNoticeInfo := []*noticeInfo{
		{
			userID: prev.User,
			ruleID: prev.ID,
			data:   map[string]string{"removed": "expired"},
		},
		{
			userID: final.User,
			ruleID: final.ID,
			data:   nil,
		},
	}
	s.checkNewNotices(c, expectedNoticeInfo)
}

func (s *requestrulesSuite) TestAddRulePartiallyExpired(c *C) {
	rdb, err := requestrules.New(s.defaultNotifyRule)
	c.Assert(err, IsNil)

	user := s.defaultUser
	snap := "firefox"
	iface := "home"

	constraints1 := &prompting.Constraints{
		PathPattern: mustParsePathPattern(c, "/path/to/{foo,bar}"),
		Permissions: prompting.PermissionMap{
			"read": &prompting.PermissionEntry{
				Outcome:  prompting.OutcomeAllow,
				Lifespan: prompting.LifespanTimespan,
				Duration: "1ns",
			},
			"write": &prompting.PermissionEntry{
				Outcome:  prompting.OutcomeAllow,
				Lifespan: prompting.LifespanForever,
			},
			"execute": &prompting.PermissionEntry{
				Outcome:  prompting.OutcomeDeny,
				Lifespan: prompting.LifespanTimespan,
				Duration: "1ns",
			},
		},
	}
	rule1, err := rdb.AddRule(user, snap, iface, constraints1)
	c.Assert(err, IsNil)
	c.Assert(rule1, NotNil)
	s.checkWrittenRuleDB(c, []*requestrules.Rule{rule1})
	s.checkNewNoticesSimple(c, nil, rule1)

	constraints2 := &prompting.Constraints{
		PathPattern: mustParsePathPattern(c, "/path/to/{bar,baz}"), // overlap
		Permissions: prompting.PermissionMap{
			"read": &prompting.PermissionEntry{
				Outcome:  prompting.OutcomeDeny, // conflicting
				Lifespan: prompting.LifespanTimespan,
				Duration: "1ns",
			},
			"execute": &prompting.PermissionEntry{
				Outcome:  prompting.OutcomeDeny,
				Lifespan: prompting.LifespanTimespan,
				Duration: "10s",
			},
		},
	}
	rule2, err := rdb.AddRule(user, snap, iface, constraints2)
	c.Assert(err, IsNil)
	c.Assert(rule2, NotNil)
	s.checkWrittenRuleDB(c, []*requestrules.Rule{rule1, rule2})
	s.checkNewNoticesSimple(c, nil, rule2)

	// Check that "read" and "execute" were removed from rule1
	_, exists := rule1.Constraints.Permissions["read"]
	c.Check(exists, Equals, false)
	// Even though "execute" did not conflict, expired entries are removed from
	// the variant entry's rule entries whenever a new entry is added to it.
	_, exists = rule1.Constraints.Permissions["execute"]
	c.Check(exists, Equals, false)

	// Check that "write" was not removed from rule1
	_, exists = rule1.Constraints.Permissions["write"]
	c.Check(exists, Equals, true)

	// Check that "read" was not removed from rule2 (even though it's since expired)
	_, exists = rule2.Constraints.Permissions["read"]
	c.Check(exists, Equals, true)

	// Check that "execute" was not removed from rule2
	_, exists = rule2.Constraints.Permissions["execute"]
	c.Check(exists, Equals, true)
}

func (s *requestrulesSuite) TestIsPathAllowedSimple(c *C) {
	// Target
	user := s.defaultUser
	snap := "firefox"
	iface := "home"
	path := "/home/test/path/to/file.txt"
	permission := "read"

	template := &addRuleContents{
		User:        user,
		Snap:        snap,
		Interface:   iface,
		PathPattern: path,
		Permissions: []string{permission},
		Outcome:     prompting.OutcomeAllow,
		Lifespan:    prompting.LifespanForever,
	}

	for _, testCase := range []struct {
		ruleContents *addRuleContents
		allowed      bool
		err          error
	}{
		{ // No rules
			ruleContents: nil,
			allowed:      false,
			err:          prompting_errors.ErrNoMatchingRule,
		},
		{ // Matching allow rule
			ruleContents: template,
			allowed:      true,
			err:          nil,
		},
		{ // Matching deny rule
			ruleContents: &addRuleContents{Outcome: prompting.OutcomeDeny},
			allowed:      false,
			err:          nil,
		},
		{ // Matching allow rule with expiration
			ruleContents: &addRuleContents{Lifespan: prompting.LifespanTimespan, Duration: "10s"},
			allowed:      true,
			err:          nil,
		},
		{ // Matching deny rule with expiration
			ruleContents: &addRuleContents{Outcome: prompting.OutcomeDeny, Lifespan: prompting.LifespanTimespan, Duration: "24h"},
			allowed:      false,
			err:          nil,
		},
		{ // Rule with wrong user
			ruleContents: &addRuleContents{User: s.defaultUser + 1},
			allowed:      false,
			err:          prompting_errors.ErrNoMatchingRule,
		},
		{ // Rule with wrong snap
			ruleContents: &addRuleContents{Snap: "thunderbird"},
			allowed:      false,
			err:          prompting_errors.ErrNoMatchingRule,
		},
		{ // Rule with wrong pattern
			ruleContents: &addRuleContents{PathPattern: "/home/test/path/to/other.txt"},
			allowed:      false,
			err:          prompting_errors.ErrNoMatchingRule,
		},
		{ // Rule with wrong permissions
			ruleContents: &addRuleContents{Permissions: []string{"write"}},
			allowed:      false,
			err:          prompting_errors.ErrNoMatchingRule,
		},
	} {
		rdb, err := requestrules.New(s.defaultNotifyRule)
		c.Assert(err, IsNil)
		c.Assert(rdb, NotNil)

		if testCase.ruleContents != nil {
			rule, err := addRuleFromTemplate(c, rdb, template, testCase.ruleContents)
			c.Assert(err, IsNil)
			c.Assert(rule, NotNil)
			s.checkWrittenRuleDB(c, []*requestrules.Rule{rule})
			s.checkNewNoticesSimple(c, nil, rule)
		} else {
			s.checkNewNoticesSimple(c, nil)
		}

		allowed, err := rdb.IsPathAllowed(user, snap, iface, path, permission)
		c.Check(err, Equals, testCase.err)
		c.Check(allowed, Equals, testCase.allowed)
		// Check that no notices were recorded when checking
		s.checkNewNoticesSimple(c, nil)

		if testCase.ruleContents != nil {
			// Clean up the rules DB so the next rdb has a clean slate
			dbPath := filepath.Join(prompting.StateDir(), "request-rules.json")
			c.Assert(os.Remove(dbPath), IsNil)
		}
	}
}

func (s *requestrulesSuite) TestIsPathAllowedPrecedence(c *C) {
	// Target
	user := s.defaultUser
	snap := "firefox"
	iface := "home"
	path := "/home/test/Documents/foo/bar/baz/file.txt"
	permission := "read"

	template := &addRuleContents{
		User:        user,
		Snap:        snap,
		Interface:   iface,
		PathPattern: path,
		Permissions: []string{permission},
		Outcome:     prompting.OutcomeAllow,
		Lifespan:    prompting.LifespanForever,
	}

	rdb, err := requestrules.New(s.defaultNotifyRule)
	c.Assert(err, IsNil)
	c.Assert(rdb, NotNil)

	var addedRules []*requestrules.Rule

	// Add these rules in order, where each has higher precedence than prior
	// rules. After adding each, check whether file is allowed. Result should
	// always match the most recent rule contents.
	for i, ruleContents := range []*addRuleContents{
		{PathPattern: "/home/test/**"},
		{PathPattern: "/home/test/Doc*/**"},
		{PathPattern: "/home/test/Documents/**"},
		{PathPattern: "/home/test/Documents/foo/**"},
		{PathPattern: "/home/test/Documents/foo/**/ba?/*.txt"},
		{PathPattern: "/home/test/Documents/foo/**/ba?/file.txt"},
		{PathPattern: "/home/test/Documents/foo/**/bar/**"},
		{PathPattern: "/home/test/Documents/foo/**/bar/baz/**"},
		{PathPattern: "/home/test/Documents/foo/**/bar/baz/*"},
		{PathPattern: "/home/test/Documents/foo/**/bar/baz/*.txt"},
		{PathPattern: "/home/test/Documents/foo/**/bar/{somewhere/else,baz}/file.txt"},
		{PathPattern: "/home/test/Documents/foo/*/baz/file.{txt,md,pdf}"},
		{PathPattern: "/home/test/{Documents,Pictures}/foo/bar/baz/file.{txt,md,pdf,png,jpg,svg}"},
	} {
		if i%2 == 0 {
			ruleContents.Outcome = prompting.OutcomeAllow
		} else {
			ruleContents.Outcome = prompting.OutcomeDeny
		}
		rule, err := addRuleFromTemplate(c, rdb, template, ruleContents)
		c.Assert(err, IsNil)
		c.Assert(rule, NotNil)
		addedRules = append(addedRules, rule)
		s.checkWrittenRuleDB(c, addedRules)
		s.checkNewNoticesSimple(c, nil, rule)

		mostRecentOutcome, err := ruleContents.Outcome.AsBool()
		c.Check(err, IsNil)

		allowed, err := rdb.IsPathAllowed(user, snap, iface, path, permission)
		c.Check(err, IsNil)
		c.Check(allowed, Equals, mostRecentOutcome, Commentf("most recent: %+v", ruleContents))
	}
}

func (s *requestrulesSuite) TestIsPathAllowedExpiration(c *C) {
	// Target
	user := s.defaultUser
	snap := "firefox"
	iface := "home"
	path := "/home/test/Documents/foo/bar/baz/file.txt"
	permission := "read"

	template := &addRuleContents{
		User:        user,
		Snap:        snap,
		Interface:   iface,
		PathPattern: path,
		Permissions: []string{permission},
		Outcome:     prompting.OutcomeAllow,
		Lifespan:    prompting.LifespanTimespan,
		Duration:    "1h",
	}

	rdb, err := requestrules.New(s.defaultNotifyRule)
	c.Assert(err, IsNil)
	c.Assert(rdb, NotNil)

	var addedRules []*requestrules.Rule

	// Add these rules, where each has higher precedence than prior rules.
	// Then, from last to first, mark the rule as expired by setting the
	// expiration timestamp to the past, and then test that the
	// always match the most recent rule contents.
	for i, ruleContents := range []*addRuleContents{
		{Duration: "1h", PathPattern: "/home/test/**"},
		{Duration: "1h", PathPattern: "/home/test/Doc*/**"},
		{Duration: "1h", PathPattern: "/home/test/Documents/**"},
		{Duration: "1h", PathPattern: "/home/test/Documents/foo/**"},
		{Duration: "1h", PathPattern: "/home/test/Documents/foo/**/ba?/*.txt"},
		{Duration: "1h", PathPattern: "/home/test/Documents/foo/**/ba?/file.txt"},
		{Duration: "1h", PathPattern: "/home/test/Documents/foo/**/bar/**"},
		{Duration: "1h", PathPattern: "/home/test/Documents/foo/**/bar/baz/**"},
		{Duration: "1h", PathPattern: "/home/test/Documents/foo/**/bar/baz/*"},
		{Duration: "1h", PathPattern: "/home/test/Documents/foo/**/bar/baz/*.txt"},
		{Duration: "1h", PathPattern: "/home/test/Documents/foo/**/bar/{somewhere/else,baz}/file.txt"},
		{Duration: "1h", PathPattern: "/home/test/Documents/foo/*/baz/file.{txt,md,pdf}"},
		{Duration: "1h", PathPattern: "/home/test/{Documents,Pictures}/foo/bar/baz/file.{txt,md,pdf,png,jpg,svg}"},
	} {
		if i%2 == 0 {
			ruleContents.Outcome = prompting.OutcomeAllow
		} else {
			ruleContents.Outcome = prompting.OutcomeDeny
		}
		rule, err := addRuleFromTemplate(c, rdb, template, ruleContents)
		c.Assert(err, IsNil)
		c.Assert(rule, NotNil)
		addedRules = append(addedRules, rule)
		s.checkWrittenRuleDB(c, addedRules)
		s.checkNewNoticesSimple(c, nil, rule)
	}

	for i := len(addedRules) - 1; i >= 0; i-- {
		rule := addedRules[i]
		expectedOutcome, err := rule.Constraints.Permissions["read"].Outcome.AsBool()
		c.Check(err, IsNil)

		// Check that the outcome of the most specific unexpired rule has precedence
		allowed, err := rdb.IsPathAllowed(user, snap, iface, path, permission)
		c.Check(err, IsNil)
		c.Check(allowed, Equals, expectedOutcome, Commentf("last unexpired: %+v", rule))

		// Check that no new notices are recorded from lookup or expiration
		s.checkNewNoticesSimple(c, nil)

		// Expire the highest precedence rule
		rule.Constraints.Permissions["read"].Expiration = time.Now()
	}
}

func (s *requestrulesSuite) TestRuleWithID(c *C) {
	rdb, _ := requestrules.New(s.defaultNotifyRule)

	template := &addRuleContents{
		User:        s.defaultUser,
		Snap:        "lxd",
		Interface:   "home",
		PathPattern: "/home/test/Documents/**",
		Permissions: []string{"read", "write", "execute"},
		Outcome:     prompting.OutcomeAllow,
		Lifespan:    prompting.LifespanForever,
	}

	rule, err := addRuleFromTemplate(c, rdb, template, template)
	c.Assert(err, IsNil)
	c.Assert(rule, NotNil)

	s.checkWrittenRuleDB(c, []*requestrules.Rule{rule})
	s.checkNewNoticesSimple(c, nil, rule)

	// Should find correct rule for user and ID
	accessedRule, err := rdb.RuleWithID(s.defaultUser, rule.ID)
	c.Check(err, IsNil)
	c.Check(accessedRule, DeepEquals, rule)

	// Should not find rule for correct user and incorrect ID
	accessedRule, err = rdb.RuleWithID(s.defaultUser, prompting.IDType(1234567890))
	c.Check(err, Equals, prompting_errors.ErrRuleNotFound)
	c.Check(accessedRule, IsNil)

	// Should not find rule for incorrect user and correct ID
	accessedRule, err = rdb.RuleWithID(s.defaultUser+1, rule.ID)
	c.Check(err, Equals, prompting_errors.ErrRuleNotAllowed)
	c.Check(accessedRule, IsNil)

	// Reading (or failing to read) a notice should not record a notice
	s.checkNewNoticesSimple(c, nil)
}

func (s *requestrulesSuite) TestRules(c *C) {
	rdb, err := requestrules.New(s.defaultNotifyRule)
	c.Assert(err, IsNil)
	c.Assert(rdb, NotNil)

	c.Check(rdb.Rules(s.defaultUser), HasLen, 0)

	rules := s.prepRuleDBForRulesForSnapInterface(c, rdb)

	// The final rule is for another user, and should be excluded
	c.Check(rdb.Rules(s.defaultUser), DeepEquals, rules[:4])

	// Getting rules should cause no notices
	s.checkNewNoticesSimple(c, nil)
}

func (s *requestrulesSuite) prepRuleDBForRulesForSnapInterface(c *C, rdb *requestrules.RuleDB) []*requestrules.Rule {
	template := &addRuleContents{
		User:        s.defaultUser,
		Snap:        "spotify",
		Interface:   "home",
		PathPattern: "/home/test/Music/**/*.{flac,mp3,aac,m4a}",
		Permissions: []string{"read"},
		Outcome:     prompting.OutcomeAllow,
		Lifespan:    prompting.LifespanForever,
		Duration:    "",
	}

	var addedRules []*requestrules.Rule

	for _, ruleContents := range []*addRuleContents{
		{},
		{Permissions: []string{"write"}},
		{Snap: "amberol"},
		{Snap: "amberol", Permissions: []string{"write"}}, // change interface later
		{User: s.defaultUser + 1},
	} {
		rule, err := addRuleFromTemplate(c, rdb, template, ruleContents)
		c.Assert(err, IsNil)
		c.Assert(rule, NotNil)
		addedRules = append(addedRules, rule)
		s.checkWrittenRuleDB(c, addedRules)
		s.checkNewNoticesSimple(c, nil, rule)
	}

	// Change interface of rules[3]
	addedRules[3].Interface = "audio-playback"

	return addedRules
}

func (s *requestrulesSuite) TestRulesExpired(c *C) {
	rdb, err := requestrules.New(s.defaultNotifyRule)
	c.Assert(err, IsNil)
	c.Assert(rdb, NotNil)

	c.Check(rdb.Rules(s.defaultUser), HasLen, 0)

	rules := s.prepRuleDBForRulesForSnapInterface(c, rdb)

	// Set some rules to be expired
	// This is brittle, relies on details of the rules added by prepRuleDBForRulesForSnapInterface
	rules[0].Constraints.Permissions["read"].Lifespan = prompting.LifespanTimespan
	rules[0].Constraints.Permissions["read"].Expiration = time.Now()
	rules[2].Constraints.Permissions["read"].Lifespan = prompting.LifespanTimespan
	rules[2].Constraints.Permissions["read"].Expiration = time.Now()
	rules[4].Constraints.Permissions["read"].Lifespan = prompting.LifespanTimespan
	rules[4].Constraints.Permissions["read"].Expiration = time.Now()

	// Expired rules are excluded from the Rules*() functions
	c.Check(rdb.Rules(s.defaultUser), DeepEquals, []*requestrules.Rule{rules[1], rules[3]})

	// Getting rules should cause no notices
	s.checkNewNoticesSimple(c, nil)
}

func (s *requestrulesSuite) TestRulesForSnap(c *C) {
	rdb, err := requestrules.New(s.defaultNotifyRule)
	c.Assert(err, IsNil)
	c.Assert(rdb, NotNil)

	c.Check(rdb.Rules(s.defaultUser), HasLen, 0)

	rules := s.prepRuleDBForRulesForSnapInterface(c, rdb)

	c.Check(rdb.RulesForSnap(s.defaultUser, "amberol"), DeepEquals, rules[2:4])

	// Getting rules should cause no notices
	s.checkNewNoticesSimple(c, nil)
}

func (s *requestrulesSuite) TestRulesForInterface(c *C) {
	rdb, err := requestrules.New(s.defaultNotifyRule)
	c.Assert(err, IsNil)
	c.Assert(rdb, NotNil)

	c.Check(rdb.Rules(s.defaultUser), HasLen, 0)

	rules := s.prepRuleDBForRulesForSnapInterface(c, rdb)

	c.Check(rdb.RulesForInterface(s.defaultUser, "home"), DeepEquals, rules[:3])

	// Getting rules should cause no notices
	s.checkNewNoticesSimple(c, nil)
}

func (s *requestrulesSuite) TestRulesForSnapInterface(c *C) {
	rdb, err := requestrules.New(s.defaultNotifyRule)
	c.Assert(err, IsNil)
	c.Assert(rdb, NotNil)

	c.Check(rdb.Rules(s.defaultUser), HasLen, 0)

	rules := s.prepRuleDBForRulesForSnapInterface(c, rdb)

	c.Check(rdb.RulesForSnapInterface(s.defaultUser, "amberol", "audio-playback"), DeepEquals, []*requestrules.Rule{rules[3]})

	// Getting rules should cause no notices
	s.checkNewNoticesSimple(c, nil)
}

func (s *requestrulesSuite) TestRemoveRuleForward(c *C) {
	rdb, err := requestrules.New(s.defaultNotifyRule)
	c.Assert(err, IsNil)

	addedRules := s.prepRuleDBForRulesForSnapInterface(c, rdb)

	for _, rule := range addedRules {
		s.testRemoveRule(c, rdb, rule)
	}
}

func (s *requestrulesSuite) TestRemoveRuleBackward(c *C) {
	rdb, err := requestrules.New(s.defaultNotifyRule)
	c.Assert(err, IsNil)

	addedRules := s.prepRuleDBForRulesForSnapInterface(c, rdb)

	for i := len(addedRules) - 1; i >= 0; i-- {
		rule := addedRules[i]
		s.testRemoveRule(c, rdb, rule)
	}
}

func (s *requestrulesSuite) testRemoveRule(c *C, rdb *requestrules.RuleDB, rule *requestrules.Rule) {
	// Pre-check that rule exists
	found, err := rdb.RuleWithID(rule.User, rule.ID)
	c.Check(err, IsNil)
	c.Check(found, DeepEquals, rule)

	// Remove the rule
	removed, err := rdb.RemoveRule(rule.User, rule.ID)
	c.Check(err, IsNil)
	c.Check(removed, DeepEquals, rule)

	// Notice should be recorded immediately
	s.checkNewNoticesSimple(c, map[string]string{"removed": "removed"}, removed)

	// Post-check that rule no longer exists
	missing, err := rdb.RuleWithID(rule.User, rule.ID)
	c.Check(err, Equals, prompting_errors.ErrRuleNotFound)
	c.Check(missing, IsNil)
}

func (s *requestrulesSuite) TestRemoveRuleErrors(c *C) {
	rdb, err := requestrules.New(s.defaultNotifyRule)
	c.Assert(err, IsNil)

	addedRules := s.prepRuleDBForRulesForSnapInterface(c, rdb)
	rule := addedRules[0]

	// Attempt to remove rule with wrong user
	result, err := rdb.RemoveRule(rule.User+1, rule.ID)
	c.Check(err, Equals, prompting_errors.ErrRuleNotAllowed)
	c.Check(result, IsNil)

	// Attempt to remove rule with wrong ID
	result, err = rdb.RemoveRule(rule.User, rule.ID+1234)
	c.Check(err, Equals, prompting_errors.ErrRuleNotFound)
	c.Check(result, IsNil)

	// Failure to save rule DB should roll-back removal and leave DB unchanged.
	// Set DB parent directory as read-only.
	c.Assert(os.Chmod(prompting.StateDir(), 0o500), IsNil)
	result, err = rdb.RemoveRule(rule.User, rule.ID)
	c.Check(err, NotNil)
	c.Check(result, IsNil)
	c.Assert(os.Chmod(prompting.StateDir(), 0o700), IsNil)

	// Check that rule remains and no notices have been recorded
	accessed, err := rdb.RuleWithID(rule.User, rule.ID)
	c.Check(err, IsNil)
	c.Check(accessed, DeepEquals, rule)
	s.checkNewNoticesSimple(c, nil)

	// Corrupt rules to trigger internal errors, which aren't returned.
	// Internal errors while removing are ignored and the rule is still removed.
	// Cause "path pattern variant maps to different rule ID"
	addedRules[1].Snap = addedRules[0].Snap
	result, err = rdb.RemoveRule(addedRules[1].User, addedRules[1].ID)
	c.Check(err, IsNil)
	c.Check(result, DeepEquals, addedRules[1])
	// Cause "variant not found in rule tree"
	addedRules[2].Constraints.PathPattern = mustParsePathPattern(c, "/path/to/nowhere")
	result, err = rdb.RemoveRule(addedRules[2].User, addedRules[2].ID)
	c.Check(err, IsNil)
	c.Check(result, DeepEquals, addedRules[2])
	// Cause "no rules in rule tree for..."
	addedRules[3].Snap = "invalid"
	result, err = rdb.RemoveRule(addedRules[3].User, addedRules[3].ID)
	c.Check(err, IsNil)
	c.Check(result, DeepEquals, addedRules[3])

	// Since removal succeeded for all corrupted rules (despite internal errors),
	// should get "removed" notices for each removed rule.
	s.checkNewNoticesSimple(c, map[string]string{"removed": "removed"}, addedRules[1:4]...)

	// Removing a rule after the rule DB has been closed should return an error
	// immediately.
	c.Assert(rdb.Close(), IsNil)
	result, err = rdb.RemoveRule(rule.User, rule.ID)
	c.Check(err, NotNil)
	c.Check(result, IsNil)

	// Check that rule remains and no notices have been recorded.
	// RuleWithID still works after the rule DB has been closed, so we can
	// still check that the rule remains. XXX: don't rely on this.
	accessed, err = rdb.RuleWithID(rule.User, rule.ID)
	c.Check(err, IsNil)
	c.Check(accessed, DeepEquals, rule)
	s.checkNewNoticesSimple(c, nil)
}

func (s *requestrulesSuite) TestRemoveRulesForSnap(c *C) {
	rdb, err := requestrules.New(s.defaultNotifyRule)
	c.Assert(err, IsNil)
	c.Assert(rdb, NotNil)

	rules := s.prepRuleDBForRulesForSnapInterface(c, rdb)

	removed, err := rdb.RemoveRulesForSnap(s.defaultUser, "amberol")
	c.Check(err, IsNil)

	c.Check(removed, DeepEquals, rules[2:4])

	s.checkWrittenRuleDB(c, append(rules[:2], rules[4]))
	s.checkNewNoticesSimple(c, map[string]string{"removed": "removed"}, removed...)
}

func (s *requestrulesSuite) TestRemoveRulesForInterface(c *C) {
	rdb, err := requestrules.New(s.defaultNotifyRule)
	c.Assert(err, IsNil)
	c.Assert(rdb, NotNil)

	rules := s.prepRuleDBForRulesForSnapInterface(c, rdb)

	removed, err := rdb.RemoveRulesForInterface(s.defaultUser, "home")
	c.Check(err, IsNil)

	c.Check(removed, DeepEquals, rules[:3])

	s.checkWrittenRuleDB(c, []*requestrules.Rule{rules[4], rules[3]}) // removal reorders
	s.checkNewNoticesSimple(c, map[string]string{"removed": "removed"}, removed...)
}

func (s *requestrulesSuite) TestRemoveRulesForSnapInterface(c *C) {
	rdb, err := requestrules.New(s.defaultNotifyRule)
	c.Assert(err, IsNil)
	c.Assert(rdb, NotNil)

	rules := s.prepRuleDBForRulesForSnapInterface(c, rdb)

	removed, err := rdb.RemoveRulesForSnapInterface(s.defaultUser, "amberol", "audio-playback")
	c.Check(err, IsNil)

	c.Check(removed, DeepEquals, []*requestrules.Rule{rules[3]})

	s.checkWrittenRuleDB(c, append(rules[:3], rules[4]))
	s.checkNewNoticesSimple(c, map[string]string{"removed": "removed"}, removed...)
}

func (s *requestrulesSuite) TestRemoveRulesForSnapInterfaceErrors(c *C) {
	dbPath := filepath.Join(prompting.StateDir(), "request-rules.json")

	rdb, err := requestrules.New(s.defaultNotifyRule)
	c.Assert(err, IsNil)
	c.Assert(rdb, NotNil)

	// Removing when no rules exist should not be an error
	removed, err := rdb.RemoveRulesForSnap(s.defaultUser, "foo")
	c.Check(err, IsNil)
	c.Check(removed, HasLen, 0)
	removed, err = rdb.RemoveRulesForInterface(s.defaultUser, "foo")
	c.Check(err, IsNil)
	c.Check(removed, HasLen, 0)
	removed, err = rdb.RemoveRulesForSnapInterface(s.defaultUser, "foo", "bar")
	c.Check(err, IsNil)
	c.Check(removed, HasLen, 0)

	// Add some rules with different snaps and interfaces
	rules := s.prepRuleDBForRulesForSnapInterface(c, rdb)
	c.Assert(rules, HasLen, 5)

	// Removing when rules exist but none match should not be an error
	removed, err = rdb.RemoveRulesForSnap(s.defaultUser, "foo")
	c.Check(err, IsNil)
	c.Check(removed, HasLen, 0)
	removed, err = rdb.RemoveRulesForInterface(s.defaultUser, "foo")
	c.Check(err, IsNil)
	c.Check(removed, HasLen, 0)
	removed, err = rdb.RemoveRulesForSnapInterface(s.defaultUser, "foo", "bar")
	c.Check(err, IsNil)
	c.Check(removed, HasLen, 0)

	// Check that original rules are still on disk, and no notices were recorded.
	// Be careful, since no write has occurred, the manually-edited rules[3]
	// still has "home" interface on disk.
	c.Assert(rdb.Rules(s.defaultUser), DeepEquals, rules[:4])
	rules[3].Interface = "home"
	s.checkWrittenRuleDB(c, rules)
	rules[3].Interface = "audio-playback"
	s.checkNewNoticesSimple(c, nil)

	// Failure to save rule DB should roll-back removing the rules and leave the
	// DB unchanged. Set DB parent directory as read-only.
	func() {
		c.Assert(os.Chmod(prompting.StateDir(), 0o500), IsNil)
		defer os.Chmod(prompting.StateDir(), 0o700)

		removed, err := rdb.RemoveRulesForSnap(s.defaultUser, "amberol")
		c.Check(err, ErrorMatches, ".*permission denied")
		c.Check(removed, IsNil)
		c.Check(rdb.Rules(s.defaultUser), DeepEquals, rules[:4])

		removed, err = rdb.RemoveRulesForInterface(s.defaultUser, "audio-playback")
		c.Check(err, ErrorMatches, ".*permission denied")
		c.Check(removed, IsNil)
		c.Check(rdb.Rules(s.defaultUser), DeepEquals, rules[:4])

		removed, err = rdb.RemoveRulesForSnapInterface(s.defaultUser, "amberol", "audio-playback")
		c.Check(err, ErrorMatches, ".*permission denied")
		c.Check(removed, IsNil)
		c.Check(rdb.Rules(s.defaultUser), DeepEquals, rules[:4])

		// Check that original rules are still on disk, and no notices were recorded.
		// Be careful, since no write has occurred, the manually-edited rules[3]
		// still has "home" interface on disk.
		rules[3].Interface = "home"
		defer func() { rules[3].Interface = "audio-playback" }()
		s.checkWrittenRuleDB(c, rules)
		s.checkNewNoticesSimple(c, nil)
	}()

	// Removing rules after the DB has been closed should return an error
	// immediately.
	c.Assert(rdb.Close(), IsNil)
	c.Assert(rdb.Rules(s.defaultUser), DeepEquals, rules[:4])
	// For some reason, Close() calling save() causes the rules to be reordered
	// on disk, but not in memory. Preserve current written DB so we can check
	// that it hasn't changed after trying to remove rules.
	currentWrittenData, err := os.ReadFile(dbPath)
	c.Assert(err, IsNil)

	removed, err = rdb.RemoveRulesForSnap(s.defaultUser, "amberol")
	c.Check(err, Equals, prompting_errors.ErrRulesClosed)
	c.Check(removed, IsNil)
	c.Check(rdb.Rules(s.defaultUser), DeepEquals, rules[:4])

	removed, err = rdb.RemoveRulesForInterface(s.defaultUser, "audio-playback")
	c.Check(err, Equals, prompting_errors.ErrRulesClosed)
	c.Check(removed, IsNil)
	c.Check(rdb.Rules(s.defaultUser), DeepEquals, rules[:4])

	removed, err = rdb.RemoveRulesForSnapInterface(s.defaultUser, "amberol", "audio-playback")
	c.Check(err, Equals, prompting_errors.ErrRulesClosed)
	c.Check(removed, IsNil)
	c.Check(rdb.Rules(s.defaultUser), DeepEquals, rules[:4])

	// Check that the data on disk has not changed
	finalWrittenData, err := os.ReadFile(dbPath)
	c.Assert(err, IsNil)
	c.Assert(string(finalWrittenData), Equals, string(currentWrittenData))
	// Check that no notices have been recorded
	s.checkNewNoticesSimple(c, nil)
}

func (s *requestrulesSuite) TestPatchRule(c *C) {
	rdb, err := requestrules.New(s.defaultNotifyRule)
	c.Assert(err, IsNil)

	template := &addRuleContents{
		User:        s.defaultUser,
		Snap:        "thunderbird",
		Interface:   "home",
		PathPattern: "/home/test/{Downloads,Documents}/**/*.{ical,mail,txt,gpg}",
		Permissions: []string{"write"},
		Outcome:     prompting.OutcomeAllow,
		Lifespan:    prompting.LifespanForever,
		Duration:    "",
	}

	var rules []*requestrules.Rule

	for _, ruleContents := range []*addRuleContents{
		{},
		{Permissions: []string{"read"}},
	} {
		rule, err := addRuleFromTemplate(c, rdb, template, ruleContents)
		c.Check(err, IsNil)
		c.Check(rule, NotNil)
		rules = append(rules, rule)
		s.checkWrittenRuleDB(c, rules)
		s.checkNewNoticesSimple(c, nil, rule)
	}

	// Patch last rule in various ways, then patch it back to its original state
	rule := rules[len(rules)-1]
	origRule := *rule

	// Check that patching with no changes works fine, and updates timestamp
	patched, err := rdb.PatchRule(rule.User, rule.ID, nil)
	c.Assert(err, IsNil)
	s.checkWrittenRuleDB(c, append(rules[:len(rules)-1], patched))
	s.checkNewNoticesSimple(c, nil, rule)
	// Check that timestamp has changed
	c.Check(patched.Timestamp.Equal(rule.Timestamp), Equals, false)
	// After making timestamp the same again, check that the rules are identical
	patched.Timestamp = rule.Timestamp
	c.Check(patched, DeepEquals, rule)

	rule = patched

	// Check that patching with identical content works fine, and updates timestamp
	newConstraints := &prompting.PatchConstraints{
		PathPattern: rule.Constraints.PathPattern,
		Permissions: prompting.PermissionMap{
			"read": &prompting.PermissionEntry{
				Outcome:  rule.Constraints.Permissions["read"].Outcome,
				Lifespan: rule.Constraints.Permissions["read"].Lifespan,
			},
		},
	}
	patched, err = rdb.PatchRule(rule.User, rule.ID, newConstraints)
	c.Assert(err, IsNil)
	s.checkWrittenRuleDB(c, append(rules[:len(rules)-1], patched))
	s.checkNewNoticesSimple(c, nil, rule)
	// Check that timestamp has changed
	c.Check(patched.Timestamp.Equal(rule.Timestamp), Equals, false)
	// After making timestamp the same again, check that the rules are identical
	patched.Timestamp = origRule.Timestamp
	c.Check(patched, DeepEquals, &origRule)

	rule = patched

	newConstraints = &prompting.PatchConstraints{
		Permissions: prompting.PermissionMap{
			"execute": &prompting.PermissionEntry{
				Outcome:  rule.Constraints.Permissions["read"].Outcome,
				Lifespan: rule.Constraints.Permissions["read"].Lifespan,
			},
		},
	}
	patched, err = rdb.PatchRule(rule.User, rule.ID, newConstraints)
	c.Assert(err, IsNil)
	s.checkWrittenRuleDB(c, append(rules[:len(rules)-1], patched))
	s.checkNewNoticesSimple(c, nil, rule)
	// Check that timestamp has changed
	c.Check(patched.Timestamp.Equal(rule.Timestamp), Equals, false)
	// After making timestamp the same again, check that the rules are identical
	patched.Timestamp = rule.Timestamp
	rule.Constraints = &prompting.RuleConstraints{
		PathPattern: patched.Constraints.PathPattern,
		Permissions: prompting.RulePermissionMap{
			"read": &prompting.RulePermissionEntry{
				Outcome:  patched.Constraints.Permissions["read"].Outcome,
				Lifespan: patched.Constraints.Permissions["read"].Lifespan,
			},
			"execute": &prompting.RulePermissionEntry{
				Outcome:  patched.Constraints.Permissions["read"].Outcome,
				Lifespan: patched.Constraints.Permissions["read"].Lifespan,
			},
		},
	}
	c.Check(patched, DeepEquals, rule)

	rule = patched

	newConstraints = &prompting.PatchConstraints{
		Permissions: prompting.PermissionMap{
			"read": &prompting.PermissionEntry{
				Outcome:  prompting.OutcomeDeny,
				Lifespan: prompting.LifespanForever,
			},
			"execute": &prompting.PermissionEntry{
				Outcome:  prompting.OutcomeDeny,
				Lifespan: prompting.LifespanForever,
			},
		},
	}
	patched, err = rdb.PatchRule(rule.User, rule.ID, newConstraints)
	c.Assert(err, IsNil)
	s.checkWrittenRuleDB(c, append(rules[:len(rules)-1], patched))
	s.checkNewNoticesSimple(c, nil, rule)
	// Check that timestamp has changed
	c.Check(patched.Timestamp.Equal(rule.Timestamp), Equals, false)
	// After making timestamp the same again, check that the rules are identical
	patched.Timestamp = rule.Timestamp
	rule.Constraints.Permissions["read"].Outcome = prompting.OutcomeDeny
	rule.Constraints.Permissions["execute"].Outcome = prompting.OutcomeDeny
	c.Check(patched, DeepEquals, rule)

	rule = patched

	newConstraints = &prompting.PatchConstraints{
		Permissions: prompting.PermissionMap{
			"read": &prompting.PermissionEntry{
				Outcome:  prompting.OutcomeDeny,
				Lifespan: prompting.LifespanTimespan,
				Duration: "10s",
			},
			"execute": &prompting.PermissionEntry{
				Outcome:  prompting.OutcomeDeny,
				Lifespan: prompting.LifespanTimespan,
				Duration: "10s",
			},
		},
	}
	patched, err = rdb.PatchRule(rule.User, rule.ID, newConstraints)
	c.Assert(err, IsNil)
	s.checkWrittenRuleDB(c, append(rules[:len(rules)-1], patched))
	s.checkNewNoticesSimple(c, nil, rule)
	// Check that timestamp has changed
	c.Check(patched.Timestamp.Equal(rule.Timestamp), Equals, false)
	// After making timestamp the same again, check that the rules are identical
	patched.Timestamp = rule.Timestamp
	rule.Constraints.Permissions["read"].Lifespan = prompting.LifespanTimespan
	rule.Constraints.Permissions["execute"].Lifespan = prompting.LifespanTimespan
	rule.Constraints.Permissions["read"].Expiration = patched.Constraints.Permissions["read"].Expiration
	rule.Constraints.Permissions["execute"].Expiration = patched.Constraints.Permissions["execute"].Expiration
	c.Check(patched, DeepEquals, rule)

	rule = patched

	newConstraints = &prompting.PatchConstraints{
		Permissions: prompting.PermissionMap{
			"read": &prompting.PermissionEntry{
				Outcome:  origRule.Constraints.Permissions["read"].Outcome,
				Lifespan: origRule.Constraints.Permissions["read"].Lifespan,
			},
			"execute": nil,
		},
	}
	patched, err = rdb.PatchRule(rule.User, rule.ID, newConstraints)
	c.Assert(err, IsNil)
	s.checkWrittenRuleDB(c, append(rules[:len(rules)-1], patched))
	s.checkNewNoticesSimple(c, nil, rule)
	// Check that timestamp has changed
	c.Check(patched.Timestamp.Equal(rule.Timestamp), Equals, false)
	// After making timestamp the same again, check that the rules are identical
	patched.Timestamp = origRule.Timestamp
	c.Check(patched, DeepEquals, &origRule)
}

func (s *requestrulesSuite) TestPatchRuleErrors(c *C) {
	rdb, err := requestrules.New(s.defaultNotifyRule)
	c.Assert(err, IsNil)

	template := &addRuleContents{
		User:        s.defaultUser,
		Snap:        "thunderbird",
		Interface:   "home",
		PathPattern: "/home/test/{Downloads,Documents}/**/*.{ical,mail,txt,gpg}",
		Permissions: []string{"write"},
		Outcome:     prompting.OutcomeAllow,
		Lifespan:    prompting.LifespanForever,
		Duration:    "",
	}

	var rules []*requestrules.Rule

	for _, ruleContents := range []*addRuleContents{
		{},
		{Permissions: []string{"read"}},
	} {
		rule, err := addRuleFromTemplate(c, rdb, template, ruleContents)
		c.Check(err, IsNil)
		c.Check(rule, NotNil)
		rules = append(rules, rule)
		s.checkWrittenRuleDB(c, rules)
		s.checkNewNoticesSimple(c, nil, rule)
	}

	// Patch last rule in various ways, then patch it back to its original state
	rule := rules[len(rules)-1]

	// Wrong user
	result, err := rdb.PatchRule(rule.User+1, rule.ID, nil)
	c.Check(err, Equals, prompting_errors.ErrRuleNotAllowed)
	c.Check(result, IsNil)
	s.checkWrittenRuleDB(c, rules)
	s.checkNewNoticesSimple(c, nil)

	// Wrong ID
	result, err = rdb.PatchRule(rule.User, prompting.IDType(1234), nil)
	c.Check(err, Equals, prompting_errors.ErrRuleNotFound)
	c.Check(result, IsNil)
	s.checkWrittenRuleDB(c, rules)
	s.checkNewNoticesSimple(c, nil)

	// Invalid lifespan
	badConstraints := &prompting.PatchConstraints{
		Permissions: prompting.PermissionMap{
			"read": &prompting.PermissionEntry{
				Outcome:  prompting.OutcomeAllow,
				Lifespan: prompting.LifespanSingle,
			},
		},
	}
	result, err = rdb.PatchRule(rule.User, rule.ID, badConstraints)
	c.Check(err, ErrorMatches, prompting_errors.NewRuleLifespanSingleError(prompting.SupportedRuleLifespans).Error())
	c.Check(result, IsNil)
	s.checkWrittenRuleDB(c, rules)
	s.checkNewNoticesSimple(c, nil)

	// Conflicting rule
	conflictingConstraints := &prompting.PatchConstraints{
		Permissions: prompting.PermissionMap{
			"read": &prompting.PermissionEntry{
				Outcome:  prompting.OutcomeDeny,
				Lifespan: prompting.LifespanForever,
			},
			"write": &prompting.PermissionEntry{
				Outcome:  prompting.OutcomeDeny,
				Lifespan: prompting.LifespanForever,
			},
			"execute": &prompting.PermissionEntry{
				Outcome:  prompting.OutcomeDeny,
				Lifespan: prompting.LifespanForever,
			},
		},
	}
	result, err = rdb.PatchRule(rule.User, rule.ID, conflictingConstraints)
	c.Check(err, ErrorMatches, fmt.Sprintf("cannot patch rule: %v", prompting_errors.ErrRuleConflict))
	c.Check(result, IsNil)
	s.checkWrittenRuleDB(c, rules)
	s.checkNewNoticesSimple(c, nil)

	// Save fails
	func() {
		c.Assert(os.Chmod(prompting.StateDir(), 0o500), IsNil)
		defer os.Chmod(prompting.StateDir(), 0o700)
		result, err = rdb.PatchRule(rule.User, rule.ID, nil)
		c.Check(err, NotNil)
		c.Check(result, IsNil)
		s.checkWrittenRuleDB(c, rules)
		s.checkNewNoticesSimple(c, nil)
	}()

	// DB Closed
	c.Assert(rdb.Close(), IsNil)
	result, err = rdb.PatchRule(rule.User, rule.ID, nil)
	c.Check(err, Equals, prompting_errors.ErrRulesClosed)
	c.Check(result, IsNil)
	s.checkWrittenRuleDB(c, rules)
	s.checkNewNoticesSimple(c, nil)
}

func (s *requestrulesSuite) TestPatchRuleExpired(c *C) {
	rdb, err := requestrules.New(s.defaultNotifyRule)
	c.Assert(err, IsNil)

	template := &addRuleContents{
		User:        s.defaultUser,
		Snap:        "thunderbird",
		Interface:   "home",
		PathPattern: "/home/test/{Downloads,Documents}/**/*.{ical,mail,txt,gpg}",
		Permissions: []string{"write"},
		Outcome:     prompting.OutcomeAllow,
		Lifespan:    prompting.LifespanForever,
		Duration:    "",
	}

	var rules []*requestrules.Rule

	for _, ruleContents := range []*addRuleContents{
		{Lifespan: prompting.LifespanTimespan, Duration: "1ms"},
		{Lifespan: prompting.LifespanTimespan, Duration: "1ms", Permissions: []string{"read"}},
		{Permissions: []string{"execute"}},
	} {
		rule, err := addRuleFromTemplate(c, rdb, template, ruleContents)
		c.Check(err, IsNil)
		c.Check(rule, NotNil)
		rules = append(rules, rule)
		s.checkWrittenRuleDB(c, rules)
		s.checkNewNoticesSimple(c, nil, rule)
	}

	time.Sleep(time.Millisecond)

	// Patching doesn't conflict with already-expired rules
	rule := rules[2]
	newConstraints := &prompting.PatchConstraints{
		Permissions: prompting.PermissionMap{
			"read": &prompting.PermissionEntry{
				Outcome:  prompting.OutcomeDeny,
				Lifespan: prompting.LifespanForever,
			},
			"write": &prompting.PermissionEntry{
				Outcome:  prompting.OutcomeDeny,
				Lifespan: prompting.LifespanForever,
			},
			"execute": &prompting.PermissionEntry{
				Outcome:  prompting.OutcomeDeny,
				Lifespan: prompting.LifespanForever,
			},
		},
	}
	patched, err := rdb.PatchRule(rule.User, rule.ID, newConstraints)
	c.Assert(err, IsNil)
	s.checkWrittenRuleDB(c, []*requestrules.Rule{patched})
	expectedNotices := []*noticeInfo{
		{
			userID: rules[1].User,
			ruleID: rules[1].ID,
			data:   map[string]string{"removed": "expired"},
		},
		{
			userID: rules[0].User,
			ruleID: rules[0].ID,
			data:   map[string]string{"removed": "expired"},
		},
		{
			userID: rules[2].User,
			ruleID: rules[2].ID,
			data:   nil,
		},
	}
	s.checkNewNoticesUnordered(c, expectedNotices)
	// Check that timestamp has changed
	c.Check(patched.Timestamp.Equal(rule.Timestamp), Equals, false)
	// After making timestamp the same again, check that the rules are identical
	patched.Timestamp = rule.Timestamp
	rule.Constraints.Permissions = prompting.RulePermissionMap{
		"read": &prompting.RulePermissionEntry{
			Outcome:  prompting.OutcomeDeny,
			Lifespan: prompting.LifespanForever,
		},
		"write": &prompting.RulePermissionEntry{
			Outcome:  prompting.OutcomeDeny,
			Lifespan: prompting.LifespanForever,
		},
		"execute": &prompting.RulePermissionEntry{
			Outcome:  prompting.OutcomeDeny,
			Lifespan: prompting.LifespanForever,
		},
	}
	c.Check(patched, DeepEquals, rule)
}
