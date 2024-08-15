// Copyright (c) 2024 Canonical Ltd
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License version 3 as
// published by the Free Software Foundation.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License
// along with this program.  If not, see <http://www.gnu.org/licenses/>.

package daemon_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/daemon"
	"github.com/snapcore/snapd/interfaces/prompting"
	"github.com/snapcore/snapd/interfaces/prompting/patterns"
	"github.com/snapcore/snapd/interfaces/prompting/requestprompts"
	"github.com/snapcore/snapd/interfaces/prompting/requestrules"
	"github.com/snapcore/snapd/overlord/ifacestate/apparmorprompting"
	"github.com/snapcore/snapd/testutil"
)

var _ = Suite(&promptingSuite{})

type fakeInterfacesRequestsManager struct {
	// Values to return
	prompts      []*requestprompts.Prompt
	rules        []*requestrules.Rule
	prompt       *requestprompts.Prompt
	rule         *requestrules.Rule
	satisfiedIDs []prompting.IDType
	err          error

	// Store most recent received values
	userID      uint32
	snap        string
	iface       string
	id          prompting.IDType // used for prompt ID or rule ID
	constraints *prompting.Constraints
	outcome     prompting.OutcomeType
	lifespan    prompting.LifespanType
	duration    string
}

func (m *fakeInterfacesRequestsManager) Prompts(userID uint32) ([]*requestprompts.Prompt, error) {
	m.userID = userID
	return m.prompts, m.err
}

func (m *fakeInterfacesRequestsManager) PromptWithID(userID uint32, promptID prompting.IDType) (*requestprompts.Prompt, error) {
	m.userID = userID
	m.id = promptID
	return m.prompt, m.err
}

func (m *fakeInterfacesRequestsManager) HandleReply(userID uint32, promptID prompting.IDType, constraints *prompting.Constraints, outcome prompting.OutcomeType, lifespan prompting.LifespanType, duration string) ([]prompting.IDType, error) {
	m.userID = userID
	m.id = promptID
	m.constraints = constraints
	m.outcome = outcome
	m.lifespan = lifespan
	m.duration = duration
	return m.satisfiedIDs, m.err
}

func (m *fakeInterfacesRequestsManager) Rules(userID uint32, snap string, iface string) ([]*requestrules.Rule, error) {
	m.userID = userID
	m.snap = snap
	m.iface = iface
	return m.rules, m.err
}

func (m *fakeInterfacesRequestsManager) AddRule(userID uint32, snap string, iface string, constraints *prompting.Constraints, outcome prompting.OutcomeType, lifespan prompting.LifespanType, duration string) (*requestrules.Rule, error) {
	m.userID = userID
	m.snap = snap
	m.iface = iface
	m.constraints = constraints
	m.outcome = outcome
	m.lifespan = lifespan
	m.duration = duration
	return m.rule, m.err
}

func (m *fakeInterfacesRequestsManager) RemoveRules(userID uint32, snap string, iface string) ([]*requestrules.Rule, error) {
	m.userID = userID
	m.snap = snap
	m.iface = iface
	return m.rules, m.err
}

func (m *fakeInterfacesRequestsManager) RuleWithID(userID uint32, ruleID prompting.IDType) (*requestrules.Rule, error) {
	m.userID = userID
	m.id = ruleID
	return m.rule, m.err
}

func (m *fakeInterfacesRequestsManager) PatchRule(userID uint32, ruleID prompting.IDType, constraints *prompting.Constraints, outcome prompting.OutcomeType, lifespan prompting.LifespanType, duration string) (*requestrules.Rule, error) {
	m.userID = userID
	m.id = ruleID
	m.constraints = constraints
	m.outcome = outcome
	m.lifespan = lifespan
	m.duration = duration
	return m.rule, m.err
}

func (m *fakeInterfacesRequestsManager) RemoveRule(userID uint32, ruleID prompting.IDType) (*requestrules.Rule, error) {
	m.userID = userID
	m.id = ruleID
	return m.rule, m.err
}

type promptingSuite struct {
	apiBaseSuite

	// Set this to true to disable prompting
	appArmorPromptingRunning bool
	manager                  *fakeInterfacesRequestsManager
}

// Implement daemon.interfaceManager using the suite itself
func (s *promptingSuite) AppArmorPromptingRunning() bool {
	return s.appArmorPromptingRunning
}

func (s *promptingSuite) InterfacesRequestsManager() apparmorprompting.Interface {
	return s.manager
}

