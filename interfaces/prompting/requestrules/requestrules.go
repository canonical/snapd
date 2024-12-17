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

// Package requestrules provides support for storing request rules for AppArmor
// prompting.
package requestrules

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/snapcore/snapd/interfaces/prompting"
	prompting_errors "github.com/snapcore/snapd/interfaces/prompting/errors"
	"github.com/snapcore/snapd/interfaces/prompting/internal/maxidmmap"
	"github.com/snapcore/snapd/interfaces/prompting/patterns"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
)

// Rule stores the contents of a request rule.
type Rule struct {
	ID          prompting.IDType           `json:"id"`
	Timestamp   time.Time                  `json:"timestamp"`
	User        uint32                     `json:"user"`
	Snap        string                     `json:"snap"`
	Interface   string                     `json:"interface"`
	Constraints *prompting.RuleConstraints `json:"constraints"`
}

// Validate verifies internal correctness of the rule's constraints and
// permissions and prunes any expired permissions. If all permissions are
// expired, then returns true. If the rule is invalid, returns an error.
func (rule *Rule) validate(currTime time.Time) (expired bool, err error) {
	return rule.Constraints.ValidateForInterface(rule.Interface, currTime)
}

// expired returns true if all permissions for the receiving rule have expired.
func (rule *Rule) expired(currTime time.Time) bool {
	return rule.Constraints.Permissions.Expired(currTime)
}

// variantEntry stores the actual pattern variant struct which can be used to
// match paths, and a map from rule IDs whose path patterns render to this
// variant to the relevant permission entry from that rule. All non-expired
// permission entry values in the map must have the same outcome (as long as
// the entry has not expired), and that outcome is also stored directly in the
// variant entry itself.
//
// Use the rendered string as the key for this entry, since pattern variants
// cannot otherwise be easily checked for equality.
type variantEntry struct {
	Variant     patterns.PatternVariant
	Outcome     prompting.OutcomeType
	RuleEntries map[prompting.IDType]*prompting.RulePermissionEntry
}

// permissionDB stores a map from path pattern variant to the ID of the rule
// associated with the variant for the permission associated with the permission
// DB.
type permissionDB struct {
	// permissionDB contains a map from path pattern variant to rule ID
	VariantEntries map[string]variantEntry
}

// interfaceDB stores a map from permission to the DB of rules pertaining to that
// permission for the interface associated with the interface DB.
type interfaceDB struct {
	// interfaceDB contains a map from permission to permissionDB for a particular interface
	PerPermission map[string]*permissionDB
}

// snapDB stores a map from interface name to the DB of rules pertaining to that
// interface for the snap associated with the snap DB.
type snapDB struct {
	// snapDB contains a map from interface to interfaceDB for a particular snap
	PerInterface map[string]*interfaceDB
}

// userDB stores a map from snap name to the DB of rules pertaining to that
// snap for the user associated with the user DB.
type userDB struct {
	// userDB contains a map from snap to snapDB for a particular user
	PerSnap map[string]*snapDB
}

// RuleDB stores a mapping from rule ID to rule, and a tree of rule IDs
// searchable by user, snap, interface, permission, and pattern variant.
type RuleDB struct {
	mutex     sync.RWMutex
	maxIDMmap maxidmmap.MaxIDMmap

	// index to the rules by their rule IR
	indexByID map[prompting.IDType]int
	rules     []*Rule

	// Rules are stored in a tree according to user, snap, interface, and
	// permission to simplify the process of checking whether a given request
	// is matched by existing rules, and which of those rules has precedence.
	perUser map[uint32]*userDB

	dbPath string
	// notifyRule is a closure which will be called to record a notice when a
	// rule is added, patched, or removed.
	notifyRule func(userID uint32, ruleID prompting.IDType, data map[string]string) error
}

// New creates a new rule database, loads existing rules from the database file,
// and returns the populated database.
//
// The given notifyRule closure may be called before `New()` returns, if a
// previously-saved rule has expired or if there are conflicts between rules.
//
// The given notifyRule closure will be called when a rule is added, modified,
// expired, or removed. In order to guarantee the order of notices, notifyRule
// is called with the prompt DB lock held, so it should not block for a
// substantial amount of time (such as to lock and modify snapd state).
func New(notifyRule func(userID uint32, ruleID prompting.IDType, data map[string]string) error) (*RuleDB, error) {
	maxIDFilepath := filepath.Join(prompting.StateDir(), "request-rule-max-id")

	if err := prompting.EnsureStateDir(); err != nil {
		return nil, err
	}

	maxIDMmap, err := maxidmmap.OpenMaxIDMmap(maxIDFilepath)
	if err != nil {
		return nil, err
	}

	rdb := &RuleDB{
		maxIDMmap:  maxIDMmap,
		notifyRule: notifyRule,
		dbPath:     filepath.Join(prompting.StateDir(), "request-rules.json"),
	}
	if err = rdb.load(); err != nil {
		logger.Noticef("cannot load rule database: %v; using new empty rule database", err)
	}
	return rdb, nil
}

