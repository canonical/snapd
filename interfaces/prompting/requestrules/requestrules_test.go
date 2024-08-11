package requestrules_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/interfaces/prompting"
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

func (s *requestrulesSuite) TestRuleValidate(c *C) {
	iface := "home"
	pathPattern := mustParsePathPattern(c, "/home/test/**")

	validConstraints := &prompting.Constraints{
		PathPattern: pathPattern,
		Permissions: []string{"read"},
	}
	invalidConstraints := &prompting.Constraints{
		PathPattern: pathPattern,
		Permissions: []string{"foo"},
	}

	validOutcome := prompting.OutcomeAllow
	invalidOutcome := prompting.OutcomeUnset

	validLifespan := prompting.LifespanTimespan
	invalidLifespan := prompting.LifespanSingle

	currTime := time.Now()

	validExpiration := currTime.Add(time.Millisecond)
	invalidExpiration := currTime.Add(-time.Millisecond)

	rule := requestrules.Rule{
		// ID is not validated
		// Timestamp is not validated
		// User is not validated
		// Snap is not validated
		Interface:   iface,
		Constraints: validConstraints,
		Outcome:     validOutcome,
		Lifespan:    validLifespan,
		Expiration:  validExpiration,
	}
	c.Check(rule.Validate(currTime), IsNil)

	rule.Expiration = invalidExpiration
	c.Check(rule.Validate(currTime), ErrorMatches, fmt.Sprintf("%v:.*", prompting.ErrExpirationInThePast))

	rule.Lifespan = invalidLifespan
	c.Check(rule.Validate(currTime), Equals, requestrules.ErrLifespanSingle)

	rule.Outcome = invalidOutcome
	c.Check(rule.Validate(currTime), ErrorMatches, "internal error: invalid outcome:.*")

	rule.Constraints = invalidConstraints
	c.Check(rule.Validate(currTime), ErrorMatches, "invalid constraints:.*")
}

func mustParsePathPattern(c *C, patternStr string) *patterns.PathPattern {
	pattern, err := patterns.ParsePathPattern(patternStr)
	c.Assert(err, IsNil)
	return pattern
}

func (s *requestrulesSuite) TestRuleExpired(c *C) {
	currTime := time.Now()
	rule := requestrules.Rule{
		// Other fields are not relevant
		Lifespan:   prompting.LifespanTimespan,
		Expiration: currTime,
	}
	c.Check(rule.Expired(currTime), Equals, false)
	c.Check(rule.Expired(currTime.Add(time.Millisecond)), Equals, true)
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
	good1 := s.ruleTemplate(c, prompting.IDType(1))
	bad := s.ruleTemplate(c, prompting.IDType(2))
	bad.Interface = "foo" // will cause validate() to fail with invalid constraints
	good2 := s.ruleTemplate(c, prompting.IDType(3))
	// Doesn't matter that rules have conflicting patterns/permissions,
	// validate() should catch invalid rule and exit before attempting to add.

	rules := []*requestrules.Rule{good1, bad, good2}
	s.writeRules(c, dbPath, rules)

	checkWritten := true
	s.testLoadError(c, "internal error: invalid constraints: unsupported interface: foo.*", rules, checkWritten)
}

