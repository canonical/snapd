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

package prompting

import (
	"errors"

	prompting_errors "github.com/snapcore/snapd/interfaces/prompting/errors"
	"github.com/snapcore/snapd/interfaces/prompting/patterns"
)

type PromptConstraintsHome struct {
	// Path is the path to which the application is requesting access.
	Path string `json:"path"`
	// Embed PromptPermissions to inherit their marshalling
	PromptPermissions
}

func (pc *PromptConstraintsHome) Permissions() *PromptPermissions {
	return &pc.PromptPermissions
}

func (pc *PromptConstraintsHome) Equal(other PromptConstraints) bool {
	otherConstraints, ok := other.(*PromptConstraintsHome)
	if !ok {
		return false
	}
	if pc.Path != otherConstraints.Path {
		return false
	}
	return pc.Permissions().OriginalPermissionsEqual(other.Permissions())
}

// ReplyConstraintsHome hold information about the applicability of a reply to
// particular paths and permissions. Upon receiving the reply, snapd converts
// ReplyConstraints to Constraints.
type ReplyConstraintsHome struct {
	PathPattern    *patterns.PathPattern `json:"path-pattern"`
	PermissionList []string              `json:"permissions"`
}

func (rc *ReplyConstraintsHome) Permissions() []string {
	return rc.PermissionList
}

// ToConstraints validates the given reply constraints, outcome, lifespan, and
// duration, and uses them to construct constraints.
func (rc *ReplyConstraintsHome) ToConstraints(outcome OutcomeType, lifespan LifespanType, duration string) (Constraints, error) {
	if rc.PathPattern == nil {
		return nil, prompting_errors.NewInvalidPathPatternError("", "no path pattern")
	}
	const iface = "home"
	permissionMap, err := NewPermissionMap(iface, rc.PermissionList, outcome, lifespan, duration)
	if err != nil {
		return nil, err
	}
	constraints := &ConstraintsHome{
		PathPattern:   rc.PathPattern,
		PermissionMap: permissionMap,
	}
	return constraints, nil
}

// ConstraintsHome hold information about the applicability of a new rule to
// particular paths and permissions. When creating a new rule, snapd converts
// Constraints to RuleConstraints.
type ConstraintsHome struct {
	PathPattern   *patterns.PathPattern `json:"path-pattern"`
	PermissionMap PermissionMap         `json:"permissions"`
}

func (c *ConstraintsHome) Permissions() PermissionMap {
	return c.PermissionMap
}

// MatchPromptConstraints returns an error if the constraints do not match the
// given prompt constraints.
//
// This method is only intended to be called on constraints which have just
// been created from a reply, to check that the reply covers the request.
func (c *ConstraintsHome) MatchPromptConstraints(promptConstraints PromptConstraints) error {
	if c.PathPattern == nil {
		return prompting_errors.NewInvalidPathPatternError("", "no path pattern")
	}
	promptConstraintsHome, ok := promptConstraints.(*PromptConstraintsHome)
	if !ok {
		// Error should not occur, since we create the constraints for the
		// corresponding interface internally
		return errors.New(`internal error: cannot match "home" constraints against prompt constraints for another interface`)
	}
	match, err := c.PathPattern.Match(promptConstraintsHome.Path)
	if err != nil {
		// Error should not occur, since it was parsed internally
		return prompting_errors.NewInvalidPathPatternError(c.PathPattern.String(), err.Error())
	}
	if !match {
		return &prompting_errors.RequestedPathNotMatchedError{
			Requested: promptConstraintsHome.Path,
			Replied:   c.PathPattern.String(),
		}
	}
	return nil
}

// ToRuleConstraints validates the receiving Constraints and converts it to
// RuleConstraints. If the constraints are not valid, returns an error.
func (c *ConstraintsHome) ToRuleConstraints(at At) (RuleConstraints, error) {
	if c.PathPattern == nil {
		return nil, prompting_errors.NewInvalidPathPatternError("", "no path pattern")
	}
	const iface = "home"
	rulePermissions, err := c.PermissionMap.toRulePermissionMap(iface, at)
	if err != nil {
		return nil, err
	}
	ruleConstraints := &RuleConstraintsHome{
		Pattern:       c.PathPattern,
		PermissionMap: rulePermissions,
	}
	return ruleConstraints, nil
}