// rulesDBJSON is a helper type for wrapping request rule DB for serialization
// when storing to disk. Should not used in contexts relating to the API.
type rulesDBJSON struct {
	Rules []*Rule `json:"rules"`
}

// load resets the receiving rule database to empty and then reads the stored
// rules from the database file and populates the database.
//
// Removes any expired rules while loading the database. If any rules expired,
// saves the database to disk.
//
// Returns an error if an existing rule DB cannot be loaded, if any rules are
// invalid or in conflict, or if there is an error while saving the database to
// disk.
//
// If an error occurs after, the rule database is reset to empty and saved to
// disk.
func (rdb *RuleDB) load() (retErr error) {
	rdb.indexByID = make(map[prompting.IDType]int)
	rdb.rules = make([]*Rule, 0)
	rdb.perUser = make(map[uint32]*userDB)

	expiredRules := make(map[prompting.IDType]bool)

	f, err := os.Open(rdb.dbPath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("cannot open rule database file: %w", err)
	}

	var wrapped rulesDBJSON
	err = json.NewDecoder(f).Decode(&wrapped)
	f.Close() // Close now since we're done reading and might need to save later
	if err != nil {
		// TODO: store rules separately per-user, so a corrupted rule for one
		// user can't impact rules for another user.
		loadErr := fmt.Errorf("cannot read stored request rules: %w", err)
		// Save the empty rule DB to disk to overwrite the previous one which
		// could not be decoded.
		return prompting_errors.Join(loadErr, rdb.save())
	}

	currTime := time.Now()

	var errInvalid error
	for _, rule := range wrapped.Rules {
		expired, err := rule.validate(currTime)
		if err != nil {
			// we're loading previously saved rules, so this should not happen
			errInvalid = fmt.Errorf("internal error: %w", err)
			break
		}
		if expired {
			expiredRules[rule.ID] = true
			continue
		}

		conflictErr := rdb.addRule(rule)
		if conflictErr != nil {
			// Duplicate rules on disk or conflicting rule, should not occur
			errInvalid = fmt.Errorf("cannot add rule: %w", conflictErr)
			break
		}
	}

	if errInvalid != nil {
		// The DB on disk was invalid, so drop every rule and start over
		data := map[string]string{"removed": "dropped"}
		for _, rule := range wrapped.Rules {
			rdb.notifyRule(rule.User, rule.ID, data)
		}
		rdb.indexByID = make(map[prompting.IDType]int)
		rdb.rules = make([]*Rule, 0)
		rdb.perUser = make(map[uint32]*userDB)

		// Save the empty rule DB to disk to overwrite the previous one which
		// was invalid.
		return prompting_errors.Join(errInvalid, rdb.save())
	}

	expiredData := map[string]string{"removed": "expired"}
	for _, rule := range wrapped.Rules {
		var data map[string]string
		if expiredRules[rule.ID] {
			data = expiredData
		}
		rdb.notifyRule(rule.User, rule.ID, data)
	}

	if len(expiredRules) > 0 {
		return rdb.save()
	}

	return nil
}

// save writes the current state of the rule database to the database file.
//
// The caller must ensure that the database lock is held.
func (rdb *RuleDB) save() error {
	b, err := json.Marshal(rulesDBJSON{Rules: rdb.rules})
	if err != nil {
		// Should not occur, marshalling should always succeed
		logger.Noticef("cannot marshal rule DB: %v", err)
		return fmt.Errorf("cannot marshal rule DB: %w", err)
	}
	return osutil.AtomicWriteFile(rdb.dbPath, b, 0o600, 0)
}

// lookupRuleByID returns the rule with the given ID from the rule DB.
//
// The caller must ensure that the database lock is held.
func (rdb *RuleDB) lookupRuleByID(id prompting.IDType) (*Rule, error) {
	index, exists := rdb.indexByID[id]
	if !exists {
		return nil, prompting_errors.ErrRuleNotFound
	}
	// XXX: should we check whether a rule is expired and throw ErrRuleNotFound
	// if so?
	if index >= len(rdb.rules) {
		// Internal inconsistency between rules list and IDs map, should not occur
		return nil, prompting_errors.ErrRuleDBInconsistent
	}
	return rdb.rules[index], nil
}

// addRuleToRulesList adds the given rule to the rules list of the rule DB.
// Whenever possible, it is preferred to use `addRule` directly instead, since
// it ensures consistency between the rules list and the per-user rules tree.
//
// However, to allow for simpler error handling and safer rollback when saving
// the DB to disk after removing a rule, this method is necessary.
//
// The caller must ensure that the database lock is held for writing.
func (rdb *RuleDB) addRuleToRulesList(rule *Rule) error {
	_, exists := rdb.indexByID[rule.ID]
	if exists {
		return prompting_errors.ErrRuleIDConflict
	}
	rdb.rules = append(rdb.rules, rule)
	rdb.indexByID[rule.ID] = len(rdb.rules) - 1
	return nil
}