// ruleTemplate returns a rule with valid contents, intended to be customized.
func (s *requestrulesSuite) ruleTemplate(c *C, id prompting.IDType) *requestrules.Rule {
	constraints := prompting.Constraints{
		PathPattern: mustParsePathPattern(c, "/home/test/foo"),
		Permissions: []string{"read"},
	}
	rule := requestrules.Rule{
		ID:          id,
		Timestamp:   time.Now(),
		User:        s.defaultUser,
		Snap:        "firefox",
		Interface:   "home",
		Constraints: &constraints,
		Outcome:     prompting.OutcomeAllow,
		Lifespan:    prompting.LifespanForever,
		// Skip Expiration
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
	good := s.ruleTemplate(c, prompting.IDType(1))
	// Expired rules should still get a {"removed": "dropped"} notice, even if they don't conflict
	expired := s.ruleTemplate(c, prompting.IDType(2))
	setPathPatternAndExpiration(c, expired, "/home/test/other", currTime.Add(-10*time.Second))
	// Add rule which conflicts with IDs but doesn't otherwise conflict
	conflicting := s.ruleTemplate(c, prompting.IDType(1))
	conflicting.Constraints.PathPattern = mustParsePathPattern(c, "/home/test/another")

	rules := []*requestrules.Rule{good, expired, conflicting}
	s.writeRules(c, dbPath, rules)

	checkWritten := true
	s.testLoadError(c, fmt.Sprintf("cannot add rule: %v.*", requestrules.ErrRuleIDConflict), rules, checkWritten)
}

func setPathPatternAndExpiration(c *C, rule *requestrules.Rule, pathPattern string, expiration time.Time) {
	rule.Constraints.PathPattern = mustParsePathPattern(c, pathPattern)
	rule.Lifespan = prompting.LifespanTimespan
	rule.Expiration = expiration
}

func (s *requestrulesSuite) TestLoadErrorConflictingPattern(c *C) {
	dbPath := s.prepDBPath(c)
	currTime := time.Now()
	good := s.ruleTemplate(c, prompting.IDType(1))
	// Expired rules should still get a {"removed": "dropped"} notice, even if they don't conflict
	expired := s.ruleTemplate(c, prompting.IDType(2))
	setPathPatternAndExpiration(c, expired, "/home/test/other", currTime.Add(-10*time.Second))
	// Add rule with conflicting pattern and permissions but not conflicting ID.
	conflicting := s.ruleTemplate(c, prompting.IDType(3))
	// Even with not all permissions being in conflict, still error
	conflicting.Constraints.Permissions = []string{"read", "write"}

	rules := []*requestrules.Rule{good, expired, conflicting}
	s.writeRules(c, dbPath, rules)

	checkWritten := true
	s.testLoadError(c, fmt.Sprintf("cannot add rule: %v.*", requestrules.ErrPathPatternConflict), rules, checkWritten)
}

func (s *requestrulesSuite) TestLoadExpiredRules(c *C) {
	dbPath := s.prepDBPath(c)
	currTime := time.Now()

	good1 := s.ruleTemplate(c, prompting.IDType(1))

	// At the moment, expired rules with conflicts are discarded without error,
	// but we don't want to test this as part of our contract

	expired1 := s.ruleTemplate(c, prompting.IDType(2))
	setPathPatternAndExpiration(c, expired1, "/home/test/other", currTime.Add(-10*time.Second))

	// Rules with same pattern but non-conflicting permissions do not conflict
	good2 := s.ruleTemplate(c, prompting.IDType(3))
	good2.Constraints.Permissions = []string{"write"}

	expired2 := s.ruleTemplate(c, prompting.IDType(4))
	setPathPatternAndExpiration(c, expired2, "/home/test/another", currTime.Add(-time.Nanosecond))

	// Rules with different pattern and conflicting permissions do not conflict
	good3 := s.ruleTemplate(c, prompting.IDType(5))
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

	good1 := s.ruleTemplate(c, prompting.IDType(1))

	// Rules with different users don't conflict
	good2 := s.ruleTemplate(c, prompting.IDType(2))
	good2.User = s.defaultUser + 1

	// Rules with different snaps don't conflict
	good3 := s.ruleTemplate(c, prompting.IDType(3))
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
	// which is ErrInternalInconsistency
	errs := []error{nil, errFoo, nil}
	err := requestrules.JoinInternalErrors(errs)
	c.Check(errors.Is(err, requestrules.ErrInternalInconsistency), Equals, true)
	// XXX: check the following when we're on golang v1.20+
	// c.Check(errors.Is(err, errFoo), Equals, true)
	c.Check(errors.Is(err, errBar), Equals, false)
	c.Check(fmt.Sprintf("%v", err), Equals, fmt.Sprintf("%v\n%v", requestrules.ErrInternalInconsistency, errFoo))

	errs = append(errs, errBar, errBaz)
	err = requestrules.JoinInternalErrors(errs)
	c.Check(errors.Is(err, requestrules.ErrInternalInconsistency), Equals, true)
	// XXX: check the following when we're on golang v1.20+
	// c.Check(errors.Is(err, errFoo), Equals, true)
	// c.Check(errors.Is(err, errBar), Equals, true)
	// c.Check(errors.Is(err, errBaz), Equals, true)
	c.Check(fmt.Sprintf("%v", err), Equals, fmt.Sprintf("%v\n%v\n%v\n%v", requestrules.ErrInternalInconsistency, errFoo, errBar, errBaz))
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
	constraints := &prompting.Constraints{
		PathPattern: mustParsePathPattern(c, partial.PathPattern),
		Permissions: partial.Permissions,
	}
	return rdb.AddRule(partial.User, partial.Snap, partial.Interface, constraints, partial.Outcome, partial.Lifespan, partial.Duration)
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
			"cannot have specified duration.*",
		},
		{ // Empty duration with lifespan Timespan
			&addRuleContents{Lifespan: prompting.LifespanTimespan},
			"cannot have unspecified duration.*",
		},
		{ // Invalid duration
			&addRuleContents{Lifespan: prompting.LifespanTimespan, Duration: "invalid"},
			"cannot parse duration:.*",
		},
		{ // Negative duration
			&addRuleContents{Lifespan: prompting.LifespanTimespan, Duration: "-10s"},
			"cannot have zero or negative duration:.*",
		},
		{ // Invalid lifespan
			&addRuleContents{Lifespan: prompting.LifespanType("invalid")},
			"internal error: invalid lifespan:.*",
		},
		{ // Invalid outcome
			&addRuleContents{Outcome: prompting.OutcomeType("invalid")},
			"internal error: invalid outcome:.*",
		},
		{ // Invalid lifespan (for rules)
			&addRuleContents{Lifespan: prompting.LifespanSingle},
			fmt.Sprintf("%v", requestrules.ErrLifespanSingle),
		},
		{ // Conflicting rule
			&addRuleContents{
				PathPattern: "/home/test/Pictures/**/*.{svg,jpg}",
				Permissions: []string{"read", "write"},
			},
			fmt.Sprintf("cannot add rule: %v: conflicts:.* permission: 'write'", requestrules.ErrPathPatternConflict),
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

	// Check that the conflicting rule error can be unwrapped as ErrPathPatternConflict
	c.Check(errors.Is(finalErr, requestrules.ErrPathPatternConflict), Equals, true)

	// Failure to save rule DB should roll-back adding the rule and leave the
	// DB unchanged. Set DB parent directory as read-only.
	c.Assert(os.Chmod(prompting.StateDir(), 0o500), IsNil)
	defer os.Chmod(prompting.StateDir(), 0o700)
	result, err := addRuleFromTemplate(c, rdb, template, &addRuleContents{Permissions: []string{"execute"}})
	c.Check(err, NotNil)
	c.Check(result, IsNil)
	// Failure should result in no changes to written rules and no notices
	s.checkWrittenRuleDB(c, []*requestrules.Rule{good})
	s.checkNewNoticesSimple(c, nil)
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
		{}, // use template
		{PathPattern: "/home/test/{**/secret,**/private}/**"},
		{PathPattern: "/home/test/**/{sec,priv}{ret,ate}/**", Permissions: []string{"read", "write"}},
		{Permissions: []string{"write", "execute"}},
	} {
		ruleContents.Lifespan = prompting.LifespanTimespan
		ruleContents.Duration = "1ms"
		newRule, err := addRuleFromTemplate(c, rdb, template, ruleContents)
		c.Check(err, IsNil)
		c.Check(newRule, NotNil)
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
			err:          requestrules.ErrNoMatchingRule,
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
			err:          requestrules.ErrNoMatchingRule,
		},
		{ // Rule with wrong snap
			ruleContents: &addRuleContents{Snap: "thunderbird"},
			allowed:      false,
			err:          requestrules.ErrNoMatchingRule,
		},
		{ // Rule with wrong pattern
			ruleContents: &addRuleContents{PathPattern: "/home/test/path/to/other.txt"},
			allowed:      false,
			err:          requestrules.ErrNoMatchingRule,
		},
		{ // Rule with wrong permissions
			ruleContents: &addRuleContents{Permissions: []string{"write"}},
			allowed:      false,
			err:          requestrules.ErrNoMatchingRule,
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
		expectedOutcome, err := rule.Outcome.AsBool()
		c.Check(err, IsNil)

		// Check that the outcome of the most specific unexpired rule has precedence
		allowed, err := rdb.IsPathAllowed(user, snap, iface, path, permission)
		c.Check(err, IsNil)
		c.Check(allowed, Equals, expectedOutcome, Commentf("last unexpired: %+v", rule))

		// Check that no new notices are recorded from lookup or expiration
		s.checkNewNoticesSimple(c, nil)

		// Expire the highest precedence rule
		rule.Expiration = time.Now()
	}
}

