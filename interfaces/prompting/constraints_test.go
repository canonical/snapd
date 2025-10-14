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
	"github.com/snapcore/snapd/testutil"
)

type constraintsSuite struct{}

var _ = Suite(&constraintsSuite{})

func mustParsePathPattern(c *C, patternStr string) *patterns.PathPattern {
	pattern, err := patterns.ParsePathPattern(patternStr)
	c.Assert(err, IsNil)
	return pattern
}

func (s *constraintsSuite) TestParseInterfaceSpecificConstraintsHappy(c *C) {
	for _, testCase := range []struct {
		iface               string
		constraintsJSON     prompting.ConstraintsJSON
		isPatch             bool
		expected            prompting.InterfaceSpecificConstraints
		expectedPathPattern *patterns.PathPattern
	}{
		{
			iface: "home",
			constraintsJSON: prompting.ConstraintsJSON{
				"path-pattern": json.RawMessage(`"/home/test/foo"`),
			},
			isPatch: false,
			expected: &prompting.InterfaceSpecificConstraintsHome{
				Pattern: mustParsePathPattern(c, "/home/test/foo"),
			},
			expectedPathPattern: mustParsePathPattern(c, "/home/test/foo"),
		},
		{
			iface: "home",
			constraintsJSON: prompting.ConstraintsJSON{
				"path-pattern": json.RawMessage(`"/home/me/**"`),
				"foo":          json.RawMessage(`"bar"`),
			},
			isPatch: false,
			expected: &prompting.InterfaceSpecificConstraintsHome{
				Pattern: mustParsePathPattern(c, "/home/me/**"),
			},
			expectedPathPattern: mustParsePathPattern(c, "/home/me/**"),
		},
		{
			iface:               "home",
			constraintsJSON:     prompting.ConstraintsJSON{},
			isPatch:             true,
			expected:            &prompting.InterfaceSpecificConstraintsHome{},
			expectedPathPattern: nil,
		},
		{
			iface: "home",
			constraintsJSON: prompting.ConstraintsJSON{
				"path-pattern": json.RawMessage(`"/home/you/**/*.pdf"`),
				"fizz":         json.RawMessage(`"buzz"`),
			},
			isPatch: true,
			expected: &prompting.InterfaceSpecificConstraintsHome{
				Pattern: mustParsePathPattern(c, "/home/you/**/*.pdf"),
			},
			expectedPathPattern: mustParsePathPattern(c, "/home/you/**/*.pdf"),
		},
		{
			iface:               "camera",
			constraintsJSON:     prompting.ConstraintsJSON{},
			isPatch:             false,
			expected:            &prompting.InterfaceSpecificConstraintsCamera{},
			expectedPathPattern: mustParsePathPattern(c, "/**"),
		},
		{
			iface:               "camera",
			constraintsJSON:     prompting.ConstraintsJSON{},
			isPatch:             true,
			expected:            &prompting.InterfaceSpecificConstraintsCamera{},
			expectedPathPattern: mustParsePathPattern(c, "/**"),
		},
		{
			iface:               "camera",
			constraintsJSON:     prompting.ConstraintsJSON{"foo": json.RawMessage(`"bar"`)},
			isPatch:             false,
			expected:            &prompting.InterfaceSpecificConstraintsCamera{},
			expectedPathPattern: mustParsePathPattern(c, "/**"),
		},
		{
			iface:               "camera",
			constraintsJSON:     prompting.ConstraintsJSON{"foo": json.RawMessage(`"bar"`)},
			isPatch:             false,
			expected:            &prompting.InterfaceSpecificConstraintsCamera{},
			expectedPathPattern: mustParsePathPattern(c, "/**"),
		},
	} {
		result, err := prompting.ParseInterfaceSpecificConstraints(testCase.iface, testCase.constraintsJSON, testCase.isPatch)
		c.Check(err, IsNil, Commentf("testCase: %+v", testCase))
		c.Check(result, DeepEquals, testCase.expected, Commentf("testCase: %+v", testCase))
		c.Check(prompting.InterfaceSpecificConstraintsPathPattern(result), DeepEquals, testCase.expectedPathPattern, Commentf("testCase: %+v", testCase))
	}
}

func (s *constraintsSuite) TestParseInterfaceSpecificConstraintsUnhappy(c *C) {
	for _, testCase := range []struct {
		iface           string
		constraintsJSON prompting.ConstraintsJSON
		isPatch         bool
		expectedErr     string
	}{
		{
			iface: "notaninterface",
			constraintsJSON: prompting.ConstraintsJSON{
				"path-pattern": json.RawMessage(`"/home/test/foo"`),
			},
			isPatch:     false,
			expectedErr: `invalid interface: "notaninterface"`,
		},
		{
			iface:           "home",
			constraintsJSON: prompting.ConstraintsJSON{},
			isPatch:         false,
			expectedErr:     `invalid path pattern: no path pattern: ""`,
		},
		{
			iface: "home",
			constraintsJSON: prompting.ConstraintsJSON{
				"path-pattern": json.RawMessage(`"invalid-pattern"`),
			},
			isPatch:     false,
			expectedErr: `invalid path pattern: pattern must start with '/': "invalid-pattern"`,
		},
	} {
		result, err := prompting.ParseInterfaceSpecificConstraints(testCase.iface, testCase.constraintsJSON, testCase.isPatch)
		c.Check(result, IsNil, Commentf("testCase: %+v", testCase))
		c.Check(err, ErrorMatches, testCase.expectedErr, Commentf("testCase: %+v", testCase))
	}
}