// addRule adds the given rule to the rule DB.
//
// If there is a conflicting rule, returns an error, and the rule DB is left
// unchanged.
//
// The caller must ensure that the database lock is held for writing.
func (rdb *RuleDB) addRule(rule *Rule) error {
	if err := rdb.addRuleToRulesList(rule); err != nil {
		return err
	}
	conflictErr := rdb.addRuleToTree(rule)
	if conflictErr == nil {
		return nil
	}
	// remove just-added rule from rules list and IDs
	rdb.rules = rdb.rules[:len(rdb.rules)-1]
	delete(rdb.indexByID, rule.ID)
	return conflictErr
}

// removeRuleByIDFromRulesList removes the rule with the given ID from the rules
// list in the rule DB, but not from the rules tree. Whenever possible, it is
// preferred to use `removeRuleByID` directly instead, since it ensures
// consistency between the rules list and the per-user rules tree.
//
// However, to allow for simpler error handling with safer rollback when saving
// the DB to disk after removing a rule, this method is necessary.
//
// The caller must ensure that the database lock is held for writing.
func (rdb *RuleDB) removeRuleByIDFromRulesList(id prompting.IDType) (*Rule, error) {
	index, exists := rdb.indexByID[id]
	if !exists {
		return nil, prompting_errors.ErrRuleNotFound
	}
	if index >= len(rdb.rules) {
		return nil, prompting_errors.ErrRuleDBInconsistent
	}
	rule := rdb.rules[index]
	// Remove the rule with the given ID by copying the final rule in rdb.rules
	// to its index.
	rdb.rules[index] = rdb.rules[len(rdb.rules)-1]
	// Record the ID of the moved rule now before truncating, in case the rule
	// to remove is the moved rule (so nothing was moved).
	movedID := rdb.rules[index].ID
	// Truncate rules to remove the final element, which was just copied.
	rdb.rules = rdb.rules[:len(rdb.rules)-1]
	// Update the ID-index mapping of the moved rule.
	rdb.indexByID[movedID] = index
	delete(rdb.indexByID, id)

	return rule, nil
}

// removeRuleByID removes the rule with the given ID from the rule DB.
//
// If an error occurs, the rule DB is left unchanged. Otherwise, the rule is
// fully removed from the rule list and corresponding variant tree.
//
// The caller must ensure that the database lock is held for writing.
func (rdb *RuleDB) removeRuleByID(id prompting.IDType) (*Rule, error) {
	rule, err := rdb.removeRuleByIDFromRulesList(id)
	if err != nil {
		return nil, err
	}

	// Remove the rule from the rule tree. If an error occurs, the rule is
	// fully removed from the DB, and we have no guarantee that the removed
	// rule will be able to be re-added again cleanly, so don't even try.
	rdb.removeRuleFromTree(rule)

	return rule, nil
}

// addRuleToTree adds the given rule to the rule tree.
//
// If there are other rules which have a conflicting path pattern and
// permission, returns an error with information about the conflicting rules.
//
// Assumes that the rule has already been internally validated. No additional
// validation is done in this function, nor is the expiration of the permissions
// checked.
//
// The caller must ensure that the database lock is held for writing.
func (rdb *RuleDB) addRuleToTree(rule *Rule) *prompting_errors.RuleConflictError {
	addedPermissions := make([]string, 0, len(rule.Constraints.Permissions))
	var conflicts []prompting_errors.RuleConflict
	for permission, entry := range rule.Constraints.Permissions {
		permConflicts := rdb.addRulePermissionToTree(rule, permission, entry)
		if len(permConflicts) > 0 {
			conflicts = append(conflicts, permConflicts...)
			continue
		}
		addedPermissions = append(addedPermissions, permission)
	}

	if len(conflicts) > 0 {
		// remove the rule permissions we just added
		for _, prevPerm := range addedPermissions {
			rdb.removeRulePermissionFromTree(rule, prevPerm)
		}
		return &prompting_errors.RuleConflictError{
			Conflicts: conflicts,
		}
	}

	return nil
}

