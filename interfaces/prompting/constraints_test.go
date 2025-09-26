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

package prompting_test

import (
	"encoding/json"
	"fmt"
	"runtime"
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/interfaces/prompting"
	prompting_errors "github.com/snapcore/snapd/interfaces/prompting/errors"
	"github.com/snapcore/snapd/interfaces/prompting/patterns"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/sandbox/apparmor/notify"
	"github.com/snapcore/snapd/sandbox/apparmor/notify/listener"
	"github.com/snapcore/snapd/testutil"
)

type constraintsSuite struct{}

var _ = Suite(&constraintsSuite{})

func mustParsePathPattern(c *C, patternStr string) *patterns.PathPattern {
	pattern, err := patterns.ParsePathPattern(patternStr)
	c.Assert(err, IsNil)
	return pattern
}

// Test NewPromptConstraints and the simple methods associated with each prompt
// constraints, including MarshalJSON and Permissions.
func (s *constraintsSuite) TestNewPromptConstraintsMarshalJSONPermissions(c *C) {
	for _, testCase := range []struct {
		iface                  string
		originalPermissions    []string
		outstandingPermissions []string
		req                    *listener.Request
		expectedType           string
		expectedJSON           string
		expectedAvailablePerms []string
	}{
		{
			iface:                  "home",
			originalPermissions:    []string{"read", "write"},
			outstandingPermissions: []string{"read"},
			req: &listener.Request{
				Path: "/path/to/foo",
			},
			expectedType:           "*prompting.PromptConstraintsHome",
			expectedJSON:           `{"path":"/path/to/foo","requested-permissions":["read"],"available-permissions":["read","write","execute"]}`,
			expectedAvailablePerms: []string{"read", "write", "execute"},
		},
		{
			iface:                  "camera",
			originalPermissions:    []string{"access"},
			outstandingPermissions: []string{"access"},
			req:                    &listener.Request{},
			expectedType:           "*prompting.PromptConstraintsCamera",
			expectedJSON:           `{"requested-permissions":["access"],"available-permissions":["access"]}`,
			expectedAvailablePerms: []string{"access"},
		},
	} {
		promptConstraints, err := prompting.NewPromptConstraints(testCase.iface, testCase.originalPermissions, testCase.outstandingPermissions, testCase.req)
		c.Assert(err, IsNil)
		c.Check(fmt.Sprintf("%T", promptConstraints), Equals, testCase.expectedType)
		marshalled, err := json.Marshal(promptConstraints)
		c.Check(err, IsNil)
		c.Check(string(marshalled), Equals, testCase.expectedJSON)
		promptPerms := promptConstraints.Permissions()
		c.Assert(promptPerms, NotNil)
		c.Check(promptPerms.OriginalPermissions, DeepEquals, testCase.originalPermissions)
		c.Check(promptPerms.OutstandingPermissions, DeepEquals, testCase.outstandingPermissions)
		c.Check(promptPerms.AvailablePermissions, DeepEquals, testCase.expectedAvailablePerms)
	}

	// Check that bad interface results in error
	result, err := prompting.NewPromptConstraints("foo", nil, nil, nil)
	c.Check(err, ErrorMatches, "cannot get available permissions: unsupported interface: foo")
	c.Check(result, IsNil)
}

func (s *constraintsSuite) TestUnmarshalReplyConstraints(c *C) {
	for _, testCase := range []struct {
		iface               string
		rawJSON             string
		expectedType        string
		expectedPermissions []string
	}{
		{
			iface:               "home",
			rawJSON:             `{"path-pattern":"/path/to/foo","permissions":["read","write"]}`,
			expectedType:        "*prompting.ReplyConstraintsHome",
			expectedPermissions: []string{"read", "write"},
		},
		{
			iface:               "camera",
			rawJSON:             `{"permissions":["access"]}`,
			expectedType:        "*prompting.ReplyConstraintsCamera",
			expectedPermissions: []string{"access"},
		},
	} {
		rawJSON := json.RawMessage([]byte(testCase.rawJSON))
		replyConstraints, err := prompting.UnmarshalReplyConstraints(testCase.iface, rawJSON)
		c.Assert(err, IsNil)
		c.Check(fmt.Sprintf("%T", replyConstraints), Equals, testCase.expectedType)
		c.Check(replyConstraints.Permissions(), DeepEquals, testCase.expectedPermissions)
	}

	// Check that bad interface results in error
	result, err := prompting.UnmarshalReplyConstraints("foo", nil)
	c.Check(err, ErrorMatches, `invalid interface: "foo"`)
	c.Check(result, IsNil)

	// Check that bad json results in error
	result, err = prompting.UnmarshalReplyConstraints("home", json.RawMessage([]byte(`{"permissions":"not a list"}`)))
	c.Check(err, ErrorMatches, `json: cannot unmarshal string .*`)
	c.Check(result, IsNil)
}

func (s *constraintsSuite) TestUnmarshalConstraints(c *C) {
	for _, testCase := range []struct {
		iface               string
		rawJSON             string
		expectedType        string
		expectedPermissions prompting.PermissionMap
	}{
		{
			iface:        "home",
			rawJSON:      `{"path-pattern":"/path/to/foo","permissions":{"read":{"outcome":"allow","lifespan":"forever"},"write":{"outcome":"deny","lifespan":"session"},"execute":{"outcome":"allow","lifespan":"timespan","duration":"10s"}}}`,
			expectedType: "*prompting.ConstraintsHome",
			expectedPermissions: prompting.PermissionMap{
				"read": &prompting.PermissionEntry{
					Outcome:  prompting.OutcomeAllow,
					Lifespan: prompting.LifespanForever,
				},
				"write": &prompting.PermissionEntry{
					Outcome:  prompting.OutcomeDeny,
					Lifespan: prompting.LifespanSession,
				},
				"execute": &prompting.PermissionEntry{
					Outcome:  prompting.OutcomeAllow,
					Lifespan: prompting.LifespanTimespan,
					Duration: "10s",
				},
			},
		},
		{
			iface:        "camera",
			rawJSON:      `{"permissions":{"access":{"outcome":"deny","lifespan":"session"}}}`,
			expectedType: "*prompting.ConstraintsCamera",
			expectedPermissions: prompting.PermissionMap{
				"access": &prompting.PermissionEntry{
					Outcome:  prompting.OutcomeDeny,
					Lifespan: prompting.LifespanSession,
				},
			},
		},
	} {
		rawJSON := json.RawMessage([]byte(testCase.rawJSON))
		constraints, err := prompting.UnmarshalConstraints(testCase.iface, rawJSON)
		c.Assert(err, IsNil)
		c.Check(fmt.Sprintf("%T", constraints), Equals, testCase.expectedType)
		c.Check(constraints.Permissions(), DeepEquals, testCase.expectedPermissions)
	}

	// Check that bad interface results in error
	result, err := prompting.UnmarshalConstraints("foo", nil)
	c.Check(err, ErrorMatches, `invalid interface: "foo"`)
	c.Check(result, IsNil)

	// Check that bad json results in error
	result, err = prompting.UnmarshalConstraints("home", json.RawMessage([]byte(`{"permissions":{"bad":{"outcome":"terrible"}}}`)))
	c.Check(err, ErrorMatches, `invalid outcome: "terrible"`)
	c.Check(result, IsNil)
}

