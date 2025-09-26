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
	"encoding/json"
	"fmt"
	"sort"
	"time"

	prompting_errors "github.com/snapcore/snapd/interfaces/prompting/errors"
	"github.com/snapcore/snapd/interfaces/prompting/patterns"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/sandbox/apparmor/notify"
	"github.com/snapcore/snapd/sandbox/apparmor/notify/listener"
	"github.com/snapcore/snapd/strutil"
)

// PromptConstraints store details about the object of the request and the
// requested permissions.
type PromptConstraints interface {
	Permissions() *PromptPermissions
	// Equal returns true if the fields of the receiver match those of the
	// given constraints, other than the outstanding permissions, as some
	// permissions may have been satisfied by existing rules.
	Equal(other PromptConstraints) bool
}

func NewPromptConstraints(iface string, originalPermissions []string, outstandingPermissions []string, req *listener.Request) (PromptConstraints, error) {
	availablePermissions, err := AvailablePermissions(iface)
	if err != nil {
		return nil, err
	}
	switch iface {
	case "home":
		return &PromptConstraintsHome{
			Path: req.Path,
			PromptPermissions: PromptPermissions{
				OriginalPermissions:    originalPermissions,
				OutstandingPermissions: outstandingPermissions,
				AvailablePermissions:   availablePermissions,
			},
		}, nil
	default:
		// This should be impossible, since AvailablePermissions should throw
		// an error for unknown interfaces.
		return nil, fmt.Errorf("internal error: cannot create prompt constraints for unrecognized interface: %s", iface)
	}
}

type ReplyConstraints interface {
	Permissions() []string
	ToConstraints(outcome OutcomeType, lifespan LifespanType, duration string) (Constraints, error)
}

func UnmarshalReplyConstraints(iface string, rawJSON json.RawMessage) (ReplyConstraints, error) {
	var replyConstraints ReplyConstraints
	switch iface {
	case "home":
		replyConstraints = &ReplyConstraintsHome{}
	default:
		return nil, prompting_errors.NewInvalidInterfaceError(iface, availableInterfaces())
	}
	if err := json.Unmarshal(rawJSON, replyConstraints); err != nil {
		return nil, err
	}
	return replyConstraints, nil
}

type Constraints interface {
	Permissions() PermissionMap
	// MatchPromptConstraints returns an error if the constraints do not match
	// the given prompt constraints.
	// XXX: for "home" interface, this should be RequestedPathNotMatchedError
	MatchPromptConstraints(promptConstraints PromptConstraints) error
	// ToRuleConstraints validates the receiver and converts it to RuleConstraints.
	ToRuleConstraints(at At) (RuleConstraints, error)
}

func UnmarshalConstraints(iface string, rawJSON json.RawMessage) (Constraints, error) {
	var constraints Constraints
	switch iface {
	case "home":
		constraints = &ConstraintsHome{}
	default:
		return nil, prompting_errors.NewInvalidInterfaceError(iface, availableInterfaces())
	}
	if err := json.Unmarshal(rawJSON, constraints); err != nil {
		return nil, err
	}
	return constraints, nil
}

type RuleConstraints interface {
	// Validate checks that the rule constraints are valid, and prunes any
	// permissions which are expired at the given point in time. If all
	// permissions have expired, then returns true. If the rule is invalid,
	// returns an error.
	Validate(at At) (expired bool, err error)
	// Permissions returns the permission map embedded in the rule constraints.
	Permissions() RulePermissionMap
	// MatchPromptConstraints returns true if the rule constraints match the
	// given prompt constraints.
	MatchPromptConstraints(pc PromptConstraints) (bool, error)
	// CloneWithPermissions returns a copy of the constraints with the given
	// permission map set as its permissions.
	CloneWithPermissions(permissions RulePermissionMap) RuleConstraints
	// PathPattern returns the path pattern which should be used to match
	// incoming requests. For interfaces which don't use path patterns, this
	// should return patterns.ParsePathPattern("/**").
	//
	// XXX: this is rather nonsensical. It would better to remove this method
	// from the generic RuleConstraints interface and instead have a more
	// specific RuleConstraintsWithPathPattern interface, which the concrete
	// constraints types can implement if relevant. Then the callers can try
	// to do a type assertion into that more specific interface and use the
	// method, else proceed without assuming a relevant path pattern exists.
	PathPattern() *patterns.PathPattern
}

