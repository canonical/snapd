// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2025 Canonical Ltd
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
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/interfaces/prompting"
)

func (s *constraintsSuite) TestPromptConstraintsHomeEqual(c *C) {
	type fakePromptConstraints struct {
		prompting.PromptConstraintsHome
	}

	for _, testCase := range []struct {
		left  *prompting.PromptConstraintsHome
		right prompting.PromptConstraints // test against non-home constraints
		equal bool
	}{
		{
			left: &prompting.PromptConstraintsHome{
				Path: "/path/to/foo",
				PromptPermissions: prompting.PromptPermissions{
					OriginalPermissions: []string{"read", "write"},
				},
			},
			right: &prompting.PromptConstraintsHome{
				Path: "/path/to/foo",
				PromptPermissions: prompting.PromptPermissions{
					OriginalPermissions: []string{"read", "write"},
				},
			},
			equal: true,
		},
		{
			left: &prompting.PromptConstraintsHome{
				Path: "/path/to/foo",
				PromptPermissions: prompting.PromptPermissions{
					OriginalPermissions: []string{"read", "write"},
				},
			},
			right: &fakePromptConstraints{
				prompting.PromptConstraintsHome{
					Path: "/path/to/foo",
					PromptPermissions: prompting.PromptPermissions{
						OriginalPermissions: []string{"read", "write"},
					},
				},
			},
			equal: false,
		},
		{
			left: &prompting.PromptConstraintsHome{
				Path: "/path/to/foo",
				PromptPermissions: prompting.PromptPermissions{
					OriginalPermissions:    []string{"read", "write"},
					OutstandingPermissions: []string{"read"},
				},
			},
			right: &prompting.PromptConstraintsHome{
				Path: "/path/to/foo",
				PromptPermissions: prompting.PromptPermissions{
					OriginalPermissions:    []string{"read", "write"},
					OutstandingPermissions: []string{"read", "write"},
					AvailablePermissions:   []string{"read", "write", "execute"},
				},
			},
			equal: true,
		},
		{
			left: &prompting.PromptConstraintsHome{
				Path: "/path/to/foo",
				PromptPermissions: prompting.PromptPermissions{
					OriginalPermissions:    []string{"read", "write"},
					OutstandingPermissions: []string{"read"},
				},
			},
			right: &prompting.PromptConstraintsHome{
				Path: "/path/to/foo",
				PromptPermissions: prompting.PromptPermissions{
					OriginalPermissions:    []string{"read"},
					OutstandingPermissions: []string{"read"},
				},
			},
			equal: false,
		},
		{
			left: &prompting.PromptConstraintsHome{
				Path: "/path/to/foo",
				PromptPermissions: prompting.PromptPermissions{
					OriginalPermissions: []string{"read", "write"},
				},
			},
			right: &prompting.PromptConstraintsHome{
				Path: "/path/to/bar",
				PromptPermissions: prompting.PromptPermissions{
					OriginalPermissions: []string{"read", "write"},
				},
			},
			equal: false,
		},
	} {
		c.Check(testCase.left.Equal(testCase.right), Equals, testCase.equal, Commentf("testCase: %+v", testCase))
	}
}

func (s *constraintsSuite) TestReplyConstraintsHomeToConstraints(c *C) {
	c.Fatalf("TODO")
}

func (s *constraintsSuite) TestConstraintsHomeMatchPromptConstraints(c *C) {
	c.Fatalf("TODO")
}

func (s *constraintsSuite) TestConstraintsHomeToRuleConstraintsHappy(c *C) {
	at := prompting.At{
		Time:      time.Now(),
		SessionID: prompting.IDType(0x12345),
	}
	pathPattern := mustParsePathPattern(c, "/path/to/{foo,*or*,bar}{,/}**")
	constraints := &prompting.ConstraintsHome{
		PathPattern: pathPattern,
		PermissionMap: prompting.PermissionMap{
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
	}
	expectedRuleConstraints := &prompting.RuleConstraintsHome{
		Pattern: pathPattern,
		PermissionMap: prompting.RulePermissionMap{
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
	}
	result, err := constraints.ToRuleConstraints(at)
	c.Assert(err, IsNil)
	c.Assert(result, DeepEquals, expectedRuleConstraints)
}

func (s *constraintsSuite) TestConstraintsHomeToRuleConstraintsUnhappy(c *C) {
	at := prompting.At{
		Time:      time.Now(),
		SessionID: prompting.IDType(0),
	}

	badConstraints := &prompting.ConstraintsHome{}
	result, err := badConstraints.ToRuleConstraints(at)
	c.Check(result, IsNil)
	c.Check(err, ErrorMatches, `invalid path pattern: no path pattern.*`)

	badConstraints.PathPattern = mustParsePathPattern(c, "/path/to/foo")
	result, err = badConstraints.ToRuleConstraints(at)
	c.Check(result, IsNil)
	c.Check(err, ErrorMatches, `invalid permissions for home interface: permissions empty`)
}

func (s *constraintsSuite) TestRuleConstraintsHomeValidate(c *C) {
	c.Fatalf("TODO")
}

func (s *constraintsSuite) TestRuleConstraintsHomeMatchPromptConstraints(c *C) {
	c.Fatalf("TODO")
}

func (s *constraintsSuite) TestRuleConstraintsHomeCloneWithPermissions(c *C) {
	c.Fatalf("TODO")
}

func (s *constraintsSuite) TestRuleConstraintsPatchHomePatchRuleConstraints(c *C) {
	c.Fatalf("TODO")
}