func (s *constraintsSuite) TestUnmarshalRuleConstraints(c *C) {
	for _, testCase := range []struct {
		iface               string
		rawJSON             string
		expectedType        string
		expectedPermissions prompting.RulePermissionMap
		expectedPathPattern *patterns.PathPattern
	}{
		{
			iface:        "home",
			rawJSON:      `{"path-pattern":"/path/to/foo","permissions":{"read":{"outcome":"allow","lifespan":"forever"},"write":{"outcome":"deny","lifespan":"session","session-id":"0123456789ABCDEF"},"execute":{"outcome":"allow","lifespan":"session","session-id":"1234123412341234"}}}`,
			expectedType: "*prompting.RuleConstraintsHome",
			expectedPermissions: prompting.RulePermissionMap{
				"read": &prompting.RulePermissionEntry{
					Outcome:  prompting.OutcomeAllow,
					Lifespan: prompting.LifespanForever,
				},
				"write": &prompting.RulePermissionEntry{
					Outcome:   prompting.OutcomeDeny,
					Lifespan:  prompting.LifespanSession,
					SessionID: prompting.IDType(0x0123456789ABCDEF),
				},
				"execute": &prompting.RulePermissionEntry{
					Outcome:   prompting.OutcomeAllow,
					Lifespan:  prompting.LifespanSession,
					SessionID: prompting.IDType(0x1234123412341234),
				},
			},
			expectedPathPattern: mustParsePathPattern(c, "/path/to/foo"),
		},
		{
			iface:        "camera",
			rawJSON:      `{"permissions":{"access":{"outcome":"deny","lifespan":"session","session-id":"0123456789ABCDEF"}}}`,
			expectedType: "*prompting.RuleConstraintsCamera",
			expectedPermissions: prompting.RulePermissionMap{
				"access": &prompting.RulePermissionEntry{
					Outcome:   prompting.OutcomeDeny,
					Lifespan:  prompting.LifespanSession,
					SessionID: prompting.IDType(0x0123456789ABCDEF),
				},
			},
			expectedPathPattern: mustParsePathPattern(c, "/**"),
		},
	} {
		rawJSON := json.RawMessage([]byte(testCase.rawJSON))
		ruleConstraints, err := prompting.UnmarshalRuleConstraints(testCase.iface, rawJSON)
		c.Assert(err, IsNil)
		c.Check(fmt.Sprintf("%T", ruleConstraints), Equals, testCase.expectedType)
		c.Check(ruleConstraints.Permissions(), DeepEquals, testCase.expectedPermissions)
		c.Check(ruleConstraints.PathPattern(), DeepEquals, testCase.expectedPathPattern)
	}

	// Check that bad interface results in error
	result, err := prompting.UnmarshalRuleConstraints("foo", nil)
	c.Check(err, ErrorMatches, `invalid interface: "foo"`)
	c.Check(result, IsNil)

	// Check that bad json results in error
	result, err = prompting.UnmarshalRuleConstraints("home", json.RawMessage([]byte(`{"permissions":{"bad":{"outcome":"terrible"}}}`)))
	c.Check(err, ErrorMatches, `invalid outcome: "terrible"`)
	c.Check(result, IsNil)
}

func (s *constraintsSuite) TestUnmarshalRuleConstraintsPatch(c *C) {
	for _, testCase := range []struct {
		iface        string
		rawJSON      string
		expectedType string
	}{
		{
			iface:        "home",
			rawJSON:      `{"path-pattern":"/path/to/foo","permissions":{"read":{"outcome":"allow","lifespan":"forever"},"write":{"outcome":"deny","lifespan":"session"},"execute":{"outcome":"allow","lifespan":"timespan","duration":"10s"}}}`,
			expectedType: "*prompting.RuleConstraintsPatchHome",
		},
		{
			iface:        "camera",
			rawJSON:      `{"permissions":{"access":{"outcome":"allow","lifespan":"forever"}}}`,
			expectedType: "*prompting.RuleConstraintsPatchCamera",
		},
	} {
		rawJSON := json.RawMessage([]byte(testCase.rawJSON))
		constraintsPatch, err := prompting.UnmarshalRuleConstraintsPatch(testCase.iface, rawJSON)
		c.Assert(err, IsNil)
		c.Check(fmt.Sprintf("%T", constraintsPatch), Equals, testCase.expectedType)
	}

	// Check that bad interface results in error
	result, err := prompting.UnmarshalRuleConstraintsPatch("foo", nil)
	c.Check(err, ErrorMatches, `internal error: cannot decode constraints patch: unsupported interface: foo`)
	c.Check(result, IsNil)

	// Check that bad json results in error
	result, err = prompting.UnmarshalRuleConstraintsPatch("home", json.RawMessage([]byte(`{"permissions":{"bad":{"outcome":"terrible"}}}`)))
	c.Check(err, ErrorMatches, `invalid outcome: "terrible"`)
	c.Check(result, IsNil)
}

func (s *constraintsSuite) TestPromptPermissionsOriginalPermissionsEqual(c *C) {
	for _, testCase := range []struct {
		left  *prompting.PromptPermissions
		right *prompting.PromptPermissions
		equal bool
	}{
		{
			left: &prompting.PromptPermissions{
				OriginalPermissions: []string{"foo", "bar"},
			},
			right: &prompting.PromptPermissions{
				OriginalPermissions: []string{"foo", "bar"},
			},
			equal: true,
		},
		{
			left: &prompting.PromptPermissions{
				OriginalPermissions:    []string{"foo", "bar"},
				OutstandingPermissions: []string{"foo", "bar"},
			},
			right: &prompting.PromptPermissions{
				OriginalPermissions:    []string{"foo", "bar"},
				OutstandingPermissions: []string{"foo"},
				AvailablePermissions:   []string{"foo", "bar", "baz"},
			},
			equal: true,
		},
		{
			left: &prompting.PromptPermissions{
				OriginalPermissions:    []string{"foo"},
				OutstandingPermissions: []string{"foo"},
				AvailablePermissions:   []string{"foo", "bar", "baz"},
			},
			right: &prompting.PromptPermissions{
				OriginalPermissions:    []string{"foo", "bar"},
				OutstandingPermissions: []string{"foo"},
				AvailablePermissions:   []string{"foo", "bar", "baz"},
			},
			equal: false,
		},
		{
			left: &prompting.PromptPermissions{
				OriginalPermissions: []string{"foo", "bar"},
			},
			right: &prompting.PromptPermissions{
				OriginalPermissions: []string{"foo", "bar", "baz"},
			},
			equal: false,
		},
	} {
		c.Check(testCase.left.OriginalPermissionsEqual(testCase.right), Equals, testCase.equal, Commentf("testCase: %+v", testCase))
	}
}

func (s *constraintsSuite) TestPromptPermissionsApplyRulePermissions(c *C) {
	c.Fatalf("TODO")
}

func (s *constraintsSuite) TestPromptPermissionsBuildResponsePermissions(c *C) {
	c.Fatalf("TODO")
}

func (s *constraintsSuite) TestNewPermissionMapHappy(c *C) {
	for _, testCase := range []struct {
		iface       string
		permissions []string
		outcome     prompting.OutcomeType
		lifespan    prompting.LifespanType
		duration    string
		expected    prompting.PermissionMap
	}{
		{
			iface:       "home",
			permissions: []string{"read", "write", "execute"},
			outcome:     prompting.OutcomeAllow,
			lifespan:    prompting.LifespanForever,
			expected: prompting.PermissionMap{
				"read": &prompting.PermissionEntry{
					Outcome:  prompting.OutcomeAllow,
					Lifespan: prompting.LifespanForever,
				},
				"write": &prompting.PermissionEntry{
					Outcome:  prompting.OutcomeAllow,
					Lifespan: prompting.LifespanForever,
				},
				"execute": &prompting.PermissionEntry{
					Outcome:  prompting.OutcomeAllow,
					Lifespan: prompting.LifespanForever,
				},
			},
		},
		{
			iface:       "home",
			permissions: []string{"write", "read"},
			outcome:     prompting.OutcomeDeny,
			lifespan:    prompting.LifespanTimespan,
			duration:    "10m",
			expected: prompting.PermissionMap{
				"read": &prompting.PermissionEntry{
					Outcome:  prompting.OutcomeDeny,
					Lifespan: prompting.LifespanTimespan,
					Duration: "10m",
				},
				"write": &prompting.PermissionEntry{
					Outcome:  prompting.OutcomeDeny,
					Lifespan: prompting.LifespanTimespan,
					Duration: "10m",
				},
			},
		},
		{
			iface:       "camera",
			permissions: []string{"access"},
			outcome:     prompting.OutcomeAllow,
			lifespan:    prompting.LifespanSession,
			expected: prompting.PermissionMap{
				"access": &prompting.PermissionEntry{
					Outcome:  prompting.OutcomeAllow,
					Lifespan: prompting.LifespanSession,
				},
			},
		},
	} {
		result, err := prompting.NewPermissionMap(testCase.iface, testCase.permissions, testCase.outcome, testCase.lifespan, testCase.duration)
		c.Check(err, IsNil)
		c.Check(result, DeepEquals, testCase.expected)
	}
}