// addRulePermissionToTree adds all the path pattern variants for the given
// rule to the map for the given permission.
//
// If there are identical pattern variants from other non-expired rules and the
// outcomes of all those rules match the outcome of the new rule, then the ID
// of the new rule is added to the set of rule IDs in the existing variant
// entry.
//
// If there are identical pattern variants from other non-expired rules and the
// outcomes of those rules differ from that of the new rule, then there is a
// conflict, and all variants which were previously added during this function
// call are removed from the variant map, leaving it unchanged, and the list of
// conflicts is returned. If there are no conflicts, returns nil.
//
// Rules which are expired according to the timestamp of the rule being added,
// whether their outcome conflicts with the new rule or not, are ignored and
// never treated as conflicts. If there are no conflicts with non-expired
// rules, then all expired rules are removed from the tree entry (though not
// removed from the rule DB as a whole, nor is a notice recorded). If there is
// a conflict with a non-expired rule, then nothing about the rule DB state is
// changed, including expired rules.
//
// The caller must ensure that the database lock is held for writing, and that
// the given entry is not expired.
func (rdb *RuleDB) addRulePermissionToTree(rule *Rule, permission string, permissionEntry *prompting.RulePermissionEntry) []prompting_errors.RuleConflict {
	permVariants := rdb.ensurePermissionDBForUserSnapInterfacePermission(rule.User, rule.Snap, rule.Interface, permission)

	newVariantEntries := make(map[string]variantEntry, rule.Constraints.PathPattern.NumVariants())
	partiallyExpiredRules := make(map[prompting.IDType]bool)
	var conflicts []prompting_errors.RuleConflict

	addVariant := func(index int, variant patterns.PatternVariant) {
		variantStr := variant.String()
		existingEntry, exists := permVariants.VariantEntries[variantStr]
		if !exists {
			newVariantEntries[variantStr] = variantEntry{
				Variant:     variant,
				Outcome:     permissionEntry.Outcome,
				RuleEntries: map[prompting.IDType]*prompting.RulePermissionEntry{rule.ID: permissionEntry},
			}
			return
		}
		newVariantEntry := variantEntry{
			Variant:     variant,
			Outcome:     permissionEntry.Outcome,
			RuleEntries: make(map[prompting.IDType]*prompting.RulePermissionEntry, len(existingEntry.RuleEntries)+1),
		}
		newVariantEntry.RuleEntries[rule.ID] = permissionEntry
		newVariantEntries[variantStr] = newVariantEntry
		for id, entry := range existingEntry.RuleEntries {
			if entry.Expired(rule.Timestamp) {
				// Don't preserve expired rules, and don't care if they conflict
				partiallyExpiredRules[id] = true
				continue
			}
			if existingEntry.Outcome == permissionEntry.Outcome {
				// Preserve non-expired rule which doesn't conflict
				newVariantEntry.RuleEntries[id] = entry
				continue
			}
			// Conflicting non-expired rule
			conflicts = append(conflicts, prompting_errors.RuleConflict{
				Permission:    permission,
				Variant:       variantStr,
				ConflictingID: id.String(),
			})
		}
	}
	rule.Constraints.PathPattern.RenderAllVariants(addVariant)

	if len(conflicts) > 0 {
		// If there are any conflicts, discard all changes, and do nothing
		// about any expired rules.
		return conflicts
	}

	expiredData := map[string]string{"removed": "expired"}
	for ruleID := range partiallyExpiredRules {
		maybeExpired, err := rdb.lookupRuleByIDForUser(rule.User, ruleID)
		if err != nil {
			// Error shouldn't occur. If it does, the rule was already removed
			continue
		}
		// Already removed the rule's permission from the tree, let's remove
		// it from the rule as well
		delete(maybeExpired.Constraints.Permissions, permission)
		if !maybeExpired.expired(rule.Timestamp) {
			continue
		}
		_, err = rdb.removeRuleByID(ruleID)
		// Error shouldn't occur. If it does, the rule was already removed.
		if err == nil {
			rdb.notifyRule(maybeExpired.User, maybeExpired.ID, expiredData)
		}
	}

	for variantStr, variantEntry := range newVariantEntries {
		// Replace the old variant entries with the new ones.
		// This removes any expired rules from the entries, since these were
		// not preserved in the new variant entries.
		permVariants.VariantEntries[variantStr] = variantEntry
	}

	return nil
}

// removeRuleFromTree fully removes the given rule from the rules tree, even if
// an error occurs. Whenever possible, it is preferred to use `removeRuleByID`
// directly instead, since it ensures consistency between the rules list and the
// per-user rules tree.
//
// The caller must ensure that the database lock is held for writing.
func (rdb *RuleDB) removeRuleFromTree(rule *Rule) error {
	var errs []error
	for permission := range rule.Constraints.Permissions {
		if err := rdb.removeRulePermissionFromTree(rule, permission); err != nil {
			// Database was left inconsistent, should not occur.
			// Store the errors, but keep removing.
			errs = append(errs, err)
		}
	}
	return joinInternalErrors(errs)
}

