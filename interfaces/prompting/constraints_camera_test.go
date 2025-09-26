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

func (s *constraintsSuite) TestPromptConstraintsCameraEqual(c *C) {
	type fakePromptConstraints struct {
		prompting.PromptConstraintsCamera
	}

	for _, testCase := range []struct {
		left  *prompting.PromptConstraintsCamera
		right prompting.PromptConstraints // test against non-camera constraints
		equal bool
	}{
		{
			left: &prompting.PromptConstraintsCamera{
				PromptPermissions: prompting.PromptPermissions{
					OriginalPermissions: []string{"read", "write"},
				},
			},
			right: &prompting.PromptConstraintsCamera{
				PromptPermissions: prompting.PromptPermissions{
					OriginalPermissions: []string{"read", "write"},
				},
			},
			equal: true,
		},
		{
			left: &prompting.PromptConstraintsCamera{
				PromptPermissions: prompting.PromptPermissions{
					OriginalPermissions: []string{"read", "write"},
				},
			},
			right: &fakePromptConstraints{
				prompting.PromptConstraintsCamera{
					PromptPermissions: prompting.PromptPermissions{
						OriginalPermissions: []string{"read", "write"},
					},
				},
			},
			equal: false,
		},
		{
			left: &prompting.PromptConstraintsCamera{
				PromptPermissions: prompting.PromptPermissions{
					OriginalPermissions:    []string{"read", "write"},
					OutstandingPermissions: []string{"read"},
				},
			},
			right: &prompting.PromptConstraintsCamera{
				PromptPermissions: prompting.PromptPermissions{
					OriginalPermissions:    []string{"read", "write"},
					OutstandingPermissions: []string{"read", "write"},
					AvailablePermissions:   []string{"read", "write", "execute"},
				},
			},
			equal: true,
		},
		{
			left: &prompting.PromptConstraintsCamera{
				PromptPermissions: prompting.PromptPermissions{
					OriginalPermissions:    []string{"read", "write"},
					OutstandingPermissions: []string{"read"},
				},
			},
			right: &prompting.PromptConstraintsCamera{
				PromptPermissions: prompting.PromptPermissions{
					OriginalPermissions:    []string{"read"},
					OutstandingPermissions: []string{"read"},
				},
			},
			equal: false,
		},
	} {
		c.Check(testCase.left.Equal(testCase.right), Equals, testCase.equal, Commentf("testCase: %+v", testCase))
	}
}

func (s *constraintsSuite) TestReplyConstraintsCameraToConstraints(c *C) {
	c.Fatalf("TODO")
}

func (s *constraintsSuite) TestConstraintsCameraMatchPromptConstraints(c *C) {
	c.Fatalf("TODO")
}

func (s *constraintsSuite) TestConstraintsCameraToRuleConstraintsHappy(c *C) {
	at := prompting.At{
		Time:      time.Now(),
		SessionID: prompting.IDType(0x12345),
	}
	constraints := &prompting.ConstraintsCamera{
		PermissionMap: prompting.PermissionMap{
			"access": &prompting.PermissionEntry{
				Outcome:  prompting.OutcomeAllow,
				Lifespan: prompting.LifespanSession,
			},
		},
	}
	expectedRuleConstraints := &prompting.RuleConstraintsCamera{
		PermissionMap: prompting.RulePermissionMap{
			"access": &prompting.RulePermissionEntry{
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

func (s *constraintsSuite) TestConstraintsCameraToRuleConstraintsUnhappy(c *C) {
	at := prompting.At{
		Time:      time.Now(),
		SessionID: prompting.IDType(0),
	}

	badConstraints := &prompting.ConstraintsCamera{}
	result, err := badConstraints.ToRuleConstraints(at)
	c.Check(result, IsNil)
	c.Check(err, ErrorMatches, `invalid permissions for camera interface: permissions empty`)
}

func (s *constraintsSuite) TestRuleConstraintsCameraValidate(c *C) {
	c.Fatalf("TODO")
}

func (s *constraintsSuite) TestRuleConstraintsCameraMatchPromptConstraints(c *C) {
	c.Fatalf("TODO")
}

func (s *constraintsSuite) TestRuleConstraintsCameraCloneWithPermissions(c *C) {
	c.Fatalf("TODO")
}

func (s *constraintsSuite) TestRuleConstraintsPatchCameraPatchRuleConstraints(c *C) {
	c.Fatalf("TODO")
}