func (s *constraintsSuite) TestNewPermissionMapUnhappy(c *C) {
	for _, testCase := range []struct {
		iface       string
		permissions []string
		outcome     prompting.OutcomeType
		lifespan    prompting.LifespanType
		duration    string
		errStr      string
	}{
		{
			outcome: prompting.OutcomeType("foo"),
			errStr:  `invalid outcome: "foo"`,
		},
		{
			outcome:  prompting.OutcomeAllow,
			lifespan: prompting.LifespanTimespan,
			duration: "",
			errStr:   `invalid duration: cannot have unspecified duration when lifespan is "timespan":.*`,
		},
		{
			outcome:  prompting.OutcomeAllow,
			lifespan: prompting.LifespanForever,
			duration: "10s",
			errStr:   `invalid duration: cannot have specified duration when lifespan is "forever":.*`,
		},
		{
			outcome:  prompting.OutcomeAllow,
			lifespan: prompting.LifespanForever,
			iface:    "foo",
			errStr:   `invalid interface: "foo"`,
		},
		{
			outcome:     prompting.OutcomeAllow,
			lifespan:    prompting.LifespanForever,
			iface:       "home",
			permissions: make([]string, 0),
			errStr:      `invalid permissions for home interface: permissions empty`,
		},
		{
			outcome:     prompting.OutcomeAllow,
			lifespan:    prompting.LifespanForever,
			iface:       "camera",
			permissions: make([]string, 0),
			errStr:      `invalid permissions for camera interface: permissions empty`,
		},
		{
			outcome:     prompting.OutcomeAllow,
			lifespan:    prompting.LifespanForever,
			iface:       "home",
			permissions: []string{"read", "append", "write", "access", "create", "execute"},
			errStr:      `invalid permissions for home interface: "append", "access", "create"`,
		},
		{
			outcome:     prompting.OutcomeAllow,
			lifespan:    prompting.LifespanForever,
			iface:       "camera",
			permissions: []string{"read", "append", "write", "access", "create", "execute"},
			errStr:      `invalid permissions for camera interface: "read", "append", "write", "create", "execute"`,
		},
	} {
		result, err := prompting.NewPermissionMap(testCase.iface, testCase.permissions, testCase.outcome, testCase.lifespan, testCase.duration)
		c.Check(result, IsNil, Commentf("testCase: %+v", testCase))
		c.Check(err, ErrorMatches, testCase.errStr, Commentf("testCase: %+v", testCase))
	}
}

func (s *constraintsSuite) TestPermissionMapContainsOutstandingPermissions(c *C) {
	cases := []struct {
		permMapPerms []string
		queryPerms   []string
		contained    bool
	}{
		{
			[]string{"read", "write", "execute"},
			[]string{"read", "write", "execute"},
			true,
		},
		{
			[]string{"execute", "write", "read"},
			[]string{"read", "write", "execute"},
			true,
		},
		{
			[]string{"read", "write", "execute"},
			[]string{"read"},
			true,
		},
		{
			[]string{"read", "write", "execute"},
			[]string{"execute"},
			true,
		},
		{
			[]string{"read", "write", "execute"},
			[]string{"read", "write", "execute", "append"},
			false,
		},
		{
			[]string{"read", "write", "execute"},
			[]string{"read", "append"},
			false,
		},
		{
			[]string{"foo", "bar", "baz"},
			[]string{"foo", "bar"},
			true,
		},
		{
			[]string{"foo", "bar", "baz"},
			[]string{"fizz", "buzz"},
			false,
		},
	}
	for _, testCase := range cases {
		permMap := make(prompting.PermissionMap)
		fakeEntry := &prompting.PermissionEntry{}
		for _, perm := range testCase.permMapPerms {
			permMap[perm] = fakeEntry
		}
		promptPerms := &prompting.PromptPermissions{
			OutstandingPermissions: testCase.queryPerms,
		}
		contained := permMap.ContainsOutstandingPermissions(promptPerms)
		c.Check(contained, Equals, testCase.contained, Commentf("testCase: %+v", testCase))
	}
}

func (s *constraintsSuite) TestPermissionMapToRulePermissionMapHappy(c *C) {
	at := prompting.At{
		Time:      time.Now(),
		SessionID: prompting.IDType(0x12345),
	}
	for _, testCase := range []struct {
		permissionMap prompting.PermissionMap
		iface         string
		expected      prompting.RulePermissionMap
	}{
		{
			permissionMap: prompting.PermissionMap{
				"read": &prompting.PermissionEntry{
					Outcome:  prompting.OutcomeAllow,
					Lifespan: prompting.LifespanForever,
				},
				"write": &prompting.PermissionEntry{
					Outcome:  prompting.OutcomeDeny,
					Lifespan: prompting.LifespanTimespan,
					Duration: "10s",
				},
				"execute": &prompting.PermissionEntry{
					Outcome:  prompting.OutcomeAllow,
					Lifespan: prompting.LifespanSession,
				},
			},
			iface: "home",
			expected: prompting.RulePermissionMap{
				"read": &prompting.RulePermissionEntry{
					Outcome:  prompting.OutcomeAllow,
					Lifespan: prompting.LifespanForever,
				},
				"write": &prompting.RulePermissionEntry{
					Outcome:    prompting.OutcomeDeny,
					Lifespan:   prompting.LifespanTimespan,
					Expiration: at.Time.Add(10 * time.Second),
				},
				"execute": &prompting.RulePermissionEntry{
					Outcome:   prompting.OutcomeAllow,
					Lifespan:  prompting.LifespanSession,
					SessionID: at.SessionID,
				},
			},
		},
		{
			permissionMap: prompting.PermissionMap{
				"access": &prompting.PermissionEntry{
					Outcome:  prompting.OutcomeAllow,
					Lifespan: prompting.LifespanSession,
				},
			},
			iface: "camera",
			expected: prompting.RulePermissionMap{
				"access": &prompting.RulePermissionEntry{
					Outcome:   prompting.OutcomeAllow,
					Lifespan:  prompting.LifespanSession,
					SessionID: at.SessionID,
				},
			},
		},
	} {
		result, err := testCase.permissionMap.ToRulePermissionMap(testCase.iface, at)
		c.Check(err, IsNil)
		c.Check(result, DeepEquals, testCase.expected)
	}
}

func (s *constraintsSuite) TestPermissionMapToRulePermissionMapUnhappy(c *C) {
	at := prompting.At{
		Time:      time.Now(),
		SessionID: prompting.IDType(0), // will result in session not found error
	}
	for _, testCase := range []struct {
		permissionMap prompting.PermissionMap
		iface         string
		expectedErr   string
	}{
		{
			permissionMap: prompting.PermissionMap{
				"read": &prompting.PermissionEntry{
					Outcome:  prompting.OutcomeAllow,
					Lifespan: prompting.LifespanForever,
				},
				"write": &prompting.PermissionEntry{
					Outcome:  prompting.OutcomeDeny,
					Lifespan: prompting.LifespanTimespan,
					Duration: "10s",
				},
				"execute": &prompting.PermissionEntry{
					Outcome:  prompting.OutcomeAllow,
					Lifespan: prompting.LifespanSession,
				},
			},
			iface:       "foo",
			expectedErr: `invalid interface: "foo"`,
		},
		{
			permissionMap: nil,
			iface:         "home",
			expectedErr:   `invalid permissions for home interface: permissions empty`,
		},
		{
			permissionMap: nil,
			iface:         "camera",
			expectedErr:   `invalid permissions for camera interface: permissions empty`,
		},
		{
			permissionMap: prompting.PermissionMap{},
			iface:         "home",
			expectedErr:   `invalid permissions for home interface: permissions empty`,
		},
		{
			permissionMap: prompting.PermissionMap{},
			iface:         "camera",
			expectedErr:   `invalid permissions for camera interface: permissions empty`,
		},
		{
			permissionMap: prompting.PermissionMap{
				"create": &prompting.PermissionEntry{
					Outcome:  prompting.OutcomeAllow,
					Lifespan: prompting.LifespanForever,
				},
			},
			iface:       "home",
			expectedErr: `invalid permissions for home interface: "create"`,
		},
		{
			permissionMap: prompting.PermissionMap{
				"read": &prompting.PermissionEntry{
					Outcome:  prompting.OutcomeAllow,
					Lifespan: prompting.LifespanForever,
				},
			},
			iface:       "camera",
			expectedErr: `invalid permissions for camera interface: "read"`,
		},
		{
			permissionMap: prompting.PermissionMap{
				"read": &prompting.PermissionEntry{
					Outcome:  prompting.OutcomeAllow,
					Lifespan: prompting.LifespanTimespan,
				},
			},
			iface:       "home",
			expectedErr: `invalid duration: cannot have unspecified duration when lifespan is "timespan".*`,
		},
		{
			permissionMap: prompting.PermissionMap{
				"access": &prompting.PermissionEntry{
					Outcome:  prompting.OutcomeDeny,
					Lifespan: prompting.LifespanSession,
					Duration: "5s",
				},
			},
			iface:       "camera",
			expectedErr: `invalid duration: cannot have specified duration when lifespan is "session":.*`,
		},
		{
			permissionMap: prompting.PermissionMap{
				"write": &prompting.PermissionEntry{
					Outcome:  prompting.OutcomeDeny,
					Lifespan: prompting.LifespanSession,
				},
			},
			iface: "home",
			// Error will occur because current session is 0 (not found) below
			expectedErr: prompting_errors.ErrNewSessionRuleNoSession.Error(),
		},
		{
			permissionMap: prompting.PermissionMap{
				"read": &prompting.PermissionEntry{
					Outcome:  prompting.OutcomeAllow,
					Lifespan: prompting.LifespanTimespan,
				},
				"write": &prompting.PermissionEntry{
					Outcome:  prompting.OutcomeDeny,
					Lifespan: prompting.LifespanSession,
					Duration: "5s",
				},
				"create": &prompting.PermissionEntry{
					Outcome:  prompting.OutcomeAllow,
					Lifespan: prompting.LifespanForever,
				},
			},
			iface:       "home",
			expectedErr: joinErrorsUnordered(joinErrorsUnordered(`invalid duration: cannot have unspecified duration when lifespan is "timespan": ""`, `invalid duration: cannot have specified duration when lifespan is "session":.*`), `invalid permissions for home interface: "create"`),
		},
	} {
		result, err := testCase.permissionMap.ToRulePermissionMap(testCase.iface, at)
		c.Check(err, ErrorMatches, testCase.expectedErr, Commentf("testCase: %+v", testCase))
		c.Check(result, IsNil)
	}
}