func (s *requestrulesSuite) TestRuleWithID(c *C) {
	rdb, _ := requestrules.New(s.defaultNotifyRule)

	snap := "lxd"
	iface := "home"
	constraints := &prompting.Constraints{
		PathPattern: mustParsePathPattern(c, "/home/test/Documents/**"),
		Permissions: []string{"read", "write", "execute"},
	}
	outcome := prompting.OutcomeAllow
	lifespan := prompting.LifespanForever
	duration := ""

	rule, err := rdb.AddRule(s.defaultUser, snap, iface, constraints, outcome, lifespan, duration)
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
	c.Check(err, Equals, requestrules.ErrRuleIDNotFound)
	c.Check(accessedRule, IsNil)

	// Should not find rule for incorrect user and correct ID
	accessedRule, err = rdb.RuleWithID(s.defaultUser+1, rule.ID)
	c.Check(err, Equals, requestrules.ErrUserNotAllowed)
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
		c.Check(err, IsNil)
		c.Check(rule, NotNil)
		addedRules = append(addedRules, rule)
		s.checkWrittenRuleDB(c, addedRules)
		s.checkNewNoticesSimple(c, nil, rule)
	}

	// Change final rule interface
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
	rules[0].Lifespan = prompting.LifespanTimespan
	rules[0].Expiration = time.Now()
	rules[2].Lifespan = prompting.LifespanTimespan
	rules[2].Expiration = time.Now()
	rules[4].Lifespan = prompting.LifespanTimespan
	rules[4].Expiration = time.Now()

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
	c.Check(err, Equals, requestrules.ErrRuleIDNotFound)
	c.Check(missing, IsNil)
}