// removeRulePermissionFromTree removes all the path patterns variants for the
// given rule from the map for the given permission.
//
// If a pattern variant is not found or maps to a different rule ID than that
// of the given rule, continue to remove all other variants from the permission
// map (unless they map to a different rule ID), and return a slice of all
// errors which occurred.
//
// The caller must ensure that the database lock is held for writing.
func (rdb *RuleDB) removeRulePermissionFromTree(rule *Rule, permission string) error {
	permVariants, ok := rdb.permissionDBForUserSnapInterfacePermission(rule.User, rule.Snap, rule.Interface, permission)
	if !ok || permVariants == nil {
		err := fmt.Errorf("internal error: no rules in the rule tree for user %d, snap %q, interface %q, permission %q", rule.User, rule.Snap, rule.Interface, permission)
		return err
	}
	seenVariants := make(map[string]bool, rule.Constraints.PathPattern.NumVariants())
	removeVariant := func(index int, variant patterns.PatternVariant) {
		variantStr := variant.String()
		if seenVariants[variantStr] {
			return
		}
		seenVariants[variantStr] = true
		variantEntry, exists := permVariants.VariantEntries[variantStr]
		if !exists {
			// If doesn't exist, could have been removed due to another rule's
			// variant being removed and, finding all other rules' permissions
			// for this variant expired, removing the variant entry.
			return
		}
		delete(variantEntry.RuleEntries, rule.ID)
		if len(variantEntry.RuleEntries) == 0 {
			delete(permVariants.VariantEntries, variantStr)
		}
	}
	rule.Constraints.PathPattern.RenderAllVariants(removeVariant)
	return nil
}

// joinInternalErrors wraps a prompting_errors.ErrRuleDBInconsistent with the given errors.
//
// If there are no non-nil errors in the given errs list, return nil.
func joinInternalErrors(errs []error) error {
	joinedErr := prompting_errors.Join(errs...)
	if joinedErr == nil {
		return nil
	}
	// TODO: wrap joinedErr as well once we're on golang v1.20+
	return fmt.Errorf("%w\n%v", prompting_errors.ErrRuleDBInconsistent, joinedErr)
}

// permissionDBForUserSnapInterfacePermission returns the permission DB for the
// given user, snap, interface, and permission, if it exists.
//
// The caller must ensure that the database lock is held.
func (rdb *RuleDB) permissionDBForUserSnapInterfacePermission(user uint32, snap string, iface string, permission string) (*permissionDB, bool) {
	userSnaps := rdb.perUser[user]
	if userSnaps == nil {
		return nil, false
	}
	snapInterfaces := userSnaps.PerSnap[snap]
	if snapInterfaces == nil {
		return nil, false
	}
	interfacePerms := snapInterfaces.PerInterface[iface]
	if interfacePerms == nil {
		return nil, false
	}
	permVariants := interfacePerms.PerPermission[permission]
	if permVariants == nil {
		return nil, false
	}
	return permVariants, true
}

// ensurePermissionDBForUserSnapInterfacePermission returns the permission DB
// for the given user, snap, interface, and permission, or creates it if it
// does not yet exist.
//
// The caller must ensure that the database lock is held for writing.
func (rdb *RuleDB) ensurePermissionDBForUserSnapInterfacePermission(user uint32, snap string, iface string, permission string) *permissionDB {
	userSnaps := rdb.perUser[user]
	if userSnaps == nil {
		userSnaps = &userDB{
			PerSnap: make(map[string]*snapDB),
		}
		rdb.perUser[user] = userSnaps
	}
	snapInterfaces := userSnaps.PerSnap[snap]
	if snapInterfaces == nil {
		snapInterfaces = &snapDB{
			PerInterface: make(map[string]*interfaceDB),
		}
		userSnaps.PerSnap[snap] = snapInterfaces
	}
	interfacePerms := snapInterfaces.PerInterface[iface]
	if interfacePerms == nil {
		interfacePerms = &interfaceDB{
			PerPermission: make(map[string]*permissionDB),
		}
		snapInterfaces.PerInterface[iface] = interfacePerms
	}
	permVariants := interfacePerms.PerPermission[permission]
	if permVariants == nil {
		permVariants = &permissionDB{
			VariantEntries: make(map[string]variantEntry),
		}
		interfacePerms.PerPermission[permission] = permVariants
	}
	return permVariants
}

// Close closes the max ID mmap and prevents the rule DB from being modified.
func (rdb *RuleDB) Close() error {
	rdb.mutex.Lock()
	defer rdb.mutex.Unlock()

	if rdb.maxIDMmap.IsClosed() {
		return prompting_errors.ErrRulesClosed
	}

	if err := rdb.maxIDMmap.Close(); err != nil {
		return fmt.Errorf("cannot close max ID mmap: %w", err)
	}

	return rdb.save()
}

// Creates a rule with the given information and adds it to the rule database.
// If any of the given parameters are invalid, returns an error. Otherwise,
// returns the newly-added rule, and saves the database to disk.
func (rdb *RuleDB) AddRule(user uint32, snap string, iface string, constraints *prompting.Constraints) (*Rule, error) {
	rdb.mutex.Lock()
	defer rdb.mutex.Unlock()

	if rdb.maxIDMmap.IsClosed() {
		return nil, prompting_errors.ErrRulesClosed
	}

	newRule, err := rdb.makeNewRule(user, snap, iface, constraints)
	if err != nil {
		return nil, err
	}
	if err := rdb.addRule(newRule); err != nil {
		// Cannot have expired, since the expiration is based on the lifespan,
		// duration, and timestamp, all of which were validated and set in
		// makeNewRule.
		return nil, fmt.Errorf("cannot add rule: %w", err)
	}

	if err := rdb.save(); err != nil {
		// Failed to save, so revert the rule addition so no change occurred
		// and the rule DB state matches that preserved on disk.
		rdb.removeRuleByID(newRule.ID)
		// We know that this rule exists, since we just added it, so no error
		// can occur.
		return nil, err
	}

	rdb.notifyRule(user, newRule.ID, nil)
	return newRule, nil
}

