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

package daemon_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/daemon"
	"github.com/snapcore/snapd/interfaces/prompting"
	prompting_errors "github.com/snapcore/snapd/interfaces/prompting/errors"
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
	userID               uint32
	snap                 string
	iface                string
	id                   prompting.IDType // used for prompt ID or rule ID
	addRuleConstraints   prompting.Constraints
	constraintsPatchJSON json.RawMessage
	replyConstraintsJSON json.RawMessage
	outcome              prompting.OutcomeType
	lifespan             prompting.LifespanType
	duration             string
	clientActivity       bool
}

func (m *fakeInterfacesRequestsManager) Prompts(userID uint32, clientActivity bool) ([]*requestprompts.Prompt, error) {
	m.userID = userID
	m.clientActivity = clientActivity
	return m.prompts, m.err
}

func (m *fakeInterfacesRequestsManager) PromptWithID(userID uint32, promptID prompting.IDType, clientActivity bool) (*requestprompts.Prompt, error) {
	m.userID = userID
	m.id = promptID
	m.clientActivity = clientActivity
	return m.prompt, m.err
}

func (m *fakeInterfacesRequestsManager) HandleReply(userID uint32, promptID prompting.IDType, constraintsJSON json.RawMessage, outcome prompting.OutcomeType, lifespan prompting.LifespanType, duration string, clientActivity bool) ([]prompting.IDType, error) {
	m.userID = userID
	m.id = promptID
	m.replyConstraintsJSON = constraintsJSON
	m.outcome = outcome
	m.lifespan = lifespan
	m.duration = duration
	m.clientActivity = clientActivity
	return m.satisfiedIDs, m.err
}

func (m *fakeInterfacesRequestsManager) Rules(userID uint32, snap string, iface string) ([]*requestrules.Rule, error) {
	m.userID = userID
	m.snap = snap
	m.iface = iface
	return m.rules, m.err
}

