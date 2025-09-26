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

	"github.com/snapcore/snapd/interfaces/prompting/patterns"
)

type PromptConstraintsCamera struct {
	// Embed PromptPermissions to inherit their marshalling
	PromptPermissions
}

func (pc *PromptConstraintsCamera) Permissions() *PromptPermissions {
	return &pc.PromptPermissions
}

func (pc *PromptConstraintsCamera) Equal(other PromptConstraints) bool {
	_, ok := other.(*PromptConstraintsCamera)
	if !ok {
		return false
	}
	return pc.Permissions().OriginalPermissionsEqual(other.Permissions())
}

// ReplyConstraintsCamera hold information about the applicability of a reply
// particular permissions. Upon receiving the reply, snapd converts
// ReplyConstraints to Constraints.
type ReplyConstraintsCamera struct {
	PermissionList []string `json:"permissions"`
}

func (rc *ReplyConstraintsCamera) Permissions() []string {
	return rc.PermissionList
}

// ToConstraints validates the given reply constraints, outcome, lifespan, and
// duration, and uses them to construct constraints.
func (rc *ReplyConstraintsCamera) ToConstraints(outcome OutcomeType, lifespan LifespanType, duration string) (Constraints, error) {
	const iface = "camera"
	permissionMap, err := NewPermissionMap(iface, rc.PermissionList, outcome, lifespan, duration)
	if err != nil {
		return nil, err
	}
	constraints := &ConstraintsCamera{
		PermissionMap: permissionMap,
	}
	return constraints, nil
}

// ConstraintsCamera hold information about the applicability of a new rule to
// particular permissions. When creating a new rule, snapd converts Constraints
// to RuleConstraints.
type ConstraintsCamera struct {
	PermissionMap PermissionMap `json:"permissions"`
}

func (c *ConstraintsCamera) Permissions() PermissionMap {
	return c.PermissionMap
}

// MatchPromptConstraints returns an error if the constraints do not match the
// given prompt constraints.
//
// This method is only intended to be called on constraints which have just
// been created from a reply, to check that the reply covers the request.
func (c *ConstraintsCamera) MatchPromptConstraints(promptConstraints PromptConstraints) error {
	_, ok := promptConstraints.(*PromptConstraintsCamera)
	if !ok {
		// Error should not occur, since we create the constraints for the
		// corresponding interface internally
		return errors.New(`internal error: cannot match "camera" constraints against prompt constraints for another interface`)
	}
	return nil
}

// ToRuleConstraints validates the receiving Constraints and converts it to
// RuleConstraints. If the constraints are not valid, returns an error.
func (c *ConstraintsCamera) ToRuleConstraints(at At) (RuleConstraints, error) {
	const iface = "camera"
	rulePermissions, err := c.PermissionMap.toRulePermissionMap(iface, at)
	if err != nil {
		return nil, err
	}
	ruleConstraints := &RuleConstraintsCamera{
		PermissionMap: rulePermissions,
	}
	return ruleConstraints, nil
}

// RuleConstraintsCamera hold information about the applicability of an existing
// rule to particular permissions. A request will be matched by the rule
// constraints if one or more requested permissions are denied in the permission
// map, or all of the requested permissions are allowed in the map.
type RuleConstraintsCamera struct {
	PermissionMap RulePermissionMap `json:"permissions"`
}

// Validate checks that the rule constraints are valid, and prunes any
// permissions which are expired at the given point in time. If all permissions
// have expired, then returns true. If the rule is invalid, returns an error.
func (c *RuleConstraintsCamera) Validate(at At) (expired bool, err error) {
	return c.PermissionMap.validateForInterface("camera", at)
}

// Permissions returns the permission map embedded in the rule constraints.
func (c *RuleConstraintsCamera) Permissions() RulePermissionMap {
	return c.PermissionMap
}

// MatchPromptConstraints returns true if the rule constraints match the given
// prompt constraints.
//
// If the constraints are invalid, returns an error.
func (c *RuleConstraintsCamera) MatchPromptConstraints(promptConstraints PromptConstraints) (bool, error) {
	_, ok := promptConstraints.(*PromptConstraintsCamera)
	if !ok {
		return false, nil
	}
	return true, nil
}

// CloneWithPermissions returns a copy of the constraints with the given
// permission map set as its permissions.
func (c *RuleConstraintsCamera) CloneWithPermissions(permissions RulePermissionMap) RuleConstraints {
	return &RuleConstraintsCamera{
		PermissionMap: permissions,
	}
}

// PathPattern returns the path pattern which should be used to match incoming
// requests.
func (c *RuleConstraintsCamera) PathPattern() *patterns.PathPattern {
	// Camera rules match any camera request, pattern doesn't matter. Since
	// the rule DB requires a path pattern, return a pattern which will match
	// any request path, even though we shouldn't be matching against paths.
	placeholderPattern, _ := patterns.ParsePathPattern("/**")
	// We know this pattern will be parsed properly, error cannot occur.
	return placeholderPattern
}

// RuleConstraintsPatchCamera holds partial rule contents which will be used to
// modify an existing rule. When snapd modifies the rule using RuleConstraintsPatch,
// it converts the RuleConstraintsPatch to RuleConstraints, using the rule's
// existing constraints wherever a field is omitted from the
// RuleConstraintsPatch.
//
// Any permissions which are omitted from the new permission map are left
// unchanged from the existing rule. To remove an existing permission from the
// rule, the permission should map to null.
type RuleConstraintsPatchCamera struct {
	PermissionMap PermissionMap `json:"permissions,omitempty"`
}

// PatchRuleConstraints validates the receiving RuleConstraintsPatchCamera and
// uses the given existing rule constraints to construct new rule constraints.
//
// If the permissions field is omitted, the permissions are left unchanged from
// the existing rule. If the permissions field is present in the patch, then
// any non-expired permissions which are omitted from the patch's permission map
// are left unchanged from the existing rule. To remove an existing permission
// from the rule, the permission should map to null in the permission map of the
// patch.
//
// The given at information is used to prune any existing expired permissions
// and compute any expirations for new permissions.
//
// The existing rule constraints are not mutated.
func (c *RuleConstraintsPatchCamera) PatchRuleConstraints(existing RuleConstraints, at At) (RuleConstraints, error) {
	if c == nil {
		c = &RuleConstraintsPatchCamera{}
	}
	existingConstraints, ok := existing.(*RuleConstraintsCamera)
	if !ok {
		// Error should never occur, caller should ensure interfaces match
		return nil, errors.New(`internal error: cannot use a constraints patch for the "camera" interface to patch constraints for another interface`)
	}
	ruleConstraints := &RuleConstraintsCamera{}
	if c.PermissionMap == nil {
		ruleConstraints.PermissionMap = existingConstraints.PermissionMap
		return ruleConstraints, nil
	}
	// Permissions are specified in the patch, need to merge them
	const iface = "camera"
	newPermissions, err := c.PermissionMap.patchRulePermissions(existingConstraints.PermissionMap, iface, at)
	if err != nil {
		return nil, err
	}
	ruleConstraints.PermissionMap = newPermissions
	return ruleConstraints, nil
}