// RuleConstraintsHome hold information about the applicability of an existing rule
// to particular paths and permissions. A request will be matched by the rule
// constraints if the requested path is matched by the path pattern (according
// to bash's globstar matching) and one or more requested permissions are denied
// in the permission map, or all of the requested permissions are allowed in the
// map.
type RuleConstraintsHome struct {
	Pattern       *patterns.PathPattern `json:"path-pattern"`
	PermissionMap RulePermissionMap     `json:"permissions"`
}

// Validate checks that the rule constraints are valid, and prunes any
// permissions which are expired at the given point in time. If all permissions
// have expired, then returns true. If the rule is invalid, returns an error.
func (c *RuleConstraintsHome) Validate(at At) (expired bool, err error) {
	if c.Pattern == nil {
		return false, prompting_errors.NewInvalidPathPatternError("", "no path pattern")
	}
	return c.PermissionMap.validateForInterface("home", at)
}

// Permissions returns the permission map embedded in the rule constraints.
func (c *RuleConstraintsHome) Permissions() RulePermissionMap {
	return c.PermissionMap
}

// MatchPromptConstraints returns true if the rule constraints match the given
// prompt constraints.
//
// If the constraints are invalid, returns an error.
func (c *RuleConstraintsHome) MatchPromptConstraints(promptConstraints PromptConstraints) (bool, error) {
	if c.Pattern == nil {
		return false, prompting_errors.NewInvalidPathPatternError("", "no path pattern")
	}
	promptConstraintsHome, ok := promptConstraints.(*PromptConstraintsHome)
	if !ok {
		return false, nil
	}
	match, err := c.Pattern.Match(promptConstraintsHome.Path)
	if err != nil {
		// Error should not occur, since it was parsed internally
		return false, prompting_errors.NewInvalidPathPatternError(c.Pattern.String(), err.Error())
	}
	return match, nil
}

// CloneWithPermissions returns a copy of the constraints with the given
// permission map set as its permissions.
func (c *RuleConstraintsHome) CloneWithPermissions(permissions RulePermissionMap) RuleConstraints {
	return &RuleConstraintsHome{
		Pattern:       c.Pattern,
		PermissionMap: permissions,
	}
}

// PathPattern returns the path pattern which should be used to match incoming
// requests.
func (c *RuleConstraintsHome) PathPattern() *patterns.PathPattern {
	return c.Pattern
}

// RuleConstraintsPatchHome holds partial rule contents which will be used to modify
// an existing rule. When snapd modifies the rule using RuleConstraintsPatch,
// it converts the RuleConstraintsPatch to RuleConstraints, using the rule's
// existing constraints wherever a field is omitted from the
// RuleConstraintsPatch.
//
// Any permissions which are omitted from the new permission map are left
// unchanged from the existing rule. To remove an existing permission from the
// rule, the permission should map to null.
type RuleConstraintsPatchHome struct {
	Pattern       *patterns.PathPattern `json:"path-pattern,omitempty"`
	PermissionMap PermissionMap         `json:"permissions,omitempty"`
}

// PatchRuleConstraints validates the receiving RuleConstraintsPatchHome and
// uses the given existing rule constraints to construct new rule constraints.
//
// If the path pattern or permissions fields are omitted, they are left
// unchanged from the existing rule. If the permissions field is present in
// the patch, then any permissions which are omitted from the patch's
// permission map are left unchanged from the existing rule. To remove an
// existing permission from the rule, the permission should map to null in the
// permission map of the patch.
//
// The given at information is used to prune any existing expired permissions
// and compute any expirations for new permissions.
//
// The existing rule constraints are not mutated.
func (c *RuleConstraintsPatchHome) PatchRuleConstraints(existing RuleConstraints, at At) (RuleConstraints, error) {
	if c == nil {
		c = &RuleConstraintsPatchHome{}
	}
	existingConstraints, ok := existing.(*RuleConstraintsHome)
	if !ok {
		// Error should never occur, caller should ensure interfaces match
		return nil, errors.New(`internal error: cannot use a constraints patch for the "home" interface to patch constraints for another interface`)
	}
	ruleConstraints := &RuleConstraintsHome{
		Pattern: c.Pattern,
	}
	if c.Pattern == nil {
		ruleConstraints.Pattern = existingConstraints.Pattern
	}
	if c.PermissionMap == nil {
		ruleConstraints.PermissionMap = existingConstraints.PermissionMap
		return ruleConstraints, nil
	}
	// Permissions are specified in the patch, need to merge them
	const iface = "home"
	newPermissions, err := c.PermissionMap.patchRulePermissions(existingConstraints.PermissionMap, iface, at)
	if err != nil {
		return nil, err
	}
	ruleConstraints.PermissionMap = newPermissions
	return ruleConstraints, nil
}
