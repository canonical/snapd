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
	"sync"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"

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
	c.Assert(os.MkdirAll(dirs.SnapdStateDir(dirs.GlobalRootDir), 0o755), IsNil)
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
	// Create file at dirs.SnapInterfacesRequestsStateDir so that dir creation fails
	stateDir := dirs.SnapInterfacesRequestsStateDir
	f, err := os.Create(stateDir)
	c.Assert(err, IsNil)
	f.Close() // No need to keep the file open

	rdb, err := requestrules.New(s.defaultNotifyRule)
	c.Assert(err, ErrorMatches, "cannot create interfaces requests state directory.*") // Error from os.MkdirAll
	c.Assert(rdb, IsNil)

	// Remove the state dir file so we can continue
	c.Assert(os.Remove(stateDir), IsNil)

	// Create directory to conflict with max ID mmap
	maxIDFilepath := filepath.Join(dirs.SnapInterfacesRequestsStateDir, "request-rule-max-id")
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
	dbPath := filepath.Join(dirs.SnapInterfacesRequestsStateDir, "request-rules.json")
	parent := filepath.Dir(dbPath)
	c.Assert(os.MkdirAll(parent, 0o755), IsNil)
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
	dbPath := filepath.Join(dirs.SnapInterfacesRequestsStateDir, "request-rules.json")

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