// makeNewRule creates a new Rule with the given contents.
//
// Constructs a new rule with the given parameters as values. The given
// constraints are converted to rule constraints, using the timestamp of the
// new rule as the baseline with which to compute an expiration from any given
// duration. If any of the given parameters are invalid, returns an error.
func (rdb *RuleDB) makeNewRule(user uint32, snap string, iface string, constraints *prompting.Constraints) (*Rule, error) {
	currTime := time.Now()
	ruleConstraints, err := constraints.ToRuleConstraints(iface, currTime)
	if err != nil {
		return nil, err
	}

	// Don't consume an ID until now, when we know the rule is valid
	id, _ := rdb.maxIDMmap.NextID()

	newRule := Rule{
		ID:          id,
		Timestamp:   currTime,
		User:        user,
		Snap:        snap,
		Interface:   iface,
		Constraints: ruleConstraints,
	}

	return &newRule, nil
}

// IsPathAllowed checks whether the given path with the given permission is
// allowed or denied by existing rules for the given user, snap, and interface.
// If no rule applies, returns prompting_errors.ErrNoMatchingRule.
func (rdb *RuleDB) IsPathAllowed(user uint32, snap string, iface string, path string, permission string) (bool, error) {
	rdb.mutex.RLock()
	defer rdb.mutex.RUnlock()
	permissionMap, ok := rdb.permissionDBForUserSnapInterfacePermission(user, snap, iface, permission)
	if !ok || permissionMap == nil {
		return false, prompting_errors.ErrNoMatchingRule
	}
	variantMap := permissionMap.VariantEntries
	var matchingVariants []patterns.PatternVariant
	// Make sure all rules use the same expiration timestamp, so a rule with
	// an earlier expiration cannot outlive another rule with a later one.
	currTime := time.Now()
	for variantStr, variantEntry := range variantMap {
		nonExpired := false
		for _, rulePermissionEntry := range variantEntry.RuleEntries {
			if !rulePermissionEntry.Expired(currTime) {
				nonExpired = true
				break
			}
		}
		if !nonExpired {
			continue
		}

		// Need to compare the path pattern variant, not the rule's path
		// pattern, so that only variants which match are included,
		// and the highest precedence variant can be computed.
		matched, err := patterns.PathPatternMatches(variantStr, path)
		if err != nil {
			// Only possible error is ErrBadPattern, which should not occur
			return false, fmt.Errorf("internal error: while matching path pattern: %w", err)
		}
		if matched {
			matchingVariants = append(matchingVariants, variantEntry.Variant)
		}
	}
	if len(matchingVariants) == 0 {
		return false, prompting_errors.ErrNoMatchingRule
	}
	highestPrecedenceVariant, err := patterns.HighestPrecedencePattern(matchingVariants, path)
	if err != nil {
		return false, err
	}
	matchingEntry := variantMap[highestPrecedenceVariant.String()]
	return matchingEntry.Outcome.AsBool()
}

// RuleWithID returns the rule with the given ID.
// If the rule is not found, returns ErrRuleNotFound.
// If the rule does not apply to the given user, returns
// prompting_errors.ErrRuleNotAllowed.
func (rdb *RuleDB) RuleWithID(user uint32, id prompting.IDType) (*Rule, error) {
	rdb.mutex.RLock()
	defer rdb.mutex.RUnlock()
	return rdb.lookupRuleByIDForUser(user, id)
}

// Rules returns all rules which apply to the given user.
func (rdb *RuleDB) Rules(user uint32) []*Rule {
	rdb.mutex.RLock()
	defer rdb.mutex.RUnlock()
	ruleFilter := func(rule *Rule) bool {
		return rule.User == user
	}
	return rdb.rulesInternal(ruleFilter)
}

// rulesInternal returns all rules matching the given filter.
//
// The caller must ensure that the database lock is held.
//
// TODO: store rules separately per user, snap, and interface, so actions which
// look up or delete all rules for a given user/snap/interface are much faster.
// This is safe, since rules must each apply to exactly one user, snap and
// interface, but may apply to multiple permissions.
func (rdb *RuleDB) rulesInternal(ruleFilter func(rule *Rule) bool) []*Rule {
	rules := make([]*Rule, 0)
	currTime := time.Now()
	for _, rule := range rdb.rules {
		if rule.expired(currTime) {
			// XXX: it would be nice if we pruned expired permissions from a
			// rule before including it in the rules list, if it's not expired.
			// Since we don't hold the write lock, we don't want to
			// automatically prune expired permissions here. Should this change?
			continue
		}

		if ruleFilter(rule) {
			rules = append(rules, rule)
		}
	}
	return rules
}

