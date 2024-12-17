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

package prompting

import (
	"fmt"
	"sort"
	"time"

	prompting_errors "github.com/snapcore/snapd/interfaces/prompting/errors"
	"github.com/snapcore/snapd/interfaces/prompting/patterns"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/sandbox/apparmor/notify"
	"github.com/snapcore/snapd/strutil"
)

// Constraints hold information about the applicability of a new rule to
// particular paths or permissions. A request will be matched by the constraints
// if the requested path is matched by the path pattern (according to bash's
// globstar matching) and one or more requested permissions are denied in the
// permission map, or all of the requested permissions are allowed in the map.
// When snapd creates a new rule, it converts Constraints to RuleConstraints.
type Constraints struct {
	PathPattern *patterns.PathPattern `json:"path-pattern"`
	Permissions PermissionMap         `json:"permissions"`
}

// Match returns true if the constraints match the given path, otherwise false.
//
// If the constraints or path are invalid, returns an error.
//
// This method is only intended to be called on constraints which have just
// been created from a reply, to check that the reply covers the request.
func (c *Constraints) Match(path string) (bool, error) {
	if c.PathPattern == nil {
		return false, prompting_errors.NewInvalidPathPatternError("", "no path pattern")
	}
	match, err := c.PathPattern.Match(path)
	if err != nil {
		// Error should not occur, since it was parsed internally
		return false, prompting_errors.NewInvalidPathPatternError(c.PathPattern.String(), err.Error())
	}
	return match, nil
}

// ContainPermissions returns true if the permission map in the constraints
// includes every one of the given permissions.
//
// This method is only intended to be called on constraints which have just
// been created from a reply, to check that the reply covers the request.
func (c *Constraints) ContainPermissions(permissions []string) bool {
	for _, perm := range permissions {
		if _, exists := c.Permissions[perm]; !exists {
			return false
		}
	}
	return true
}

// ToRuleConstraints validates the receiving Constraints and converts it to
// RuleConstraints. If the constraints are not valid with respect to the given
// interface, returns an error.
func (c *Constraints) ToRuleConstraints(iface string, currTime time.Time) (*RuleConstraints, error) {
	if c.PathPattern == nil {
		return nil, prompting_errors.NewInvalidPathPatternError("", "no path pattern")
	}
	rulePermissions, err := c.Permissions.toRulePermissionMap(iface, currTime)
	if err != nil {
		return nil, err
	}
	ruleConstraints := &RuleConstraints{
		PathPattern: c.PathPattern,
		Permissions: rulePermissions,
	}
	return ruleConstraints, nil
}

// RuleConstraints hold information about the applicability of an existing rule
// to particular paths or permissions. A request will be matched by the rule
// constraints if the requested path is matched by the path pattern (according
// to bash's globstar matching) and one or more requested permissions are denied
// in the permission map, or all of the requested permissions are allowed in the
// map.
type RuleConstraints struct {
	PathPattern *patterns.PathPattern `json:"path-pattern"`
	Permissions RulePermissionMap     `json:"permissions"`
}

// ValidateForInterface checks that the rule constraints are valid for the given
// interface. Any permissions which have expired relative to the given current
// time are pruned. If all permissions have expired, then returns true. If the
// rule is invalid, returns an error.
func (c *RuleConstraints) ValidateForInterface(iface string, currTime time.Time) (expired bool, err error) {
	if c.PathPattern == nil {
		return false, prompting_errors.NewInvalidPathPatternError("", "no path pattern")
	}
	return c.Permissions.validateForInterface(iface, currTime)
}

// Match returns true if the constraints match the given path, otherwise false.
//
// If the constraints or path are invalid, returns an error.
func (c *RuleConstraints) Match(path string) (bool, error) {
	if c.PathPattern == nil {
		return false, prompting_errors.NewInvalidPathPatternError("", "no path pattern")
	}
	match, err := c.PathPattern.Match(path)
	if err != nil {
		// Error should not occur, since it was parsed internally
		return false, prompting_errors.NewInvalidPathPatternError(c.PathPattern.String(), err.Error())
	}
	return match, nil
}

// ReplyConstraints hold information about the applicability of a reply to
// particular paths or permissions. Upon receiving the reply, snapd converts
// ReplyConstraints to Constraints.
type ReplyConstraints struct {
	PathPattern *patterns.PathPattern `json:"path-pattern"`
	Permissions []string              `json:"permissions"`
}