func (s *requestrulesSuite) ruleTemplateWithReadPathPattern(c *C, id prompting.IDType, pattern string) *requestrules.Rule {
	rule := s.ruleTemplateWithRead(c, id)
	rule.Constraints.PathPattern = mustParsePathPattern(c, pattern)
	return rule
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

// ruleTemplateWithPathPattern returns a rule with valid contents, intended to be customized.
func (s *requestrulesSuite) ruleTemplateWithPathPattern(c *C, id prompting.IDType, pattern string) *requestrules.Rule {
	rule := s.ruleTemplate(c, id)
	rule.Constraints.PathPattern = mustParsePathPattern(c, pattern)
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
	expired := s.ruleTemplateWithPathPattern(c, prompting.IDType(2), "/home/test/other")
	setPermissionsOutcomeLifespanExpirationSession(c, expired, []string{"read"}, prompting.OutcomeAllow, prompting.LifespanTimespan, currTime.Add(-10*time.Second), 0)
	// Add rule which conflicts with IDs but doesn't otherwise conflict
	conflicting := s.ruleTemplateWithRead(c, prompting.IDType(1))
	conflicting.Constraints.PathPattern = mustParsePathPattern(c, "/home/test/another")

	rules := []*requestrules.Rule{good, expired, conflicting}
	s.writeRules(c, dbPath, rules)

	checkWritten := true
	s.testLoadError(c, fmt.Sprintf("cannot add rule: %v.*", prompting_errors.ErrRuleIDConflict), rules, checkWritten)
}

func setPermissionsOutcomeLifespanExpirationSession(c *C, rule *requestrules.Rule, permissions []string, outcome prompting.OutcomeType, lifespan prompting.LifespanType, expiration time.Time, userSessionID prompting.IDType) {
	for _, perm := range permissions {
		rule.Constraints.Permissions[perm] = &prompting.RulePermissionEntry{
			Outcome:    outcome,
			Lifespan:   lifespan,
			Expiration: expiration,
			SessionID:  userSessionID,
		}
	}
}

func (s *requestrulesSuite) TestLoadErrorConflictingPattern(c *C) {
	dbPath := s.prepDBPath(c)
	currTime := time.Now()
	good := s.ruleTemplateWithReadPathPattern(c, prompting.IDType(1), "/home/test/{foo,bar}")
	// Expired rules should still get a {"removed": "expired"} notice, even if they don't conflict
	expired := s.ruleTemplateWithRead(c, prompting.IDType(2))
	expired.Constraints.PathPattern = mustParsePathPattern(c, "/home/test/other")
	setPermissionsOutcomeLifespanExpirationSession(c, expired, []string{"read"}, prompting.OutcomeAllow, prompting.LifespanTimespan, currTime.Add(-10*time.Second), 0)
	// Add rule with conflicting permissions but not conflicting ID.
	conflicting := s.ruleTemplateWithReadPathPattern(c, prompting.IDType(3), "/home/test/{bar,foo}")
	// Even with not all permissions being in conflict, still error
	var timeZero time.Time
	setPermissionsOutcomeLifespanExpirationSession(c, conflicting, []string{"read", "write"}, prompting.OutcomeDeny, prompting.LifespanForever, timeZero, 0)

	rules := []*requestrules.Rule{good, expired, conflicting}
	s.writeRules(c, dbPath, rules)

	checkWritten := true
	s.testLoadError(c, fmt.Sprintf("cannot add rule: %v.*", prompting_errors.ErrRuleConflict), rules, checkWritten)
}

func (s *requestrulesSuite) TestLoadExpiredRules(c *C) {
	dbPath := s.prepDBPath(c)
	currTime := time.Now()
	var timeZero time.Time

	good1 := s.ruleTemplateWithReadPathPattern(c, prompting.IDType(1), "/home/test/{foo,bar}")

	// At the moment, expired rules with conflicts are discarded without error,
	// but we don't want to test this as part of our contract

	expired1 := s.ruleTemplateWithPathPattern(c, prompting.IDType(2), "/home/test/other")
	setPermissionsOutcomeLifespanExpirationSession(c, expired1, []string{"read"}, prompting.OutcomeAllow, prompting.LifespanSession, timeZero, prompting.IDType(0x12345))

	// Rules with overlapping pattern but non-conflicting permissions do not conflict
	good2 := s.ruleTemplateWithPathPattern(c, prompting.IDType(3), "/home/test/{bar,foo}")
	setPermissionsOutcomeLifespanExpirationSession(c, good2, []string{"write"}, prompting.OutcomeDeny, prompting.LifespanForever, timeZero, 0)

	expired2 := s.ruleTemplateWithPathPattern(c, prompting.IDType(4), "/home/test/another")
	setPermissionsOutcomeLifespanExpirationSession(c, expired2, []string{"read"}, prompting.OutcomeAllow, prompting.LifespanTimespan, currTime.Add(-time.Nanosecond), 0)

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

func (s *requestrulesSuite) TestLoadMergedRules(c *C) {
	dbPath := s.prepDBPath(c)

	good1 := s.ruleTemplateWithReadPathPattern(c, prompting.IDType(1), "/home/test/{foo,bar}")
	identical1 := s.ruleTemplateWithReadPathPattern(c, prompting.IDType(2), "/home/test/{foo,bar}")
	expected1 := good1
	expected1.Timestamp = identical1.Timestamp // Timestamp should be the second timestamp

	// Rules with identical pattern but non-overlapping permissions do not conflict
	good2 := s.ruleTemplateWithPathPattern(c, prompting.IDType(3), "/home/test/something")
	good2.Constraints.Permissions = prompting.RulePermissionMap{
		"read": &prompting.RulePermissionEntry{
			Outcome:  prompting.OutcomeAllow,
			Lifespan: prompting.LifespanForever,
		},
	}
	nonOverlap2 := s.ruleTemplateWithPathPattern(c, prompting.IDType(4), "/home/test/something")
	nonOverlap2.Constraints.Permissions = prompting.RulePermissionMap{
		"write": &prompting.RulePermissionEntry{
			Outcome:    prompting.OutcomeDeny,
			Lifespan:   prompting.LifespanTimespan,
			Expiration: nonOverlap2.Timestamp.Add(10 * time.Second),
		},
	}
	expected2 := s.ruleTemplateWithPathPattern(c, prompting.IDType(3), "/home/test/something")
	expected2.Timestamp = nonOverlap2.Timestamp // Timestamp should be the second timestamp
	expected2.Constraints.Permissions = prompting.RulePermissionMap{
		"read": &prompting.RulePermissionEntry{
			Outcome:  prompting.OutcomeAllow,
			Lifespan: prompting.LifespanForever,
		},
		"write": &prompting.RulePermissionEntry{
			Outcome:  prompting.OutcomeDeny,
			Lifespan: prompting.LifespanTimespan,
			// Expiration should be based on nonOverlap2 timestamp
			Expiration: nonOverlap2.Timestamp.Add(10 * time.Second),
		},
	}

	// Rules which overlap but don't conflict preserve longer lifespan
	good3 := s.ruleTemplateWithPathPattern(c, prompting.IDType(5), "/home/test/another")
	good3.Constraints.Permissions = prompting.RulePermissionMap{
		"read": &prompting.RulePermissionEntry{
			Outcome:  prompting.OutcomeDeny,
			Lifespan: prompting.LifespanForever,
		},
		"write": &prompting.RulePermissionEntry{
			Outcome:    prompting.OutcomeAllow,
			Lifespan:   prompting.LifespanTimespan,
			Expiration: good3.Timestamp.Add(10 * time.Second),
		},
		"execute": &prompting.RulePermissionEntry{
			Outcome:    prompting.OutcomeAllow,
			Lifespan:   prompting.LifespanTimespan,
			Expiration: good3.Timestamp.Add(time.Second),
		},
	}
	overlap3 := s.ruleTemplateWithPathPattern(c, prompting.IDType(6), "/home/test/another")
	overlap3.Constraints.Permissions = prompting.RulePermissionMap{
		"read": &prompting.RulePermissionEntry{
			Outcome:    prompting.OutcomeDeny,
			Lifespan:   prompting.LifespanTimespan,
			Expiration: overlap3.Timestamp.Add(10 * time.Second),
		},
		"write": &prompting.RulePermissionEntry{
			Outcome:  prompting.OutcomeAllow,
			Lifespan: prompting.LifespanForever,
		},
	}
	expected3 := s.ruleTemplateWithPathPattern(c, prompting.IDType(5), "/home/test/another")
	expected3.Timestamp = overlap3.Timestamp // Timestamp should be the second timestamp
	expected3.Constraints.Permissions = prompting.RulePermissionMap{
		"read": &prompting.RulePermissionEntry{
			Outcome:  prompting.OutcomeDeny,
			Lifespan: prompting.LifespanForever,
		},
		"write": &prompting.RulePermissionEntry{
			Outcome:  prompting.OutcomeAllow,
			Lifespan: prompting.LifespanForever,
		},
		"execute": &prompting.RulePermissionEntry{
			Outcome:  prompting.OutcomeAllow,
			Lifespan: prompting.LifespanTimespan,
			// Expiration should be based on good3 timestamp
			Expiration: good3.Timestamp.Add(time.Second),
		},
	}

	// Rules which overlap but don't conflict preserve longer lifespan, and
	// will be merged into existing rule even if that rule is completely
	// superseded.
	good4 := s.ruleTemplateWithPathPattern(c, prompting.IDType(7), "/home/test/one/more")
	good4.Constraints.Permissions = prompting.RulePermissionMap{
		"read": &prompting.RulePermissionEntry{
			Outcome:    prompting.OutcomeDeny,
			Lifespan:   prompting.LifespanTimespan,
			Expiration: good4.Timestamp.Add(10 * time.Second),
		},
		"write": &prompting.RulePermissionEntry{
			Outcome:    prompting.OutcomeAllow,
			Lifespan:   prompting.LifespanTimespan,
			Expiration: good4.Timestamp.Add(10 * time.Second),
		},
		"execute": &prompting.RulePermissionEntry{
			Outcome:    prompting.OutcomeAllow,
			Lifespan:   prompting.LifespanTimespan,
			Expiration: good4.Timestamp.Add(time.Nanosecond), // will expire
		},
	}
	overlap4 := s.ruleTemplateWithPathPattern(c, prompting.IDType(8), "/home/test/one/more")
	overlap4.Constraints.Permissions = prompting.RulePermissionMap{
		"read": &prompting.RulePermissionEntry{
			Outcome:    prompting.OutcomeDeny,
			Lifespan:   prompting.LifespanTimespan,
			Expiration: overlap4.Timestamp.Add(20 * time.Second),
		},
		"write": &prompting.RulePermissionEntry{
			Outcome:  prompting.OutcomeAllow,
			Lifespan: prompting.LifespanForever,
		},
	}
	expected4 := s.ruleTemplateWithPathPattern(c, prompting.IDType(7), "/home/test/one/more")
	expected4.Timestamp = overlap4.Timestamp // Timestamp should be the second timestamp
	expected4.Constraints.Permissions = prompting.RulePermissionMap{
		"read": &prompting.RulePermissionEntry{
			Outcome:  prompting.OutcomeDeny,
			Lifespan: prompting.LifespanTimespan,
			// Expiration should be based on overlap4 timestamp
			Expiration: overlap4.Timestamp.Add(20 * time.Second),
		},
		"write": &prompting.RulePermissionEntry{
			Outcome:  prompting.OutcomeAllow,
			Lifespan: prompting.LifespanForever,
		},
	}

	rules := []*requestrules.Rule{good1, identical1, good2, nonOverlap2, good3, overlap3, good4, overlap4}
	s.writeRules(c, dbPath, rules)

	logbuf, restore := logger.MockLogger()
	defer restore()
	rdb, err := requestrules.New(s.defaultNotifyRule)
	c.Check(err, IsNil)
	c.Check(rdb, NotNil)
	// Check that no error was logged
	c.Check(logbuf.String(), HasLen, 0)

	expectedWrittenRules := []*requestrules.Rule{expected1, expected2, expected3, expected4}
	s.checkWrittenRuleDB(c, expectedWrittenRules)

	expectedNoticeInfo := []*noticeInfo{
		{
			userID: good1.User,
			ruleID: good1.ID,
			data:   nil,
		},
		{
			userID: identical1.User,
			ruleID: identical1.ID,
			data: map[string]string{
				"removed":     "merged",
				"merged-into": good1.ID.String(),
			},
		},
		{
			userID: good2.User,
			ruleID: good2.ID,
			data:   nil,
		},
		{
			userID: nonOverlap2.User,
			ruleID: nonOverlap2.ID,
			data: map[string]string{
				"removed":     "merged",
				"merged-into": good2.ID.String(),
			},
		},
		{
			userID: good3.User,
			ruleID: good3.ID,
			data:   nil,
		},
		{
			userID: overlap3.User,
			ruleID: overlap3.ID,
			data: map[string]string{
				"removed":     "merged",
				"merged-into": good3.ID.String(),
			},
		},
		{
			userID: good4.User,
			ruleID: good4.ID,
			data:   nil,
		},
		{
			userID: overlap4.User,
			ruleID: overlap4.ID,
			data: map[string]string{
				"removed":     "merged",
				"merged-into": good4.ID.String(),
			},
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
	c.Assert(os.Chmod(dirs.SnapInterfacesRequestsStateDir, 0o500), IsNil)
	defer os.Chmod(dirs.SnapInterfacesRequestsStateDir, 0o755)

	c.Check(rdb.Close(), NotNil)
}

func (s *requestrulesSuite) TestUserSessionPath(c *C) {
	for _, testCase := range []struct {
		userID   uint32
		expected string
	}{
		{1000, "/run/user/1000"},
		{0, "/run/user/0"},
		{1, "/run/user/1"},
		{65535, "/run/user/65535"},
		{65536, "/run/user/65536"},
		{4294967295, "/run/user/4294967295"},
	} {
		expectedWithTestPrefix := filepath.Join(dirs.GlobalRootDir, testCase.expected)
		c.Check(requestrules.UserSessionPath(testCase.userID), Equals, expectedWithTestPrefix)
	}
}

func (s *requestrulesSuite) TestReadOrAssignUserSessionID(c *C) {
	userSessionIDXattr, restore := requestrules.MockUserSessionIDXattr()
	defer restore()

	rdb, err := requestrules.New(s.defaultNotifyRule)
	c.Assert(err, IsNil)

	// If there is no user session dir, expect errNoUserSession
	noSessionID, err := rdb.ReadOrAssignUserSessionID(1000)
	c.Assert(err, ErrorMatches, "cannot find systemd user session tmpfs for user: 1000")
	c.Assert(noSessionID, Equals, prompting.IDType(0))

	// Make user session dir, as if systemd had done so for this user
	c.Assert(os.MkdirAll(filepath.Join(dirs.GlobalRootDir, "run/user/1000"), 0o700), IsNil)

	// If there is a user session dir, expect some non-zero user ID
	origID, err := rdb.ReadOrAssignUserSessionID(1000)
	if errors.Is(err, syscall.EOPNOTSUPP) {
		c.Skip("xattrs are not supported on this system")
	}
	c.Assert(err, IsNil)
	c.Assert(origID, Not(Equals), prompting.IDType(0))

	// If a user session ID is already present for this session, retrieve it
	// rather than defining a new one
	retrievedID, err := rdb.ReadOrAssignUserSessionID(1000)
	c.Assert(err, IsNil)
	c.Assert(retrievedID, Equals, origID)
	// Try again, for good measure
	retrievedID, err = rdb.ReadOrAssignUserSessionID(1000)
	c.Assert(err, IsNil)
	c.Assert(retrievedID, Equals, origID)

	// If the user session restarts, the user session tmpfs is deleted and
	// re-created, so the xattr is no longer present. So we set a new ID.
	c.Assert(os.Remove(filepath.Join(dirs.GlobalRootDir, "run/user/1000")), IsNil)
	c.Assert(os.MkdirAll(filepath.Join(dirs.GlobalRootDir, "run/user/1000"), 0o700), IsNil)
	newID, err := rdb.ReadOrAssignUserSessionID(1000)
	c.Assert(err, IsNil)
	c.Assert(newID, Not(Equals), 0)
	c.Assert(newID, Not(Equals), origID)

	// If we try for a different user without a session, we get the error
	noSessionID, err = rdb.ReadOrAssignUserSessionID(1234)
	c.Assert(err, ErrorMatches, "cannot find systemd user session tmpfs for user: 1234")
	c.Assert(noSessionID, Equals, prompting.IDType(0))

	// Make user session dir, as if systemd had done so for this user
	c.Assert(os.MkdirAll(filepath.Join(dirs.GlobalRootDir, "run/user/1234"), 0o700), IsNil)

	// If there is a user session dir, expect some non-zero user ID different
	// from that of the other user
	secondUserID, err := rdb.ReadOrAssignUserSessionID(1234)
	c.Assert(err, IsNil)
	c.Assert(secondUserID, Not(Equals), prompting.IDType(0))
	c.Assert(secondUserID, Not(Equals), newID)

	// If we get the first user's session ID, it's still the same
	firstUserID, err := rdb.ReadOrAssignUserSessionID(1000)
	c.Assert(err, IsNil)
	c.Assert(firstUserID, Not(Equals), prompting.IDType(0))
	c.Assert(firstUserID, Not(Equals), secondUserID)
	c.Assert(firstUserID, Equals, newID)

	// If we remove the user session for the first user, we get the error again
	c.Assert(os.Remove(filepath.Join(dirs.GlobalRootDir, "run/user/1000")), IsNil)
	noSessionID, err = rdb.ReadOrAssignUserSessionID(1000)
	c.Assert(err, ErrorMatches, "cannot find systemd user session tmpfs for user: 1000")
	c.Assert(noSessionID, Equals, prompting.IDType(0))

	// But we can still retrieve the session ID for the second user
	retrievedID, err = rdb.ReadOrAssignUserSessionID(1234)
	c.Assert(err, IsNil)
	c.Assert(retrievedID, Not(Equals), prompting.IDType(0))
	c.Assert(retrievedID, Equals, secondUserID)

	// If the xattr is corrupted, we get a new ID
	c.Assert(unix.Setxattr(filepath.Join(dirs.GlobalRootDir, "run/user/1234"), userSessionIDXattr, []byte("foo"), 0), IsNil)
	regeneratedID, err := rdb.ReadOrAssignUserSessionID(1234)
	c.Assert(err, IsNil)
	c.Assert(regeneratedID, Not(Equals), prompting.IDType(0))
	c.Assert(regeneratedID, Not(Equals), secondUserID)
}

func (s *requestrulesSuite) TestReadOrAssignUserSessionIDConcurrent(c *C) {
	_, restore := requestrules.MockUserSessionIDXattr()
	defer restore()

	rdb, err := requestrules.New(s.defaultNotifyRule)
	c.Assert(err, IsNil)

	// Multiple threads acting at once all return the same ID
	c.Assert(os.MkdirAll(filepath.Join(dirs.GlobalRootDir, "run/user/5000"), 0o700), IsNil)
	startChan := make(chan struct{}) // close to broadcast to all threads
	count := 10
	var startWG sync.WaitGroup
	startWG.Add(count)
	resultChan := make(chan prompting.IDType, count)
	errChan := make(chan error, count)
	for i := 0; i < count; i++ {
		go func() {
			startWG.Done()
			<-startChan // wait for broadcast
			sessionID, err := rdb.ReadOrAssignUserSessionID(5000)
			if err != nil {
				errChan <- err
			} else {
				c.Assert(sessionID, Not(Equals), prompting.IDType(0))
				resultChan <- sessionID
			}
		}()
	}
	startWG.Wait()
	time.Sleep(10 * time.Millisecond) // wait until they're all waiting on startChan
	// Start all goroutines simultaneously
	close(startChan)

	// Get session ID from first that sends one
	var firstID prompting.IDType
	select {
	case firstID = <-resultChan:
		c.Assert(firstID, NotNil)
		c.Assert(firstID, Not(Equals), prompting.IDType(0))
	case err := <-errChan:
		if errors.Is(err, syscall.EOPNOTSUPP) {
			c.Skip("xattrs are not supported on this system")
		}
		c.Assert(err, IsNil)
	case <-time.NewTimer(time.Second).C:
		c.Fatal("timed out waiting for first user ID")
	}

	// Check that each other goroutine retrieved the same session ID
	for i := 1; i < count; i++ {
		select {
		case retrievedID := <-resultChan:
			c.Assert(retrievedID, NotNil)
			c.Assert(retrievedID, Not(Equals), prompting.IDType(0))
			c.Assert(retrievedID, Equals, firstID)
		case <-time.NewTimer(time.Second).C:
			c.Fatalf("timed out waiting for %dth ID", i)
		}
	}
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
	currSession := prompting.IDType(0x12345)
	restore := requestrules.MockReadOrAssignUserSessionID(func(rdb *requestrules.RuleDB, user uint32) (prompting.IDType, error) {
		return currSession, nil
	})
	defer restore()

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

	// Add one rule matching template, then rules with at least one differing
	// field and check that all rules add without error.
	for _, ruleContents := range []*addRuleContents{
		{}, // use template
		{User: s.defaultUser + 1},
		{Snap: "thunderbird"},
		// Can't change interface, as only "home" is supported for now (TODO: update)
		{PathPattern: "/home/test/**/*.{jpg,png,svg}"}, // no /Pictures/
		// Differing Outcome, Lifespan or Duration does not prevent conflict
		{PathPattern: "/home/test/1", Outcome: prompting.OutcomeDeny},
		{PathPattern: "/home/test/2", Lifespan: prompting.LifespanTimespan, Duration: "10s"},
		{PathPattern: "/home/test/3", Lifespan: prompting.LifespanSession},
	} {
		rule, err := addRuleFromTemplate(c, rdb, template, ruleContents)
		c.Assert(err, IsNil)
		c.Assert(rule, NotNil)
		addedRules = append(addedRules, rule)
		s.checkWrittenRuleDB(c, addedRules)
		s.checkNewNoticesSimple(c, nil, rule)
	}

	// Add a rule identical to the template but with additional permissions,
	// and see that it merges with the existing rule with the same path pattern.
	contents := &addRuleContents{Permissions: []string{"read", "execute"}}
	rule, err := addRuleFromTemplate(c, rdb, template, contents)
	c.Check(err, IsNil)
	c.Check(rule.ID, Equals, addedRules[0].ID)
	// Prepare the list of current rules, which should contain the new rule and
	// not the original rule with that ID. The order is based on the current
	// implementation, is not part of the contract and is subject to change, but
	// it makes testing easier.
	expected := make([]*requestrules.Rule, 0, len(addedRules))
	// Final rule was swapped to the start when the first was removed
	expected = append(expected, addedRules[len(addedRules)-1])
	// The rest of the rules between the first and the last were left unchanged
	expected = append(expected, addedRules[1:len(addedRules)-1]...)
	// The "new" rule with the same ID as the original rule was appended
	expected = append(expected, rule)
	s.checkWrittenRuleDB(c, expected)
	// We get a notice because the original rule was "modified" when the new
	// rule was merged into it (though no notice for its intermediate removal).
	s.checkNewNoticesSimple(c, nil, rule)
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
		{ // Invalid lifespan "session" when no active user session
			&addRuleContents{Lifespan: prompting.LifespanSession},
			prompting_errors.ErrNewSessionRuleNoSession.Error(),
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
		{ // Conflicting rule with overlapping pattern variants
			&addRuleContents{
				PathPattern: "/home/test/Pictures/**/*.{svg,jpg}",
				Permissions: []string{"read", "write"},
				Outcome:     prompting.OutcomeDeny,
			},
			fmt.Sprintf("cannot add rule: %v", prompting_errors.ErrRuleConflict),
		},
		{ // Conflicting rule with identical path pattern
			&addRuleContents{
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

	// Failure to save rule DB should roll-back adding the rule when it does not
	// merge with an existing rule, and leave the DB unchanged.
	// Set DB parent directory as read-only.
	c.Assert(os.Chmod(dirs.SnapInterfacesRequestsStateDir, 0o500), IsNil)
	result, err := addRuleFromTemplate(c, rdb, template, &addRuleContents{PathPattern: "/other", Permissions: []string{"execute"}})
	c.Assert(os.Chmod(dirs.SnapInterfacesRequestsStateDir, 0o755), IsNil)
	c.Check(err, NotNil)
	c.Check(result, IsNil)
	// Failure should result in no changes to rules, written or in-memory, and no notices
	s.checkWrittenRuleDB(c, []*requestrules.Rule{good})
	s.checkNewNoticesSimple(c, nil)
	c.Check(rdb.Rules(s.defaultUser), DeepEquals, []*requestrules.Rule{good})

	// Failure to save rule DB should roll-back adding the rule when it merges
	// with an existing rule, re-add the original rule, and leave the DB
	// unchanged.
	// Set DB parent directory as read-only.
	c.Assert(os.Chmod(dirs.SnapInterfacesRequestsStateDir, 0o500), IsNil)
	result, err = addRuleFromTemplate(c, rdb, template, &addRuleContents{Permissions: []string{"execute"}})
	c.Assert(os.Chmod(dirs.SnapInterfacesRequestsStateDir, 0o755), IsNil)
	c.Check(err, NotNil)
	c.Check(result, IsNil)
	// Failure should result in no changes to rules, written or in-memory, and no notices
	s.checkWrittenRuleDB(c, []*requestrules.Rule{good})
	s.checkNewNoticesSimple(c, nil)
	c.Check(rdb.Rules(s.defaultUser), DeepEquals, []*requestrules.Rule{good})

	// Remove read-only so we can continue

	// Adding rule after the rule DB has been closed should return an error
	// immediately.
	c.Assert(rdb.Close(), IsNil)
	// Make sure the save triggered by Close() didn't mess up the written rules
	s.checkWrittenRuleDB(c, []*requestrules.Rule{good})
	s.checkNewNoticesSimple(c, nil)
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
		{PathPattern: "/home/test/Pictures/**/*.{jp,pn,sv}g"},
		{PathPattern: "/home/test/{Documents,Pictures}/**/*.{jpg,png,svg}", Permissions: []string{"read", "write", "execute"}},
	} {
		rule, err := addRuleFromTemplate(c, rdb, template, ruleContents)
		c.Check(err, IsNil)
		c.Check(rule, NotNil)
		addedRules = append(addedRules, rule)
		s.checkWrittenRuleDB(c, addedRules)
		s.checkNewNoticesSimple(c, nil, rule)
	}

	// Add conflicting rule, and check that it conflicts with all the prior rules.
	//
	// Due to implementation details, its path pattern cannot be identical to
	// any of the previous rules, else it will only conflict with that rule, as
	// this is checked before checking other rules with different path patterns.
	rule, err := addRuleFromTemplate(c, rdb, template, &addRuleContents{
		PathPattern: "/home/test/{Pictures,Videos}/**/*.png",
		Outcome:     prompting.OutcomeDeny,
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

func (s *requestrulesSuite) TestAddRuleMerges(c *C) {
	currSession := prompting.IDType(0x12345)
	// Session will be found for all test cases, so rules with LifespanSession
	// will never be expired. It would be nice to test expired "session" rules
	// here too, but we can do this in other easier to implement tests.
	restore := requestrules.MockReadOrAssignUserSessionID(func(rdb *requestrules.RuleDB, user uint32) (prompting.IDType, error) {
		return currSession, nil
	})
	defer restore()

	for _, testCase := range []struct {
		input  []prompting.PermissionMap
		output []prompting.PermissionMap
	}{
		{
			input: []prompting.PermissionMap{
				{
					"read": &prompting.PermissionEntry{
						Outcome:  prompting.OutcomeAllow,
						Lifespan: prompting.LifespanForever,
					},
				},
				{
					"write": &prompting.PermissionEntry{
						Outcome:  prompting.OutcomeDeny,
						Lifespan: prompting.LifespanTimespan,
						Duration: "10s",
					},
				},
			},
			output: []prompting.PermissionMap{
				{
					"read": &prompting.PermissionEntry{
						Outcome:  prompting.OutcomeAllow,
						Lifespan: prompting.LifespanForever,
					},
					"write": &prompting.PermissionEntry{
						Outcome:  prompting.OutcomeDeny,
						Lifespan: prompting.LifespanTimespan,
						Duration: "10s",
					},
				},
			},
		},
		{
			input: []prompting.PermissionMap{
				{
					"read": &prompting.PermissionEntry{
						Outcome:  prompting.OutcomeDeny,
						Lifespan: prompting.LifespanForever,
					},
					"write": &prompting.PermissionEntry{
						Outcome:  prompting.OutcomeAllow,
						Lifespan: prompting.LifespanTimespan,
						Duration: "10s",
					},
					"execute": &prompting.PermissionEntry{
						Outcome:  prompting.OutcomeAllow,
						Lifespan: prompting.LifespanTimespan,
						Duration: "1s",
					},
				},
				{
					"read": &prompting.PermissionEntry{
						Outcome:  prompting.OutcomeDeny,
						Lifespan: prompting.LifespanTimespan,
						Duration: "10s",
					},
					"write": &prompting.PermissionEntry{
						Outcome:  prompting.OutcomeAllow,
						Lifespan: prompting.LifespanSession,
					},
				},
			},
			output: []prompting.PermissionMap{
				{
					"read": &prompting.PermissionEntry{
						Outcome:  prompting.OutcomeDeny,
						Lifespan: prompting.LifespanForever,
					},
					"write": &prompting.PermissionEntry{
						Outcome:  prompting.OutcomeAllow,
						Lifespan: prompting.LifespanSession,
					},
					"execute": &prompting.PermissionEntry{
						Outcome:  prompting.OutcomeAllow,
						Lifespan: prompting.LifespanTimespan,
						Duration: "1s",
					},
				},
			},
		},
		{
			// First rule is entirely superseded by the latter, but still use
			// the ID of the former in the merged rule.
			input: []prompting.PermissionMap{
				{
					"read": &prompting.PermissionEntry{
						Outcome:  prompting.OutcomeDeny,
						Lifespan: prompting.LifespanTimespan,
						Duration: "10s",
					},
					"write": &prompting.PermissionEntry{
						Outcome:  prompting.OutcomeAllow,
						Lifespan: prompting.LifespanSession,
					},
					"execute": &prompting.PermissionEntry{
						Outcome:  prompting.OutcomeAllow,
						Lifespan: prompting.LifespanTimespan,
						Duration: "1ns", // Will expire and be dropped
					},
				},
				{
					"read": &prompting.PermissionEntry{
						Outcome:  prompting.OutcomeDeny,
						Lifespan: prompting.LifespanTimespan,
						Duration: "20s",
					},
					"write": &prompting.PermissionEntry{
						Outcome:  prompting.OutcomeAllow,
						Lifespan: prompting.LifespanForever,
					},
				},
			},
			output: []prompting.PermissionMap{
				{
					"read": &prompting.PermissionEntry{
						Outcome:  prompting.OutcomeDeny,
						Lifespan: prompting.LifespanTimespan,
						Duration: "20s",
					},
					"write": &prompting.PermissionEntry{
						Outcome:  prompting.OutcomeAllow,
						Lifespan: prompting.LifespanForever,
					},
				},
			},
		},
		{
			// First rule is fully expired but would otherwise conflict with
			// the second rule.
			input: []prompting.PermissionMap{
				{
					"read": &prompting.PermissionEntry{
						Outcome:  prompting.OutcomeDeny,
						Lifespan: prompting.LifespanTimespan,
						Duration: "1ns", // Will expire and not conflict
					},
					"write": &prompting.PermissionEntry{
						Outcome:  prompting.OutcomeAllow,
						Lifespan: prompting.LifespanTimespan,
						Duration: "1ns", // Will expire and not conflict
					},
				},
				{
					"read": &prompting.PermissionEntry{
						Outcome:  prompting.OutcomeAllow,
						Lifespan: prompting.LifespanTimespan,
						Duration: "10s",
					},
					"write": &prompting.PermissionEntry{
						Outcome:  prompting.OutcomeDeny,
						Lifespan: prompting.LifespanForever,
					},
				},
			},
			output: []prompting.PermissionMap{
				{
					"read": &prompting.PermissionEntry{
						Outcome:  prompting.OutcomeAllow,
						Lifespan: prompting.LifespanTimespan,
						Duration: "10s",
					},
					"write": &prompting.PermissionEntry{
						Outcome:  prompting.OutcomeDeny,
						Lifespan: prompting.LifespanForever,
					},
				},
			},
		},
		{
			// First rule is partially expired but would otherwise conflict
			// with the second rule.
			input: []prompting.PermissionMap{
				{
					"read": &prompting.PermissionEntry{
						Outcome:  prompting.OutcomeDeny,
						Lifespan: prompting.LifespanTimespan,
						Duration: "1ns", // Will expire and not conflict
					},
					"write": &prompting.PermissionEntry{
						Outcome:  prompting.OutcomeDeny,
						Lifespan: prompting.LifespanTimespan,
						Duration: "10s",
					},
					"execute": &prompting.PermissionEntry{
						Outcome:  prompting.OutcomeAllow,
						Lifespan: prompting.LifespanForever,
					},
				},
				{
					"read": &prompting.PermissionEntry{
						Outcome:  prompting.OutcomeAllow,
						Lifespan: prompting.LifespanTimespan,
						Duration: "10s",
					},
					"write": &prompting.PermissionEntry{
						Outcome:  prompting.OutcomeDeny,
						Lifespan: prompting.LifespanForever,
					},
					"execute": &prompting.PermissionEntry{
						Outcome:  prompting.OutcomeAllow,
						Lifespan: prompting.LifespanTimespan,
						Duration: "10s",
					},
				},
			},
			output: []prompting.PermissionMap{
				{
					"read": &prompting.PermissionEntry{
						Outcome:  prompting.OutcomeAllow,
						Lifespan: prompting.LifespanTimespan,
						Duration: "10s",
					},
					"write": &prompting.PermissionEntry{
						Outcome:  prompting.OutcomeDeny,
						Lifespan: prompting.LifespanForever,
					},
					"execute": &prompting.PermissionEntry{
						Outcome:  prompting.OutcomeAllow,
						Lifespan: prompting.LifespanForever,
					},
				},
			},
		},
	} {
		// Set root so rule creation does not interfere between test cases
		dirs.SetRootDir(c.MkDir())
		s.AddCleanup(func() { dirs.SetRootDir("") })
		c.Assert(os.MkdirAll(dirs.SnapdStateDir(dirs.GlobalRootDir), 0o755), IsNil)

		rdb, err := requestrules.New(s.defaultNotifyRule)
		c.Assert(err, IsNil)

		user := s.defaultUser
		snap := "firefox"
		iface := "home"
		pathPattern := mustParsePathPattern(c, "/path/to/foo/ba{r,z}/**")

		// Add all the rules
		for _, perms := range testCase.input {
			constraints := &prompting.Constraints{
				PathPattern: pathPattern,
				Permissions: perms,
			}
			_, err = rdb.AddRule(user, snap, iface, constraints)
			c.Assert(err, IsNil, Commentf("\ntestCase: %+v\nperms: %+v", testCase, perms))
		}

		rules := rdb.Rules(s.defaultUser)
		c.Check(rules, HasLen, len(testCase.output), Commentf("\ntestCase: %+v\nrules: %+v", testCase, rules))
		for i, perms := range testCase.output {
			// Build RuleConstraints based on output perms using the timestamp
			// of the corresponding rule.
			rule := rules[i]
			constraints := &prompting.Constraints{
				PathPattern: pathPattern,
				Permissions: perms,
			}
			at := prompting.At{
				Time:      rule.Timestamp,
				SessionID: prompting.IDType(0x12345),
			}
			ruleConstraints, err := constraints.ToRuleConstraints(iface, at)
			c.Assert(err, IsNil)
			expectedPerms := ruleConstraints.Permissions
			// Check that the permissions match what is expected.
			// Other parameters should be trivially identical.
			// Need to be careful because timestamps aren't "DeepEqual", so
			// first set equivalent timestamps equal to each other.
			for perm, entry := range rule.Constraints.Permissions {
				expectedEntry, exists := expectedPerms[perm]
				c.Assert(exists, Equals, true, Commentf("\ntestCase: %+v\nrules: %+v\npermission not found: %s", testCase, rules, perm))
				c.Check(entry.Lifespan, Equals, expectedEntry.Lifespan, Commentf("\ntestCase: %+v\nrules: %+v\nlifespans not equal: %v != %v", testCase, rules, entry.Lifespan, expectedEntry.Lifespan))
				// Expiration will be duration after the timestamp of one of
				// the created rules, but it may not be the final one which was
				// merged into the resulting rule, so we don't actually have an
				// absolute timestamp with which we can compute an expiration
				// using the duration. So subtract the timestamps and check
				// that the difference is less than 100ms. We'll always have
				// deltas of 1s for time differences we care about.
				difference := entry.Expiration.Sub(expectedEntry.Expiration)
				// TODO: call Abs() once we're on Go 1.19+
				if difference < 0 {
					difference *= -1
				}
				c.Check(difference < 100*time.Millisecond, Equals, true, Commentf("\ntestCase: %+v\nrules: %+v\nexpirations not within 100ms: %v != %v", testCase, rules, entry.Expiration, expectedEntry.Expiration))
				expectedEntry.Expiration = entry.Expiration
			}
			c.Check(rule.Constraints.Permissions, DeepEquals, expectedPerms)
		}

		c.Assert(rdb.Close(), IsNil)
	}
}

func (s *requestrulesSuite) TestAddRuleExpired(c *C) {
	var currSession prompting.IDType
	restore := requestrules.MockReadOrAssignUserSessionID(func(rdb *requestrules.RuleDB, user uint32) (prompting.IDType, error) {
		return currSession, nil
	})
	defer restore()

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
	// initialSessionAllow rule should still be on disk, along with new rule
	s.checkWrittenRuleDB(c, []*requestrules.Rule{good})
	s.checkNewNoticesSimple(c, nil, good)

	// First add deny rule with lifespan "session"
	currSession = prompting.IDType(0x12345)
	initialSessionDeny, err := addRuleFromTemplate(c, rdb, template, &addRuleContents{
		Outcome: prompting.OutcomeDeny,
		// Make path pattern conflict but not be identical
		PathPattern: "/home/test/**/{secret,private,foo}/**",
		Lifespan:    prompting.LifespanSession,
	})
	c.Assert(err, IsNil)
	c.Assert(initialSessionDeny, NotNil)
	// Rule should be on disk and have notice
	s.checkWrittenRuleDB(c, []*requestrules.Rule{good, initialSessionDeny})
	s.checkNewNoticesSimple(c, nil, initialSessionDeny)

	// Next add conflicting allow rule with lifespan "session"
	currSession = prompting.IDType(0xabcdef)
	initialSessionAllow, err := addRuleFromTemplate(c, rdb, template, &addRuleContents{
		Outcome: prompting.OutcomeAllow,
		// Make path pattern conflict but not be identical
		PathPattern: "/home/test/**/{secret,private,bar}/**",
		Lifespan:    prompting.LifespanSession,
	})
	c.Assert(err, IsNil)
	c.Assert(initialSessionAllow, NotNil)
	// Rule should be on disk and have notice, along with notice for expired rule
	s.checkWrittenRuleDB(c, []*requestrules.Rule{good, initialSessionAllow})
	expectedNoticeInfo := []*noticeInfo{
		{
			userID: initialSessionDeny.User,
			ruleID: initialSessionDeny.ID,
			data:   map[string]string{"removed": "expired"},
		},
		{
			userID: initialSessionAllow.User,
			ruleID: initialSessionAllow.ID,
		},
	}
	s.checkNewNotices(c, expectedNoticeInfo)
	// Change user session to 0 (as if session ended) so future timespan rule
	// will conflict and expire this rule.
	currSession = prompting.IDType(0)

	// Add initial LifespanTimespan rule which will conflict with
	// initialSessionAllow and then expire quickly
	prev, err := addRuleFromTemplate(c, rdb, template, &addRuleContents{
		Outcome:  prompting.OutcomeDeny,
		Lifespan: prompting.LifespanTimespan,
		Duration: "1ms",
	})
	c.Assert(err, IsNil)
	c.Assert(prev, NotNil)
	time.Sleep(time.Millisecond)

	// Both rules should be on disk and have notices
	s.checkWrittenRuleDB(c, []*requestrules.Rule{good, prev})
	expectedNoticeInfo = []*noticeInfo{
		{
			userID: initialSessionAllow.User,
			ruleID: initialSessionAllow.ID,
			data:   map[string]string{"removed": "expired"},
		},
		{
			userID: prev.User,
			ruleID: prev.ID,
		},
	}
	s.checkNewNotices(c, expectedNoticeInfo)

	// Add rules which all conflict but each expire before the next is added,
	// thus causing the prior one to be removed and not causing a conflict error.
	for _, ruleContents := range []*addRuleContents{
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
	expectedNoticeInfo = []*noticeInfo{
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
	var currSession prompting.IDType
	restore := requestrules.MockReadOrAssignUserSessionID(func(rdb *requestrules.RuleDB, user uint32) (prompting.IDType, error) {
		return currSession, nil
	})
	defer restore()

	rdb, err := requestrules.New(s.defaultNotifyRule)
	c.Assert(err, IsNil)

	user := s.defaultUser
	snap := "firefox"
	iface := "home"

	currSession = prompting.IDType(0x12345)
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
				Lifespan: prompting.LifespanSession,
			},
		},
	}
	rule1, err := rdb.AddRule(user, snap, iface, constraints1)
	c.Assert(err, IsNil)
	c.Assert(rule1, NotNil)
	s.checkWrittenRuleDB(c, []*requestrules.Rule{rule1})
	s.checkNewNoticesSimple(c, nil, rule1)
	// Now that the rule has been added, change the user session ID so the
	// execute permission is treated as expired
	currSession = prompting.IDType(0xf00)

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

type isPathPermAllowedResult struct {
	allowed bool
	err     error
}

func (s *requestrulesSuite) TestIsRequestAllowed(c *C) {
	rdb, err := requestrules.New(nil)
	c.Assert(err, IsNil)
	c.Assert(rdb, NotNil)

	user := uint32(1234)
	snap := "firefox"
	iface := "powerful"
	path := "/path/to/something"

	for _, testCase := range []struct {
		requestedPerms   []string
		permReturns      map[string]isPathPermAllowedResult
		allowedPerms     []string
		anyDenied        bool
		outstandingPerms []string
		errStr           string
	}{
		{
			requestedPerms: []string{"foo", "bar", "baz"},
			permReturns: map[string]isPathPermAllowedResult{
				"foo": {true, nil},
				"bar": {true, nil},
				"baz": {true, nil},
			},
			allowedPerms:     []string{"foo", "bar", "baz"},
			anyDenied:        false,
			outstandingPerms: []string{},
			errStr:           "",
		},
		{
			requestedPerms: []string{"foo", "bar", "baz"},
			permReturns: map[string]isPathPermAllowedResult{
				"foo": {true, prompting_errors.ErrNoMatchingRule},
				"bar": {false, prompting_errors.ErrNoMatchingRule},
				"baz": {true, prompting_errors.ErrNoMatchingRule},
			},
			allowedPerms:     []string{},
			anyDenied:        false,
			outstandingPerms: []string{"foo", "bar", "baz"},
			errStr:           "",
		},
		{
			requestedPerms: []string{"foo", "bar", "baz"},
			permReturns: map[string]isPathPermAllowedResult{
				"foo": {true, prompting_errors.ErrNoMatchingRule},
				"bar": {true, nil},
				"baz": {false, prompting_errors.ErrNoMatchingRule},
			},
			allowedPerms:     []string{"bar"},
			anyDenied:        false,
			outstandingPerms: []string{"foo", "baz"},
			errStr:           "",
		},
		{
			requestedPerms: []string{"foo", "bar", "baz"},
			permReturns: map[string]isPathPermAllowedResult{
				"foo": {false, nil},
				"bar": {true, nil},
				"baz": {false, prompting_errors.ErrNoMatchingRule},
			},
			allowedPerms:     []string{"bar"},
			anyDenied:        true,
			outstandingPerms: []string{"baz"},
			errStr:           "",
		},
		{
			requestedPerms: []string{"foo", "bar"},
			permReturns: map[string]isPathPermAllowedResult{
				"foo": {true, nil},
				"bar": {true, nil},
				"baz": {true, fmt.Errorf("baz")},
			},
			allowedPerms:     []string{"foo", "bar"},
			anyDenied:        false,
			outstandingPerms: []string{},
			errStr:           "",
		},
		{
			requestedPerms: []string{"foo", "bar", "baz"},
			permReturns: map[string]isPathPermAllowedResult{
				"foo": {true, fmt.Errorf("foo")},
				"bar": {true, nil},
				"baz": {true, nil},
			},
			allowedPerms:     []string{"bar", "baz"},
			anyDenied:        false,
			outstandingPerms: []string{},
			errStr:           "foo",
		},
		{
			requestedPerms: []string{"foo", "bar", "baz", "qux", "fizz", "buzz"},
			permReturns: map[string]isPathPermAllowedResult{
				"foo":  {true, fmt.Errorf("foo")},
				"bar":  {true, nil},
				"baz":  {true, prompting_errors.ErrNoMatchingRule},
				"qux":  {false, fmt.Errorf("qux")},
				"fizz": {false, nil},
				"buzz": {false, prompting_errors.ErrNoMatchingRule},
			},
			allowedPerms:     []string{"bar"},
			anyDenied:        true,
			outstandingPerms: []string{"baz", "buzz"},
			errStr:           "foo\nqux",
		},
	} {
		before := time.Now()

		restore := requestrules.MockIsPathPermAllowed(func(r *requestrules.RuleDB, u uint32, s string, i string, p string, perm string, at prompting.At) (bool, error) {
			c.Assert(r, Equals, rdb)
			c.Assert(u, Equals, user)
			c.Assert(s, Equals, snap)
			c.Assert(i, Equals, iface)
			c.Assert(p, Equals, path)
			c.Assert(at.Time.IsZero(), Equals, false)
			c.Assert(at.Time.After(before), Equals, true)
			c.Assert(at.Time.Before(time.Now()), Equals, true)
			result := testCase.permReturns[perm]
			return result.allowed, result.err
		})
		defer restore()

		allowedPerms, anyDenied, outstandingPerms, err := rdb.IsRequestAllowed(user, snap, iface, path, testCase.requestedPerms)
		c.Check(allowedPerms, DeepEquals, testCase.allowedPerms)
		c.Check(anyDenied, Equals, testCase.anyDenied)
		c.Check(outstandingPerms, DeepEquals, testCase.outstandingPerms)
		if testCase.errStr == "" {
			c.Check(err, IsNil)
		} else {
			c.Check(err, ErrorMatches, testCase.errStr)
		}
	}
}

func (s *requestrulesSuite) TestIsPathPermAllowedSimple(c *C) {
	currSession := prompting.IDType(0x12345)
	restore := requestrules.MockReadOrAssignUserSessionID(func(rdb *requestrules.RuleDB, user uint32) (prompting.IDType, error) {
		return currSession, nil
	})
	defer restore()

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
		{ // Matching allow session rule
			ruleContents: &addRuleContents{Lifespan: prompting.LifespanSession},
			allowed:      true,
			err:          nil,
		},
		{ // Matching deny session rule
			ruleContents: &addRuleContents{Outcome: prompting.OutcomeDeny, Lifespan: prompting.LifespanSession},
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

		at := prompting.At{
			Time:      time.Now(),
			SessionID: currSession,
		}
		allowed, err := rdb.IsPathPermAllowed(user, snap, iface, path, permission, at)
		c.Check(err, Equals, testCase.err)
		c.Check(allowed, Equals, testCase.allowed)
		// Check that no notices were recorded when checking
		s.checkNewNoticesSimple(c, nil)

		if testCase.ruleContents != nil {
			// Clean up the rules DB so the next rdb has a clean slate
			dbPath := filepath.Join(dirs.SnapInterfacesRequestsStateDir, "request-rules.json")
			c.Assert(os.Remove(dbPath), IsNil)
		}
	}
}

func (s *requestrulesSuite) TestIsPathPermAllowedPrecedence(c *C) {
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

		// The point in time doesn't matter for this test
		at := prompting.At{
			Time:      time.Now(),
			SessionID: prompting.IDType(0x12345),
		}

		allowed, err := rdb.IsPathPermAllowed(user, snap, iface, path, permission, at)
		c.Check(err, IsNil)
		c.Check(allowed, Equals, mostRecentOutcome, Commentf("most recent: %+v", ruleContents))
	}
}

func (s *requestrulesSuite) TestIsPathPermAllowedExpiration(c *C) {
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
	toAdd := []*addRuleContents{
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
	}
	for i, ruleContents := range toAdd {
		// Set duration to 1h for last rule, 2h for second to last rule, etc.
		ruleContents.Duration = fmt.Sprintf("%dh", len(toAdd)-i)
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

	// The point in time is set after all rules have been added but before any
	// have expired.
	at := prompting.At{
		Time:      time.Now(),
		SessionID: prompting.IDType(0x12345), // doesn't matter for this test
	}

	for i := len(addedRules) - 1; i >= 0; i-- {
		rule := addedRules[i]
		expectedOutcome, err := rule.Constraints.Permissions["read"].Outcome.AsBool()
		c.Check(err, IsNil)

		// Check that the outcome of the most specific unexpired rule has precedence
		allowed, err := rdb.IsPathPermAllowed(user, snap, iface, path, permission, at)
		c.Check(err, IsNil)
		c.Check(allowed, Equals, expectedOutcome, Commentf("last unexpired: %+v", rule))

		// Check that no new notices are recorded from lookup or expiration
		s.checkNewNoticesSimple(c, nil)

		// Advance the point in time to cause highest precedence rule to expire.
		at.Time = at.Time.Add(time.Hour)
	}
}

func (s *requestrulesSuite) TestIsPathPermAllowedSession(c *C) {
	var currSession prompting.IDType
	restore := requestrules.MockReadOrAssignUserSessionID(func(rdb *requestrules.RuleDB, user uint32) (prompting.IDType, error) {
		return currSession, nil
	})
	defer restore()

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
		Lifespan:    prompting.LifespanSession,
		Duration:    "1h",
	}

	rdb, err := requestrules.New(s.defaultNotifyRule)
	c.Assert(err, IsNil)
	c.Assert(rdb, NotNil)

	// Add all rules initially with session ID 0x12345
	currSession = prompting.IDType(0x12345)
	// Define another session ID which rules will change to later to emulate expiration
	otherSession := prompting.IDType(0xabcd)
	var addedRules []*requestrules.Rule
	at := prompting.At{
		Time:      time.Now(), // doesn't matter for this test
		SessionID: currSession,
	}

	// Add these rules, where each has higher precedence than prior rules.
	// Then, from last to first, mark the rule as expired by setting the
	// session ID to something else, and then test that they always match the
	// most specific rule contents.
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
	}

	// Rules were added from lowest to highest precedence, so iterate backwards
	// to check that the last rule has precedence, then expire it, then check
	// that the second to last rule has precedence, etc.
	for i := len(addedRules) - 1; i >= 0; i-- {
		rule := addedRules[i]
		expectedOutcome, err := rule.Constraints.Permissions["read"].Outcome.AsBool()
		c.Check(err, IsNil)

		// Check that the outcome of the most specific unexpired rule has precedence
		allowed, err := rdb.IsPathPermAllowed(user, snap, iface, path, permission, at)
		c.Check(err, IsNil)
		c.Check(allowed, Equals, expectedOutcome, Commentf("last unexpired: %+v", rule))

		// Check that no new notices are recorded from lookup or expiration
		s.checkNewNoticesSimple(c, nil)

		// Expire the highest precedence rule by changing its session to be
		// different from the session at the current point in time
		rule.Constraints.Permissions["read"].SessionID = otherSession
	}

	// Now that currSession is different from the session of any of the rules,
	// there should be no matching rules.
	allowed, err := rdb.IsPathPermAllowed(user, snap, iface, path, permission, at)
	c.Check(err, Equals, prompting_errors.ErrNoMatchingRule)
	c.Check(allowed, Equals, false)

	// Same if the user session ends (at.SessionID = 0)
	at.SessionID = 0
	allowed, err = rdb.IsPathPermAllowed(user, snap, iface, path, permission, at)
	c.Check(err, Equals, prompting_errors.ErrNoMatchingRule)
	c.Check(allowed, Equals, false)

	// But if the session matches that of the rules, they'll again match
	at.SessionID = otherSession
	_, err = rdb.IsPathPermAllowed(user, snap, iface, path, permission, at)
	c.Check(err, IsNil)
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
		{PathPattern: "/home/test/other", Permissions: []string{"write"}},
		{Snap: "amberol"},
		{Snap: "amberol", PathPattern: "/home/test/other", Permissions: []string{"write"}}, // change interface later
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
	currSession := prompting.IDType(0x12345)
	restore := requestrules.MockReadOrAssignUserSessionID(func(rdb *requestrules.RuleDB, user uint32) (prompting.IDType, error) {
		return currSession, nil
	})
	defer restore()

	rdb, err := requestrules.New(s.defaultNotifyRule)
	c.Assert(err, IsNil)
	c.Assert(rdb, NotNil)

	c.Check(rdb.Rules(s.defaultUser), HasLen, 0)

	rules := s.prepRuleDBForRulesForSnapInterface(c, rdb)

	// Set some rules to be expired
	// This is brittle, relies on details of the rules added by prepRuleDBForRulesForSnapInterface
	rules[0].Constraints.Permissions["read"].Lifespan = prompting.LifespanTimespan
	rules[0].Constraints.Permissions["read"].Expiration = time.Now()
	rules[1].Constraints.Permissions["write"].Lifespan = prompting.LifespanSession
	rules[1].Constraints.Permissions["write"].SessionID = currSession // not expired
	rules[2].Constraints.Permissions["read"].Lifespan = prompting.LifespanSession
	rules[2].Constraints.Permissions["read"].SessionID = currSession + 1 // expired
	rules[4].Constraints.Permissions["read"].Lifespan = prompting.LifespanTimespan
	rules[4].Constraints.Permissions["read"].Expiration = time.Now()

	// Expired rules are excluded from the Rules*() functions
	c.Check(rdb.Rules(s.defaultUser), DeepEquals, []*requestrules.Rule{rules[1], rules[3]})

	// If we set the current session to 0, all LifespanSession rules should be
	// treated as expired.
	currSession = 0
	c.Check(rdb.Rules(s.defaultUser), DeepEquals, []*requestrules.Rule{rules[3]})

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
	c.Assert(os.Chmod(dirs.SnapInterfacesRequestsStateDir, 0o500), IsNil)
	result, err = rdb.RemoveRule(rule.User, rule.ID)
	c.Check(err, NotNil)
	c.Check(result, IsNil)
	c.Assert(os.Chmod(dirs.SnapInterfacesRequestsStateDir, 0o755), IsNil)

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
	dbPath := filepath.Join(dirs.SnapInterfacesRequestsStateDir, "request-rules.json")

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
		c.Assert(os.Chmod(dirs.SnapInterfacesRequestsStateDir, 0o500), IsNil)
		defer os.Chmod(dirs.SnapInterfacesRequestsStateDir, 0o755)

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
	currSession := prompting.IDType(0x12345)
	restore := requestrules.MockReadOrAssignUserSessionID(func(rdb *requestrules.RuleDB, user uint32) (prompting.IDType, error) {
		return currSession, nil
	})
	defer restore()

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
		{PathPattern: "/home/test/foo"},
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
	constraintsPatch := &prompting.RuleConstraintsPatch{
		PathPattern: rule.Constraints.PathPattern,
		Permissions: prompting.PermissionMap{
			"read": &prompting.PermissionEntry{
				Outcome:  rule.Constraints.Permissions["read"].Outcome,
				Lifespan: rule.Constraints.Permissions["read"].Lifespan,
			},
		},
	}
	patched, err = rdb.PatchRule(rule.User, rule.ID, constraintsPatch)
	c.Assert(err, IsNil)
	s.checkWrittenRuleDB(c, append(rules[:len(rules)-1], patched))
	s.checkNewNoticesSimple(c, nil, rule)
	// Check that timestamp has changed
	c.Check(patched.Timestamp.Equal(rule.Timestamp), Equals, false)
	// After making timestamp the same again, check that the rules are identical
	patched.Timestamp = origRule.Timestamp
	c.Check(patched, DeepEquals, &origRule)

	rule = patched

	constraintsPatch = &prompting.RuleConstraintsPatch{
		Permissions: prompting.PermissionMap{
			"execute": &prompting.PermissionEntry{
				Outcome:  rule.Constraints.Permissions["read"].Outcome,
				Lifespan: rule.Constraints.Permissions["read"].Lifespan,
			},
		},
	}
	patched, err = rdb.PatchRule(rule.User, rule.ID, constraintsPatch)
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

	constraintsPatch = &prompting.RuleConstraintsPatch{
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
	patched, err = rdb.PatchRule(rule.User, rule.ID, constraintsPatch)
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

	constraintsPatch = &prompting.RuleConstraintsPatch{
		Permissions: prompting.PermissionMap{
			"read": &prompting.PermissionEntry{
				Outcome:  prompting.OutcomeDeny,
				Lifespan: prompting.LifespanTimespan,
				Duration: "10s",
			},
			"execute": &prompting.PermissionEntry{
				Outcome:  prompting.OutcomeDeny,
				Lifespan: prompting.LifespanSession,
			},
		},
	}
	patched, err = rdb.PatchRule(rule.User, rule.ID, constraintsPatch)
	c.Assert(err, IsNil)
	s.checkWrittenRuleDB(c, append(rules[:len(rules)-1], patched))
	s.checkNewNoticesSimple(c, nil, rule)
	// Check that timestamp has changed
	c.Check(patched.Timestamp.Equal(rule.Timestamp), Equals, false)
	// After making timestamp the same again, check that the rules are identical
	patched.Timestamp = rule.Timestamp
	rule.Constraints.Permissions["read"].Lifespan = prompting.LifespanTimespan
	rule.Constraints.Permissions["read"].Expiration = patched.Constraints.Permissions["read"].Expiration
	rule.Constraints.Permissions["execute"].Lifespan = prompting.LifespanSession
	rule.Constraints.Permissions["execute"].SessionID = currSession
	c.Check(patched, DeepEquals, rule)

	rule = patched

	constraintsPatch = &prompting.RuleConstraintsPatch{
		Permissions: prompting.PermissionMap{
			"read": &prompting.PermissionEntry{
				Outcome:  origRule.Constraints.Permissions["read"].Outcome,
				Lifespan: origRule.Constraints.Permissions["read"].Lifespan,
			},
			"execute": nil,
		},
	}
	patched, err = rdb.PatchRule(rule.User, rule.ID, constraintsPatch)
	c.Assert(err, IsNil)
	s.checkWrittenRuleDB(c, append(rules[:len(rules)-1], patched))
	s.checkNewNoticesSimple(c, nil, rule)
	// Check that timestamp has changed
	c.Check(patched.Timestamp.Equal(rule.Timestamp), Equals, false)
	// After making timestamp the same again, check that the rules are identical
	patched.Timestamp = origRule.Timestamp
	c.Check(patched, DeepEquals, &origRule)

	rule = patched

	// Patch rule so it has the same path pattern as an existing rule, and check
	// that this results in the rules being merged
	constraintsPatch = &prompting.RuleConstraintsPatch{
		PathPattern: mustParsePathPattern(c, "/home/test/foo"),
	}
	patched, err = rdb.PatchRule(rule.User, rule.ID, constraintsPatch)
	c.Assert(err, IsNil)
	// Check that rule inherited the ID of the rule with which it was merged
	c.Check(patched.ID, Equals, rules[0].ID)
	// Check that the ID of the patched rule changed, rather than that of the
	// rule into which it was merged.
	c.Check(patched.ID, Not(Equals), rule.ID)
	s.checkWrittenRuleDB(c, []*requestrules.Rule{patched})
	s.checkNewNoticesSimple(c, nil, patched)
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
		{PathPattern: "/home/test/foo"},
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
	badPatch := &prompting.RuleConstraintsPatch{
		Permissions: prompting.PermissionMap{
			"read": &prompting.PermissionEntry{
				Outcome:  prompting.OutcomeAllow,
				Lifespan: prompting.LifespanSingle,
			},
		},
	}
	result, err = rdb.PatchRule(rule.User, rule.ID, badPatch)
	c.Check(err, ErrorMatches, prompting_errors.NewRuleLifespanSingleError(prompting.SupportedRuleLifespans).Error())
	c.Check(result, IsNil)
	s.checkWrittenRuleDB(c, rules)
	s.checkNewNoticesSimple(c, nil)

	// Conflicting with other rule
	conflictingPatch := &prompting.RuleConstraintsPatch{
		PathPattern: mustParsePathPattern(c, "/home/test/{foo,{Downloads,Documents}/**/*.{ical,mail,txt,gpg}}"),
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
	result, err = rdb.PatchRule(rule.User, rule.ID, conflictingPatch)
	c.Check(err, ErrorMatches, fmt.Sprintf("cannot patch rule: %v", prompting_errors.ErrRuleConflict))
	c.Check(result, IsNil)
	s.checkWrittenRuleDB(c, rules)
	s.checkNewNoticesSimple(c, nil)

	// Save fails
	func() {
		c.Assert(os.Chmod(dirs.SnapInterfacesRequestsStateDir, 0o500), IsNil)
		defer os.Chmod(dirs.SnapInterfacesRequestsStateDir, 0o755)
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
	currSession := prompting.IDType(0x12345)
	restore := requestrules.MockReadOrAssignUserSessionID(func(rdb *requestrules.RuleDB, user uint32) (prompting.IDType, error) {
		return currSession, nil
	})
	defer restore()

	rdb, err := requestrules.New(s.defaultNotifyRule)
	c.Assert(err, IsNil)

	template := &addRuleContents{
		User:        s.defaultUser,
		Snap:        "thunderbird",
		Interface:   "home",
		Permissions: []string{"write"},
		Outcome:     prompting.OutcomeAllow,
		Lifespan:    prompting.LifespanForever,
		Duration:    "",
	}

	var rules []*requestrules.Rule

	for _, ruleContents := range []*addRuleContents{
		{PathPattern: "/foo", Lifespan: prompting.LifespanTimespan, Duration: "1ms"},
		{PathPattern: "/bar", Lifespan: prompting.LifespanSession, Permissions: []string{"read"}},
		{PathPattern: "/baz", Permissions: []string{"execute"}},
	} {
		rule, err := addRuleFromTemplate(c, rdb, template, ruleContents)
		c.Check(err, IsNil)
		c.Check(rule, NotNil)
		rules = append(rules, rule)
		s.checkWrittenRuleDB(c, rules)
		s.checkNewNoticesSimple(c, nil, rule)
	}

	// Expire first two rules by advancing time and changing current session ID
	time.Sleep(time.Millisecond)
	currSession += 1

	// Patching doesn't conflict with already-expired rules
	rule := rules[2]
	constraintsPatch := &prompting.RuleConstraintsPatch{
		PathPattern: mustParsePathPattern(c, "/{foo,bar}"),
		Permissions: prompting.PermissionMap{
			"read": &prompting.PermissionEntry{
				Outcome:  prompting.OutcomeDeny,
				Lifespan: prompting.LifespanSession,
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
	patched, err := rdb.PatchRule(rule.User, rule.ID, constraintsPatch)
	c.Assert(err, IsNil)
	s.checkWrittenRuleDB(c, []*requestrules.Rule{patched})
	expectedNotices := []*noticeInfo{
		{
			userID: rules[0].User,
			ruleID: rules[0].ID,
			data:   map[string]string{"removed": "expired"},
		},
		{
			userID: rules[1].User,
			ruleID: rules[1].ID,
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
	// After making timestamp and constraints the same again, check that the rules are identical
	patched.Timestamp = rule.Timestamp
	rule.Constraints = &prompting.RuleConstraints{
		PathPattern: mustParsePathPattern(c, "/{foo,bar}"),
		Permissions: prompting.RulePermissionMap{
			"read": &prompting.RulePermissionEntry{
				Outcome:   prompting.OutcomeDeny,
				Lifespan:  prompting.LifespanSession,
				SessionID: currSession,
			},
			"write": &prompting.RulePermissionEntry{
				Outcome:  prompting.OutcomeDeny,
				Lifespan: prompting.LifespanForever,
			},
			"execute": &prompting.RulePermissionEntry{
				Outcome:  prompting.OutcomeDeny,
				Lifespan: prompting.LifespanForever,
			},
		},
	}
	c.Check(patched, DeepEquals, rule)

	// If the user session ends, any entries with LifespanSession expire
	currSession = 0
	constraintsPatch = &prompting.RuleConstraintsPatch{
		Permissions: prompting.PermissionMap{
			"execute": &prompting.PermissionEntry{
				Outcome:  prompting.OutcomeAllow,
				Lifespan: prompting.LifespanForever,
			},
		},
	}
	patched, err = rdb.PatchRule(rule.User, rule.ID, constraintsPatch)
	c.Assert(err, IsNil)
	s.checkWrittenRuleDB(c, []*requestrules.Rule{patched})
	s.checkNewNoticesSimple(c, nil, patched)
	// Check that timestamp has changed
	c.Check(patched.Timestamp.Equal(rule.Timestamp), Equals, false)
	// Update the timestamp and check that the patched rule no longer has the
	// permission entry for "read", which was LifespanSession for expired session
	patched.Timestamp = rule.Timestamp
	rule.Constraints.Permissions["execute"].Outcome = prompting.OutcomeAllow
	delete(rule.Constraints.Permissions, "read")
	c.Check(patched, DeepEquals, rule)
}

func (s *requestrulesSuite) TestUserSessionIDCache(c *C) {
	rdb, err := requestrules.New(s.defaultNotifyRule)
	c.Assert(err, IsNil)

	checkedDiskForUser := make(map[uint32]int)
	restore := requestrules.MockReadOrAssignUserSessionID(func(ruleDB *requestrules.RuleDB, user uint32) (prompting.IDType, error) {
		c.Assert(ruleDB, Equals, rdb)
		checkedDiskForUser[user] += 1

		switch user {
		case 1000:
			return prompting.IDType(0x12345), nil
		case 1234:
			return prompting.IDType(0xabcd), nil
		case 11235:
			// Pretend there's no user session for this user
			return prompting.IDType(0), fmt.Errorf("foo: %w", requestrules.ErrNoUserSession)
		case 5000:
			// Pretend there's some other error
			return prompting.IDType(0), errors.New("foo")
		}
		c.Fatalf("unexpected user: %d", user)
		return 0, fmt.Errorf("unexpected user: %d", user)
	})
	defer restore()

	cache := make(requestrules.UserSessionIDCache)

	// Get the same user multiple times
	for i := 0; i < 5; i++ {
		result, err := cache.GetUserSessionID(rdb, 1000)
		c.Assert(err, IsNil)
		c.Assert(result, Equals, prompting.IDType(0x12345))
	}
	// Check that readOrAssignUserSessionID was only called once
	count, ok := checkedDiskForUser[1000]
	c.Assert(count, Equals, 1)
	c.Assert(ok, Equals, true)

	// Get some other user several times
	for i := 0; i < 5; i++ {
		result, err := cache.GetUserSessionID(rdb, 1234)
		c.Assert(err, IsNil)
		c.Assert(result, Equals, prompting.IDType(0xabcd))
	}
	// Check that readOrAssignUserSessionID was only called once
	count, ok = checkedDiskForUser[1234]
	c.Assert(count, Equals, 1)
	c.Assert(ok, Equals, true)

	// Get a user which has no session
	for i := 0; i < 5; i++ {
		result, err := cache.GetUserSessionID(rdb, 11235)
		// Error should be nil even though there was no session
		c.Assert(err, IsNil)
		c.Assert(result, Equals, prompting.IDType(0))
	}
	// Check that readOrAssignUserSessionID was only called once
	count, ok = checkedDiskForUser[11235]
	c.Assert(count, Equals, 1)
	c.Assert(ok, Equals, true)

	// Get a user which causes error
	for i := 0; i < 5; i++ {
		result, err := cache.GetUserSessionID(rdb, 5000)
		c.Assert(err, ErrorMatches, "foo")
		c.Assert(result, Equals, prompting.IDType(0))
	}
	// With other error, readOrAssignUserSessionID should be called every time
	count, ok = checkedDiskForUser[5000]
	c.Assert(count, Equals, 5)
	c.Assert(ok, Equals, true)

	// Get a previous user again, it should again reuse the cache
	result, err := cache.GetUserSessionID(rdb, 1000)
	c.Assert(err, IsNil)
	c.Assert(result, Equals, prompting.IDType(0x12345))
	// Check that readOrAssignUserSessionID was only called once
	count, ok = checkedDiskForUser[1000]
	c.Assert(count, Equals, 1)
	c.Assert(ok, Equals, true)
}