// RulesForSnap returns all rules which apply to the given user and snap.
func (rdb *RuleDB) RulesForSnap(user uint32, snap string) []*Rule {
	rdb.mutex.RLock()
	defer rdb.mutex.RUnlock()
	ruleFilter := func(rule *Rule) bool {
		return rule.User == user && rule.Snap == snap
	}
	return rdb.rulesInternal(ruleFilter)
}

// RulesForInterface returns all rules which apply to the given user and
// interface.
func (rdb *RuleDB) RulesForInterface(user uint32, iface string) []*Rule {
	rdb.mutex.RLock()
	defer rdb.mutex.RUnlock()
	ruleFilter := func(rule *Rule) bool {
		return rule.User == user && rule.Interface == iface
	}
	return rdb.rulesInternal(ruleFilter)
}

// RulesForSnapInterface returns all rules which apply to the given user, snap,
// and interface.
func (rdb *RuleDB) RulesForSnapInterface(user uint32, snap string, iface string) []*Rule {
	rdb.mutex.RLock()
	defer rdb.mutex.RUnlock()
	ruleFilter := func(rule *Rule) bool {
		return rule.User == user && rule.Snap == snap && rule.Interface == iface
	}
	return rdb.rulesInternal(ruleFilter)
}

// lookupRuleByIDForUser returns the rule with the given ID, if it exists, for the
// given user. Otherwise, returns an error.
//
// The caller must ensure that the database lock is held.
func (rdb *RuleDB) lookupRuleByIDForUser(user uint32, id prompting.IDType) (*Rule, error) {
	rule, err := rdb.lookupRuleByID(id)
	if err != nil {
		return nil, err
	}
	if rule.User != user {
		return nil, prompting_errors.ErrRuleNotAllowed
	}
	return rule, nil
}

// RemoveRule the rule with the given ID from the rule database. If the rule
// does not apply to the given user, returns prompting_errors.ErrRuleNotAllowed.
// If successful, saves the database to disk.
func (rdb *RuleDB) RemoveRule(user uint32, id prompting.IDType) (*Rule, error) {
	rdb.mutex.Lock()
	defer rdb.mutex.Unlock()

	if rdb.maxIDMmap.IsClosed() {
		return nil, prompting_errors.ErrRulesClosed
	}

	rule, err := rdb.lookupRuleByIDForUser(user, id)
	if err != nil {
		// The rule doesn't exist or the user doesn't have access
		return nil, err
	}

	rdb.removeRuleByIDFromRulesList(id)
	// We know the rule exists, so this should not error

	// Now that rule is removed from rules list, can try to save
	if err := rdb.save(); err != nil {
		// Roll back the change by re-adding the removed rule to the rules list
		rdb.addRuleToRulesList(rule)
		return nil, err
	}

	// Rule removed, and saved, so remove it from the tree as well
	rdb.removeRuleFromTree(rule)
	// If error occurs, rule was still fully removed from tree, and no other
	// rule was affected. We want the rule fully removed, so this is fine.

	data := map[string]string{"removed": "removed"}
	rdb.notifyRule(user, id, data)
	return rule, nil
}

// RemoveRulesForSnap removes all rules pertaining to the given snap for the
// user with the given user ID.
func (rdb *RuleDB) RemoveRulesForSnap(user uint32, snap string) ([]*Rule, error) {
	rdb.mutex.Lock()
	defer rdb.mutex.Unlock()
	ruleFilter := func(rule *Rule) bool {
		return rule.User == user && rule.Snap == snap
	}
	rules := rdb.rulesInternal(ruleFilter)
	if err := rdb.removeRulesInternal(user, rules); err != nil {
		return nil, err
	}
	return rules, nil
}

// removeRulesInternal removes all of the given rules from the rule DB and
// records a notice for each one.
//
// The caller must ensure that the database lock is held for writing.
func (rdb *RuleDB) removeRulesInternal(user uint32, rules []*Rule) error {
	if rdb.maxIDMmap.IsClosed() {
		return prompting_errors.ErrRulesClosed
	}

	if len(rules) == 0 {
		return nil
	}

	for _, rule := range rules {
		// Remove rule from the rules list. Caller should ensure that the rule
		// exists, and thus this should not error. We don't want to return any
		// error here, because that would leave some of the given rules removed
		// and others not, and the caller can ensure that this will not happen.
		rdb.removeRuleByIDFromRulesList(rule.ID)
	}

	// Now that rules have been removed from rules list, attempt to save
	if err := rdb.save(); err != nil {
		// Roll back the change by re-adding all removed rules
		for _, rule := range rules {
			rdb.addRuleToRulesList(rule)
		}
		return err
	}

	// Save successful, now remove rules' variants from tree
	data := map[string]string{"removed": "removed"}
	for _, rule := range rules {
		rdb.removeRuleFromTree(rule)
		// If error occurs, rule was still fully removed from tree, and no other
		// rule was affected. We want the rule fully removed, so this is fine.
		rdb.notifyRule(user, rule.ID, data)
	}
	return nil
}