// ToConstraints validates the receiving ReplyConstraints with respect to the
// given interface, along with the given outcome, lifespan, and duration, and
// constructs an equivalent Constraints from the ReplyConstraints.
func (c *ReplyConstraints) ToConstraints(iface string, outcome OutcomeType, lifespan LifespanType, duration string) (*Constraints, error) {
	if _, err := outcome.AsBool(); err != nil {
		// Should not occur, as outcome is validated when unmarshalled
		return nil, err
	}
	if _, err := lifespan.ParseDuration(duration, time.Now()); err != nil {
		return nil, err
	}
	if c.PathPattern == nil {
		return nil, prompting_errors.NewInvalidPathPatternError("", "no path pattern")
	}
	availablePerms, ok := interfacePermissionsAvailable[iface]
	if !ok {
		return nil, prompting_errors.NewInvalidInterfaceError(iface, availableInterfaces())
	}
	if len(c.Permissions) == 0 {
		return nil, prompting_errors.NewPermissionsListEmptyError(iface, availablePerms)
	}
	var invalidPerms []string
	permissionMap := make(PermissionMap, len(c.Permissions))
	for _, perm := range c.Permissions {
		if !strutil.ListContains(availablePerms, perm) {
			invalidPerms = append(invalidPerms, perm)
			continue
		}
		permissionMap[perm] = &PermissionEntry{
			Outcome:  outcome,
			Lifespan: lifespan,
			Duration: duration,
		}
	}
	if len(invalidPerms) > 0 {
		return nil, prompting_errors.NewInvalidPermissionsError(iface, invalidPerms, availablePerms)
	}
	constraints := &Constraints{
		PathPattern: c.PathPattern,
		Permissions: permissionMap,
	}
	return constraints, nil
}

// PatchConstraints hold information about the applicability of the modified
// rule to particular paths or permissions. A request will be matched by the
// constraints if the requested path is matched by the path pattern (according
// to bash's globstar matching) and one or more requested permissions are
// denied in the permission map, or all of the requested permissions are
// allowed in the map. When snapd modifies the rule, it converts
// PatchConstraints to RuleConstraints, using the rule's existing constraints
// wherever a field is omitted.
//
// Any permissions which are omitted from the new permission map are left
// unchanged from the existing rule. To remove an existing permission from the
// rule, the permission should map to null.
type PatchConstraints struct {
	PathPattern *patterns.PathPattern `json:"path-pattern,omitempty"`
	Permissions PermissionMap         `json:"permissions,omitempty"`
}

// PatchRuleConstraints validates the receiving PatchConstraints and uses the
// existing rule constraints to construct a new RuleConstraints.
//
// If the path pattern or permissions fields are omitted, they are left
// unchanged from the existing rule. If the permissions field is present in
// the patch constraints, then any permissions which are omitted from the
// patch constrants' permission map are left unchanged from the existing rule.
// To remove an an existing permission from the rule, the permission should map
// to null in the permission map of the patch constraints.
//
// The existing rule constraints should never be modified.
func (c *PatchConstraints) PatchRuleConstraints(existing *RuleConstraints, iface string, currTime time.Time) (*RuleConstraints, error) {
	ruleConstraints := &RuleConstraints{
		PathPattern: c.PathPattern,
	}
	if c.PathPattern == nil {
		ruleConstraints.PathPattern = existing.PathPattern
	}
	if c.Permissions == nil {
		ruleConstraints.Permissions = existing.Permissions
		return ruleConstraints, nil
	}
	// Permissions are specified in the patch constraints, need to merge them
	newPermissions := make(RulePermissionMap, len(c.Permissions)+len(existing.Permissions))
	// Pre-populate newPermissions with all the non-expired existing permissions
	for perm, entry := range existing.Permissions {
		if !entry.Expired(currTime) {
			newPermissions[perm] = entry
		}
	}
	availablePerms, ok := interfacePermissionsAvailable[iface]
	if !ok {
		// Should not occur, as we should use the interface from the existing rule
		return nil, prompting_errors.NewInvalidInterfaceError(iface, availableInterfaces())
	}
	var errs []error
	var invalidPerms []string
	for perm, entry := range c.Permissions {
		if !strutil.ListContains(availablePerms, perm) {
			invalidPerms = append(invalidPerms, perm)
			continue
		}
		if entry == nil {
			// nil value for permission indicates that it should be removed.
			// (In contrast, omitted permissions are left unchanged from the
			// original constraints.)
			delete(newPermissions, perm)
			continue
		}
		ruleEntry, err := entry.toRulePermissionEntry(currTime)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		newPermissions[perm] = ruleEntry
	}
	if len(invalidPerms) > 0 {
		errs = append(errs, prompting_errors.NewInvalidPermissionsError(iface, invalidPerms, availablePerms))
	}
	if len(errs) > 0 {
		return nil, prompting_errors.Join(errs...)
	}
	ruleConstraints.Permissions = newPermissions
	return ruleConstraints, nil
}