// UnmarshalRuleConstraints unmarshals the given json message into the concrete
// type which implements RuleConstraints for the given interface.
//
// The caller is responsible for validating the contents of the constraints.
// XXX: should they be validated here? Probably... split Validate() and Expired() ?
func UnmarshalRuleConstraints(iface string, rawJSON json.RawMessage) (RuleConstraints, error) {
	var ruleConstraints RuleConstraints
	switch iface {
	case "home":
		ruleConstraints = &RuleConstraintsHome{}
	default:
		return nil, prompting_errors.NewInvalidInterfaceError(iface, availableInterfaces())
	}
	if err := json.Unmarshal(rawJSON, ruleConstraints); err != nil {
		return nil, err
	}
	return ruleConstraints, nil
}

type RuleConstraintsPatch interface {
	// PatchRuleConstraints validates the receiving RuleConstraintsPatch and
	// uses the given existing rule constraints to construct new rule
	// constraints.
	//
	// If any top-level fields are omitted, they are left unchanged from the
	// existing rule. If the permissions field is present in the patch, then
	// any permissions which are omitted from the patch's permission map are
	// left unchanged from the existing rule. To remove an existing permission
	// from the rule, the permission should map to null in the permission map
	// of the patch.
	//
	// The the given at information is used to prune any existing expired
	// permissions and compute any expirations for new permissions.
	//
	// The existing rule constraints are not mutated.
	PatchRuleConstraints(existing RuleConstraints, at At) (RuleConstraints, error)
}

func UnmarshalRuleConstraintsPatch(iface string, rawJSON json.RawMessage) (RuleConstraintsPatch, error) {
	var constraintsPatch RuleConstraintsPatch
	switch iface {
	case "home":
		constraintsPatch = &RuleConstraintsPatchHome{}
	default:
		// This is an internal error, not a BadRequest error, and should never
		// occur, since the caller derives the interface from the existing rule.
		return nil, fmt.Errorf("internal error: cannot decode constraints patch: unsupported interface: %s", iface)
	}
	if err := json.Unmarshal(rawJSON, constraintsPatch); err != nil {
		return nil, err
	}
	return constraintsPatch, nil
}

type PromptPermissions struct {
	// OriginalPermissions preserve the permissions corresponding to the
	// original request. A prompt's permissions may be partially satisfied over
	// time as new rules are added, but we need to keep track of the originally
	// requested permissions so that we can still send back a response to the
	// kernel with all of the originally requested permissions which were
	// explicitly allowed by the user, even if some of those permissions were
	// allowed by rules instead of by the direct reply to the prompt.
	// XXX: this is only expored so that o/i/a/prompting.go can use it to build
	// a nice error message...
	OriginalPermissions []string `json:"-"`
	// OutstandingPermissions are the outstanding unsatisfied permissions for
	// which the application is requesting access.
	OutstandingPermissions []string `json:"requested-permissions"`
	// AvailablePermissions are the permissions which are supported by the
	// interface associated with the prompt to which the constraints apply.
	AvailablePermissions []string `json:"available-permissions"`
}

func (pp *PromptPermissions) OriginalPermissionsEqual(other *PromptPermissions) bool {
	if len(pp.OriginalPermissions) != len(other.OriginalPermissions) {
		return false
	}
	// Avoid using reflect.DeepEquals to compare []string contents
	for i := range pp.OriginalPermissions {
		if pp.OriginalPermissions[i] != other.OriginalPermissions[i] {
			return false
		}
	}
	return true
}