func (s *promptingSuite) SetUpTest(c *C) {
	s.apiBaseSuite.SetUpTest(c)

	// Enable prompting and create fake manager
	s.appArmorPromptingRunning = true
	s.manager = &fakeInterfacesRequestsManager{}

	// Mock getInterfaceManager to return the suite itself
	daemon.MockInterfaceManager(s)

	s.expectReadAccess(daemon.InterfaceOpenAccess{Interfaces: []string{"snap-interfaces-requests-control"}})

}

func (s *promptingSuite) TestGetUserID(c *C) {
	s.daemon(c)

	for _, testCase := range []struct {
		path         string
		uid          string
		expectedUser uint32
		expectedCode int
		expectedErr  string
	}{
		{
			path:         "/v2/interfaces/requests/prompts",
			uid:          "invalid",
			expectedUser: 0,
			expectedCode: 403,
			expectedErr:  "cannot get remote user: ",
		},
		{
			path:         "/v2/interfaces/requests/prompts",
			uid:          "1000",
			expectedUser: 1000,
			expectedCode: 200,
			expectedErr:  "",
		},
		{
			path:         "/v2/interfaces/requests/prompts?user-id=1000",
			uid:          "1000",
			expectedUser: 0,
			expectedCode: 403,
			expectedErr:  `only admins may use the "user-id" parameter`,
		},
		{
			path:         "/v2/interfaces/requests/prompts?user-id=1000&user-id=1234",
			uid:          "0",
			expectedUser: 0,
			expectedCode: 400,
			expectedErr:  `invalid "user-id" parameter: must only include one "user-id"`,
		},
		{
			path:         "/v2/interfaces/requests/prompts?user-id=invalid",
			uid:          "0",
			expectedUser: 0,
			expectedCode: 400,
			expectedErr:  `invalid "user-id" parameter: `,
		},
		{
			path:         "/v2/interfaces/requests/prompts?user-id=-1",
			uid:          "0",
			expectedUser: 0,
			expectedCode: 400,
			expectedErr:  `invalid "user-id" parameter: user ID is not a valid uint32: `,
		},
		{
			path:         "/v2/interfaces/requests/prompts?user-id=1234",
			uid:          "0",
			expectedUser: 1234,
			expectedCode: 200,
			expectedErr:  "",
		},
	} {
		req, err := http.NewRequest("GET", testCase.path, nil)
		c.Assert(err, IsNil)
		req.RemoteAddr = fmt.Sprintf("pid=100;uid=%s;socket=;", testCase.uid)

		userID, rsp := daemon.GetUserID(req)
		if testCase.expectedErr == "" {
			c.Check(rsp, IsNil)
		} else {
			rspe, ok := rsp.(*daemon.APIError)
			c.Assert(ok, Equals, true)
			c.Check(rspe.Status, Equals, testCase.expectedCode)
			c.Check(rspe.Message, testutil.Contains, testCase.expectedErr)
		}
		c.Check(userID, Equals, testCase.expectedUser)
	}
}

func (s *promptingSuite) TestGetPromptsHappy(c *C) {
	s.daemon(c)

	s.manager.prompts = make([]*requestprompts.Prompt, 3)

	rsp := s.makeSyncReq(c, "GET", "/v2/interfaces/requests/prompts", 1000, nil)

	// Check parameters
	c.Check(s.manager.userID, Equals, uint32(1000))

	// Check return value
	prompts, ok := rsp.Result.([]*requestprompts.Prompt)
	c.Check(ok, Equals, true)
	c.Check(prompts, DeepEquals, s.manager.prompts)
}

func (s *promptingSuite) makeSyncReq(c *C, method string, path string, uid uint32, data []byte) *daemon.RespJSON {
	body := &bytes.Reader{}
	if len(data) > 0 {
		body = bytes.NewReader(data)
	}
	req, err := http.NewRequest(method, path, body)
	c.Assert(err, IsNil)
	req.RemoteAddr = fmt.Sprintf("pid=100;uid=%d;socket=;", uid)
	rsp := s.syncReq(c, req, nil)
	c.Check(rsp.Status, Equals, 200)
	return rsp
}

func (s *promptingSuite) TestGetPromptHappy(c *C) {
	s.daemon(c)

	s.manager.prompt = &requestprompts.Prompt{}

	rsp := s.makeSyncReq(c, "GET", "/v2/interfaces/requests/prompts/0123456789ABCDEF", 1000, nil)

	// Check parameters
	c.Check(s.manager.userID, Equals, uint32(1000))
	c.Check(s.manager.id, Equals, prompting.IDType(0x0123456789abcdef))

	// Check return value
	prompt, ok := rsp.Result.(*requestprompts.Prompt)
	c.Check(ok, Equals, true)
	c.Check(prompt, DeepEquals, s.manager.prompt)
}