func joinErrorsUnordered(err1, err2 string) string {
	return fmt.Sprintf("(%s\n%s|%s\n%s)", err1, err2, err2, err1)
}

func (s *constraintsSuite) TestPermissionMapPatchRulePermissionsHappy(c *C) {
	origTime := time.Now()
	origSession := prompting.IDType(0x12345)
	patchTime := origTime.Add(time.Second)
	patchSession := prompting.IDType(0xf00ba4)
	patchAt := prompting.At{
		Time:      patchTime,
		SessionID: patchSession,
	}

	for i, testCase := range []struct {
		iface   string
		initial prompting.RulePermissionMap
		patch   prompting.PermissionMap
		final   prompting.RulePermissionMap
	}{
		{
			iface: "home",
			initial: prompting.RulePermissionMap{
				"write": &prompting.RulePermissionEntry{
					Outcome:    prompting.OutcomeDeny,
					Lifespan:   prompting.LifespanTimespan,
					Expiration: patchTime.Add(time.Second),
				},
			},
			patch: nil,
			final: prompting.RulePermissionMap{
				"write": &prompting.RulePermissionEntry{
					Outcome:    prompting.OutcomeDeny,
					Lifespan:   prompting.LifespanTimespan,
					Expiration: patchTime.Add(time.Second),
				},
			},
		},
		{
			iface: "home",
			initial: prompting.RulePermissionMap{
				"read": &prompting.RulePermissionEntry{
					Outcome:    prompting.OutcomeAllow,
					Lifespan:   prompting.LifespanTimespan,
					Expiration: patchTime.Add(time.Second),
				},
				"write": &prompting.RulePermissionEntry{
					Outcome:    prompting.OutcomeDeny,
					Lifespan:   prompting.LifespanTimespan,
					Expiration: origTime,
				},
			},
			patch: prompting.PermissionMap{
				// Remove read permissions, let write permission expire,
				// and add new execute permission
				"read": nil,
				"execute": &prompting.PermissionEntry{
					Outcome:  prompting.OutcomeDeny,
					Lifespan: prompting.LifespanTimespan,
					Duration: "1m",
				},
			},
			final: prompting.RulePermissionMap{
				"execute": &prompting.RulePermissionEntry{
					Outcome:    prompting.OutcomeDeny,
					Lifespan:   prompting.LifespanTimespan,
					Expiration: patchTime.Add(time.Minute),
				},
			},
		},
		{
			iface: "home",
			initial: prompting.RulePermissionMap{
				"read": &prompting.RulePermissionEntry{
					Outcome:   prompting.OutcomeAllow,
					Lifespan:  prompting.LifespanSession,
					SessionID: patchSession,
				},
				"write": &prompting.RulePermissionEntry{
					Outcome:   prompting.OutcomeDeny,
					Lifespan:  prompting.LifespanSession,
					SessionID: origSession,
				},
			},
			patch: prompting.PermissionMap{
				"execute": &prompting.PermissionEntry{
					Outcome:  prompting.OutcomeDeny,
					Lifespan: prompting.LifespanSession,
				},
			},
			final: prompting.RulePermissionMap{
				"read": &prompting.RulePermissionEntry{
					Outcome:   prompting.OutcomeAllow,
					Lifespan:  prompting.LifespanSession,
					SessionID: patchSession,
				},
				"execute": &prompting.RulePermissionEntry{
					Outcome:   prompting.OutcomeDeny,
					Lifespan:  prompting.LifespanSession,
					SessionID: patchSession,
				},
			},
		},
		{
			iface: "home",
			initial: prompting.RulePermissionMap{
				"read": &prompting.RulePermissionEntry{
					Outcome:  prompting.OutcomeAllow,
					Lifespan: prompting.LifespanForever,
				},
				"write": &prompting.RulePermissionEntry{
					Outcome:    prompting.OutcomeDeny,
					Lifespan:   prompting.LifespanTimespan,
					Expiration: origTime,
				},
				"execute": &prompting.RulePermissionEntry{
					Outcome:  prompting.OutcomeAllow,
					Lifespan: prompting.LifespanForever,
				},
			},
			patch: prompting.PermissionMap{
				"read": nil,
				"execute": &prompting.PermissionEntry{
					Outcome:  prompting.OutcomeAllow,
					Lifespan: prompting.LifespanSession,
				},
			},
			final: prompting.RulePermissionMap{
				"execute": &prompting.RulePermissionEntry{
					Outcome:   prompting.OutcomeAllow,
					Lifespan:  prompting.LifespanSession,
					SessionID: patchSession,
				},
			},
		},
		{
			iface: "camera",
			initial: prompting.RulePermissionMap{
				"access": &prompting.RulePermissionEntry{
					Outcome:  prompting.OutcomeAllow,
					Lifespan: prompting.LifespanForever,
				},
			},
			patch: prompting.PermissionMap{
				"access": &prompting.PermissionEntry{
					Outcome:  prompting.OutcomeAllow,
					Lifespan: prompting.LifespanSession,
				},
			},
			final: prompting.RulePermissionMap{
				"access": &prompting.RulePermissionEntry{
					Outcome:   prompting.OutcomeAllow,
					Lifespan:  prompting.LifespanSession,
					SessionID: patchSession,
				},
			},
		},
		{
			iface: "camera",
			initial: prompting.RulePermissionMap{
				"access": &prompting.RulePermissionEntry{
					Outcome:   prompting.OutcomeAllow,
					Lifespan:  prompting.LifespanSession,
					SessionID: patchSession,
				},
			},
			patch: nil,
			final: prompting.RulePermissionMap{
				"access": &prompting.RulePermissionEntry{
					Outcome:   prompting.OutcomeAllow,
					Lifespan:  prompting.LifespanSession,
					SessionID: patchSession,
				},
			},
		},
	} {
		result, err := testCase.patch.PatchRulePermissions(testCase.initial, testCase.iface, patchAt)
		c.Check(err, IsNil, Commentf("testCase %d: %+v", i, testCase))
		c.Check(result, DeepEquals, testCase.final, Commentf("testCase %d: %+v", i, testCase))
	}
}