// ApplyRulePermissions modifies the prompt permissions, removing any outstanding
// permissions which are allowed by the given rule permissions.
//
// Returns whether the prompt permissions were affected by the rule permissions,
// whether the prompt requires a response (either because all permissions were
// allowed or at least one permission was denied), and the permissions which
// should be included in that response, if necessary.
//
// If the rule permissions do not include any of the outstanding prompt
// permissions, then affectedByRule is false, and no changes are made to the
// prompt permissions.
func (pp *PromptPermissions) ApplyRulePermissions(iface string, rulePerms RulePermissionMap) (affectedByRule, respond bool, responsePerms notify.AppArmorPermission, err error) {
	newOutstandingPermissions := make([]string, 0, len(pp.OutstandingPermissions))
	for _, perm := range pp.OutstandingPermissions {
		entry, exists := rulePerms[perm]
		if !exists {
			// Permission not covered by rule permissions, so permission
			// should continue to be in OutstandingPermissions.
			newOutstandingPermissions = append(newOutstandingPermissions, perm)
			continue
		}
		affectedByRule = true
		allow, err := entry.Outcome.AsBool()
		if err != nil {
			// This should not occur, as rule constraints are built internally
			return false, false, nil, err
		}
		if allow {
			continue
		}
		respond = true
		// Re-add denied permission to outstanding permissions so it will be
		// denied in the response.
		newOutstandingPermissions = append(newOutstandingPermissions, perm)
	}
	if !affectedByRule {
		return false, false, nil, nil
	}

	pp.OutstandingPermissions = newOutstandingPermissions

	if len(pp.OutstandingPermissions) == 0 {
		respond = true
	}

	if respond {
		const allowRemaining = false
		responsePerms = pp.BuildResponsePermissions(iface, allowRemaining)
	}

	return affectedByRule, respond, responsePerms, nil
}

// BuildResponsePermissions returns an AppArmor permission mask of the
// permissions to be allowed.
//
// If allowRemaining is true, then all originally-requested permissions are
// allowed. Otherwise, any originally-requested permissions which are not still
// outstanding are allowed. The allowed permissions are then converted to
// AppArmor permissions corresponding to the given interface and returned.
func (pp *PromptPermissions) BuildResponsePermissions(iface string, allowRemaining bool) notify.AppArmorPermission {
	allowedPerms := pp.OriginalPermissions
	if !allowRemaining {
		allowedPerms = make([]string, 0, len(pp.OriginalPermissions)-len(pp.OutstandingPermissions))
		for _, perm := range pp.OriginalPermissions {
			if !strutil.ListContains(pp.OutstandingPermissions, perm) {
				allowedPerms = append(allowedPerms, perm)
			}
		}
	}
	allowedPermission, err := AbstractPermissionsToAppArmorPermissions(iface, allowedPerms)
	if err != nil {
		// This should not occur, but if so, permission should be set to the
		// empty value for its corresponding permission type.
		logger.Noticef("internal error: cannot convert abstract permissions to AppArmor permissions: %v", err)
	}
	return allowedPermission
}

// PermissionMap is a map from permissions to their corresponding entries,
// which contain information about the outcome and lifespan for those
// permissions.
type PermissionMap map[string]*PermissionEntry