// PermissionMap is a map from permissions to their corresponding entries,
// which contain information about the outcome and lifespan for those
// permissions.
type PermissionMap map[string]*PermissionEntry

// toRulePermissionMap validates the receiving PermissionMap and converts it
// to a RulePermissionMap, using the given current time to convert any included
// durations to expirations. If the permission map is not valid with respect to
// the given interface, returns an error.
func (pm PermissionMap) toRulePermissionMap(iface string, currTime time.Time) (RulePermissionMap, error) {
	availablePerms, ok := interfacePermissionsAvailable[iface]
	if !ok {
		return nil, prompting_errors.NewInvalidInterfaceError(iface, availableInterfaces())
	}
	if len(pm) == 0 {
		return nil, prompting_errors.NewPermissionsListEmptyError(iface, availablePerms)
	}
	var errs []error
	var invalidPerms []string
	rulePermissionMap := make(RulePermissionMap, len(pm))
	for perm, entry := range pm {
		if !strutil.ListContains(availablePerms, perm) {
			invalidPerms = append(invalidPerms, perm)
			continue
		}
		rulePermissionEntry, err := entry.toRulePermissionEntry(currTime)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		rulePermissionMap[perm] = rulePermissionEntry
	}
	if len(invalidPerms) > 0 {
		errs = append(errs, prompting_errors.NewInvalidPermissionsError(iface, invalidPerms, availablePerms))
	}
	if len(errs) > 0 {
		return nil, prompting_errors.Join(errs...)
	}
	return rulePermissionMap, nil
}

// RulePermissionMap is a map from permissions to their corresponding entries,
// which contain information about the outcome and lifespan for those
// permissions.
type RulePermissionMap map[string]*RulePermissionEntry

// validateForInterface checks that the rule permission map is valid for the
// given interface. Any permissions which have expired relative to the given
// current time are pruned. If all permissions have expired, then returns true.
// If the permission map is invalid, returns an error.
func (pm RulePermissionMap) validateForInterface(iface string, currTime time.Time) (expired bool, err error) {
	availablePerms, ok := interfacePermissionsAvailable[iface]
	if !ok {
		return false, prompting_errors.NewInvalidInterfaceError(iface, availableInterfaces())
	}
	if len(pm) == 0 {
		return false, prompting_errors.NewPermissionsListEmptyError(iface, availablePerms)
	}
	var errs []error
	var invalidPerms []string
	var expiredPerms []string
	for perm, entry := range pm {
		if !strutil.ListContains(availablePerms, perm) {
			invalidPerms = append(invalidPerms, perm)
			continue
		}
		if err := entry.validate(); err != nil {
			errs = append(errs, err)
			continue
		}
		if entry.Expired(currTime) {
			expiredPerms = append(expiredPerms, perm)
			continue
		}
	}
	if len(invalidPerms) > 0 {
		errs = append(errs, prompting_errors.NewInvalidPermissionsError(iface, invalidPerms, availablePerms))
	}
	if len(errs) > 0 {
		return false, prompting_errors.Join(errs...)
	}
	for _, perm := range expiredPerms {
		delete(pm, perm)
	}
	if len(pm) == 0 {
		// All permissions expired
		return true, nil
	}
	return false, nil
}

// Expired returns true if all permissions in the map have expired.
func (pm RulePermissionMap) Expired(currTime time.Time) bool {
	for _, entry := range pm {
		if !entry.Expired(currTime) {
			return false
		}
	}
	return true
}

// PermissionEntry holds the outcome associated with a particular permission
// and the lifespan for which that outcome is applicable.
//
// PermissionEntry is used when replying to a prompt, creating a new rule, or
// modifying an existing rule.
type PermissionEntry struct {
	Outcome  OutcomeType  `json:"outcome"`
	Lifespan LifespanType `json:"lifespan"`
	Duration string       `json:"duration,omitempty"`
}