func (s *constraintsSuite) TestUnmarshalConstraintsHappy(c *C) {
	for _, testCase := range []struct {
		iface           string
		constraintsJSON prompting.ConstraintsJSON
		expected        *prompting.Constraints
		expectedPattern *patterns.PathPattern
	}{
		{
			iface: "home",
			constraintsJSON: prompting.ConstraintsJSON{
				"path-pattern": json.RawMessage(`"/home/test/foo"`),
				"permissions":  json.RawMessage(`{"read":{"outcome":"allow","lifespan":"forever"},"write":{"outcome":"deny","lifespan":"timespan","duration":"1h"}}`),
			},
			expected: &prompting.Constraints{
				InterfaceSpecific: &prompting.InterfaceSpecificConstraintsHome{
					Pattern: mustParsePathPattern(c, "/home/test/foo"),
				},
				Permissions: prompting.PermissionMap{
					"read": &prompting.PermissionEntry{
						Outcome:  prompting.OutcomeAllow,
						Lifespan: prompting.LifespanForever,
					},
					"write": &prompting.PermissionEntry{
						Outcome:  prompting.OutcomeDeny,
						Lifespan: prompting.LifespanTimespan,
						Duration: "1h",
					},
				},
			},
			expectedPattern: mustParsePathPattern(c, "/home/test/foo"),
		},
		{
			iface: "camera",
			constraintsJSON: prompting.ConstraintsJSON{
				"permissions": json.RawMessage(`{"access":{"outcome":"allow","lifespan":"session"}}`),
			},
			expected: &prompting.Constraints{
				InterfaceSpecific: &prompting.InterfaceSpecificConstraintsCamera{},
				Permissions: prompting.PermissionMap{
					"access": &prompting.PermissionEntry{
						Outcome:  prompting.OutcomeAllow,
						Lifespan: prompting.LifespanSession,
					},
				},
			},
			expectedPattern: mustParsePathPattern(c, "/**"),
		},
	} {
		result, err := prompting.UnmarshalConstraints(testCase.iface, testCase.constraintsJSON)
		c.Check(err, IsNil, Commentf("testCase: %+v", testCase))
		c.Check(result, DeepEquals, testCase.expected, Commentf("testCase: %+v", testCase))
		pathPattern := result.PathPattern()
		c.Check(pathPattern, DeepEquals, testCase.expectedPattern)
	}
}

func (s *constraintsSuite) TestUnmarshalConstraintsUnhappy(c *C) {
	for _, testCase := range []struct {
		iface           string
		constraintsJSON prompting.ConstraintsJSON
		expectedErr     string
	}{
		{
			iface:       "foo",
			expectedErr: `invalid interface: "foo"`,
		},
		{
			iface:           "home",
			constraintsJSON: prompting.ConstraintsJSON{},
			expectedErr:     `invalid path pattern: no path pattern: ""`,
		},
		{
			iface:           "camera",
			constraintsJSON: prompting.ConstraintsJSON{},
			expectedErr:     "invalid permissions for camera interface: permissions empty",
		},
		{
			iface: "camera",
			constraintsJSON: prompting.ConstraintsJSON{
				// invalid outcome
				"permissions": json.RawMessage(`{"access":{"outcome":"foo","lifespan":"forever"}}`),
			},
			expectedErr: `invalid outcome: "foo"`,
		},
		{
			iface: "camera",
			constraintsJSON: prompting.ConstraintsJSON{
				// invalid lifespan
				"permissions": json.RawMessage(`{"read":{"outcome":"allow","lifespan":"foo"}}`),
			},
			expectedErr: `invalid lifespan: "foo"`,
		},
	} {
		result, err := prompting.UnmarshalConstraints(testCase.iface, testCase.constraintsJSON)
		c.Check(result, IsNil, Commentf("testCase: %+v", testCase))
		c.Check(err, ErrorMatches, testCase.expectedErr, Commentf("testCase: %+v", testCase))
	}
}

func (s *constraintsSuite) TestConstraintsMatch(c *C) {
	cases := []struct {
		interfaceSpecific prompting.InterfaceSpecificConstraints
		path              string
		expected          bool
	}{
		{
			&prompting.InterfaceSpecificConstraintsHome{
				Pattern: mustParsePathPattern(c, "/home/test/Documents/foo.txt"),
			},
			"/home/test/Documents/foo.txt",
			true,
		},
		{
			&prompting.InterfaceSpecificConstraintsHome{
				Pattern: mustParsePathPattern(c, "/home/test/Documents/bar.txt"),
			},
			"/home/test/Documents/foo.txt",
			false,
		},
		{
			&prompting.InterfaceSpecificConstraintsCamera{},
			"/dev/video0",
			true,
		},
		{
			&prompting.InterfaceSpecificConstraintsCamera{},
			"anything",
			false, // XXX: unfortunately...
		},
		{
			&prompting.InterfaceSpecificConstraintsCamera{},
			"",
			true, // XXX: surprisingly...
		},
	}
	for _, testCase := range cases {
		constraints := &prompting.Constraints{
			InterfaceSpecific: testCase.interfaceSpecific,
		}
		result, err := constraints.Match(testCase.path)
		c.Check(err, IsNil)
		c.Check(result, Equals, testCase.expected, Commentf("testCase: %+v", testCase))

		ruleConstraints := &prompting.RuleConstraints{
			InterfaceSpecific: testCase.interfaceSpecific,
		}
		result, err = ruleConstraints.Match(testCase.path)
		c.Check(err, IsNil)
		c.Check(result, Equals, testCase.expected, Commentf("testCase: %+v", testCase))
	}
}

func (s *constraintsSuite) TestConstraintsContainPermissions(c *C) {
	cases := []struct {
		constPerms []string
		queryPerms []string
		contained  bool
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
		constraints := &prompting.Constraints{
			Permissions: make(prompting.PermissionMap),
		}
		fakeEntry := &prompting.PermissionEntry{}
		for _, perm := range testCase.constPerms {
			constraints.Permissions[perm] = fakeEntry
		}
		contained := constraints.ContainPermissions(testCase.queryPerms)
		c.Check(contained, Equals, testCase.contained, Commentf("testCase: %+v", testCase))
	}
}