// RemoveRulesForInterface removes all rules pertaining to the given interface
// for the user with the given user ID.
func (rdb *RuleDB) RemoveRulesForInterface(user uint32, iface string) ([]*Rule, error) {
	rdb.mutex.Lock()
	defer rdb.mutex.Unlock()
	ruleFilter := func(rule *Rule) bool {
		return rule.User == user && rule.Interface == iface
	}
	rules := rdb.rulesInternal(ruleFilter)
	if err := rdb.removeRulesInternal(user, rules); err != nil {
		return nil, err
	}
	return rules, nil
}

// RemoveRulesForSnapInterface removes all rules pertaining to the given snap
// and interface for the user with the given user ID.
func (rdb *RuleDB) RemoveRulesForSnapInterface(user uint32, snap string, iface string) ([]*Rule, error) {
	rdb.mutex.Lock()
	defer rdb.mutex.Unlock()
	ruleFilter := func(rule *Rule) bool {
		return rule.User == user && rule.Snap == snap && rule.Interface == iface
	}
	rules := rdb.rulesInternal(ruleFilter)
	if err := rdb.removeRulesInternal(user, rules); err != nil {
		return nil, err
	}
	return rules, nil
}

// PatchRule modifies the rule with the given ID by updating the rule's
// constraints for any constraint field or permission which is set/non-empty.
//
// If the path pattern is nil, it is left unchanged from the existing rule.
// Any permissions which are omitted from the new permissions map are left
// unchanged from the existing rule. To remove an existing permission from
// the rule, the permission should map to an empty permission entry.
//
// Permission entries must be provided as complete units, containing both
// outcome and lifespan (and duration, if lifespan is timespan).
// XXX: does API unmarshalling ensures this, or do we need explicit checks?
//
// Even if the given new rule contents exactly match the existing rule contents,
// the timestamp of the rule is updated to the current time. If there is any
// error while modifying the rule, the rule is rolled back to its previous
// unmodified state, leaving the database unchanged. If the database is changed,
// it is saved to disk.
//
// XXX: should we just remove this method entirely?
// Clients can always delete a rule and re-add it later, which is basically what
// this method already does.
func (rdb *RuleDB) PatchRule(user uint32, id prompting.IDType, patchConstraints *prompting.PatchConstraints) (r *Rule, err error) {
	rdb.mutex.Lock()
	defer rdb.mutex.Unlock()

	if rdb.maxIDMmap.IsClosed() {
		return nil, prompting_errors.ErrRulesClosed
	}

	origRule, err := rdb.lookupRuleByIDForUser(user, id)
	if err != nil {
		return nil, err
	}

	// XXX: we don't currently check whether the rule is expired or not.
	// Do we want to support patching a rule for which all the permissions
	// have already expired? Or say if a rule has already expired, we don't
	// support patching it? Currently, we don't include fully expired rules
	// in the output of Rules(), should the same be done here?

	currTime := time.Now()

	if patchConstraints == nil {
		patchConstraints = &prompting.PatchConstraints{}
	}
	ruleConstraints, err := patchConstraints.PatchRuleConstraints(origRule.Constraints, origRule.Interface, currTime)
	if err != nil {
		return nil, err
	}

	newRule := &Rule{
		ID:          origRule.ID,
		Timestamp:   currTime,
		User:        origRule.User,
		Snap:        origRule.Snap,
		Interface:   origRule.Interface,
		Constraints: ruleConstraints,
	}

	// Remove the existing rule from the tree. An error should not occur, since
	// we just looked up the rule and know it exists.
	rdb.removeRuleByID(origRule.ID)

	if addErr := rdb.addRule(newRule); addErr != nil {
		err := fmt.Errorf("cannot patch rule: %w", addErr)
		// Try to re-add original rule so all is unchanged.
		if origErr := rdb.addRule(origRule); origErr != nil {
			// Error should not occur, but if it does, wrap it in the other error
			err = prompting_errors.Join(err, fmt.Errorf("cannot re-add original rule: %w", origErr))
		}
		return nil, err
	}

	if err := rdb.save(); err != nil {
		// Should not occur, but if it does, roll back to the original state.
		// All of the following should succeed, since we're reversing what we
		// just successfully completed.
		rdb.removeRuleByID(newRule.ID)
		rdb.addRule(origRule)
		return nil, err
	}

	rdb.notifyRule(newRule.User, newRule.ID, nil)
	return newRule, nil
}
