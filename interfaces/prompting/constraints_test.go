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

func (s *constraintsSuite) TestConstraintsMatch(c *C) {
	cases := []struct {
		pattern string
		path    string
		matches bool
	}{
		{
			"/home/test/Documents/foo.txt",
			"/home/test/Documents/foo.txt",
			true,
		},
		{
			"/home/test/Documents/foo",
			"/home/test/Documents/foo.txt",
			false,
		},
	}
	for _, testCase := range cases {
		pattern := mustParsePathPattern(c, testCase.pattern)

		constraints := &prompting.Constraints{
			PathPattern: pattern,
		}
		result, err := constraints.Match(testCase.path)
		c.Check(err, IsNil, Commentf("test case: %+v", testCase))
		c.Check(result, Equals, testCase.matches, Commentf("test case: %+v", testCase))

		ruleConstraints := &prompting.RuleConstraints{
			PathPattern: pattern,
		}
		result, err = ruleConstraints.Match(testCase.path)
		c.Check(err, IsNil, Commentf("test case: %+v", testCase))
		c.Check(result, Equals, testCase.matches, Commentf("test case: %+v", testCase))
	}
}

func (s *constraintsSuite) TestConstraintsMatchUnhappy(c *C) {
	badPath := `bad\path\`

	badConstraints := &prompting.Constraints{
		PathPattern: nil,
	}
	matches, err := badConstraints.Match(badPath)
	c.Check(err, ErrorMatches, `invalid path pattern: no path pattern: ""`)
	c.Check(matches, Equals, false)

	badRuleConstraints := &prompting.RuleConstraints{
		PathPattern: nil,
	}
	matches, err = badRuleConstraints.Match(badPath)
	c.Check(err, ErrorMatches, `invalid path pattern: no path pattern: ""`)
	c.Check(matches, Equals, false)
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
		pathPattern := mustParsePathPattern(c, "/arbitrary")
		constraints := &prompting.Constraints{
			PathPattern: pathPattern,
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
	current := prompting.At{
		Time: time.Now(),
	}
	iface := "home"
	pathPattern := mustParsePathPattern(c, "/path/to/{foo,*or*,bar}{,/}**")

	constraints := &prompting.Constraints{
		PathPattern: pathPattern,
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
				Lifespan: prompting.LifespanTimespan,
				Duration: "1ns",
			},
		},
	}

	expectedRuleConstraints := &prompting.RuleConstraints{
		PathPattern: pathPattern,
		Permissions: prompting.RulePermissionMap{
			"read": &prompting.RulePermissionEntry{
				Outcome:  prompting.OutcomeAllow,
				Lifespan: prompting.LifespanForever,
			},
			"write": &prompting.RulePermissionEntry{
				Outcome:    prompting.OutcomeDeny,
				Lifespan:   prompting.LifespanTimespan,
				Expiration: current.Time.Add(10 * time.Second),
			},
			"execute": &prompting.RulePermissionEntry{
				Outcome:    prompting.OutcomeAllow,
				Lifespan:   prompting.LifespanTimespan,
				Expiration: current.Time.Add(time.Nanosecond),
			},
		},
	}

	result, err := constraints.ToRuleConstraints(iface, current)
	c.Assert(err, IsNil)
	c.Assert(result, DeepEquals, expectedRuleConstraints)
}

func (s *constraintsSuite) TestConstraintsToRuleConstraintsUnhappy(c *C) {
	current := prompting.At{
		Time: time.Now(),
	}
	badConstraints := &prompting.Constraints{}
	result, err := badConstraints.ToRuleConstraints("home", current)
	c.Check(result, IsNil)
	c.Check(err, ErrorMatches, `invalid path pattern: no path pattern.*`)

	constraints := &prompting.Constraints{
		PathPattern: mustParsePathPattern(c, "/path/to/foo"),
		Permissions: prompting.PermissionMap{
			"read": &prompting.PermissionEntry{
				Outcome:  prompting.OutcomeAllow,
				Lifespan: prompting.LifespanForever,
			},
		},
	}
	result, err = constraints.ToRuleConstraints("foo", current)
	c.Check(result, IsNil)
	c.Check(err, ErrorMatches, `invalid interface: "foo"`)

	for _, testCase := range []struct {
		perms  prompting.PermissionMap
		errStr string
	}{
		{
			perms:  nil,
			errStr: `invalid permissions for home interface: permissions empty`,
		},
		{
			perms: prompting.PermissionMap{
				"create": &prompting.PermissionEntry{
					Outcome:  prompting.OutcomeAllow,
					Lifespan: prompting.LifespanForever,
				},
			},
			errStr: `invalid permissions for home interface: "create"`,
		},
		{
			perms: prompting.PermissionMap{
				"read": &prompting.PermissionEntry{
					Outcome:  prompting.OutcomeAllow,
					Lifespan: prompting.LifespanTimespan,
				},
			},
			errStr: `invalid duration: cannot have unspecified duration when lifespan is "timespan".*`,
		},
		{
			perms: prompting.PermissionMap{
				"read": &prompting.PermissionEntry{
					Outcome:  prompting.OutcomeAllow,
					Lifespan: prompting.LifespanTimespan,
				},
				"create": &prompting.PermissionEntry{
					Outcome:  prompting.OutcomeAllow,
					Lifespan: prompting.LifespanForever,
				},
			},
			errStr: joinErrorsUnordered(`invalid duration: cannot have unspecified duration when lifespan is "timespan": ""`, `invalid permissions for home interface: "create"`),
		},
	} {
		constraints := &prompting.Constraints{
			PathPattern: mustParsePathPattern(c, "/path/to/foo"),
			Permissions: testCase.perms,
		}
		result, err = constraints.ToRuleConstraints("home", current)
		c.Check(result, IsNil, Commentf("testCase: %+v", testCase))
		c.Check(err, ErrorMatches, testCase.errStr, Commentf("testCase: %+v", testCase))
	}
}

func joinErrorsUnordered(err1, err2 string) string {
	return fmt.Sprintf("(%s\n%s|%s\n%s)", err1, err2, err2, err1)
}

func (s *constraintsSuite) TestRuleConstraintsValidateForInterface(c *C) {
	validPathPattern := mustParsePathPattern(c, "/path/to/foo")
	current := prompting.At{
		Time: time.Now(),
	}

	// Happy
	constraints := &prompting.RuleConstraints{
		PathPattern: validPathPattern,
		Permissions: prompting.RulePermissionMap{
			"read": &prompting.RulePermissionEntry{
				Outcome:  prompting.OutcomeAllow,
				Lifespan: prompting.LifespanForever,
			},
			"write": &prompting.RulePermissionEntry{
				Outcome:    prompting.OutcomeDeny,
				Lifespan:   prompting.LifespanTimespan,
				Expiration: current.Time.Add(time.Second),
			},
		},
	}
	expired, err := constraints.ValidateForInterface("home", current)
	c.Check(err, IsNil)
	c.Check(expired, Equals, false)

	// Bad interface or permissions
	cases := []struct {
		iface  string
		perms  prompting.RulePermissionMap
		errStr string
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
			"home",
			prompting.RulePermissionMap{
				"read": &prompting.RulePermissionEntry{
					Outcome:    prompting.OutcomeType("bar"),
					Lifespan:   prompting.LifespanTimespan,
					Expiration: current.Time.Add(-time.Second),
				},
			},
			`invalid outcome: "bar"`,
		},
	}
	for _, testCase := range cases {
		constraints := &prompting.RuleConstraints{
			PathPattern: validPathPattern,
			Permissions: testCase.perms,
		}
		expired, err = constraints.ValidateForInterface(testCase.iface, current)
		c.Check(err, ErrorMatches, testCase.errStr)
		c.Check(expired, Equals, false)
	}

	// Check missing path pattern
	constraints = &prompting.RuleConstraints{
		Permissions: prompting.RulePermissionMap{
			"read": &prompting.RulePermissionEntry{
				Outcome:  prompting.OutcomeAllow,
				Lifespan: prompting.LifespanForever,
			},
		},
	}
	_, err = constraints.ValidateForInterface("home", current)
	c.Check(err, ErrorMatches, `invalid path pattern: no path pattern: ""`)
}

func (s *constraintsSuite) TestRuleConstraintsValidateForInterfaceExpiration(c *C) {
	pathPattern := mustParsePathPattern(c, "/path/to/foo")
	current := prompting.At{
		Time: time.Now(),
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
					Expiration: current.Time,
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
					Expiration: current.Time.Add(-time.Minute),
				},
				"write": &prompting.RulePermissionEntry{
					Outcome:    prompting.OutcomeDeny,
					Lifespan:   prompting.LifespanTimespan,
					Expiration: current.Time.Add(time.Minute),
				},
				"execute": &prompting.RulePermissionEntry{
					Outcome:    prompting.OutcomeDeny,
					Lifespan:   prompting.LifespanTimespan,
					Expiration: current.Time,
				},
			},
			false,
			prompting.RulePermissionMap{
				"write": &prompting.RulePermissionEntry{
					Outcome:    prompting.OutcomeDeny,
					Lifespan:   prompting.LifespanTimespan,
					Expiration: current.Time.Add(time.Minute),
				},
			},
		},
		{
			prompting.RulePermissionMap{
				"read": &prompting.RulePermissionEntry{
					Outcome:    prompting.OutcomeAllow,
					Lifespan:   prompting.LifespanTimespan,
					Expiration: current.Time.Add(-time.Minute),
				},
				"write": &prompting.RulePermissionEntry{
					Outcome:    prompting.OutcomeDeny,
					Lifespan:   prompting.LifespanTimespan,
					Expiration: current.Time,
				},
				"execute": &prompting.RulePermissionEntry{
					Outcome:    prompting.OutcomeAllow,
					Lifespan:   prompting.LifespanTimespan,
					Expiration: current.Time,
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
					Expiration: current.Time.Add(-time.Minute),
				},
				"write": &prompting.RulePermissionEntry{
					Outcome:    prompting.OutcomeAllow,
					Lifespan:   prompting.LifespanTimespan,
					Expiration: current.Time.Add(time.Minute),
				},
				"execute": &prompting.RulePermissionEntry{
					Outcome:  prompting.OutcomeDeny,
					Lifespan: prompting.LifespanForever,
				},
			},
			false,
			prompting.RulePermissionMap{
				"write": &prompting.RulePermissionEntry{
					Outcome:    prompting.OutcomeAllow,
					Lifespan:   prompting.LifespanTimespan,
					Expiration: current.Time.Add(time.Minute),
				},
				"execute": &prompting.RulePermissionEntry{
					Outcome:  prompting.OutcomeDeny,
					Lifespan: prompting.LifespanForever,
				},
			},
		},
	} {
		copiedPerms := make(prompting.RulePermissionMap, len(testCase.perms))
		for perm, entry := range testCase.perms {
			copiedPerms[perm] = entry
		}
		constraints := &prompting.RuleConstraints{
			PathPattern: pathPattern,
			Permissions: copiedPerms,
		}
		expired, err := constraints.ValidateForInterface("home", current)
		c.Check(err, IsNil)
		c.Check(expired, Equals, testCase.expired, Commentf("testCase: %+v\nremaining perms: %+v", testCase, constraints.Permissions))
		c.Check(constraints.Permissions, DeepEquals, testCase.expected, Commentf("testCase: %+v\nremaining perms: %+v", testCase, constraints.Permissions))
	}
}

func (s *constraintsSuite) TestReplyConstraintsToConstraintsHappy(c *C) {
	iface := "home"
	pathPattern := mustParsePathPattern(c, "/path/to/dir/{foo*,ba?/**}")

	for _, testCase := range []struct {
		pathPattern *patterns.PathPattern
		permissions []string
		outcome     prompting.OutcomeType
		lifespan    prompting.LifespanType
		duration    string
		expected    *prompting.Constraints
	}{
		{
			permissions: []string{"read", "write", "execute"},
			outcome:     prompting.OutcomeAllow,
			lifespan:    prompting.LifespanForever,
			expected: &prompting.Constraints{
				PathPattern: pathPattern,
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
			permissions: []string{"write", "read"},
			outcome:     prompting.OutcomeDeny,
			lifespan:    prompting.LifespanTimespan,
			duration:    "10m",
			expected: &prompting.Constraints{
				PathPattern: pathPattern,
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
	} {
		replyConstraints := &prompting.ReplyConstraints{
			PathPattern: pathPattern,
			Permissions: testCase.permissions,
		}
		constraints, err := replyConstraints.ToConstraints(iface, testCase.outcome, testCase.lifespan, testCase.duration)
		c.Check(err, IsNil)
		c.Check(constraints, DeepEquals, testCase.expected)
	}
}

func (s *constraintsSuite) TestReplyConstraintsToConstraintsUnhappy(c *C) {
	for _, testCase := range []struct {
		nilPattern  bool
		permissions []string
		iface       string
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
			nilPattern: true,
			errStr:     `invalid path pattern: no path pattern: ""`,
		},
		{
			iface:  "foo",
			errStr: `invalid interface: "foo"`,
		},
		{
			permissions: make([]string, 0),
			errStr:      `invalid permissions for home interface: permissions empty`,
		},
		{
			permissions: []string{"read", "append", "write", "create", "execute"},
			errStr:      `invalid permissions for home interface: "append", "create"`,
		},
	} {
		replyConstraints := &prompting.ReplyConstraints{
			PathPattern: mustParsePathPattern(c, "/path/to/foo"),
			Permissions: []string{"read", "write", "execute"},
		}
		if testCase.nilPattern {
			replyConstraints.PathPattern = nil
		}
		if testCase.permissions != nil {
			replyConstraints.Permissions = testCase.permissions
		}
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
		duration := ""
		if testCase.duration != "" {
			duration = testCase.duration
		}
		result, err := replyConstraints.ToConstraints(iface, outcome, lifespan, duration)
		c.Check(result, IsNil, Commentf("testCase: %+v", testCase))
		c.Check(err, ErrorMatches, testCase.errStr, Commentf("testCase: %+v", testCase))
	}
}

func (s *constraintsSuite) TestPatchRuleConstraintsHappy(c *C) {
	origTime := time.Now()
	patchTime := origTime.Add(time.Second)
	patchAt := prompting.At{
		Time: patchTime,
	}
	iface := "home"

	pathPattern := mustParsePathPattern(c, "/path/to/foo/ba?/**")

	for i, testCase := range []struct {
		initial *prompting.RuleConstraints
		patch   *prompting.RuleConstraintsPatch
		final   *prompting.RuleConstraints
	}{
		{
			initial: &prompting.RuleConstraints{
				PathPattern: pathPattern,
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
				},
			},
			patch: &prompting.RuleConstraintsPatch{},
			final: &prompting.RuleConstraints{
				PathPattern: pathPattern,
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
				},
			},
		},
		{
			initial: &prompting.RuleConstraints{
				PathPattern: pathPattern,
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
					// Remove both existing permissions, but add a new permission
					"read":  nil,
					"write": nil,
					"execute": &prompting.PermissionEntry{
						Outcome:  prompting.OutcomeDeny,
						Lifespan: prompting.LifespanTimespan,
						Duration: "1m",
					},
				},
			},
			final: &prompting.RuleConstraints{
				PathPattern: pathPattern,
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
				PathPattern: pathPattern,
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
					"write": nil,
					"execute": &prompting.PermissionEntry{
						Outcome:  prompting.OutcomeDeny,
						Lifespan: prompting.LifespanTimespan,
						Duration: "1m",
					},
				},
			},
			final: &prompting.RuleConstraints{
				PathPattern: pathPattern,
				Permissions: prompting.RulePermissionMap{
					"read": &prompting.RulePermissionEntry{
						Outcome:    prompting.OutcomeAllow,
						Lifespan:   prompting.LifespanTimespan,
						Expiration: patchTime.Add(time.Second),
					},
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
				PathPattern: pathPattern,
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
					"execute": &prompting.RulePermissionEntry{
						Outcome:  prompting.OutcomeAllow,
						Lifespan: prompting.LifespanForever,
					},
				},
			},
			patch: &prompting.RuleConstraintsPatch{
				Permissions: prompting.PermissionMap{
					"execute": &prompting.PermissionEntry{
						Outcome:  prompting.OutcomeAllow,
						Lifespan: prompting.LifespanForever,
					},
				},
			},
			final: &prompting.RuleConstraints{
				PathPattern: pathPattern,
				Permissions: prompting.RulePermissionMap{
					"read": &prompting.RulePermissionEntry{
						Outcome:    prompting.OutcomeAllow,
						Lifespan:   prompting.LifespanTimespan,
						Expiration: patchTime.Add(time.Second),
					},
					"execute": &prompting.RulePermissionEntry{
						Outcome:  prompting.OutcomeAllow,
						Lifespan: prompting.LifespanForever,
					},
				},
			},
		},
	} {
		patched, err := testCase.patch.PatchRuleConstraints(testCase.initial, iface, patchAt)
		c.Check(err, IsNil, Commentf("testCase %d", i))
		c.Check(patched, DeepEquals, testCase.final, Commentf("testCase %d", i))
	}
}

func (s *constraintsSuite) TestPatchRuleConstraintsUnhappy(c *C) {
	origTime := time.Now()
	patchTime := origTime.Add(time.Second)
	patchAt := prompting.At{
		Time: patchTime,
	}
	iface := "home"

	pathPattern := mustParsePathPattern(c, "/path/to/foo/ba{r,z{,/**/}}")

	goodRule := &prompting.RuleConstraints{
		PathPattern: pathPattern,
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
	goodPatch := &prompting.RuleConstraintsPatch{
		Permissions: prompting.PermissionMap{
			"write": nil,
			"execute": &prompting.PermissionEntry{
				Outcome:  prompting.OutcomeDeny,
				Lifespan: prompting.LifespanForever,
			},
		},
	}

	badIface := "foo"
	result, err := goodPatch.PatchRuleConstraints(goodRule, badIface, patchAt)
	c.Check(err, ErrorMatches, `invalid interface: "foo"`)
	c.Check(result, IsNil)

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
	expected := joinErrorsUnordered(`invalid duration: cannot have unspecified duration when lifespan is "timespan": ""`, `cannot create rule with lifespan "single"`) + "\n" + `invalid permissions for home interface: ("create", "lock"|"lock", "create")`

	result, err = badPatch.PatchRuleConstraints(goodRule, iface, patchAt)
	c.Check(err, ErrorMatches, expected)
	c.Check(result, IsNil)

	badPatch = &prompting.RuleConstraintsPatch{
		Permissions: prompting.PermissionMap{
			// Remove all permissions
			"read":  nil,
			"write": nil,
		},
	}
	result, err = badPatch.PatchRuleConstraints(goodRule, iface, patchAt)
	c.Check(err, Equals, prompting_errors.ErrPatchedRuleHasNoPerms)
	c.Check(result, IsNil)
}

func (s *constraintsSuite) TestRulePermissionMapExpired(c *C) {
	current := prompting.At{
		Time: time.Now(),
	}
	for _, pm := range []prompting.RulePermissionMap{
		{},
		{
			"read": &prompting.RulePermissionEntry{
				Outcome:    prompting.OutcomeAllow,
				Lifespan:   prompting.LifespanTimespan,
				Expiration: current.Time,
			},
		},
		{
			"read": &prompting.RulePermissionEntry{
				Outcome:    prompting.OutcomeAllow,
				Lifespan:   prompting.LifespanTimespan,
				Expiration: current.Time.Add(-time.Second),
			},
			"write": &prompting.RulePermissionEntry{
				Outcome:    prompting.OutcomeDeny,
				Lifespan:   prompting.LifespanTimespan,
				Expiration: current.Time,
			},
		},
	} {
		c.Check(pm.Expired(current), Equals, true, Commentf("%+v", pm))
	}

	for _, pm := range []prompting.RulePermissionMap{
		{
			"read": &prompting.RulePermissionEntry{
				Outcome:    prompting.OutcomeAllow,
				Lifespan:   prompting.LifespanTimespan,
				Expiration: current.Time.Add(-time.Second),
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
				Expiration: current.Time.Add(-time.Second),
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
				Expiration: current.Time.Add(time.Second),
			},
		},
	} {
		c.Check(pm.Expired(current), Equals, false, Commentf("%+v", pm))
	}
}

func (s *constraintsSuite) TestRulePermissionEntrySupersedes(c *C) {
	currTime := time.Now()
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
				Lifespan: prompting.LifespanSingle,
			},
			expected: true,
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
		c.Check(testCase.entry.Supersedes(testCase.other), Equals, testCase.expected, Commentf("testCase:\n\tentry: %+v\n\tother: %+v\n\texpected: %v", testCase.entry, testCase.other, testCase.expected))
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
	}
	for _, testCase := range cases {
		_, err := prompting.AbstractPermissionsToAppArmorPermissions(testCase.iface, testCase.perms)
		c.Check(err, ErrorMatches, testCase.errStr)
	}
}