func (s *constraintsSuite) TestConstraintsToRuleConstraintsHappy(c *C) {
	at := prompting.At{
		Time:      time.Now(),
		SessionID: prompting.IDType(0x12345),
	}

	for _, testCase := range []struct {
		iface       string
		constraints *prompting.Constraints
		expected    *prompting.RuleConstraints
	}{
		{
			iface: "home",
			constraints: &prompting.Constraints{
				InterfaceSpecific: &prompting.InterfaceSpecificConstraintsHome{
					Pattern: mustParsePathPattern(c, "/path/to/{foo,*or*,bar}{,/}**"),
				},
				Permissions: prompting.PermissionMap{
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
			},
			expected: &prompting.RuleConstraints{
				InterfaceSpecific: &prompting.InterfaceSpecificConstraintsHome{
					Pattern: mustParsePathPattern(c, "/path/to/{foo,*or*,bar}{,/}**"),
				},
				Permissions: prompting.RulePermissionMap{
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
		},
		{
			iface: "camera",
			constraints: &prompting.Constraints{
				InterfaceSpecific: &prompting.InterfaceSpecificConstraintsCamera{},
				Permissions: prompting.PermissionMap{
					"access": &prompting.PermissionEntry{
						Outcome:  prompting.OutcomeAllow,
						Lifespan: prompting.LifespanSession,
					},
				},
			},
			expected: &prompting.RuleConstraints{
				InterfaceSpecific: &prompting.InterfaceSpecificConstraintsCamera{},
				Permissions: prompting.RulePermissionMap{
					"access": &prompting.RulePermissionEntry{
						Outcome:   prompting.OutcomeAllow,
						Lifespan:  prompting.LifespanSession,
						SessionID: at.SessionID,
					},
				},
			},
		},
	} {
		result, err := testCase.constraints.ToRuleConstraints(testCase.iface, at)
		c.Check(err, IsNil)
		c.Check(result, DeepEquals, testCase.expected)
	}
}

func (s *constraintsSuite) TestConstraintsToRuleConstraintsUnhappy(c *C) {
	at := prompting.At{
		Time:      time.Now(),
		SessionID: prompting.IDType(0),
	}

	for _, testCase := range []struct {
		iface  string
		perms  prompting.PermissionMap
		errStr string
	}{
		{
			iface: "home",
			perms: prompting.PermissionMap{
				"read": &prompting.PermissionEntry{
					Outcome:  prompting.OutcomeAllow,
					Lifespan: prompting.LifespanSingle,
				},
			},
			errStr: `cannot create rule with lifespan "single"`,
		},
		{
			iface: "camera",
			perms: prompting.PermissionMap{
				"access": &prompting.PermissionEntry{
					Outcome:  prompting.OutcomeAllow,
					Lifespan: prompting.LifespanTimespan,
				},
			},
			errStr: `invalid duration: cannot have unspecified duration when lifespan is "timespan".*`,
		},
		{
			iface: "home",
			perms: prompting.PermissionMap{
				"write": &prompting.PermissionEntry{
					Outcome:  prompting.OutcomeDeny,
					Lifespan: prompting.LifespanSession,
					Duration: "5s",
				},
			},
			errStr: `invalid duration: cannot have specified duration when lifespan is "session":.*`,
		},
		{
			iface: "camera",
			perms: prompting.PermissionMap{
				"access": &prompting.PermissionEntry{
					Outcome:  prompting.OutcomeDeny,
					Lifespan: prompting.LifespanSession,
				},
			},
			// Error will occur because current session is 0 (not found) below
			errStr: prompting_errors.ErrNewSessionRuleNoSession.Error(),
		},
		{
			iface: "home",
			perms: prompting.PermissionMap{
				"read": &prompting.PermissionEntry{
					Outcome:  prompting.OutcomeAllow,
					Lifespan: prompting.LifespanTimespan,
				},
				"write": &prompting.PermissionEntry{
					Outcome:  prompting.OutcomeDeny,
					Lifespan: prompting.LifespanSession,
					Duration: "5s",
				},
			},
			errStr: joinErrorsUnordered(`invalid duration: cannot have unspecified duration when lifespan is "timespan": ""`, `invalid duration: cannot have specified duration when lifespan is "session":.*`),
		},
	} {
		constraints := &prompting.Constraints{
			Permissions: testCase.perms,
		}
		result, err := constraints.ToRuleConstraints(testCase.iface, at)
		c.Check(result, IsNil, Commentf("testCase: %+v", testCase))
		c.Check(err, ErrorMatches, testCase.errStr, Commentf("testCase: %+v", testCase))
	}
}

func joinErrorsUnordered(err1, err2 string) string {
	return fmt.Sprintf("(%s\n%s|%s\n%s)", err1, err2, err2, err1)
}

func (s *constraintsSuite) TestUnmarshalRuleConstraintsHappy(c *C) {
	for _, testCase := range []struct {
		iface           string
		constraintsJSON prompting.ConstraintsJSON
		expected        *prompting.RuleConstraints
		expectedPattern *patterns.PathPattern
	}{
		{
			iface: "home",
			constraintsJSON: prompting.ConstraintsJSON{
				"path-pattern": json.RawMessage(`"/home/test/foo"`),
				"permissions":  json.RawMessage(`{"read":{"outcome":"allow","lifespan":"forever"},"write":{"outcome":"deny","lifespan":"forever"},"execute":{"outcome":"allow","lifespan":"session","session-id":"0123456789ABCDEF"}}`),
			},
			expected: &prompting.RuleConstraints{
				InterfaceSpecific: &prompting.InterfaceSpecificConstraintsHome{
					Pattern: mustParsePathPattern(c, "/home/test/foo"),
				},
				Permissions: prompting.RulePermissionMap{
					"read": &prompting.RulePermissionEntry{
						Outcome:  prompting.OutcomeAllow,
						Lifespan: prompting.LifespanForever,
					},
					"write": &prompting.RulePermissionEntry{
						Outcome:  prompting.OutcomeDeny,
						Lifespan: prompting.LifespanForever,
					},
					"execute": &prompting.RulePermissionEntry{
						Outcome:   prompting.OutcomeAllow,
						Lifespan:  prompting.LifespanSession,
						SessionID: prompting.IDType(0x0123456789ABCDEF),
					},
				},
			},
			expectedPattern: mustParsePathPattern(c, "/home/test/foo"),
		},
		{
			iface: "camera",
			constraintsJSON: prompting.ConstraintsJSON{
				"permissions": json.RawMessage(`{"access":{"outcome":"allow","lifespan":"session","session-id":"ABCDABCD12345678"}}`),
			},
			expected: &prompting.RuleConstraints{
				InterfaceSpecific: &prompting.InterfaceSpecificConstraintsCamera{},
				Permissions: prompting.RulePermissionMap{
					"access": &prompting.RulePermissionEntry{
						Outcome:   prompting.OutcomeAllow,
						Lifespan:  prompting.LifespanSession,
						SessionID: prompting.IDType(0xABCDABCD12345678),
					},
				},
			},
			expectedPattern: mustParsePathPattern(c, "/**"),
		},
	} {
		result, err := prompting.UnmarshalRuleConstraints(testCase.iface, testCase.constraintsJSON)
		c.Check(err, IsNil, Commentf("testCase: %+v", testCase))
		c.Check(result, DeepEquals, testCase.expected, Commentf("testCase: %+v", testCase))
		pathPattern := result.PathPattern()
		c.Check(pathPattern, DeepEquals, testCase.expectedPattern)
	}
}

func (s *constraintsSuite) TestUnmarshalRuleConstraintsUnhappy(c *C) {
	for _, testCase := range []struct {
		iface           string
		constraintsJSON prompting.ConstraintsJSON
		expectedErr     string
	}{
		{
			iface:       "foo",
			expectedErr: `invalid interface: "foo"`,
		},
		{
			iface:           "home",
			constraintsJSON: prompting.ConstraintsJSON{},
			expectedErr:     `invalid path pattern: no path pattern: ""`,
		},
		{
			iface:           "camera",
			constraintsJSON: prompting.ConstraintsJSON{},
			expectedErr:     "invalid permissions for camera interface: permissions empty",
		},
		{
			iface: "camera",
			constraintsJSON: prompting.ConstraintsJSON{
				// invalid outcome
				"permissions": json.RawMessage(`{"access":{"outcome":"foo","lifespan":"forever"}}`),
			},
			expectedErr: `invalid outcome: "foo"`,
		},
		{
			iface: "camera",
			constraintsJSON: prompting.ConstraintsJSON{
				// invalid lifespan
				"permissions": json.RawMessage(`{"read":{"outcome":"allow","lifespan":"foo"}}`),
			},
			expectedErr: `invalid lifespan: "foo"`,
		},
	} {
		result, err := prompting.UnmarshalRuleConstraints(testCase.iface, testCase.constraintsJSON)
		c.Check(result, IsNil, Commentf("testCase: %+v", testCase))
		c.Check(err, ErrorMatches, testCase.expectedErr, Commentf("testCase: %+v", testCase))
	}
}

func (s *constraintsSuite) TestRuleConstraintsMarshalJSON(c *C) {
	for _, testCase := range []struct {
		constraints    *prompting.RuleConstraints
		expected       string
		expectedPre124 string
	}{
		{
			&prompting.RuleConstraints{
				InterfaceSpecific: &prompting.InterfaceSpecificConstraintsHome{
					Pattern: mustParsePathPattern(c, "/path/to/**/something*/*.txt"),
				},
				Permissions: prompting.RulePermissionMap{
					"execute": &prompting.RulePermissionEntry{
						Outcome:   prompting.OutcomeAllow,
						Lifespan:  prompting.LifespanSession,
						SessionID: prompting.IDType(0x0123456789ABCDEF),
					},
				},
			},
			`{"path-pattern":"/path/to/**/something*/*.txt","permissions":{"execute":{"outcome":"allow","lifespan":"session","session-id":"0123456789ABCDEF"}}}`,
			`{"path-pattern":"/path/to/**/something*/*.txt","permissions":{"execute":{"outcome":"allow","lifespan":"session","expiration":"0001-01-01T00:00:00Z","session-id":"0123456789ABCDEF"}}}`,
		},
		{
			&prompting.RuleConstraints{
				InterfaceSpecific: &prompting.InterfaceSpecificConstraintsCamera{},
				Permissions: prompting.RulePermissionMap{
					"access": &prompting.RulePermissionEntry{
						Outcome:  prompting.OutcomeAllow,
						Lifespan: prompting.LifespanForever,
					},
				},
			},
			`{"permissions":{"access":{"outcome":"allow","lifespan":"forever"}}}`,
			`{"permissions":{"access":{"outcome":"allow","lifespan":"forever","expiration":"0001-01-01T00:00:00Z","session-id":"0000000000000000"}}}`,
		},
	} {
		result, err := testCase.constraints.MarshalJSON()
		c.Check(err, IsNil)
		expected := testCase.expected
		if runtime.Version() < "go1.24" {
			expected = testCase.expectedPre124
		}
		c.Check(string(result), Equals, expected)
	}
}

func (s *constraintsSuite) TestRuleConstraintsPruneExpired(c *C) {
	at := prompting.At{
		Time:      time.Now(),
		SessionID: prompting.IDType(0x12345),
	}

	for _, testCase := range []struct {
		perms    prompting.RulePermissionMap
		expired  bool
		expected prompting.RulePermissionMap
	}{
		{
			prompting.RulePermissionMap{
				"read": &prompting.RulePermissionEntry{
					Outcome:  prompting.OutcomeAllow,
					Lifespan: prompting.LifespanForever,
				},
			},
			false,
			prompting.RulePermissionMap{
				"read": &prompting.RulePermissionEntry{
					Outcome:  prompting.OutcomeAllow,
					Lifespan: prompting.LifespanForever,
				},
			},
		},
		{
			prompting.RulePermissionMap{
				"read": &prompting.RulePermissionEntry{
					Outcome:    prompting.OutcomeAllow,
					Lifespan:   prompting.LifespanTimespan,
					Expiration: at.Time,
				},
			},
			true,
			prompting.RulePermissionMap{},
		},
		{
			prompting.RulePermissionMap{
				"read": &prompting.RulePermissionEntry{
					Outcome:    prompting.OutcomeAllow,
					Lifespan:   prompting.LifespanTimespan,
					Expiration: at.Time.Add(-time.Minute),
				},
				"write": &prompting.RulePermissionEntry{
					Outcome:    prompting.OutcomeDeny,
					Lifespan:   prompting.LifespanTimespan,
					Expiration: at.Time.Add(time.Minute),
				},
				"execute": &prompting.RulePermissionEntry{
					Outcome:    prompting.OutcomeDeny,
					Lifespan:   prompting.LifespanTimespan,
					Expiration: at.Time,
				},
			},
			false,
			prompting.RulePermissionMap{
				"write": &prompting.RulePermissionEntry{
					Outcome:    prompting.OutcomeDeny,
					Lifespan:   prompting.LifespanTimespan,
					Expiration: at.Time.Add(time.Minute),
				},
			},
		},
		{
			prompting.RulePermissionMap{
				"read": &prompting.RulePermissionEntry{
					Outcome:   prompting.OutcomeAllow,
					Lifespan:  prompting.LifespanSession,
					SessionID: at.SessionID,
				},
			},
			false,
			prompting.RulePermissionMap{
				"read": &prompting.RulePermissionEntry{
					Outcome:   prompting.OutcomeAllow,
					Lifespan:  prompting.LifespanSession,
					SessionID: at.SessionID,
				},
			},
		},
		{
			prompting.RulePermissionMap{
				"read": &prompting.RulePermissionEntry{
					Outcome:   prompting.OutcomeAllow,
					Lifespan:  prompting.LifespanSession,
					SessionID: at.SessionID + 1,
				},
			},
			true,
			prompting.RulePermissionMap{},
		},
		{
			prompting.RulePermissionMap{
				"read": &prompting.RulePermissionEntry{
					Outcome:    prompting.OutcomeAllow,
					Lifespan:   prompting.LifespanTimespan,
					Expiration: at.Time.Add(-time.Minute),
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
			true,
			prompting.RulePermissionMap{},
		},
		{
			prompting.RulePermissionMap{
				"read": &prompting.RulePermissionEntry{
					Outcome:    prompting.OutcomeAllow,
					Lifespan:   prompting.LifespanTimespan,
					Expiration: at.Time.Add(-time.Minute),
				},
				"write": &prompting.RulePermissionEntry{
					Outcome:    prompting.OutcomeAllow,
					Lifespan:   prompting.LifespanTimespan,
					Expiration: at.Time.Add(time.Minute),
				},
				"execute": &prompting.RulePermissionEntry{
					Outcome:   prompting.OutcomeDeny,
					Lifespan:  prompting.LifespanSession,
					SessionID: at.SessionID,
				},
			},
			false,
			prompting.RulePermissionMap{
				"write": &prompting.RulePermissionEntry{
					Outcome:    prompting.OutcomeAllow,
					Lifespan:   prompting.LifespanTimespan,
					Expiration: at.Time.Add(time.Minute),
				},
				"execute": &prompting.RulePermissionEntry{
					Outcome:   prompting.OutcomeDeny,
					Lifespan:  prompting.LifespanSession,
					SessionID: at.SessionID,
				},
			},
		},
	} {
		copiedPerms := make(prompting.RulePermissionMap, len(testCase.perms))
		for perm, entry := range testCase.perms {
			copiedPerms[perm] = entry
		}
		constraints := &prompting.RuleConstraints{
			Permissions: copiedPerms,
		}
		expired := constraints.PruneExpired(at)
		c.Check(expired, Equals, testCase.expired, Commentf("testCase: %+v\nremaining perms: %+v", testCase, constraints.Permissions))
		c.Check(constraints.Permissions, DeepEquals, testCase.expected, Commentf("testCase: %+v\nremaining perms: %+v", testCase, constraints.Permissions))
	}
}

func (s *constraintsSuite) TestUnmarshalReplyConstraintsHappy(c *C) {
	for _, testCase := range []struct {
		iface       string
		outcome     prompting.OutcomeType
		lifespan    prompting.LifespanType
		duration    string
		constraints prompting.ConstraintsJSON
		expected    *prompting.Constraints
	}{
		{
			iface:    "home",
			outcome:  prompting.OutcomeAllow,
			lifespan: prompting.LifespanForever,
			constraints: prompting.ConstraintsJSON{
				"path-pattern": json.RawMessage(`"/path/to/foo"`),
				"permissions":  json.RawMessage(`["read","write","execute"]`),
			},
			expected: &prompting.Constraints{
				InterfaceSpecific: &prompting.InterfaceSpecificConstraintsHome{
					Pattern: mustParsePathPattern(c, "/path/to/foo"),
				},
				Permissions: prompting.PermissionMap{
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
		},
		{
			iface:    "home",
			outcome:  prompting.OutcomeDeny,
			lifespan: prompting.LifespanTimespan,
			duration: "10m",
			constraints: prompting.ConstraintsJSON{
				"path-pattern": json.RawMessage(`"/home/me/**"`),
				"permissions":  json.RawMessage(`["read","write"]`),
			},
			expected: &prompting.Constraints{
				InterfaceSpecific: &prompting.InterfaceSpecificConstraintsHome{
					Pattern: mustParsePathPattern(c, "/home/me/**"),
				},
				Permissions: prompting.PermissionMap{
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
		},
		{
			iface:    "camera",
			outcome:  prompting.OutcomeAllow,
			lifespan: prompting.LifespanSession,
			constraints: prompting.ConstraintsJSON{
				"permissions": json.RawMessage(`["access"]`),
			},
			expected: &prompting.Constraints{
				InterfaceSpecific: &prompting.InterfaceSpecificConstraintsCamera{},
				Permissions: prompting.PermissionMap{
					"access": &prompting.PermissionEntry{
						Outcome:  prompting.OutcomeAllow,
						Lifespan: prompting.LifespanSession,
					},
				},
			},
		},
	} {
		result, err := prompting.UnmarshalReplyConstraints(testCase.iface, testCase.outcome, testCase.lifespan, testCase.duration, testCase.constraints)
		c.Check(err, IsNil)
		c.Check(result, DeepEquals, testCase.expected)
	}
}

func (s *constraintsSuite) TestUnmarshalReplyConstraintsUnhappy(c *C) {
	for _, testCase := range []struct {
		iface       string
		outcome     prompting.OutcomeType
		lifespan    prompting.LifespanType
		duration    string
		constraints prompting.ConstraintsJSON
		errStr      string
	}{
		{
			outcome: prompting.OutcomeType("foo"),
			errStr:  `invalid outcome: "foo"`,
		},
		{
			lifespan: prompting.LifespanTimespan,
			duration: "",
			errStr:   `invalid duration: cannot have unspecified duration when lifespan is "timespan":.*`,
		},
		{
			lifespan: prompting.LifespanForever,
			duration: "10s",
			errStr:   `invalid duration: cannot have specified duration when lifespan is "forever":.*`,
		},
		{
			iface:  "foo",
			errStr: `invalid interface: "foo"`,
		},
		{
			iface:       "home",
			constraints: prompting.ConstraintsJSON{},
			errStr:      `invalid path pattern: no path pattern: ""`,
		},
		{
			iface: "home",
			constraints: prompting.ConstraintsJSON{
				"path-pattern": json.RawMessage(`"/path/to/foo"`),
			},
			errStr: `invalid permissions for home interface: permissions empty`,
		},
		{
			iface: "home",
			constraints: prompting.ConstraintsJSON{
				"path-pattern": json.RawMessage(`"/path/to/foo"`),
				"permissions":  json.RawMessage(`[]`),
			},
			errStr: `invalid permissions for home interface: permissions empty`,
		},
		{
			iface:       "camera",
			constraints: prompting.ConstraintsJSON{},
			errStr:      `invalid permissions for camera interface: permissions empty`,
		},
		{
			iface: "camera",
			constraints: prompting.ConstraintsJSON{
				"permissions": json.RawMessage(`[]`),
			},
			errStr: `invalid permissions for camera interface: permissions empty`,
		},
		{
			iface: "home",
			constraints: prompting.ConstraintsJSON{
				"path-pattern": json.RawMessage(`"/path/to/foo"`),
				"permissions":  json.RawMessage(`["read","append","write","create","execute"]`),
			},
			errStr: `invalid permissions for home interface: "append", "create"`,
		},
		{
			iface: "camera",
			constraints: prompting.ConstraintsJSON{
				"path-pattern": json.RawMessage(`"/path/to/foo"`),
				"permissions":  json.RawMessage(`["read","access","write","create","execute"]`),
			},
			errStr: `invalid permissions for camera interface: "read", "write", "create", "execute"`,
		},
	} {
		iface := "home"
		if testCase.iface != "" {
			iface = testCase.iface
		}
		outcome := prompting.OutcomeAllow
		if testCase.outcome != prompting.OutcomeUnset {
			outcome = testCase.outcome
		}
		lifespan := prompting.LifespanForever
		if testCase.lifespan != prompting.LifespanUnset {
			lifespan = testCase.lifespan
		}
		result, err := prompting.UnmarshalReplyConstraints(iface, outcome, lifespan, testCase.duration, testCase.constraints)
		c.Check(result, IsNil, Commentf("testCase: %+v", testCase))
		c.Check(err, ErrorMatches, testCase.errStr, Commentf("testCase: %+v", testCase))
	}
}

func (s *constraintsSuite) TestUnmarshalRuleConstraintsPatchHappy(c *C) {
	for _, testCase := range []struct {
		iface           string
		constraintsJSON prompting.ConstraintsJSON
		expected        *prompting.RuleConstraintsPatch
	}{
		{
			iface:           "home",
			constraintsJSON: prompting.ConstraintsJSON{},
			expected: &prompting.RuleConstraintsPatch{
				InterfaceSpecific: &prompting.InterfaceSpecificConstraintsHome{},
			},
		},
		{
			iface:           "camera",
			constraintsJSON: prompting.ConstraintsJSON{},
			expected: &prompting.RuleConstraintsPatch{
				InterfaceSpecific: &prompting.InterfaceSpecificConstraintsCamera{},
			},
		},
		{
			iface: "home",
			constraintsJSON: prompting.ConstraintsJSON{
				"path-pattern": json.RawMessage(`"/path/to/foo"`),
			},
			expected: &prompting.RuleConstraintsPatch{
				InterfaceSpecific: &prompting.InterfaceSpecificConstraintsHome{
					Pattern: mustParsePathPattern(c, "/path/to/foo"),
				},
			},
		},
		{
			iface: "home",
			constraintsJSON: prompting.ConstraintsJSON{
				"permissions": json.RawMessage(`{"read":{"outcome":"allow","lifespan":"forever"}}`),
			},
			expected: &prompting.RuleConstraintsPatch{
				InterfaceSpecific: &prompting.InterfaceSpecificConstraintsHome{},
				Permissions: prompting.PermissionMap{
					"read": &prompting.PermissionEntry{
						Outcome:  prompting.OutcomeAllow,
						Lifespan: prompting.LifespanForever,
					},
				},
			},
		},
		{
			iface: "camera",
			constraintsJSON: prompting.ConstraintsJSON{
				"permissions": json.RawMessage(`{"access":{"outcome":"deny","lifespan":"session"}}`),
			},
			expected: &prompting.RuleConstraintsPatch{
				InterfaceSpecific: &prompting.InterfaceSpecificConstraintsCamera{},
				Permissions: prompting.PermissionMap{
					"access": &prompting.PermissionEntry{
						Outcome:  prompting.OutcomeDeny,
						Lifespan: prompting.LifespanSession,
					},
				},
			},
		},
		{
			iface: "home",
			constraintsJSON: prompting.ConstraintsJSON{
				"path-pattern": json.RawMessage(`"/home/test/foo"`),
				"permissions":  json.RawMessage(`{"read":null,"write":{"outcome":"deny","lifespan":"timespan","duration":"1h"},"execute":{"outcome":"allow","lifespan":"session"}}`),
			},
			expected: &prompting.RuleConstraintsPatch{
				InterfaceSpecific: &prompting.InterfaceSpecificConstraintsHome{
					Pattern: mustParsePathPattern(c, "/home/test/foo"),
				},
				Permissions: prompting.PermissionMap{
					"read": nil,
					"write": &prompting.PermissionEntry{
						Outcome:  prompting.OutcomeDeny,
						Lifespan: prompting.LifespanTimespan,
						Duration: "1h",
					},
					"execute": &prompting.PermissionEntry{
						Outcome:  prompting.OutcomeAllow,
						Lifespan: prompting.LifespanSession,
					},
				},
			},
		},
	} {
		result, err := prompting.UnmarshalRuleConstraintsPatch(testCase.iface, testCase.constraintsJSON)
		c.Check(err, IsNil, Commentf("testCase: %+v", testCase))
		c.Check(result, DeepEquals, testCase.expected, Commentf("testCase: %+v", testCase))
	}
}

func (s *constraintsSuite) TestUnmarshalRuleConstraintsPatchUnhappy(c *C) {
	for _, testCase := range []struct {
		iface           string
		constraintsJSON prompting.ConstraintsJSON
		expectedErr     string
	}{
		{
			iface:       "foo",
			expectedErr: `invalid interface: "foo"`,
		},
		{
			iface: "home",
			constraintsJSON: prompting.ConstraintsJSON{
				"path-pattern": json.RawMessage(`"not a pattern"`),
			},
			expectedErr: `invalid path pattern: pattern must start with '/': "not a pattern"`,
		},
		{
			iface: "camera",
			constraintsJSON: prompting.ConstraintsJSON{
				// invalid outcome
				"permissions": json.RawMessage(`{"access":{"outcome":"foo","lifespan":"forever"}}`),
			},
			expectedErr: `invalid outcome: "foo"`,
		},
		{
			iface: "camera",
			constraintsJSON: prompting.ConstraintsJSON{
				// invalid lifespan
				"permissions": json.RawMessage(`{"read":{"outcome":"allow","lifespan":"foo"}}`),
			},
			expectedErr: `invalid lifespan: "foo"`,
		},
	} {
		result, err := prompting.UnmarshalRuleConstraintsPatch(testCase.iface, testCase.constraintsJSON)
		c.Check(result, IsNil, Commentf("testCase: %+v", testCase))
		c.Check(err, ErrorMatches, testCase.expectedErr, Commentf("testCase: %+v", testCase))
	}
}

func (s *constraintsSuite) TestPatchRuleConstraintsHappy(c *C) {
	origTime := time.Now()
	origSession := prompting.IDType(0x12345)
	patchTime := origTime.Add(time.Second)
	patchSession := prompting.IDType(0xf00ba4)
	patchAt := prompting.At{
		Time:      patchTime,
		SessionID: patchSession,
	}

	pathPattern := mustParsePathPattern(c, "/path/to/foo/ba?/**")
	otherPattern := mustParsePathPattern(c, "/path/to/*/another*")

	for i, testCase := range []struct {
		initial *prompting.RuleConstraints
		patch   *prompting.RuleConstraintsPatch
		final   *prompting.RuleConstraints
	}{
		{
			initial: &prompting.RuleConstraints{
				InterfaceSpecific: &prompting.InterfaceSpecificConstraintsHome{
					Pattern: pathPattern,
				},
				Permissions: prompting.RulePermissionMap{
					"read": &prompting.RulePermissionEntry{
						Outcome:    prompting.OutcomeAllow,
						Lifespan:   prompting.LifespanTimespan,
						Expiration: origTime,
					},
					"write": &prompting.RulePermissionEntry{
						Outcome:    prompting.OutcomeDeny,
						Lifespan:   prompting.LifespanTimespan,
						Expiration: origTime.Add(-time.Second),
					},
					"execute": &prompting.RulePermissionEntry{
						Outcome:   prompting.OutcomeAllow,
						Lifespan:  prompting.LifespanSession,
						SessionID: origSession,
					},
				},
			},
			patch: &prompting.RuleConstraintsPatch{},
			final: &prompting.RuleConstraints{
				InterfaceSpecific: &prompting.InterfaceSpecificConstraintsHome{
					Pattern: pathPattern,
				},
				Permissions: prompting.RulePermissionMap{
					"read": &prompting.RulePermissionEntry{
						Outcome:    prompting.OutcomeAllow,
						Lifespan:   prompting.LifespanTimespan,
						Expiration: origTime,
					},
					"write": &prompting.RulePermissionEntry{
						Outcome:    prompting.OutcomeDeny,
						Lifespan:   prompting.LifespanTimespan,
						Expiration: origTime.Add(-time.Second), // expired perms are not pruned if patch perms are nil
					},
					"execute": &prompting.RulePermissionEntry{
						Outcome:   prompting.OutcomeAllow,
						Lifespan:  prompting.LifespanSession,
						SessionID: origSession, // expired perms are not pruned if patch perms are nil
					},
				},
			},
		},
		{
			initial: &prompting.RuleConstraints{
				InterfaceSpecific: &prompting.InterfaceSpecificConstraintsHome{
					Pattern: pathPattern,
				},
				Permissions: prompting.RulePermissionMap{
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
			},
			patch: &prompting.RuleConstraintsPatch{
				Permissions: prompting.PermissionMap{
					// Remove read permissions, let write permission expire,
					// and add new execute permission
					"read": nil,
					"execute": &prompting.PermissionEntry{
						Outcome:  prompting.OutcomeDeny,
						Lifespan: prompting.LifespanTimespan,
						Duration: "1m",
					},
				},
			},
			final: &prompting.RuleConstraints{
				InterfaceSpecific: &prompting.InterfaceSpecificConstraintsHome{
					Pattern: pathPattern,
				},
				Permissions: prompting.RulePermissionMap{
					"execute": &prompting.RulePermissionEntry{
						Outcome:    prompting.OutcomeDeny,
						Lifespan:   prompting.LifespanTimespan,
						Expiration: patchTime.Add(time.Minute),
					},
				},
			},
		},
		{
			initial: &prompting.RuleConstraints{
				InterfaceSpecific: &prompting.InterfaceSpecificConstraintsHome{
					Pattern: pathPattern,
				},
				Permissions: prompting.RulePermissionMap{
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
			},
			patch: &prompting.RuleConstraintsPatch{
				Permissions: prompting.PermissionMap{
					"execute": &prompting.PermissionEntry{
						Outcome:  prompting.OutcomeDeny,
						Lifespan: prompting.LifespanSession,
					},
				},
			},
			final: &prompting.RuleConstraints{
				InterfaceSpecific: &prompting.InterfaceSpecificConstraintsHome{
					Pattern: pathPattern,
				},
				Permissions: prompting.RulePermissionMap{
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
		},
		{
			initial: &prompting.RuleConstraints{
				InterfaceSpecific: &prompting.InterfaceSpecificConstraintsHome{
					Pattern: pathPattern,
				},
				Permissions: prompting.RulePermissionMap{
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
			},
			patch: &prompting.RuleConstraintsPatch{
				Permissions: prompting.PermissionMap{
					"read": nil,
					"execute": &prompting.PermissionEntry{
						Outcome:  prompting.OutcomeAllow,
						Lifespan: prompting.LifespanSession,
					},
				},
			},
			final: &prompting.RuleConstraints{
				InterfaceSpecific: &prompting.InterfaceSpecificConstraintsHome{
					Pattern: pathPattern,
				},
				Permissions: prompting.RulePermissionMap{
					"execute": &prompting.RulePermissionEntry{
						Outcome:   prompting.OutcomeAllow,
						Lifespan:  prompting.LifespanSession,
						SessionID: patchSession,
					},
				},
			},
		},
		{
			initial: &prompting.RuleConstraints{
				InterfaceSpecific: &prompting.InterfaceSpecificConstraintsCamera{},
				Permissions: prompting.RulePermissionMap{
					"access": &prompting.RulePermissionEntry{
						Outcome:  prompting.OutcomeAllow,
						Lifespan: prompting.LifespanForever,
					},
				},
			},
			patch: &prompting.RuleConstraintsPatch{
				Permissions: prompting.PermissionMap{
					"access": &prompting.PermissionEntry{
						Outcome:  prompting.OutcomeAllow,
						Lifespan: prompting.LifespanSession,
					},
				},
			},
			final: &prompting.RuleConstraints{
				InterfaceSpecific: &prompting.InterfaceSpecificConstraintsCamera{},
				Permissions: prompting.RulePermissionMap{
					"access": &prompting.RulePermissionEntry{
						Outcome:   prompting.OutcomeAllow,
						Lifespan:  prompting.LifespanSession,
						SessionID: patchSession,
					},
				},
			},
		},
		{
			initial: &prompting.RuleConstraints{
				InterfaceSpecific: &prompting.InterfaceSpecificConstraintsHome{
					Pattern: pathPattern,
				},
				Permissions: prompting.RulePermissionMap{
					"read": &prompting.RulePermissionEntry{
						Outcome:    prompting.OutcomeAllow,
						Lifespan:   prompting.LifespanTimespan,
						Expiration: origTime,
					},
				},
			},
			patch: &prompting.RuleConstraintsPatch{
				InterfaceSpecific: &prompting.InterfaceSpecificConstraintsHome{
					Pattern: otherPattern,
				},
			},
			final: &prompting.RuleConstraints{
				InterfaceSpecific: &prompting.InterfaceSpecificConstraintsHome{
					Pattern: otherPattern,
				},
				Permissions: prompting.RulePermissionMap{
					"read": &prompting.RulePermissionEntry{
						Outcome:    prompting.OutcomeAllow,
						Lifespan:   prompting.LifespanTimespan,
						Expiration: origTime,
					},
				},
			},
		},
		{
			initial: &prompting.RuleConstraints{
				InterfaceSpecific: &prompting.InterfaceSpecificConstraintsCamera{},
				Permissions: prompting.RulePermissionMap{
					"access": &prompting.RulePermissionEntry{
						Outcome:    prompting.OutcomeAllow,
						Lifespan:   prompting.LifespanTimespan,
						Expiration: origTime,
					},
				},
			},
			patch: &prompting.RuleConstraintsPatch{
				InterfaceSpecific: &prompting.InterfaceSpecificConstraintsCamera{},
			},
			final: &prompting.RuleConstraints{
				InterfaceSpecific: &prompting.InterfaceSpecificConstraintsCamera{},
				Permissions: prompting.RulePermissionMap{
					"access": &prompting.RulePermissionEntry{
						Outcome:    prompting.OutcomeAllow,
						Lifespan:   prompting.LifespanTimespan,
						Expiration: origTime,
					},
				},
			},
		},
	} {
		patched, err := testCase.patch.PatchRuleConstraints(testCase.initial, patchAt)
		c.Check(err, IsNil, Commentf("testCase %d", i))
		c.Check(patched, DeepEquals, testCase.final, Commentf("testCase %d", i))
	}
}

func (s *constraintsSuite) TestPatchRuleConstraintsUnhappy(c *C) {
	origTime := time.Now()
	patchTime := origTime.Add(time.Second)
	patchSession := prompting.IDType(0x12345)
	patchAt := prompting.At{
		Time:      patchTime,
		SessionID: patchSession,
	}

	pathPattern := mustParsePathPattern(c, "/path/to/foo/ba{r,z{,/**/}}")

	goodRule := &prompting.RuleConstraints{
		InterfaceSpecific: &prompting.InterfaceSpecificConstraintsHome{
			Pattern: pathPattern,
		},
		Permissions: prompting.RulePermissionMap{
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
	}

	badPatch := &prompting.RuleConstraintsPatch{
		Permissions: prompting.PermissionMap{
			"read": &prompting.PermissionEntry{
				Outcome:  prompting.OutcomeAllow,
				Lifespan: prompting.LifespanSingle,
			},
			"create": &prompting.PermissionEntry{
				Outcome:  prompting.OutcomeAllow,
				Lifespan: prompting.LifespanForever,
			},
			"lock": nil, // even if invalid permission is meant to be removed, include it
			"execute": &prompting.PermissionEntry{
				Outcome:  prompting.OutcomeAllow,
				Lifespan: prompting.LifespanTimespan,
			},
		},
	}
	expected := joinErrorsUnordered(`cannot create rule with lifespan "single"`, `invalid duration: cannot have unspecified duration when lifespan is "timespan": ""`)

	result, err := badPatch.PatchRuleConstraints(goodRule, patchAt)
	c.Check(err, ErrorMatches, expected)
	c.Check(result, IsNil)

	badPatch = &prompting.RuleConstraintsPatch{
		Permissions: prompting.PermissionMap{
			// Remove all permissions
			"read": nil,
		},
	}
	result, err = badPatch.PatchRuleConstraints(goodRule, patchAt)
	c.Check(err, Equals, prompting_errors.ErrPatchedRuleHasNoPerms)
	c.Check(result, IsNil)
}

func (s *constraintsSuite) TestRulePermissionMapExpired(c *C) {
	at := prompting.At{
		Time:      time.Now(),
		SessionID: prompting.IDType(0x12345),
	}
	for _, pm := range []prompting.RulePermissionMap{
		{},
		{
			"read": &prompting.RulePermissionEntry{
				Outcome:    prompting.OutcomeAllow,
				Lifespan:   prompting.LifespanTimespan,
				Expiration: at.Time,
			},
		},
		{
			"access": &prompting.RulePermissionEntry{
				Outcome:   prompting.OutcomeDeny,
				Lifespan:  prompting.LifespanSession,
				SessionID: at.SessionID + 1,
			},
		},
		{
			"read": &prompting.RulePermissionEntry{
				Outcome:    prompting.OutcomeAllow,
				Lifespan:   prompting.LifespanTimespan,
				Expiration: at.Time.Add(-time.Second),
			},
			"write": &prompting.RulePermissionEntry{
				Outcome:    prompting.OutcomeDeny,
				Lifespan:   prompting.LifespanTimespan,
				Expiration: at.Time,
			},
			"execute": &prompting.RulePermissionEntry{
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
			"read": &prompting.RulePermissionEntry{
				Outcome:    prompting.OutcomeAllow,
				Lifespan:   prompting.LifespanTimespan,
				Expiration: at.Time.Add(-time.Second),
			},
			"write": &prompting.RulePermissionEntry{
				Outcome:  prompting.OutcomeDeny,
				Lifespan: prompting.LifespanForever,
			},
		},
		{
			"read": &prompting.RulePermissionEntry{
				Outcome:   prompting.OutcomeAllow,
				Lifespan:  prompting.LifespanSession,
				SessionID: prompting.IDType(0xabcd),
			},
			"write": &prompting.RulePermissionEntry{
				Outcome:  prompting.OutcomeDeny,
				Lifespan: prompting.LifespanForever,
			},
		},
		{
			"read": &prompting.RulePermissionEntry{
				Outcome:    prompting.OutcomeAllow,
				Lifespan:   prompting.LifespanTimespan,
				Expiration: at.Time.Add(time.Second),
			},
		},
		{
			"read": &prompting.RulePermissionEntry{
				Outcome:   prompting.OutcomeAllow,
				Lifespan:  prompting.LifespanSession,
				SessionID: at.SessionID,
			},
		},
	} {
		c.Check(pm.Expired(at), Equals, false, Commentf("%+v", pm))
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

func (s *constraintsSuite) TestMarshalRulePermissionEntry(c *C) {
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