func (m *fakeInterfacesRequestsManager) AddRule(userID uint32, snap string, iface string, constraints prompting.Constraints) (*requestrules.Rule, error) {
	m.userID = userID
	m.snap = snap
	m.iface = iface
	m.addRuleConstraints = constraints
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

func (m *fakeInterfacesRequestsManager) PatchRule(userID uint32, ruleID prompting.IDType, constraintsPatchJSON json.RawMessage) (*requestrules.Rule, error) {
	m.userID = userID
	m.id = ruleID
	m.constraintsPatchJSON = constraintsPatchJSON
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

func (s *promptingSuite) InterfacesRequestsManager() apparmorprompting.Manager {
	if s.manager == nil {
		return nil
	}
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
			path:         fmt.Sprintf("/v2/interfaces/requests/prompts?user-id=4294967296"), // math.MaxUint32 + 1
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
		{
			path:         fmt.Sprintf("/v2/interfaces/requests/prompts?user-id=4294967295"), // math.MaxUint32
			uid:          "0",
			expectedUser: 0xffffffff,
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

func (s *promptingSuite) TestPromptingNotRunningError(c *C) {
	apiResp := daemon.PromptingNotRunningError()
	jsonResp := apiResp.JSON()
	rec := httptest.NewRecorder()
	jsonResp.ServeHTTP(rec, nil)
	var body map[string]any
	err := json.Unmarshal(rec.Body.Bytes(), &body)
	c.Check(err, IsNil)
	c.Check(body, DeepEquals, map[string]any{
		"result": map[string]any{
			"message": "AppArmor Prompting is not running",
			"kind":    string(client.ErrorKindAppArmorPromptingNotRunning),
		},
		"status":      "Internal Server Error",
		"status-code": 500.0,
		"type":        "error",
	})
}

func (s *promptingSuite) TestPromptingError(c *C) {
	for _, testCase := range []struct {
		err  error
		body map[string]any
	}{
		{
			err: prompting_errors.ErrPromptNotFound,
			body: map[string]any{
				"result": map[string]any{
					"message": prompting_errors.ErrPromptNotFound.Error(),
					"kind":    string(client.ErrorKindInterfacesRequestsPromptNotFound),
				},
				"status":      "Not Found",
				"status-code": 404.0,
				"type":        "error",
			},
		},
		{
			err: prompting_errors.ErrRuleNotFound,
			body: map[string]any{
				"result": map[string]any{
					"message": prompting_errors.ErrRuleNotFound.Error(),
					"kind":    string(client.ErrorKindInterfacesRequestsRuleNotFound),
				},
				"status":      "Not Found",
				"status-code": 404.0,
				"type":        "error",
			},
		},
		{
			err: prompting_errors.ErrRuleNotAllowed,
			body: map[string]any{
				"result": map[string]any{
					"message": prompting_errors.ErrRuleNotAllowed.Error(),
					"kind":    string(client.ErrorKindInterfacesRequestsRuleNotFound),
				},
				"status":      "Not Found",
				"status-code": 404.0,
				"type":        "error",
			},
		},
		{
			err: prompting_errors.ErrPromptsClosed,
			body: map[string]any{
				"result": map[string]any{
					"message": prompting_errors.ErrPromptsClosed.Error(),
				},
				"status":      "Internal Server Error",
				"status-code": 500.0,
				"type":        "error",
			},
		},
		{
			err: prompting_errors.ErrRulesClosed,
			body: map[string]any{
				"result": map[string]any{
					"message": prompting_errors.ErrRulesClosed.Error(),
				},
				"status":      "Internal Server Error",
				"status-code": 500.0,
				"type":        "error",
			},
		},
		{
			err: prompting_errors.ErrTooManyPrompts,
			body: map[string]any{
				"result": map[string]any{
					"message": prompting_errors.ErrTooManyPrompts.Error(),
				},
				"status":      "Internal Server Error",
				"status-code": 500.0,
				"type":        "error",
			},
		},
		{
			err: prompting_errors.ErrRuleIDConflict,
			body: map[string]any{
				"result": map[string]any{
					"message": prompting_errors.ErrRuleIDConflict.Error(),
				},
				"status":      "Internal Server Error",
				"status-code": 500.0,
				"type":        "error",
			},
		},
		{
			err: prompting_errors.ErrRuleDBInconsistent,
			body: map[string]any{
				"result": map[string]any{
					"message": prompting_errors.ErrRuleDBInconsistent.Error(),
				},
				"status":      "Internal Server Error",
				"status-code": 500.0,
				"type":        "error",
			},
		},
		{
			err: prompting_errors.NewInvalidOutcomeError("foo", []string{"bar", "baz"}),
			body: map[string]any{
				"result": map[string]any{
					"message": `invalid outcome: "foo"`,
					"kind":    "interfaces-requests-invalid-fields",
					"value": map[string]any{
						"outcome": map[string]any{
							"reason":    "unsupported-value",
							"supported": []any{"bar", "baz"},
							"value":     []any{"foo"},
						},
					},
				},
				"status":      "Bad Request",
				"status-code": 400.0,
				"type":        "error",
			},
		},
		{
			err: prompting_errors.NewInvalidLifespanError("foo", []string{"bar", "baz"}),
			body: map[string]any{
				"result": map[string]any{
					"message": `invalid lifespan: "foo"`,
					"kind":    "interfaces-requests-invalid-fields",
					"value": map[string]any{
						"lifespan": map[string]any{
							"reason":    "unsupported-value",
							"supported": []any{"bar", "baz"},
							"value":     []any{"foo"},
						},
					},
				},
				"status":      "Bad Request",
				"status-code": 400.0,
				"type":        "error",
			},
		},
		{
			err: prompting_errors.NewRuleLifespanSingleError([]string{"bar", "baz"}),
			body: map[string]any{
				"result": map[string]any{
					"message": `cannot create rule with lifespan "single"`,
					"kind":    "interfaces-requests-invalid-fields",
					"value": map[string]any{
						"lifespan": map[string]any{
							"reason":    "unsupported-value",
							"supported": []any{"bar", "baz"},
							"value":     []any{"single"},
						},
					},
				},
				"status":      "Bad Request",
				"status-code": 400.0,
				"type":        "error",
			},
		},
		{
			err: prompting_errors.NewInvalidInterfaceError("foo", []string{"bar", "baz"}),
			body: map[string]any{
				"result": map[string]any{
					"message": `invalid interface: "foo"`,
					"kind":    "interfaces-requests-invalid-fields",
					"value": map[string]any{
						"interface": map[string]any{
							"reason":    "unsupported-value",
							"supported": []any{"bar", "baz"},
							"value":     []any{"foo"},
						},
					},
				},
				"status":      "Bad Request",
				"status-code": 400.0,
				"type":        "error",
			},
		},
		{
			err: prompting_errors.NewInvalidPermissionsError("foo", []string{"bar", "baz"}, []string{"fizz", "buzz"}),
			body: map[string]any{
				"result": map[string]any{
					"message": `invalid permissions for foo interface: "bar", "baz"`,
					"kind":    "interfaces-requests-invalid-fields",
					"value": map[string]any{
						"permissions": map[string]any{
							"reason":    "unsupported-value",
							"supported": []any{"fizz", "buzz"},
							"value":     []any{"bar", "baz"},
						},
					},
				},
				"status":      "Bad Request",
				"status-code": 400.0,
				"type":        "error",
			},
		},
		{
			err: prompting_errors.NewPermissionsEmptyError("foo", []string{"bar", "baz"}),
			body: map[string]any{
				"result": map[string]any{
					"message": `invalid permissions for foo interface: permissions empty`,
					"kind":    "interfaces-requests-invalid-fields",
					"value": map[string]any{
						"permissions": map[string]any{
							"reason":    "unsupported-value",
							"supported": []any{"bar", "baz"},
							"value":     []any{},
						},
					},
				},
				"status":      "Bad Request",
				"status-code": 400.0,
				"type":        "error",
			},
		},
		{
			err: prompting_errors.NewInvalidDurationError("foo", "really terrible"),
			body: map[string]any{
				"result": map[string]any{
					"message": `invalid duration: really terrible: "foo"`,
					"kind":    "interfaces-requests-invalid-fields",
					"value": map[string]any{
						"duration": map[string]any{
							"reason": "parse-error",
							"value":  "foo",
						},
					},
				},
				"status":      "Bad Request",
				"status-code": 400.0,
				"type":        "error",
			},
		},
		{
			err: prompting_errors.NewInvalidExpirationError(time.Date(1, time.February, 3, 4, 5, 6, 7, time.UTC), "really terrible"),
			body: map[string]any{
				"result": map[string]any{
					"message": `invalid expiration: really terrible: "0001-02-03T04:05:06.000000007Z"`,
					"kind":    "interfaces-requests-invalid-fields",
					"value": map[string]any{
						"expiration": map[string]any{
							"reason": "parse-error",
							"value":  "0001-02-03T04:05:06.000000007Z",
						},
					},
				},
				"status":      "Bad Request",
				"status-code": 400.0,
				"type":        "error",
			},
		},
		{
			err: prompting_errors.NewInvalidPathPatternError(`invalid/pattern`, "must start with '/'"),
			body: map[string]any{
				"result": map[string]any{
					"message": `invalid path pattern: must start with '/': "invalid/pattern"`,
					"kind":    "interfaces-requests-invalid-fields",
					"value": map[string]any{
						"path-pattern": map[string]any{
							"reason": "parse-error",
							"value":  "invalid/pattern",
						},
					},
				},
				"status":      "Bad Request",
				"status-code": 400.0,
				"type":        "error",
			},
		},
		{
			err: prompting_errors.ErrPatchedRuleHasNoPerms,
			body: map[string]any{
				"result": map[string]any{
					"message": "cannot patch rule to have no permissions",
					"kind":    "interfaces-requests-patched-rule-has-no-permissions",
				},
				"status":      "Bad Request",
				"status-code": 400.0,
				"type":        "error",
			},
		},
		{
			err: prompting_errors.ErrNewSessionRuleNoSession,
			body: map[string]any{
				"result": map[string]any{
					"message": `cannot create rule with lifespan "session" when user session is not present`,
					"kind":    "interfaces-requests-new-session-rule-no-session",
				},
				"status":      "Bad Request",
				"status-code": 400.0,
				"type":        "error",
			},
		},
		{
			err: &prompting_errors.RequestedPathNotMatchedError{
				Requested: "foo",
				Replied:   "bar",
			},
			body: map[string]any{
				"result": map[string]any{
					"message": fmt.Sprintf(`%v "foo": "bar"`, prompting_errors.ErrReplyNotMatchRequestedPath),
					"kind":    "interfaces-requests-reply-not-match-request",
					"value": map[string]any{
						"path-pattern": map[string]any{
							"requested-path":  "foo",
							"replied-pattern": "bar",
						},
					},
				},
				"status":      "Bad Request",
				"status-code": 400.0,
				"type":        "error",
			},
		},
		{
			err: &prompting_errors.RequestedPermissionsNotMatchedError{
				Requested: []string{"foo", "bar", "baz"},
				Replied:   []string{"fizz", "buzz"},
			},
			body: map[string]any{
				"result": map[string]any{
					"message": fmt.Sprintf(`%v [foo bar baz]: [fizz buzz]`, prompting_errors.ErrReplyNotMatchRequestedPermissions),
					"kind":    "interfaces-requests-reply-not-match-request",
					"value": map[string]any{
						"permissions": map[string]any{
							"requested-permissions": []any{"foo", "bar", "baz"},
							"replied-permissions":   []any{"fizz", "buzz"},
						},
					},
				},
				"status":      "Bad Request",
				"status-code": 400.0,
				"type":        "error",
			},
		},
		{
			err: &prompting_errors.RuleConflictError{
				Conflicts: []prompting_errors.RuleConflict{
					{
						Permission:    "foo",
						Variant:       "variant 1",
						ConflictingID: "conflicting rule 1",
					},
					{
						Permission:    "bar",
						Variant:       "variant 2",
						ConflictingID: "conflicting rule 2",
					},
				},
			},
			body: map[string]any{
				"result": map[string]any{
					"message": prompting_errors.ErrRuleConflict.Error(),
					"kind":    "interfaces-requests-rule-conflict",
					"value": map[string]any{
						"conflicts": []any{
							map[string]any{
								"permission":     "foo",
								"variant":        "variant 1",
								"conflicting-id": "conflicting rule 1",
							},
							map[string]any{
								"permission":     "bar",
								"variant":        "variant 2",
								"conflicting-id": "conflicting rule 2",
							},
						},
					},
				},
				"status":      "Conflict",
				"status-code": 409.0,
				"type":        "error",
			},
		},
		{
			err: fmt.Errorf("some arbitrary error"),
			body: map[string]any{
				"result": map[string]any{
					"message": "some arbitrary error",
				},
				"status":      "Internal Server Error",
				"status-code": 500.0,
				"type":        "error",
			},
		},
	} {
		apiResp := daemon.PromptingError(testCase.err)
		jsonResp := apiResp.JSON()
		rec := httptest.NewRecorder()
		jsonResp.ServeHTTP(rec, nil)
		var body map[string]any
		err := json.Unmarshal(rec.Body.Bytes(), &body)
		c.Check(err, IsNil)
		c.Check(body, DeepEquals, testCase.body)
	}
}

func (s *promptingSuite) TestGetPromptsHappy(c *C) {
	s.daemon(c)

	s.manager.prompts = make([]*requestprompts.Prompt, 3)

	rsp := s.makeSyncReq(c, "GET", "/v2/interfaces/requests/prompts", 1000, nil)

	// Check parameters
	c.Check(s.manager.userID, Equals, uint32(1000))
	c.Check(s.manager.clientActivity, Equals, true)

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
	rsp := s.syncReq(c, req, nil, actionIsExpected)
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
	c.Check(s.manager.clientActivity, Equals, true)

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

	constraintsJSON := json.RawMessage(`{"foo":"bar"}`)
	contents := &daemon.PostPromptBody{
		Outcome:         prompting.OutcomeAllow,
		Lifespan:        prompting.LifespanTimespan,
		Duration:        "10m",
		ConstraintsJSON: constraintsJSON,
	}
	marshalled, err := json.Marshal(contents)
	c.Assert(err, IsNil)

	rsp := s.makeSyncReq(c, "POST", "/v2/interfaces/requests/prompts/0123456789ABCDEF", 1000, marshalled)

	// Check parameters
	c.Check(s.manager.userID, Equals, uint32(1000))
	c.Check(s.manager.id, Equals, prompting.IDType(0x0123456789abcdef))
	c.Check(s.manager.replyConstraintsJSON, DeepEquals, contents.ConstraintsJSON)
	c.Check(s.manager.outcome, Equals, contents.Outcome)
	c.Check(s.manager.lifespan, Equals, contents.Lifespan)
	c.Check(s.manager.duration, Equals, contents.Duration)
	c.Check(s.manager.clientActivity, Equals, true)

	// Check return value
	satisfiedIDs, ok := rsp.Result.([]prompting.IDType)
	c.Check(ok, Equals, true)
	c.Check(satisfiedIDs, DeepEquals, s.manager.satisfiedIDs)
}

func (s *promptingSuite) TestPostPromptDenyHappy(c *C) {
	s.expectWriteAccess(daemon.InterfaceOpenAccess{Interfaces: []string{"snap-interfaces-requests-control"}})

	s.daemon(c)

	s.manager.satisfiedIDs = []prompting.IDType{
		prompting.IDType(1234),
		prompting.IDType(0),
		prompting.IDType(0xFFFFFFFFFFFFFFFF),
		prompting.IDType(0xF00BA4),
	}

	constraintsJSON := json.RawMessage(`{"foo":"bar"}`)
	contents := &daemon.PostPromptBody{
		Outcome:         prompting.OutcomeDeny,
		Lifespan:        prompting.LifespanTimespan,
		Duration:        "10m",
		ConstraintsJSON: constraintsJSON,
	}
	marshalled, err := json.Marshal(contents)
	c.Assert(err, IsNil)

	rsp := s.makeSyncReq(c, "POST", "/v2/interfaces/requests/prompts/0123456789ABCDEF", 1000, marshalled)

	// Check parameters
	c.Check(s.manager.userID, Equals, uint32(1000))
	c.Check(s.manager.id, Equals, prompting.IDType(0x0123456789abcdef))
	c.Check(s.manager.replyConstraintsJSON, DeepEquals, contents.ConstraintsJSON)
	c.Check(s.manager.outcome, Equals, contents.Outcome)
	c.Check(s.manager.lifespan, Equals, contents.Lifespan)
	c.Check(s.manager.duration, Equals, contents.Duration)
	c.Check(s.manager.clientActivity, Equals, true)

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
				Constraints: &prompting.RuleConstraintsHome{
					Pattern: mustParsePathPattern(c, "/foo/bar"),
					PermissionMap: prompting.RulePermissionMap{
						"write": &prompting.RulePermissionEntry{
							Outcome:  prompting.OutcomeDeny,
							Lifespan: prompting.LifespanForever,
						},
					},
				},
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
		Constraints: &prompting.RuleConstraintsHome{
			Pattern: mustParsePathPattern(c, "/foo/bar/baz"),
			PermissionMap: prompting.RulePermissionMap{
				"write": &prompting.RulePermissionEntry{
					Outcome:  prompting.OutcomeDeny,
					Lifespan: prompting.LifespanForever,
				},
			},
		},
	}

	constraints := &prompting.ConstraintsHome{
		PathPattern: mustParsePathPattern(c, "/home/test/{foo,bar,baz}/**/*.{png,svg}"),
		PermissionMap: prompting.PermissionMap{
			"read": &prompting.PermissionEntry{
				Outcome:  prompting.OutcomeAllow,
				Lifespan: prompting.LifespanForever,
			},
			"write": &prompting.PermissionEntry{
				Outcome:  prompting.OutcomeAllow,
				Lifespan: prompting.LifespanForever,
			},
		},
	}
	constraintsJSON, err := json.Marshal(constraints)
	c.Assert(err, IsNil)
	contents := &daemon.AddRuleContents{
		Snap:            "thunderbird",
		Interface:       "home",
		ConstraintsJSON: json.RawMessage(constraintsJSON),
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
	c.Check(s.manager.addRuleConstraints, DeepEquals, constraints)

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
		s.manager.rules = []*requestrules.Rule{
			{
				ID:        prompting.IDType(1234),
				Timestamp: time.Now(),
				User:      1001,
				Snap:      "thunderird",
				Interface: "home",
				Constraints: &prompting.RuleConstraintsHome{
					Pattern: mustParsePathPattern(c, "/foo/bar/baz/qux"),
					PermissionMap: prompting.RulePermissionMap{
						"write": &prompting.RulePermissionEntry{
							Outcome:  prompting.OutcomeDeny,
							Lifespan: prompting.LifespanForever,
						},
					},
				},
			},
			{
				ID:        prompting.IDType(5678),
				Timestamp: time.Now(),
				User:      1001,
				Snap:      "thunderbird",
				Interface: "home",
				Constraints: &prompting.RuleConstraintsHome{
					Pattern: mustParsePathPattern(c, "/fizz/buzz"),
					PermissionMap: prompting.RulePermissionMap{
						"read": &prompting.RulePermissionEntry{
							Outcome:  prompting.OutcomeAllow,
							Lifespan: prompting.LifespanTimespan,
						},
						"execute": &prompting.RulePermissionEntry{
							Outcome:  prompting.OutcomeAllow,
							Lifespan: prompting.LifespanTimespan,
						},
					},
				},
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
		Constraints: &prompting.RuleConstraintsHome{
			Pattern: mustParsePathPattern(c, "/home/test/Videos/**/*.{mkv,mp4,mov}"),
			PermissionMap: prompting.RulePermissionMap{
				"read": &prompting.RulePermissionEntry{
					Outcome:    prompting.OutcomeAllow,
					Lifespan:   prompting.LifespanTimespan,
					Expiration: time.Now().Add(-24 * time.Hour),
				},
			},
		},
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

	s.manager.rule = &requestrules.Rule{
		ID:        prompting.IDType(0x01123581321),
		Timestamp: time.Now(),
		User:      999,
		Snap:      "gimp",
		Interface: "home",
		Constraints: &prompting.RuleConstraintsHome{
			Pattern: mustParsePathPattern(c, "/home/test/Pictures/**/*.{png,jpg}"),
			PermissionMap: prompting.RulePermissionMap{
				"read": &prompting.RulePermissionEntry{
					Outcome:  prompting.OutcomeAllow,
					Lifespan: prompting.LifespanForever,
				},
				"write": &prompting.RulePermissionEntry{
					Outcome:  prompting.OutcomeAllow,
					Lifespan: prompting.LifespanForever,
				},
			},
		},
	}

	constraintsPatchJSON := json.RawMessage(`{"foo":"bar"}`)
	contents := &daemon.PatchRuleContents{
		ConstraintsJSON: constraintsPatchJSON,
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
	c.Check(s.manager.constraintsPatchJSON, DeepEquals, contents.ConstraintsJSON)

	// Check return value
	rule, ok := rsp.Result.(*requestrules.Rule)
	c.Check(ok, Equals, true)
	c.Check(rule, DeepEquals, s.manager.rule)
}

func (s *promptingSuite) TestPostRuleRemoveHappy(c *C) {
	s.expectWriteAccess(daemon.InterfaceAuthenticatedAccess{Interfaces: []string{"snap-interfaces-requests-control"}, Polkit: "io.snapcraft.snapd.manage"})

	s.daemon(c)

	s.manager.rule = &requestrules.Rule{
		ID:        prompting.IDType(0x01123581321),
		Timestamp: time.Now(),
		User:      100,
		Snap:      "gimp",
		Interface: "home",
		Constraints: &prompting.RuleConstraintsHome{
			Pattern: mustParsePathPattern(c, "/home/test/Pictures/**/*.{png,jpg}"),
			PermissionMap: prompting.RulePermissionMap{
				"read": &prompting.RulePermissionEntry{
					Outcome:  prompting.OutcomeAllow,
					Lifespan: prompting.LifespanForever,
				},
				"write": &prompting.RulePermissionEntry{
					Outcome:  prompting.OutcomeAllow,
					Lifespan: prompting.LifespanForever,
				},
			},
		},
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