func (s *constraintsSuite) TestPermissionMapPatchRulePermissionsUnhappy(c *C) {
	origTime := time.Now()
	patchTime := origTime.Add(time.Second)
	patchSession := prompting.IDType(0x12345)
	patchAt := prompting.At{
		Time:      patchTime,
		SessionID: patchSession,
	}

	goodInitial := prompting.RulePermissionMap{
		"read": &prompting.RulePermissionEntry{
			Outcome:    prompting.OutcomeAllow,
			Lifespan:   prompting.LifespanTimespan,
			Expiration: patchTime.Add(time.Second),
		},
		"write": &prompting.RulePermissionEntry{
			Outcome:    prompting.OutcomeDeny,
			Lifespan:   prompting.LifespanTimespan,
			Expiration: origTime,
		},
	}
	goodPatch := prompting.PermissionMap{
		"write": nil,
		"execute": &prompting.PermissionEntry{
			Outcome:  prompting.OutcomeDeny,
			Lifespan: prompting.LifespanSession,
		},
	}

	const goodIface = "home"
	const badIface = "foo"

	result, err := goodPatch.PatchRulePermissions(goodInitial, badIface, patchAt)
	c.Check(err, ErrorMatches, `invalid interface: "foo"`)
	c.Check(result, IsNil)

	badPatch := prompting.PermissionMap{
		"read": &prompting.PermissionEntry{
			Outcome:  prompting.OutcomeAllow,
			Lifespan: prompting.LifespanSingle,
		},
		"create": &prompting.PermissionEntry{
			Outcome:  prompting.OutcomeAllow,
			Lifespan: prompting.LifespanForever,
		},
		"lock": nil, // even if invalid permission is meant to be removed, error on it
		"execute": &prompting.PermissionEntry{
			Outcome:  prompting.OutcomeAllow,
			Lifespan: prompting.LifespanTimespan,
		},
	}
	expected := joinErrorsUnordered(`cannot create rule with lifespan "single"`, `invalid duration: cannot have unspecified duration when lifespan is "timespan": ""`) + "\n" + `invalid permissions for home interface: ("create", "lock"|"lock", "create")`
	result, err = badPatch.PatchRulePermissions(goodInitial, goodIface, patchAt)
	c.Check(err, ErrorMatches, expected)
	c.Check(result, IsNil)

	badPatch = prompting.PermissionMap{
		// Remove all permissions
		"read": nil,
	}
	result, err = badPatch.PatchRulePermissions(goodInitial, goodIface, patchAt)
	c.Check(err, Equals, prompting_errors.ErrPatchedRuleHasNoPerms)
	c.Check(result, IsNil)
}

func (s *constraintsSuite) TestRulePermissionMapValidateForInterfaceHappy(c *C) {
	at := prompting.At{
		Time:      time.Now(),
		SessionID: prompting.IDType(0x12345),
	}
	for _, testCase := range []struct {
		iface       string
		permissions prompting.RulePermissionMap
		expired     bool
	}{
		{
			iface: "home",
			permissions: prompting.RulePermissionMap{
				"read": &prompting.RulePermissionEntry{
					Outcome:  prompting.OutcomeAllow,
					Lifespan: prompting.LifespanForever,
				},
				"write": &prompting.RulePermissionEntry{
					Outcome:    prompting.OutcomeDeny,
					Lifespan:   prompting.LifespanTimespan,
					Expiration: at.Time.Add(time.Second),
				},
				"execute": &prompting.RulePermissionEntry{
					Outcome:   prompting.OutcomeAllow,
					Lifespan:  prompting.LifespanSession,
					SessionID: at.SessionID,
				},
			},
			expired: false,
		},
		{
			iface: "home",
			permissions: prompting.RulePermissionMap{
				"read": &prompting.RulePermissionEntry{
					Outcome:  prompting.OutcomeAllow,
					Lifespan: prompting.LifespanForever,
				},
				"write": &prompting.RulePermissionEntry{
					Outcome:    prompting.OutcomeDeny,
					Lifespan:   prompting.LifespanTimespan,
					Expiration: at.Time,
				},
				"execute": &prompting.RulePermissionEntry{
					Outcome:   prompting.OutcomeAllow,
					Lifespan:  prompting.LifespanSession,
					SessionID: at.SessionID + 1,
				},
			},
			expired: false,
		},
		{
			iface: "home",
			permissions: prompting.RulePermissionMap{
				"read": &prompting.RulePermissionEntry{
					Outcome:    prompting.OutcomeAllow,
					Lifespan:   prompting.LifespanTimespan,
					Expiration: at.Time.Add(-time.Hour),
				},
				"write": &prompting.RulePermissionEntry{
					Outcome:    prompting.OutcomeDeny,
					Lifespan:   prompting.LifespanTimespan,
					Expiration: at.Time,
				},
				"execute": &prompting.RulePermissionEntry{
					Outcome:   prompting.OutcomeAllow,
					Lifespan:  prompting.LifespanSession,
					SessionID: at.SessionID + 1,
				},
			},
			expired: true,
		},
		{
			iface: "camera",
			permissions: prompting.RulePermissionMap{
				"access": &prompting.RulePermissionEntry{
					Outcome:   prompting.OutcomeDeny,
					Lifespan:  prompting.LifespanSession,
					SessionID: at.SessionID,
				},
			},
			expired: false,
		},
		{
			iface: "camera",
			permissions: prompting.RulePermissionMap{
				"access": &prompting.RulePermissionEntry{
					Outcome:    prompting.OutcomeDeny,
					Lifespan:   prompting.LifespanTimespan,
					Expiration: at.Time,
				},
			},
			expired: true,
		},
	} {
		expired, err := testCase.permissions.ValidateForInterface(testCase.iface, at)
		c.Check(err, IsNil, Commentf("testCase: %+v", testCase))
		c.Check(expired, Equals, testCase.expired, Commentf("testCase: %+v", testCase))
	}
}

func (s *constraintsSuite) TestRulePermissionMapValidateForInterfaceUnhappy(c *C) {
	at := prompting.At{
		Time:      time.Now(),
		SessionID: prompting.IDType(0x12345),
	}
	for _, testCase := range []struct {
		iface       string
		permissions prompting.RulePermissionMap
		expectedErr string
	}{
		{
			"foo",
			prompting.RulePermissionMap{
				"read": &prompting.RulePermissionEntry{
					Outcome:  prompting.OutcomeAllow,
					Lifespan: prompting.LifespanForever,
				},
			},
			prompting_errors.NewInvalidInterfaceError("foo", nil).Error(),
		},
		{
			"home",
			prompting.RulePermissionMap{},
			prompting_errors.NewPermissionsEmptyError("home", nil).Error(),
		},
		{
			"camera",
			prompting.RulePermissionMap{},
			prompting_errors.NewPermissionsEmptyError("camera", nil).Error(),
		},
		{
			"home",
			prompting.RulePermissionMap{
				"access": &prompting.RulePermissionEntry{
					Outcome:  prompting.OutcomeAllow,
					Lifespan: prompting.LifespanForever,
				},
			},
			prompting_errors.NewInvalidPermissionsError("home", []string{"access"}, []string{"read", "write", "execute"}).Error(),
		},
		{
			"camera",
			prompting.RulePermissionMap{
				"read": &prompting.RulePermissionEntry{
					Outcome:   prompting.OutcomeAllow,
					Lifespan:  prompting.LifespanSession,
					SessionID: at.SessionID,
				},
			},
			prompting_errors.NewInvalidPermissionsError("camera", []string{"read"}, []string{"access"}).Error(),
		},
		{
			"home",
			prompting.RulePermissionMap{
				"read": &prompting.RulePermissionEntry{
					Outcome:    prompting.OutcomeAllow,
					Lifespan:   prompting.LifespanForever,
					Expiration: time.Now().Add(time.Second),
				},
			},
			"invalid expiration: cannot have specified expiration.*",
		},
		{
			"camera",
			prompting.RulePermissionMap{
				"access": &prompting.RulePermissionEntry{
					Outcome:   prompting.OutcomeAllow,
					Lifespan:  prompting.LifespanForever,
					SessionID: at.SessionID,
				},
			},
			"invalid expiration: cannot have specified session ID.*",
		},
		{
			"home",
			prompting.RulePermissionMap{
				"read": &prompting.RulePermissionEntry{
					Outcome:  prompting.OutcomeAllow,
					Lifespan: prompting.LifespanSingle,
				},
			},
			`cannot create rule with lifespan "single"`,
		},
		{
			"camera",
			prompting.RulePermissionMap{
				"access": &prompting.RulePermissionEntry{
					Outcome:    prompting.OutcomeType("bar"),
					Lifespan:   prompting.LifespanTimespan,
					Expiration: at.Time.Add(-time.Second),
				},
			},
			`invalid outcome: "bar"`,
		},
	} {
		expired, err := testCase.permissions.ValidateForInterface(testCase.iface, at)
		c.Check(err, ErrorMatches, testCase.expectedErr, Commentf("testCase: %+v", testCase))
		c.Check(expired, Equals, false)
	}
}