func (s *requestrulesSuite) TestRemoveRuleErrors(c *C) {
	rdb, err := requestrules.New(s.defaultNotifyRule)
	c.Assert(err, IsNil)

	addedRules := s.prepRuleDBForRulesForSnapInterface(c, rdb)
	rule := addedRules[0]

	// Attempt to remove rule with wrong user
	result, err := rdb.RemoveRule(rule.User+1, rule.ID)
	c.Check(err, Equals, requestrules.ErrUserNotAllowed)
	c.Check(result, IsNil)

	// Attempt to remove rule with wrong ID
	result, err = rdb.RemoveRule(rule.User, rule.ID+1234)
	c.Check(err, Equals, requestrules.ErrRuleIDNotFound)
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

	// Failure to save rule DB should roll-back removing the rules and leave the
	// DB unchanged. Set DB parent directory as read-only.
	c.Assert(os.Chmod(prompting.StateDir(), 0o500), IsNil)
	defer os.Chmod(prompting.StateDir(), 0o700)

	removed, err = rdb.RemoveRulesForSnap(s.defaultUser, "amberol")
	c.Check(err, NotNil)
	c.Check(removed, IsNil)
	c.Check(rdb.Rules(s.defaultUser), DeepEquals, rules[:4])

	removed, err = rdb.RemoveRulesForInterface(s.defaultUser, "audio-playback")
	c.Check(err, NotNil)
	c.Check(removed, IsNil)
	c.Check(rdb.Rules(s.defaultUser), DeepEquals, rules[:4])

	removed, err = rdb.RemoveRulesForSnapInterface(s.defaultUser, "amberol", "audio-playback")
	c.Check(err, NotNil)
	c.Check(removed, IsNil)
	c.Check(rdb.Rules(s.defaultUser), DeepEquals, rules[:4])

	// Check that original rules are still on disk, and no notices were recorded.
	// Be careful, since no write has occurred, the manually-edited rules[3]
	// still has "home" interface on disk.
	rules[3].Interface = "home"
	s.checkWrittenRuleDB(c, rules)
	s.checkNewNoticesSimple(c, map[string]string{"removed": "removed"}, removed...)
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
	patched, err := rdb.PatchRule(rule.User, rule.ID, nil, prompting.OutcomeUnset, prompting.LifespanUnset, "")
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
	patched, err = rdb.PatchRule(rule.User, rule.ID, rule.Constraints, rule.Outcome, rule.Lifespan, "")
	c.Assert(err, IsNil)
	s.checkWrittenRuleDB(c, append(rules[:len(rules)-1], patched))
	s.checkNewNoticesSimple(c, nil, rule)
	// Check that timestamp has changed
	c.Check(patched.Timestamp.Equal(rule.Timestamp), Equals, false)
	// After making timestamp the same again, check that the rules are identical
	patched.Timestamp = origRule.Timestamp
	c.Check(patched, DeepEquals, &origRule)

	rule = patched

	newConstraints := &prompting.Constraints{
		PathPattern: rule.Constraints.PathPattern,
		Permissions: []string{"read", "execute"},
	}
	patched, err = rdb.PatchRule(rule.User, rule.ID, newConstraints, prompting.OutcomeUnset, prompting.LifespanUnset, "")
	c.Assert(err, IsNil)
	s.checkWrittenRuleDB(c, append(rules[:len(rules)-1], patched))
	s.checkNewNoticesSimple(c, nil, rule)
	// Check that timestamp has changed
	c.Check(patched.Timestamp.Equal(rule.Timestamp), Equals, false)
	// After making timestamp the same again, check that the rules are identical
	patched.Timestamp = rule.Timestamp
	rule.Constraints = newConstraints
	c.Check(patched, DeepEquals, rule)

	rule = patched

	patched, err = rdb.PatchRule(rule.User, rule.ID, nil, prompting.OutcomeDeny, prompting.LifespanUnset, "")
	c.Assert(err, IsNil)
	s.checkWrittenRuleDB(c, append(rules[:len(rules)-1], patched))
	s.checkNewNoticesSimple(c, nil, rule)
	// Check that timestamp has changed
	c.Check(patched.Timestamp.Equal(rule.Timestamp), Equals, false)
	// After making timestamp the same again, check that the rules are identical
	patched.Timestamp = rule.Timestamp
	rule.Outcome = prompting.OutcomeDeny
	c.Check(patched, DeepEquals, rule)

	rule = patched

	patched, err = rdb.PatchRule(rule.User, rule.ID, nil, prompting.OutcomeUnset, prompting.LifespanTimespan, "10s")
	c.Assert(err, IsNil)
	s.checkWrittenRuleDB(c, append(rules[:len(rules)-1], patched))
	s.checkNewNoticesSimple(c, nil, rule)
	// Check that timestamp has changed
	c.Check(patched.Timestamp.Equal(rule.Timestamp), Equals, false)
	// After making timestamp the same again, check that the rules are identical
	patched.Timestamp = rule.Timestamp
	rule.Lifespan = prompting.LifespanTimespan
	rule.Expiration = patched.Expiration
	c.Check(patched, DeepEquals, rule)

	rule = patched

	patched, err = rdb.PatchRule(rule.User, rule.ID, origRule.Constraints, origRule.Outcome, origRule.Lifespan, "")
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
	result, err := rdb.PatchRule(rule.User+1, rule.ID, nil, prompting.OutcomeUnset, prompting.LifespanUnset, "")
	c.Check(err, Equals, requestrules.ErrUserNotAllowed)
	c.Check(result, IsNil)
	s.checkWrittenRuleDB(c, rules)
	s.checkNewNoticesSimple(c, nil)

	// Wrong ID
	result, err = rdb.PatchRule(rule.User, prompting.IDType(1234), nil, prompting.OutcomeUnset, prompting.LifespanUnset, "")
	c.Check(err, Equals, requestrules.ErrRuleIDNotFound)
	c.Check(result, IsNil)
	s.checkWrittenRuleDB(c, rules)
	s.checkNewNoticesSimple(c, nil)

	// Invalid lifespan
	result, err = rdb.PatchRule(rule.User, rule.ID, nil, prompting.OutcomeUnset, prompting.LifespanSingle, "")
	c.Check(err, Equals, requestrules.ErrLifespanSingle)
	c.Check(result, IsNil)
	s.checkWrittenRuleDB(c, rules)
	s.checkNewNoticesSimple(c, nil)

	// Conflicting rule
	conflictingConstraints := &prompting.Constraints{
		PathPattern: mustParsePathPattern(c, template.PathPattern),
		Permissions: []string{"read", "write", "execute"},
	}
	result, err = rdb.PatchRule(rule.User, rule.ID, conflictingConstraints, prompting.OutcomeUnset, prompting.LifespanUnset, "")
	c.Check(err, ErrorMatches, fmt.Sprintf("cannot patch rule: %v: conflicts:.* permission: 'write'", requestrules.ErrPathPatternConflict))
	c.Check(result, IsNil)
	s.checkWrittenRuleDB(c, rules)
	s.checkNewNoticesSimple(c, nil)

	// Save fails
	c.Assert(os.Chmod(prompting.StateDir(), 0o500), IsNil)
	defer os.Chmod(prompting.StateDir(), 0o700)
	result, err = rdb.PatchRule(rule.User, rule.ID, nil, prompting.OutcomeUnset, prompting.LifespanUnset, "")
	c.Check(err, NotNil)
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
	newConstraints := &prompting.Constraints{
		PathPattern: mustParsePathPattern(c, template.PathPattern),
		Permissions: []string{"read", "write", "execute"},
	}
	patched, err := rdb.PatchRule(rule.User, rule.ID, newConstraints, prompting.OutcomeUnset, prompting.LifespanUnset, "")
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
	s.checkNewNotices(c, expectedNotices)
	// Check that timestamp has changed
	c.Check(patched.Timestamp.Equal(rule.Timestamp), Equals, false)
	// After making timestamp the same again, check that the rules are identical
	patched.Timestamp = rule.Timestamp
	rule.Constraints = newConstraints
	c.Check(patched, DeepEquals, rule)
}