// toRulePermissionEntry validates the receiving PermissionEntry and converts
// it to a RulePermissionEntry.
//
// Checks that the entry has a valid outcome, and that its lifespan is valid
// for a rule (i.e. not LifespanSingle), and that it has an appropriate
// duration for that lifespan. The duration, combined with the given current
// time, is used to compute an expiration time, and that is returned as part
// of the corresponding RulePermissionEntry.
func (e *PermissionEntry) toRulePermissionEntry(currTime time.Time) (*RulePermissionEntry, error) {
	if _, err := e.Outcome.AsBool(); err != nil {
		return nil, err
	}
	if e.Lifespan == LifespanSingle {
		// We don't allow rules with lifespan "single"
		return nil, prompting_errors.NewRuleLifespanSingleError(SupportedRuleLifespans)
	}
	expiration, err := e.Lifespan.ParseDuration(e.Duration, currTime)
	if err != nil {
		return nil, err
	}
	rulePermissionEntry := &RulePermissionEntry{
		Outcome:    e.Outcome,
		Lifespan:   e.Lifespan,
		Expiration: expiration,
	}
	return rulePermissionEntry, nil
}

// RulePermissionEntry holds the outcome associated with a particular permission
// and the lifespan for which that outcome is applicable.
//
// RulePermissionEntry is derived from a PermissionEntry after it has been used
// along with the rule's timestamp to define the expiration timeouts for any
// permissions which have a lifespan of "timespan". RulePermissionEntry is what
// is returned when retrieving rule contents, but PermissionEntry is used when
// replying to prompts, creating new rules, or modifying existing rules.
type RulePermissionEntry struct {
	Outcome    OutcomeType  `json:"outcome"`
	Lifespan   LifespanType `json:"lifespan"`
	Expiration time.Time    `json:"expiration,omitempty"`
}

// Expired returns true if the receiving permission entry has expired and
// should no longer be considered when matching requests.
//
// This is the case if the permission has a lifespan of timespan and the
// current time is after its expiration time.
func (e *RulePermissionEntry) Expired(currTime time.Time) bool {
	switch e.Lifespan {
	case LifespanTimespan:
		if !currTime.Before(e.Expiration) {
			return true
		}
		// TODO: add lifespan session
		//case LifespanSession:
		// TODO: return true if the user session has changed
	}
	return false
}

// validate checks that the entry has a valid outcome, and that its lifespan
// is valid for a rule (i.e. not LifespanSingle), and has an appropriate
// expiration for that lifespan.
func (e *RulePermissionEntry) validate() error {
	if _, err := e.Outcome.AsBool(); err != nil {
		return err
	}
	if e.Lifespan == LifespanSingle {
		// We don't allow rules with lifespan "single"
		return prompting_errors.NewRuleLifespanSingleError(SupportedRuleLifespans)
	}
	if err := e.Lifespan.ValidateExpiration(e.Expiration); err != nil {
		// Should never error due to an API request, since rules are always
		// added via the API using duration, rather than expiration.
		// Error may occur when validating a rule loaded from disk.
		// We don't check expiration as part of validation.
		return err
	}
	return nil
}

var (
	// List of permissions available for each interface. This also defines the
	// order in which the permissions should be presented.
	interfacePermissionsAvailable = map[string][]string{
		"home": {"read", "write", "execute"},
	}

	// A mapping from interfaces which support AppArmor file permissions to
	// the map between abstract permissions and those file permissions.
	//
	// Never include AA_MAY_OPEN in the maps below; it should always come from
	// the kernel with another permission (e.g. AA_MAY_READ or AA_MAY_WRITE),
	// and if it does not, it should be interpreted as AA_MAY_READ.
	interfaceFilePermissionsMaps = map[string]map[string]notify.FilePermission{
		"home": {
			"read":    notify.AA_MAY_READ | notify.AA_MAY_GETATTR,
			"write":   notify.AA_MAY_WRITE | notify.AA_MAY_APPEND | notify.AA_MAY_CREATE | notify.AA_MAY_DELETE | notify.AA_MAY_RENAME | notify.AA_MAY_SETATTR | notify.AA_MAY_CHMOD | notify.AA_MAY_LOCK | notify.AA_MAY_LINK,
			"execute": notify.AA_MAY_EXEC | notify.AA_EXEC_MMAP,
		},
	}
)