func (s *constraintsSuite) TestRulePermissionMapExpired(c *C) {
	// XXX: should this be moved to TestRulePermissionEntryExpired instead?
	at := prompting.At{
		Time:      time.Now(),
		SessionID: prompting.IDType(0x12345),
	}
	for _, pm := range []prompting.RulePermissionMap{
		{},
		{
			"foo": &prompting.RulePermissionEntry{
				Outcome:    prompting.OutcomeAllow,
				Lifespan:   prompting.LifespanTimespan,
				Expiration: at.Time,
			},
		},
		{
			"bar": &prompting.RulePermissionEntry{
				Outcome:   prompting.OutcomeDeny,
				Lifespan:  prompting.LifespanSession,
				SessionID: at.SessionID + 1,
			},
		},
		{
			"foo": &prompting.RulePermissionEntry{
				Outcome:    prompting.OutcomeAllow,
				Lifespan:   prompting.LifespanTimespan,
				Expiration: at.Time.Add(-time.Second),
			},
			"bar": &prompting.RulePermissionEntry{
				Outcome:    prompting.OutcomeDeny,
				Lifespan:   prompting.LifespanTimespan,
				Expiration: at.Time,
			},
			"baz": &prompting.RulePermissionEntry{
				Outcome:   prompting.OutcomeAllow,
				Lifespan:  prompting.LifespanSession,
				SessionID: prompting.IDType(0xf00),
			},
		},
	} {
		c.Check(pm.Expired(at), Equals, true, Commentf("%+v", pm))
	}

	for _, pm := range []prompting.RulePermissionMap{
		{
			"foo": &prompting.RulePermissionEntry{
				Outcome:    prompting.OutcomeAllow,
				Lifespan:   prompting.LifespanTimespan,
				Expiration: at.Time.Add(-time.Second),
			},
			"bar": &prompting.RulePermissionEntry{
				Outcome:  prompting.OutcomeDeny,
				Lifespan: prompting.LifespanForever,
			},
		},
		{
			"foo": &prompting.RulePermissionEntry{
				Outcome:   prompting.OutcomeAllow,
				Lifespan:  prompting.LifespanSession,
				SessionID: prompting.IDType(0xabcd),
			},
			"bar": &prompting.RulePermissionEntry{
				Outcome:  prompting.OutcomeDeny,
				Lifespan: prompting.LifespanForever,
			},
		},
		{
			"foo": &prompting.RulePermissionEntry{
				Outcome:    prompting.OutcomeAllow,
				Lifespan:   prompting.LifespanTimespan,
				Expiration: at.Time.Add(time.Second),
			},
		},
		{
			"foo": &prompting.RulePermissionEntry{
				Outcome:   prompting.OutcomeAllow,
				Lifespan:  prompting.LifespanSession,
				SessionID: at.SessionID,
			},
		},
	} {
		c.Check(pm.Expired(at), Equals, false, Commentf("%+v", pm))
	}
}

func (s *constraintsSuite) TestRulePermissionMapMergePermissionMap(c *C) {
	c.Fatalf("TODO")
}

func (s *constraintsSuite) TestRulePermissionEntryMarshalJSON(c *C) {
	if runtime.Version() < "go1.24" {
		c.Skip("omitzero requires go version 1.24 or higher")
	}
	timeNow := time.Date(2025, time.February, 20, 16, 0, 27, 913561089, time.UTC)
	for _, testCase := range []struct {
		entry    prompting.RulePermissionEntry
		expected string
	}{
		{
			entry: prompting.RulePermissionEntry{
				Outcome:  prompting.OutcomeAllow,
				Lifespan: prompting.LifespanForever,
			},
			expected: `{"outcome":"allow","lifespan":"forever"}`,
		},
		{
			entry: prompting.RulePermissionEntry{
				Outcome:    prompting.OutcomeDeny,
				Lifespan:   prompting.LifespanTimespan,
				Expiration: timeNow,
			},
			expected: `{"outcome":"deny","lifespan":"timespan","expiration":"2025-02-20T16:00:27.913561089Z"}`,
		},
		{
			entry: prompting.RulePermissionEntry{
				Outcome:   prompting.OutcomeAllow,
				Lifespan:  prompting.LifespanSession,
				SessionID: prompting.IDType(0x12345678),
			},
			expected: `{"outcome":"allow","lifespan":"session","session-id":"0000000012345678"}`,
		},
	} {
		marshalled, err := json.Marshal(testCase.entry)
		c.Check(err, IsNil, Commentf("testCase: %+v", testCase))
		c.Check(string(marshalled), Equals, testCase.expected, Commentf("testCase: %+v", testCase))
	}
}