func (s *promptingSuite) TestPostPromptHappy(c *C) {
	s.expectWriteAccess(daemon.InterfaceOpenAccess{Interfaces: []string{"snap-interfaces-requests-control"}})

	s.daemon(c)

	s.manager.satisfiedIDs = []prompting.IDType{
		prompting.IDType(1234),
		prompting.IDType(0),
		prompting.IDType(0xFFFFFFFFFFFFFFFF),
		prompting.IDType(0xF00BA4),
	}

	constraints := &prompting.Constraints{
		PathPattern: mustParsePathPattern(c, "/home/test/Pictures/**/*.{png,svg}"),
		Permissions: []string{"read", "execute"},
	}
	contents := &daemon.PostPromptBody{
		Outcome:     prompting.OutcomeAllow,
		Lifespan:    prompting.LifespanTimespan,
		Duration:    "10m",
		Constraints: constraints,
	}
	marshalled, err := json.Marshal(contents)
	c.Assert(err, IsNil)

	rsp := s.makeSyncReq(c, "POST", "/v2/interfaces/requests/prompts/0123456789ABCDEF", 1000, marshalled)

	// Check parameters
	c.Check(s.manager.userID, Equals, uint32(1000))
	c.Check(s.manager.id, Equals, prompting.IDType(0x0123456789abcdef))
	c.Check(s.manager.constraints, DeepEquals, contents.Constraints)
	c.Check(s.manager.outcome, Equals, contents.Outcome)
	c.Check(s.manager.lifespan, Equals, contents.Lifespan)
	c.Check(s.manager.duration, Equals, contents.Duration)

	// Check return value
	satisfiedIDs, ok := rsp.Result.([]prompting.IDType)
	c.Check(ok, Equals, true)
	c.Check(satisfiedIDs, DeepEquals, s.manager.satisfiedIDs)
}

func mustParsePathPattern(c *C, pattern string) *patterns.PathPattern {
	parsed, err := patterns.ParsePathPattern(pattern)
	c.Assert(err, IsNil)
	return parsed
}

func (s *promptingSuite) TestGetRulesHappy(c *C) {
	s.daemon(c)

	for _, testCase := range []struct {
		vars  string
		snap  string
		iface string
	}{
		{
			"",
			"",
			"",
		},
		{
			"?snap=firefox",
			"firefox",
			"",
		},
		{
			"?interface=home",
			"",
			"home",
		},
		{
			"?snap=firefox&interface=home",
			"firefox",
			"home",
		},
	} {
		// Make sure manager is zeroed out again
		s.manager = &fakeInterfacesRequestsManager{}

		// Set the rules to return
		s.manager.rules = []*requestrules.Rule{
			{
				ID:        prompting.IDType(0xabcd),
				Timestamp: time.Now(),
				User:      1234,
				Snap:      "firefox",
				Interface: "home",
				Constraints: &prompting.Constraints{
					PathPattern: mustParsePathPattern(c, "/foo/bar"),
					Permissions: []string{"write"},
				},
				Outcome:    prompting.OutcomeDeny,
				Lifespan:   prompting.LifespanForever,
				Expiration: time.Now(),
			},
		}

		rsp := s.makeSyncReq(c, "GET", fmt.Sprintf("/v2/interfaces/requests/rules%s", testCase.vars), 1234, nil)

		// Check parameters
		c.Check(s.manager.userID, Equals, uint32(1234))
		c.Check(s.manager.snap, Equals, testCase.snap)
		c.Check(s.manager.iface, Equals, testCase.iface)

		// Check return value
		rules, ok := rsp.Result.([]*requestrules.Rule)
		c.Check(ok, Equals, true)
		c.Check(rules, DeepEquals, s.manager.rules)
	}
}