func NewPermissionMap(iface string, replyPermissions []string, outcome OutcomeType, lifespan LifespanType, duration string) (PermissionMap, error) {
	if _, err := outcome.AsBool(); err != nil {
		// Should not occur, as outcome is validated when unmarshalled
		return nil, err
	}
	if _, err := lifespan.ParseDuration(duration, time.Now()); err != nil {
		return nil, err
	}
	availablePerms, ok := interfacePermissionsAvailable[iface]
	if !ok {
		return nil, prompting_errors.NewInvalidInterfaceError(iface, availableInterfaces())
	}
	if len(replyPermissions) == 0 {
		return nil, prompting_errors.NewPermissionsEmptyError(iface, availablePerms)
	}
	var invalidPerms []string
	permissionMap := make(PermissionMap, len(replyPermissions))
	for _, perm := range replyPermissions {
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
	return permissionMap, nil
}

// ContainsOutstandingPermissions returns true if the permission map includes
// every one of the outstanding permissions in the given prompt permissions.
func (pm PermissionMap) ContainsOutstandingPermissions(promptPerms *PromptPermissions) bool {
	for _, perm := range promptPerms.OutstandingPermissions {
		if _, ok := pm[perm]; !ok {
			return false
		}
	}
	return true
}

// toRulePermissionMap validates the receiving PermissionMap and converts it
// to a RulePermissionMap, using the given at information to convert each
// PermissionEntry to a RulePermissionEntry. If the permission map is not valid
// with respect to the given interface, returns an error.
func (pm PermissionMap) toRulePermissionMap(iface string, at At) (RulePermissionMap, error) {
	availablePerms, ok := interfacePermissionsAvailable[iface]
	if !ok {
		return nil, prompting_errors.NewInvalidInterfaceError(iface, availableInterfaces())
	}
	if len(pm) == 0 {
		return nil, prompting_errors.NewPermissionsEmptyError(iface, availablePerms)
	}
	var errs []error
	var invalidPerms []string
	rulePermissionMap := make(RulePermissionMap, len(pm))
	for perm, entry := range pm {
		if !strutil.ListContains(availablePerms, perm) {
			invalidPerms = append(invalidPerms, perm)
			continue
		}
		rulePermissionEntry, err := entry.toRulePermissionEntry(at)
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
		return nil, strutil.JoinErrors(errs...)
	}
	return rulePermissionMap, nil
}

// patchRulePermissions uses the receiving permissions map to patch the given
// existing rule permissions map, returning a new rule permissions map.
//
// Any permissions which are omitted from the receiver are left unchanged from
// the existing rule. To remove an existing permission from the rule, the
// receiver should map that permission to null.
//
// The given at information is used to prune any existing expired permissions
// and compute any expirations for new permissions.
//
// The existing rule constraints are not mutated.
func (pm PermissionMap) patchRulePermissions(existing RulePermissionMap, iface string, at At) (RulePermissionMap, error) {
	newPermissions := make(RulePermissionMap, len(pm)+len(existing))
	// Pre-populate newPermissions with all the non-expired existing permissions
	for perm, entry := range existing {
		if !entry.Expired(at) {
			newPermissions[perm] = entry
		}
	}
	availablePerms, ok := interfacePermissionsAvailable[iface]
	if !ok {
		// Should not occur, as the caller should be a method on an
		// interface-specific type with hard-coded known interface.
		return nil, prompting_errors.NewInvalidInterfaceError(iface, availableInterfaces())
	}
	var errs []error
	var invalidPerms []string
	for perm, entry := range pm {
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
		ruleEntry, err := entry.toRulePermissionEntry(at)
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
		return nil, strutil.JoinErrors(errs...)
	}
	if len(newPermissions) == 0 {
		return nil, prompting_errors.ErrPatchedRuleHasNoPerms
	}
	return newPermissions, nil
}

// RulePermissionMap is a map from permissions to their corresponding entries,
// which contain information about the outcome and lifespan for those
// permissions.
type RulePermissionMap map[string]*RulePermissionEntry

// validateForInterface checks that the rule permission map is valid for the
// given interface. Any permissions which have expired at the given point in
// time are pruned. If all permissions have expired, then returns true. If the
// permission map is invalid, returns an error.
//
// XXX: this should be split into validate(), which calls validate() on each
// entry and is called during Unmarshal, and this function, which also checks
// that permissions are valid and removes expired permissions.
func (pm RulePermissionMap) validateForInterface(iface string, at At) (expired bool, err error) {
	availablePerms, ok := interfacePermissionsAvailable[iface]
	if !ok {
		return false, prompting_errors.NewInvalidInterfaceError(iface, availableInterfaces())
	}
	if len(pm) == 0 {
		return false, prompting_errors.NewPermissionsEmptyError(iface, availablePerms)
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
		if entry.Expired(at) {
			expiredPerms = append(expiredPerms, perm)
			continue
		}
	}
	if len(invalidPerms) > 0 {
		errs = append(errs, prompting_errors.NewInvalidPermissionsError(iface, invalidPerms, availablePerms))
	}
	if len(errs) > 0 {
		return false, strutil.JoinErrors(errs...)
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

// Expired returns true if all permissions in the map have expired at the given
// point in time.
func (pm RulePermissionMap) Expired(at At) bool {
	for _, entry := range pm {
		if !entry.Expired(at) {
			return false
		}
	}
	return true
}

// CloneNonExpired returns a new RulePermissionMap with every non-expired
// permission entry copied from the receiver.
//
// The entries are pointer copies, not deep copies, so internally mutating a
// permission entry in the copy will affect that entry in the receiver, and
// vice versa.
func (pm RulePermissionMap) CloneNonExpired(at At) RulePermissionMap {
	newPermissions := make(RulePermissionMap)
	for perm, entry := range pm {
		if !entry.Expired(at) {
			newPermissions[perm] = entry
		}
	}
	return newPermissions
}

// MergePermissionMap merges the receiver and the given existing permission map
// into a new permission map containing the non-expired permission entries from
// both maps at the given point in time.
//
// For any permissions which occur in both maps, preserves whichever
// corresponding entry has a lifespan which supersedes that of the other.
//
// If any permissions have conflicting outcomes in the two maps, returns those
// conflicting permissions so they can be converted into an error by the caller.
// If so, the returned newPermissions map will be nil.
func (pm RulePermissionMap) MergePermissionMap(existing RulePermissionMap, at At) (newPermissions RulePermissionMap, conflictingPerms []string) {
	// Keep any non-expired permissions from the existing map. Each might later
	// be overridden by a permission entry in the new rule, if the latter has a
	// broader lifespan (and doesn't otherwise conflict).
	newPermissions = existing.CloneNonExpired(at)
	// Check whether the receiver has outcomes which conflict with the existing
	// map, otherwise add them to the new permission map.
	for perm, entry := range pm {
		existingEntry, exists := newPermissions[perm]
		if !exists {
			newPermissions[perm] = entry
			continue
		}
		if entry.Outcome != existingEntry.Outcome {
			conflictingPerms = append(conflictingPerms, perm)
			continue
		}
		// Both new and existing map has the same outcome for this perm, so
		// preserve whichever entry has the greater lifespan.
		// Since newPermissions[perm] already has the existing entry, only
		// override it if the receiver has a greater lifespan.
		if entry.Supersedes(existingEntry, at.SessionID) {
			newPermissions[perm] = entry
		}
	}
	if len(conflictingPerms) > 0 {
		return nil, conflictingPerms
	}
	return newPermissions, nil
}

// PermissionEntry holds the outcome associated with a particular permission
// and the lifespan for which that outcome is applicable.
//
// PermissionEntry is used when replying to a prompt, creating a new rule, or
// modifying an existing rule.
type PermissionEntry struct {
	Outcome  OutcomeType  `json:"outcome"`
	Lifespan LifespanType `json:"lifespan"`
	Duration string       `json:"duration,omitempty"` // XXX: could be time.Duration
}

// toRulePermissionEntry validates the receiving PermissionEntry and converts
// it to a RulePermissionEntry.
//
// Checks that the entry has a valid outcome, and that its lifespan is valid
// for a rule (i.e. not LifespanSingle), and that it has an appropriate
// duration for that lifespan. If the lifespan is LifespanTimespan, then the
// expiration is computed as the entry's duration after the given point in time.
// If the lifespan is LifepanSession, then the sessionID at the given point in
// time must be non-zero, and is saved in the RulePermissionEntry.
func (e *PermissionEntry) toRulePermissionEntry(at At) (*RulePermissionEntry, error) {
	if _, err := e.Outcome.AsBool(); err != nil {
		return nil, err
	}
	if e.Lifespan == LifespanSingle {
		// We don't allow rules with lifespan "single"
		return nil, prompting_errors.NewRuleLifespanSingleError(SupportedRuleLifespans)
	}
	expiration, err := e.Lifespan.ParseDuration(e.Duration, at.Time)
	if err != nil {
		return nil, err
	}
	var sessionIDToUse IDType
	if e.Lifespan == LifespanSession {
		// SessionID should be 0 unless the lifespan is LifespanSession
		if at.SessionID == 0 {
			return nil, prompting_errors.ErrNewSessionRuleNoSession
		}
		sessionIDToUse = at.SessionID
	}
	rulePermissionEntry := &RulePermissionEntry{
		Outcome:    e.Outcome,
		Lifespan:   e.Lifespan,
		Expiration: expiration,
		SessionID:  sessionIDToUse,
	}
	return rulePermissionEntry, nil
}

// RulePermissionEntry holds the outcome associated with a particular permission
// and the lifespan for which that outcome is applicable.
//
// Each RulePermissionEntry is derived from a PermissionEntry. A PermissionEntry
// is used when reply to a prompt, creating a new rule, or modifying an existing
// rule, while a RulePermissionEntry is what is stored as part of the resulting
// rule.
//
// If the entry has a lifespan of LifespanTimespan, the expiration time should
// be non-zero and stores the time at which the entry expires. If the entry has
// a lifespan of LifespanSession, then the session ID should be non-zero and
// stores the user session ID associated with the rule at the time it was
// created.
type RulePermissionEntry struct {
	Outcome    OutcomeType  `json:"outcome"`
	Lifespan   LifespanType `json:"lifespan"`
	Expiration time.Time    `json:"expiration,omitzero"`
	SessionID  IDType       `json:"session-id,omitzero"`
}

// Expired returns true if the receiving permission entry has expired and
// should no longer be considered when matching requests.
//
// This is the case if the permission has a lifespan of timespan and the
// expiration time has passed at the given point in time, or the permission
// has a lifespan of LifespanSession and the associated user session ID is not
// equal to the user session ID at the given point in time.
func (e *RulePermissionEntry) Expired(at At) bool {
	switch e.Lifespan {
	case LifespanTimespan:
		if !at.Time.Before(e.Expiration) {
			return true
		}
	case LifespanSession:
		if e.SessionID != at.SessionID {
			return true
		}
	}
	return false
}

// validate checks that the entry has a valid outcome, and that its lifespan
// is valid for a rule (i.e. not LifespanSingle), and has an appropriate
// expiration information for that lifespan.
func (e *RulePermissionEntry) validate() error {
	if _, err := e.Outcome.AsBool(); err != nil {
		return err
	}
	if e.Lifespan == LifespanSingle {
		// We don't allow rules with lifespan "single"
		return prompting_errors.NewRuleLifespanSingleError(SupportedRuleLifespans)
	}
	if err := e.Lifespan.ValidateExpiration(e.Expiration, e.SessionID); err != nil {
		// Should never error due to an API request, since rules are always
		// added via the API using duration, rather than expiration, and the
		// user session should be active at the time the API request is made.
		// Error may occur when validating a rule loaded from disk.
		// We don't check whether the entry has expired as part of validation.
		return err
	}
	return nil
}

// Supersedes returns true if the receiver e has a lifespan which supersedes
// that of given other entry.
//
// LifespanForever supersedes other lifespans. LifespanSession, if the entry's
// session ID is equal to the given session ID, supersedes lifespans other
// than LifespanForever; LifespanSession with an expired session ID supersedes
// nothing and is superseded by everything else. LifespanTimespan supersedes
// LifespanSingle. If the entries are both LifespanTimespan, then whichever
// entry has a later expiration timestamp supersedes the other entry.
func (e *RulePermissionEntry) Supersedes(other *RulePermissionEntry, currSession IDType) bool {
	if other.Lifespan == LifespanForever {
		// Nothing supersedes LifespanForever
		return false
	}
	if e.Lifespan == LifespanForever {
		// LifespanForever supersedes everything else
		return true
	}
	if other.Lifespan == LifespanSession && other.SessionID == currSession {
		// Validation ensures that there can be no entry with LifespanSession
		// which has a SessionID of 0. Thus, if currSession is 0 (meaning
		// there is no active user session), we'll never have other.SessionID
		// equal to currSession.

		// Nothing except LifespanForever supersedes LifespanSession with active session
		return false
	}
	if e.Lifespan == LifespanSession {
		if e.SessionID != currSession {
			// LifespanSession with expired session supersedes nothing
			return false
		}
		// LifespanSession with active session supersedes everything remaining
		return true
	}
	if other.Lifespan == LifespanSession && other.SessionID != currSession {
		// Everything except LifespanSession with expired session supersedes
		// LifespanSession with expired session
		return true
	}
	// Neither lifespan is LifespanForever or LifespanSession
	if other.Lifespan == LifespanTimespan {
		if e.Lifespan == LifespanSingle {
			// LifespanSingle does not supersede LifespanTimespan
			return false
		}
		// e also has LifespanTimespan, so supersedes if expiration is later
		return e.Expiration.After(other.Expiration)
	}
	// Other lifespan is LifespanSingle
	if e.Lifespan == LifespanTimespan {
		// LifespanTimespan supersedes LifespanSingle
		return true
	}
	// e also has LifespanSingle, which doesn't supersede other's LifespanSingle
	return false
}

var (
	// List of permissions available for each interface. This also defines the
	// order in which the permissions should be presented.
	interfacePermissionsAvailable = map[string][]string{
		"home":   {"read", "write", "execute"},
		"camera": {"access"},
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
		"camera": {
			"access": notify.AA_MAY_READ | notify.AA_MAY_GETATTR | notify.AA_MAY_WRITE | notify.AA_MAY_APPEND,
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
func AbstractPermissionsFromAppArmorPermissions(iface string, permissions notify.AppArmorPermission) ([]string, error) {
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
func AbstractPermissionsToAppArmorPermissions(iface string, permissions []string) (notify.AppArmorPermission, error) {
	// permissions may be empty, e.g. if we're constructing allowed permissions
	// and denying all of them.
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