// availableInterfaces returns the list of supported interfaces.
func availableInterfaces() []string {
	interfaces := make([]string, 0, len(interfacePermissionsAvailable))
	for iface := range interfacePermissionsAvailable {
		interfaces = append(interfaces, iface)
	}
	sort.Strings(interfaces)
	return interfaces
}

// AvailablePermissions returns the list of available permissions for the given
// interface.
func AvailablePermissions(iface string) ([]string, error) {
	available, exist := interfacePermissionsAvailable[iface]
	if !exist {
		return nil, fmt.Errorf("cannot get available permissions: unsupported interface: %s", iface)
	}
	return available, nil
}

// AbstractPermissionsFromAppArmorPermissions returns the list of permissions
// corresponding to the given AppArmor permissions for the given interface.
func AbstractPermissionsFromAppArmorPermissions(iface string, permissions any) ([]string, error) {
	filePerms, ok := permissions.(notify.FilePermission)
	if !ok {
		return nil, fmt.Errorf("cannot parse the given permissions as file permissions: %v", permissions)
	}
	if filePerms == notify.FilePermission(0) {
		return nil, fmt.Errorf("cannot get abstract permissions from empty AppArmor permissions: %q", filePerms)
	}
	abstractPermsAvailable, exists := interfacePermissionsAvailable[iface]
	if !exists {
		return nil, fmt.Errorf("cannot map the given interface to list of available permissions: %s", iface)
	}
	abstractPermsMap, exists := interfaceFilePermissionsMaps[iface]
	if !exists {
		// This should never happen, since we just found a permissions list
		// for the given interface and thus a map should exist for it as well.
		return nil, fmt.Errorf("cannot map the given interface to map from abstract permissions to AppArmor permissions: %s", iface)
	}
	if filePerms == notify.AA_MAY_OPEN {
		// Should not occur, but if a request is received for only open, treat it as read.
		filePerms = notify.AA_MAY_READ
	}
	// Discard Open permission; re-add it to the permission mask later
	filePerms &= ^notify.AA_MAY_OPEN
	abstractPerms := make([]string, 0, 1) // most requests should only include one permission
	for _, abstractPerm := range abstractPermsAvailable {
		aaPermMapping, exists := abstractPermsMap[abstractPerm]
		if !exists {
			// This should never happen, since permission mappings are
			// predefined and should be checked for correctness.
			return nil, fmt.Errorf("internal error: cannot map abstract permission to AppArmor permissions for the %s interface: %q", iface, abstractPerm)
		}
		if filePerms&aaPermMapping != 0 {
			abstractPerms = append(abstractPerms, abstractPerm)
			filePerms &= ^aaPermMapping
		}
	}
	if filePerms != notify.FilePermission(0) {
		logger.Noticef("cannot map AppArmor permission to abstract permission for the %s interface: %q", iface, filePerms)
	}
	return abstractPerms, nil
}

// AbstractPermissionsToAppArmorPermissions returns AppArmor permissions
// corresponding to the given permissions for the given interface.
func AbstractPermissionsToAppArmorPermissions(iface string, permissions []string) (any, error) {
	if len(permissions) == 0 {
		availablePerms, _ := AvailablePermissions(iface)
		// Caller should have already validated iface, so no error can occur
		return notify.FilePermission(0), prompting_errors.NewPermissionsListEmptyError(iface, availablePerms)
	}
	filePermsMap, exists := interfaceFilePermissionsMaps[iface]
	if !exists {
		// Should not occur, since we already validated iface and permissions
		return notify.FilePermission(0), fmt.Errorf("cannot map the given interface to map from abstract permissions to AppArmor permissions: %s", iface)
	}
	filePerms := notify.FilePermission(0)
	for _, perm := range permissions {
		permMask, exists := filePermsMap[perm]
		if !exists {
			// Should not occur, since stored permissions list should have been validated
			return notify.FilePermission(0), fmt.Errorf("cannot map abstract permission to AppArmor permissions for the %s interface: %q", iface, perm)
		}
		filePerms |= permMask
	}
	if filePerms&(notify.AA_MAY_EXEC|notify.AA_MAY_WRITE|notify.AA_MAY_READ|notify.AA_MAY_APPEND|notify.AA_MAY_CREATE) != 0 {
		filePerms |= notify.AA_MAY_OPEN
	}
	return filePerms, nil
}