func (s *promptingSuite) TestPostRulesAddHappy(c *C) {
	s.expectWriteAccess(daemon.InterfaceAuthenticatedAccess{Interfaces: []string{"snap-interfaces-requests-control"}, Polkit: "io.snapcraft.snapd.manage"})

	s.daemon(c)

	s.manager.rule = &requestrules.Rule{
		ID:        prompting.IDType(1234),
		Timestamp: time.Now(),
		User:      11235,
		Snap:      "firefox",
		Interface: "home",
		Constraints: &prompting.Constraints{
			PathPattern: mustParsePathPattern(c, "/foo/bar/baz"),
			Permissions: []string{"write"},
		},
		Outcome:    prompting.OutcomeDeny,
		Lifespan:   prompting.LifespanForever,
		Expiration: time.Now(),
	}

	constraints := &prompting.Constraints{
		PathPattern: mustParsePathPattern(c, "/home/test/{foo,bar,baz}/**/*.{png,svg}"),
		Permissions: []string{"read", "write"},
	}
	contents := &daemon.AddRuleContents{
		Snap:        "thunderbird",
		Interface:   "home",
		Constraints: constraints,
		Outcome:     prompting.OutcomeAllow,
		Lifespan:    prompting.LifespanForever,
		Duration:    "",
	}
	postBody := &daemon.PostRulesRequestBody{
		Action:  "add",
		AddRule: contents,
	}
	marshalled, err := json.Marshal(postBody)
	c.Assert(err, IsNil)

	rsp := s.makeSyncReq(c, "POST", "/v2/interfaces/requests/rules", 11235, marshalled)

	// Check parameters
	c.Check(s.manager.userID, Equals, uint32(11235))
	c.Check(s.manager.snap, Equals, contents.Snap)
	c.Check(s.manager.iface, Equals, contents.Interface)
	c.Check(s.manager.constraints, DeepEquals, contents.Constraints)
	c.Check(s.manager.outcome, Equals, contents.Outcome)
	c.Check(s.manager.lifespan, Equals, contents.Lifespan)
	c.Check(s.manager.duration, Equals, contents.Duration)

	// Check return value
	rule, ok := rsp.Result.(*requestrules.Rule)
	c.Check(ok, Equals, true)
	c.Check(rule, DeepEquals, s.manager.rule)
}

func (s *promptingSuite) TestPostRulesRemoveHappy(c *C) {
	s.expectWriteAccess(daemon.InterfaceAuthenticatedAccess{Interfaces: []string{"snap-interfaces-requests-control"}, Polkit: "io.snapcraft.snapd.manage"})

	s.daemon(c)

	for _, testCase := range []struct {
		snap  string
		iface string
	}{
		{
			"thunderbird",
			"",
		},
		{
			"",
			"home",
		},
		{
			"thunderbird",
			"home",
		},
	} {
		// Make sure manager is zeroed out again
		s.manager = &fakeInterfacesRequestsManager{}

		// Set the rules to return
		var timeZero time.Time
		s.manager.rules = []*requestrules.Rule{
			{
				ID:        prompting.IDType(1234),
				Timestamp: time.Now(),
				User:      1001,
				Snap:      "thunderird",
				Interface: "home",
				Constraints: &prompting.Constraints{
					PathPattern: mustParsePathPattern(c, "/foo/bar/baz/qux"),
					Permissions: []string{"write"},
				},
				Outcome:    prompting.OutcomeDeny,
				Lifespan:   prompting.LifespanForever,
				Expiration: timeZero,
			},
			{
				ID:        prompting.IDType(5678),
				Timestamp: time.Now(),
				User:      1001,
				Snap:      "thunderbird",
				Interface: "home",
				Constraints: &prompting.Constraints{
					PathPattern: mustParsePathPattern(c, "/fizz/buzz"),
					Permissions: []string{"read", "execute"},
				},
				Outcome:    prompting.OutcomeAllow,
				Lifespan:   prompting.LifespanTimespan,
				Expiration: time.Now(),
			},
		}

		contents := &daemon.RemoveRulesSelector{
			Snap:      testCase.snap,
			Interface: testCase.iface,
		}
		postBody := &daemon.PostRulesRequestBody{
			Action:         "remove",
			RemoveSelector: contents,
		}

		marshalled, err := json.Marshal(postBody)
		c.Assert(err, IsNil)

		rsp := s.makeSyncReq(c, "POST", "/v2/interfaces/requests/rules", 1234, marshalled)

		// Check parameters
		c.Check(s.manager.userID, Equals, uint32(1234))
		c.Check(s.manager.snap, Equals, testCase.snap)
		c.Check(s.manager.iface, Equals, testCase.iface)

		// Check return value
		rules, ok := rsp.Result.([]*requestrules.Rule)
		c.Check(ok, Equals, true)
		c.Check(rules, DeepEquals, s.manager.rules)
	}
}