func (s *constraintsSuite) TestRulePermissionEntrySupersedes(c *C) {
	currTime := time.Now()
	currSession := prompting.IDType(0x12345)
	for _, testCase := range []struct {
		entry    *prompting.RulePermissionEntry
		other    *prompting.RulePermissionEntry
		expected bool
	}{
		{
			entry: &prompting.RulePermissionEntry{
				Lifespan: prompting.LifespanForever,
			},
			other: &prompting.RulePermissionEntry{
				Lifespan: prompting.LifespanForever,
			},
			expected: false,
		},
		{
			entry: &prompting.RulePermissionEntry{
				Lifespan: prompting.LifespanForever,
			},
			other: &prompting.RulePermissionEntry{
				Lifespan:   prompting.LifespanTimespan,
				Expiration: currTime.Add(time.Second),
			},
			expected: true,
		},
		{
			entry: &prompting.RulePermissionEntry{
				Lifespan: prompting.LifespanForever,
			},
			other: &prompting.RulePermissionEntry{
				Lifespan:  prompting.LifespanSession,
				SessionID: currSession,
			},
			expected: true,
		},
		{
			entry: &prompting.RulePermissionEntry{
				Lifespan: prompting.LifespanForever,
			},
			other: &prompting.RulePermissionEntry{
				Lifespan: prompting.LifespanSingle,
			},
			expected: true,
		},
		{
			entry: &prompting.RulePermissionEntry{
				Lifespan:  prompting.LifespanSession,
				SessionID: currSession,
			},
			other: &prompting.RulePermissionEntry{
				Lifespan: prompting.LifespanForever,
			},
			expected: false,
		},
		{
			entry: &prompting.RulePermissionEntry{
				Lifespan:  prompting.LifespanSession,
				SessionID: currSession,
			},
			other: &prompting.RulePermissionEntry{
				Lifespan:  prompting.LifespanSession,
				SessionID: currSession,
			},
			expected: false,
		},
		{
			entry: &prompting.RulePermissionEntry{
				Lifespan:  prompting.LifespanSession,
				SessionID: currSession,
			},
			other: &prompting.RulePermissionEntry{
				Lifespan:  prompting.LifespanSession,
				SessionID: currSession + 1,
			},
			expected: true,
		},
		{
			entry: &prompting.RulePermissionEntry{
				Lifespan:  prompting.LifespanSession,
				SessionID: currSession + 1,
			},
			other: &prompting.RulePermissionEntry{
				Lifespan:  prompting.LifespanSession,
				SessionID: currSession,
			},
			expected: false,
		},
		{
			// An expired session never supersedes another
			entry: &prompting.RulePermissionEntry{
				Lifespan:  prompting.LifespanSession,
				SessionID: currSession + 1,
			},
			other: &prompting.RulePermissionEntry{
				Lifespan:  prompting.LifespanSession,
				SessionID: currSession + 1,
			},
			expected: false,
		},
		{
			entry: &prompting.RulePermissionEntry{
				Lifespan:  prompting.LifespanSession,
				SessionID: currSession,
			},
			other: &prompting.RulePermissionEntry{
				Lifespan:   prompting.LifespanTimespan,
				Expiration: currTime.Add(time.Second),
			},
			expected: true,
		},
		{
			// An expired session never supersedes another
			entry: &prompting.RulePermissionEntry{
				Lifespan:  prompting.LifespanSession,
				SessionID: currSession + 1,
			},
			other: &prompting.RulePermissionEntry{
				Lifespan:   prompting.LifespanTimespan,
				Expiration: currTime,
			},
			expected: false,
		},
		{
			entry: &prompting.RulePermissionEntry{
				Lifespan:  prompting.LifespanSession,
				SessionID: currSession,
			},
			other: &prompting.RulePermissionEntry{
				Lifespan: prompting.LifespanSingle,
			},
			expected: true,
		},
		{
			// An expired session never supersedes another
			entry: &prompting.RulePermissionEntry{
				Lifespan:  prompting.LifespanSession,
				SessionID: currSession + 1,
			},
			other: &prompting.RulePermissionEntry{
				Lifespan: prompting.LifespanSingle,
			},
			expected: false,
		},
		{
			entry: &prompting.RulePermissionEntry{
				Lifespan:   prompting.LifespanTimespan,
				Expiration: currTime.Add(time.Second),
			},
			other: &prompting.RulePermissionEntry{
				Lifespan: prompting.LifespanForever,
			},
			expected: false,
		},
		{
			// LifespanTimespan doesn't supersede LifespanSession with active session
			entry: &prompting.RulePermissionEntry{
				Lifespan:   prompting.LifespanTimespan,
				Expiration: currTime.Add(time.Second),
			},
			other: &prompting.RulePermissionEntry{
				Lifespan:  prompting.LifespanSession,
				SessionID: currSession,
			},
			expected: false,
		},
		{
			// LifespanTimespan does supersede LifespanSession with expired session
			entry: &prompting.RulePermissionEntry{
				Lifespan:   prompting.LifespanTimespan,
				Expiration: currTime,
			},
			other: &prompting.RulePermissionEntry{
				Lifespan:  prompting.LifespanSession,
				SessionID: currSession + 1,
			},
			expected: true,
		},
		{
			// Later expiration supersedes earlier, regardless of whether either is expired
			entry: &prompting.RulePermissionEntry{
				Lifespan:   prompting.LifespanTimespan,
				Expiration: currTime,
			},
			other: &prompting.RulePermissionEntry{
				Lifespan:   prompting.LifespanTimespan,
				Expiration: currTime.Add(-time.Second),
			},
			expected: true,
		},
		{
			entry: &prompting.RulePermissionEntry{
				Lifespan:   prompting.LifespanTimespan,
				Expiration: currTime,
			},
			other: &prompting.RulePermissionEntry{
				Lifespan:   prompting.LifespanTimespan,
				Expiration: currTime,
			},
			expected: false,
		},
		{
			entry: &prompting.RulePermissionEntry{
				Lifespan:   prompting.LifespanTimespan,
				Expiration: currTime,
			},
			other: &prompting.RulePermissionEntry{
				Lifespan:   prompting.LifespanTimespan,
				Expiration: currTime.Add(time.Second),
			},
			expected: false,
		},
		{
			entry: &prompting.RulePermissionEntry{
				Lifespan:   prompting.LifespanTimespan,
				Expiration: currTime,
			},
			other: &prompting.RulePermissionEntry{
				Lifespan: prompting.LifespanSingle,
			},
			expected: true,
		},
		{
			entry: &prompting.RulePermissionEntry{
				Lifespan: prompting.LifespanSingle,
			},
			other: &prompting.RulePermissionEntry{
				Lifespan: prompting.LifespanForever,
			},
			expected: false,
		},
		{
			entry: &prompting.RulePermissionEntry{
				Lifespan: prompting.LifespanSingle,
			},
			other: &prompting.RulePermissionEntry{
				Lifespan:  prompting.LifespanSession,
				SessionID: currSession,
			},
			expected: false,
		},
		{
			// LifespanSingle supersedes LifespanSession with expired session
			entry: &prompting.RulePermissionEntry{
				Lifespan: prompting.LifespanSingle,
			},
			other: &prompting.RulePermissionEntry{
				Lifespan:  prompting.LifespanSession,
				SessionID: currSession + 1,
			},
			expected: true,
		},
		{
			entry: &prompting.RulePermissionEntry{
				Lifespan: prompting.LifespanSingle,
			},
			other: &prompting.RulePermissionEntry{
				Lifespan:   prompting.LifespanTimespan,
				Expiration: currTime,
			},
			expected: false,
		},
		{
			entry: &prompting.RulePermissionEntry{
				Lifespan: prompting.LifespanSingle,
			},
			other: &prompting.RulePermissionEntry{
				Lifespan: prompting.LifespanSingle,
			},
			expected: false,
		},
	} {
		c.Check(testCase.entry.Supersedes(testCase.other, currSession), Equals, testCase.expected, Commentf("testCase:\n\tentry: %+v\n\tother: %+v\n\texpected: %v", testCase.entry, testCase.other, testCase.expected))
	}

	// Check that LifespanSession when current session ID is 0 supersedes
	// nothing and is superseded by everything.
	expiredSession := currSession
	currSession = 0
	for _, lifespan := range []prompting.LifespanType{
		prompting.LifespanSingle,
		prompting.LifespanTimespan,
		prompting.LifespanSession,
		prompting.LifespanForever,
	} {
		entry := &prompting.RulePermissionEntry{
			Lifespan:  prompting.LifespanSession,
			SessionID: expiredSession,
		}
		other := &prompting.RulePermissionEntry{
			Lifespan: lifespan,
		}
		c.Check(entry.Supersedes(other, currSession), Equals, false, Commentf("LifespanSession with expired session incorrectly superseded entry with lifespan %s", lifespan))
	}
	for _, lifespan := range []prompting.LifespanType{
		prompting.LifespanSingle,
		prompting.LifespanTimespan,
		// there can't be an entry with LifespanSession and SessionID = 0
		prompting.LifespanForever,
	} {
		entry := &prompting.RulePermissionEntry{
			Lifespan: lifespan,
		}
		other := &prompting.RulePermissionEntry{
			Lifespan:  prompting.LifespanSession,
			SessionID: expiredSession,
		}
		c.Check(entry.Supersedes(other, currSession), Equals, true, Commentf("LifespanSession with expired session was incorrectly not superseded by entry with lifespan %s", lifespan))
	}
}

func constructPermissionsMaps() []map[string]map[string]notify.AppArmorPermission {
	var permissionsMaps []map[string]map[string]notify.AppArmorPermission
	// interfaceFilePermissionsMaps
	filePermissionsMaps := make(map[string]map[string]notify.AppArmorPermission)
	for iface, permsMap := range prompting.InterfaceFilePermissionsMaps {
		filePermissionsMaps[iface] = make(map[string]notify.AppArmorPermission, len(permsMap))
		for perm, val := range permsMap {
			filePermissionsMaps[iface][perm] = val
		}
	}
	permissionsMaps = append(permissionsMaps, filePermissionsMaps)
	// TODO: do the same for other maps of permissions maps in the future
	return permissionsMaps
}

func (s *constraintsSuite) TestInterfacesAndPermissionsCompleteness(c *C) {
	permissionsMaps := constructPermissionsMaps()
	// Check that every interface in interfacePermissionsAvailable is in
	// exactly one of the permissions maps.
	// Also, check that the permissions for a given interface in
	// interfacePermissionsAvailable are identical to the permissions in the
	// interface's permissions map.
	// Also, check that each priority only occurs once.
	for iface, perms := range prompting.InterfacePermissionsAvailable {
		availablePerms, err := prompting.AvailablePermissions(iface)
		c.Check(err, IsNil, Commentf("interface missing from interfacePermissionsAvailable: %s", iface))
		c.Check(perms, Not(HasLen), 0, Commentf("interface has no available permissions: %s", iface))
		c.Check(availablePerms, DeepEquals, perms)
		found := false
		for _, permsMaps := range permissionsMaps {
			pMap, exists := permsMaps[iface]
			if !exists {
				continue
			}
			c.Check(found, Equals, false, Commentf("interface found in more than one map of interface permissions maps: %s", iface))
			found = true
			// Check that permissions in the list and map are identical
			c.Check(pMap, HasLen, len(perms), Commentf("permissions list and map inconsistent for interface: %s", iface))
			for _, perm := range perms {
				_, exists := pMap[perm]
				c.Check(exists, Equals, true, Commentf("missing permission mapping for %s interface permission: %s", iface, perm))
			}
		}
		if !found {
			c.Errorf("interface not included in any map of interface permissions maps: %s", iface)
		}
	}
}

func (s *constraintsSuite) TestInterfaceFilePermissionsMapsCorrectness(c *C) {
	for iface, permsMap := range prompting.InterfaceFilePermissionsMaps {
		seenPermissions := notify.FilePermission(0)
		for name, mask := range permsMap {
			if duplicate := seenPermissions & mask; duplicate != notify.FilePermission(0) {
				c.Errorf("AppArmor file permission found in more than one permission map for %s interface: %s", iface, duplicate.String())
			}
			c.Check(mask&notify.AA_MAY_OPEN, Equals, notify.FilePermission(0), Commentf("AA_MAY_OPEN may not be included in permissions maps, but %s interface includes it in the map for permission: %s", iface, name))
			seenPermissions |= mask
		}
	}
}