func (s *promptingSuite) TestGetRuleHappy(c *C) {
	s.daemon(c)

	s.manager.rule = &requestrules.Rule{
		ID:        prompting.IDType(0x12b),
		Timestamp: time.Now(),
		User:      1005,
		Snap:      "thunderbird",
		Interface: "home",
		Constraints: &prompting.Constraints{
			PathPattern: mustParsePathPattern(c, "/home/test/Videos/**/*.{mkv,mp4,mov}"),
			Permissions: []string{"read"},
		},
		Outcome:    prompting.OutcomeAllow,
		Lifespan:   prompting.LifespanTimespan,
		Expiration: time.Now().Add(-24 * time.Hour),
	}

	rsp := s.makeSyncReq(c, "GET", "/v2/interfaces/requests/rules/000000000000012B", 1005, nil)

	// Check parameters
	c.Check(s.manager.userID, Equals, uint32(1005))
	c.Check(s.manager.id, Equals, prompting.IDType(0x12b))

	// Check return value
	rule, ok := rsp.Result.(*requestrules.Rule)
	c.Check(ok, Equals, true)
	c.Check(rule, DeepEquals, s.manager.rule)
}

func (s *promptingSuite) TestPostRulePatchHappy(c *C) {
	s.expectWriteAccess(daemon.InterfaceAuthenticatedAccess{Interfaces: []string{"snap-interfaces-requests-control"}, Polkit: "io.snapcraft.snapd.manage"})

	s.daemon(c)

	var timeZero time.Time
	s.manager.rule = &requestrules.Rule{
		ID:        prompting.IDType(0x01123581321),
		Timestamp: time.Now(),
		User:      999,
		Snap:      "gimp",
		Interface: "home",
		Constraints: &prompting.Constraints{
			PathPattern: mustParsePathPattern(c, "/home/test/Pictures/**/*.{png,jpg}"),
			Permissions: []string{"read", "write"},
		},
		Outcome:    prompting.OutcomeAllow,
		Lifespan:   prompting.LifespanForever,
		Expiration: timeZero,
	}

	constraints := &prompting.Constraints{
		PathPattern: mustParsePathPattern(c, "/home/test/Pictures/**/*.{png,jpg}"),
		Permissions: []string{"read", "write"},
	}
	contents := &daemon.PatchRuleContents{
		Constraints: constraints,
		Outcome:     prompting.OutcomeAllow,
		Lifespan:    prompting.LifespanForever,
		Duration:    "",
	}
	postBody := &daemon.PostRuleRequestBody{
		Action:    "patch",
		PatchRule: contents,
	}
	marshalled, err := json.Marshal(postBody)
	c.Assert(err, IsNil)

	rsp := s.makeSyncReq(c, "POST", "/v2/interfaces/requests/rules/0000001123581321", 999, marshalled)

	// Check parameters
	c.Check(s.manager.userID, Equals, uint32(999))
	c.Check(s.manager.constraints, DeepEquals, contents.Constraints)
	c.Check(s.manager.outcome, Equals, contents.Outcome)
	c.Check(s.manager.lifespan, Equals, contents.Lifespan)
	c.Check(s.manager.duration, Equals, contents.Duration)

	// Check return value
	rule, ok := rsp.Result.(*requestrules.Rule)
	c.Check(ok, Equals, true)
	c.Check(rule, DeepEquals, s.manager.rule)
}

func (s *promptingSuite) TestPostRuleRemoveHappy(c *C) {
	s.expectWriteAccess(daemon.InterfaceAuthenticatedAccess{Interfaces: []string{"snap-interfaces-requests-control"}, Polkit: "io.snapcraft.snapd.manage"})

	s.daemon(c)

	var timeZero time.Time
	s.manager.rule = &requestrules.Rule{
		ID:        prompting.IDType(0x01123581321),
		Timestamp: time.Now(),
		User:      100,
		Snap:      "gimp",
		Interface: "home",
		Constraints: &prompting.Constraints{
			PathPattern: mustParsePathPattern(c, "/home/test/Pictures/**/*.{png,jpg}"),
			Permissions: []string{"read", "write"},
		},
		Outcome:    prompting.OutcomeAllow,
		Lifespan:   prompting.LifespanForever,
		Expiration: timeZero,
	}
	postBody := &daemon.PostRuleRequestBody{
		Action: "remove",
	}
	marshalled, err := json.Marshal(postBody)
	c.Assert(err, IsNil)

	rsp := s.makeSyncReq(c, "POST", "/v2/interfaces/requests/rules/0000001123581321", 100, marshalled)

	// Check parameters
	c.Check(s.manager.userID, Equals, uint32(100))
	c.Check(s.manager.id, Equals, prompting.IDType(0x01123581321))

	// Check return value
	rule, ok := rsp.Result.(*requestrules.Rule)
	c.Check(ok, Equals, true)
	c.Check(rule, DeepEquals, s.manager.rule)
}