func (s *constraintsSuite) TestAvailablePermissions(c *C) {
	for iface, perms := range prompting.InterfacePermissionsAvailable {
		available, err := prompting.AvailablePermissions(iface)
		c.Check(err, IsNil)
		c.Check(available, DeepEquals, perms)
	}
	available, err := prompting.AvailablePermissions("foo")
	c.Check(err, ErrorMatches, ".*unsupported interface.*")
	c.Check(available, IsNil)
}

func (s *constraintsSuite) TestAbstractPermissionsFromAppArmorPermissionsHappy(c *C) {
	cases := []struct {
		iface string
		perms notify.AppArmorPermission
		list  []string
	}{
		{
			"home",
			notify.AA_MAY_READ,
			[]string{"read"},
		},
		{
			"home",
			notify.AA_MAY_WRITE | notify.AA_MAY_APPEND | notify.AA_MAY_CREATE | notify.AA_MAY_DELETE | notify.AA_MAY_RENAME | notify.AA_MAY_CHMOD | notify.AA_MAY_LOCK | notify.AA_MAY_LINK,
			[]string{"write"},
		},
		{
			"home",
			notify.AA_MAY_EXEC | notify.AA_EXEC_MMAP,
			[]string{"execute"},
		},
		{
			"home",
			notify.AA_MAY_OPEN,
			[]string{"read"},
		},
		{
			"home",
			notify.AA_MAY_OPEN | notify.AA_MAY_WRITE,
			[]string{"write"},
		},
		{
			"home",
			notify.AA_MAY_EXEC | notify.AA_MAY_WRITE | notify.AA_MAY_READ,
			[]string{"read", "write", "execute"},
		},
		{
			"camera",
			notify.AA_MAY_OPEN,
			[]string{"access"},
		},
		{
			"camera",
			notify.AA_MAY_READ | notify.AA_MAY_OPEN,
			[]string{"access"},
		},
		{
			"camera",
			notify.AA_MAY_READ | notify.AA_MAY_GETATTR | notify.AA_MAY_WRITE | notify.AA_MAY_APPEND,
			[]string{"access"},
		},
	}
	for _, testCase := range cases {
		perms, err := prompting.AbstractPermissionsFromAppArmorPermissions(testCase.iface, testCase.perms)
		c.Check(err, IsNil, Commentf("testCase: %+v", testCase))
		c.Check(perms, DeepEquals, testCase.list)
	}
}

type fakeAaPerm string

func (p fakeAaPerm) AsAppArmorOpMask() uint32 {
	return uint32(len(p)) // deliberately gratuitously meaningless
}

func (p fakeAaPerm) String() string {
	return string(p)
}

func (s *constraintsSuite) TestAbstractPermissionsFromAppArmorPermissionsUnhappy(c *C) {
	for _, testCase := range []struct {
		iface  string
		perms  notify.AppArmorPermission
		errStr string
	}{
		{
			"home",
			fakeAaPerm("not a file permission"),
			"cannot parse the given permissions as file permissions.*",
		},
		{
			"home",
			notify.FilePermission(0),
			"cannot get abstract permissions from empty AppArmor permissions.*",
		},
		{
			"camera",
			fakeAaPerm("not a file permission"),
			"cannot parse the given permissions as file permissions.*",
		},
		{
			"camera",
			notify.FilePermission(0),
			"cannot get abstract permissions from empty AppArmor permissions.*",
		},
		{
			"foo",
			notify.AA_MAY_READ,
			"cannot map the given interface to list of available permissions.*",
		},
	} {
		perms, err := prompting.AbstractPermissionsFromAppArmorPermissions(testCase.iface, testCase.perms)
		c.Check(perms, IsNil, Commentf("received unexpected non-nil permissions list for test case: %+v", testCase))
		c.Check(err, ErrorMatches, testCase.errStr)
	}
	for _, testCase := range []struct {
		iface    string
		perms    notify.AppArmorPermission
		abstract []string
		errStr   string
	}{
		{
			"home",
			notify.FilePermission(1 << 17),
			[]string{},
			` cannot map AppArmor permission to abstract permission for the home interface: "0x20000"`,
		},
		{
			"home",
			notify.AA_MAY_GETCRED | notify.AA_MAY_READ,
			[]string{"read"},
			` cannot map AppArmor permission to abstract permission for the home interface: "get-cred"`,
		},
		{
			"camera",
			notify.AA_MAY_EXEC,
			[]string{},
			` cannot map AppArmor permission to abstract permission for the camera interface: "execute"`,
		},
	} {
		logbuf, restore := logger.MockLogger()
		defer restore()
		perms, err := prompting.AbstractPermissionsFromAppArmorPermissions(testCase.iface, testCase.perms)
		c.Check(err, IsNil)
		c.Check(perms, DeepEquals, testCase.abstract)
		c.Check(logbuf.String(), testutil.Contains, testCase.errStr)
	}
}

func (s *constraintsSuite) TestAbstractPermissionsToAppArmorPermissionsHappy(c *C) {
	cases := []struct {
		iface string
		list  []string
		perms notify.AppArmorPermission
	}{
		{
			"home",
			[]string{},
			notify.FilePermission(0),
		},
		{
			"camera",
			[]string{},
			notify.FilePermission(0),
		},
		{
			"home",
			[]string{"read"},
			notify.AA_MAY_OPEN | notify.AA_MAY_READ | notify.AA_MAY_GETATTR,
		},
		{
			"home",
			[]string{"write"},
			notify.AA_MAY_OPEN | notify.AA_MAY_WRITE | notify.AA_MAY_APPEND | notify.AA_MAY_CREATE | notify.AA_MAY_DELETE | notify.AA_MAY_RENAME | notify.AA_MAY_SETATTR | notify.AA_MAY_CHMOD | notify.AA_MAY_LOCK | notify.AA_MAY_LINK,
		},
		{
			"home",
			[]string{"execute"},
			notify.AA_MAY_OPEN | notify.AA_MAY_EXEC | notify.AA_EXEC_MMAP,
		},
		{
			"home",
			[]string{"read", "execute"},
			notify.AA_MAY_OPEN | notify.AA_MAY_READ | notify.AA_MAY_GETATTR | notify.AA_MAY_EXEC | notify.AA_EXEC_MMAP,
		},
		{
			"home",
			[]string{"execute", "write", "read"},
			notify.AA_MAY_OPEN | notify.AA_MAY_READ | notify.AA_MAY_GETATTR | notify.AA_MAY_EXEC | notify.AA_EXEC_MMAP | notify.AA_MAY_WRITE | notify.AA_MAY_APPEND | notify.AA_MAY_CREATE | notify.AA_MAY_DELETE | notify.AA_MAY_RENAME | notify.AA_MAY_SETATTR | notify.AA_MAY_CHMOD | notify.AA_MAY_LOCK | notify.AA_MAY_LINK,
		},
		{
			"camera",
			[]string{"access"},
			notify.AA_MAY_OPEN | notify.AA_MAY_READ | notify.AA_MAY_GETATTR | notify.AA_MAY_WRITE | notify.AA_MAY_APPEND,
		},
	}
	for _, testCase := range cases {
		ret, err := prompting.AbstractPermissionsToAppArmorPermissions(testCase.iface, testCase.list)
		c.Check(err, IsNil)
		perms, ok := ret.(notify.FilePermission)
		c.Check(ok, Equals, true, Commentf("failed to parse return value as FilePermission for test case: %+v", testCase))
		c.Check(perms, Equals, testCase.perms)
	}
}

func (s *constraintsSuite) TestAbstractPermissionsToAppArmorPermissionsUnhappy(c *C) {
	cases := []struct {
		iface  string
		perms  []string
		errStr string
	}{
		{
			"foo",
			[]string{"read"},
			"cannot map the given interface to map from abstract permissions to AppArmor permissions.*",
		},
		{
			"home",
			[]string{"foo"},
			"cannot map abstract permission to AppArmor permissions for the home interface.*",
		},
		{
			"home",
			[]string{"access"},
			"cannot map abstract permission to AppArmor permissions for the home interface.*",
		},
		{
			"home",
			[]string{"read", "foo", "write"},
			"cannot map abstract permission to AppArmor permissions for the home interface.*",
		},
		{
			"camera",
			[]string{"access", "read"},
			"cannot map abstract permission to AppArmor permissions for the camera interface.*",
		},
	}
	for _, testCase := range cases {
		_, err := prompting.AbstractPermissionsToAppArmorPermissions(testCase.iface, testCase.perms)
		c.Check(err, ErrorMatches, testCase.errStr)
	}
}
