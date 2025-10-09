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
	"strconv"
	"sync"
	"time"

	"golang.org/x/sys/unix"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/interfaces/prompting"
	prompting_errors "github.com/snapcore/snapd/interfaces/prompting/errors"
	"github.com/snapcore/snapd/interfaces/prompting/internal/maxidmmap"
	"github.com/snapcore/snapd/interfaces/prompting/patterns"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/randutil"
	"github.com/snapcore/snapd/strutil"
)

var (
	// userSessionIDXattr is a trusted xattr so unprivileged users cannot
	// interfere with the user session ID snapd assigns.
	userSessionIDXattr = "trusted.snapd_user_session_id"
	// errNoUserSession indicates that the user session tmpfs is not present.
	errNoUserSession = errors.New("cannot find systemd user session tmpfs for user")
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

func (rule *Rule) UnmarshalJSON(data []byte) error {
	type ruleJSON struct {
		ID          prompting.IDType          `json:"id"`
		Timestamp   time.Time                 `json:"timestamp"`
		User        uint32                    `json:"user"`
		Snap        string                    `json:"snap"`
		Interface   string                    `json:"interface"`
		Constraints prompting.ConstraintsJSON `json:"constraints"`
	}
	var intermediate ruleJSON
	if err := json.Unmarshal(data, &intermediate); err != nil {
		return err
	}
	constraints, err := prompting.UnmarshalRuleConstraints(intermediate.Interface, intermediate.Constraints)
	if err != nil {
		return err
	}
	rule.ID = intermediate.ID
	rule.Timestamp = intermediate.Timestamp
	rule.User = intermediate.User
	rule.Snap = intermediate.Snap
	rule.Interface = intermediate.Interface
	rule.Constraints = constraints
	return nil
}

// Validate verifies internal correctness of the rule's constraints and
// permissions and prunes any expired permissions. If all permissions are
// expired at the given point in time, then returns true. If the rule is
// invalid, returns an error.
func (rule *Rule) validate(at prompting.At) (expired bool, err error) {
	return rule.Constraints.ValidateForInterface(rule.Interface, at)
}

// expired returns true if all permissions for the receiving rule have expired
// at the given point in time.
func (rule *Rule) expired(at prompting.At) bool {
	return rule.Constraints.Permissions.Expired(at)
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

// expired returns true if every rule permission entry in this variant entry
// has expired at the given point in time.
func (e *variantEntry) expired(at prompting.At) bool {
	for _, rulePermissionEntry := range e.RuleEntries {
		if !rulePermissionEntry.Expired(at) {
			return false
		}
	}
	return true
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
	// PathPatterns maps from path pattern string to rule ID
	PathPatterns map[string]prompting.IDType
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

	// index to the rules by their rule ID
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

	// userSessionIDMu ensures that two threads cannot race to write a new user
	// session ID.
	userSessionIDMu sync.Mutex
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
	maxIDFilepath := filepath.Join(dirs.SnapInterfacesRequestsStateDir, "request-rule-max-id")
	rulesFilepath := filepath.Join(dirs.SnapInterfacesRequestsStateDir, "request-rules.json")

	if err := os.MkdirAll(dirs.SnapInterfacesRequestsStateDir, 0o755); err != nil {
		return nil, fmt.Errorf("cannot create interfaces requests state directory: %w", err)
	}

	maxIDMmap, err := maxidmmap.OpenMaxIDMmap(maxIDFilepath)
	if err != nil {
		return nil, err
	}

	rdb := &RuleDB{
		maxIDMmap:  maxIDMmap,
		notifyRule: notifyRule,
		dbPath:     rulesFilepath,
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
	// Store map of merged rules, where the original merged (removed) rule ID
	// maps to the ID of the rule into which it was merged.
	mergedRules := make(map[prompting.IDType]prompting.IDType)

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
		// Save the empty rule DB to disk to overwrite the previous one which
		// could not be decoded.
		// TODO: store rules separately per-user, so a corrupted rule for one
		// user can't impact rules for another user.
		return strutil.JoinErrors(err, rdb.save())
	}

	// Use the same point in time for every rule
	at := prompting.At{
		Time: time.Now(),
		// SessionID is set for each rule
	}
	sessionIDCache := make(userSessionIDCache)

	var errInvalid error
	for _, rule := range wrapped.Rules {
		at.SessionID, err = sessionIDCache.getUserSessionID(rdb, rule.User)
		if err != nil {
			return err
		}
		expired, err := rule.validate(at)
		if err != nil {
			// we're loading previously saved rules, so this should not happen
			errInvalid = err
			break
		}
		if expired {
			expiredRules[rule.ID] = true
			continue
		}

		const save = false
		mergedRule, merged, conflictErr := rdb.addOrMergeRule(rule, at, save)
		if conflictErr != nil {
			// Duplicate rules on disk or conflicting rule, should not occur
			errInvalid = fmt.Errorf("cannot add rule: %w", conflictErr)
			break
		}
		if merged {
			mergedRules[rule.ID] = mergedRule.ID
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
		return strutil.JoinErrors(errInvalid, rdb.save())
	}

	expiredData := map[string]string{"removed": "expired"}
	for _, rule := range wrapped.Rules {
		var data map[string]string
		if expiredRules[rule.ID] {
			data = expiredData
		} else if newID, exists := mergedRules[rule.ID]; exists {
			data = map[string]string{
				"removed":     "merged",
				"merged-into": newID.String(),
			}
		} else {
			// not expired or merged, so don't record notice
			continue
		}
		rdb.notifyRule(rule.User, rule.ID, data)
	}

	if len(expiredRules) > 0 || len(mergedRules) > 0 {
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

// lookupRuleByPathPattern checks whether there is an existing rule for the
// given user, snap, and iface, which has an identical path pattern to that in
// the given constraints. If it does exist, returns it, along with a bool
// indicating whether it exists. If an error occurs, returns it.
//
// The caller must ensure that the database lock is held.
func (rdb *RuleDB) lookupRuleByPathPattern(user uint32, snap string, iface string, constraints *prompting.RuleConstraints) (*Rule, bool, error) {
	interfaceDB := rdb.interfaceDBForUserSnapInterface(user, snap, iface)
	if interfaceDB == nil {
		return nil, false, nil
	}
	ruleID, exists := interfaceDB.PathPatterns[constraints.PathPattern().String()]
	if !exists {
		return nil, false, nil
	}
	rule, err := rdb.lookupRuleByID(ruleID)
	return rule, true, err
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

// addOrMergeRule adds the given rule to the rule DB, or merges it with an
// existing rule if there is an existing rule for the same user, snap, and
// interface with an identical path pattern.
//
// If save is true, saves the DB after the rule has been added or merged.
//
// If the rule's ID is 0 and it cannot be merged with an existing rule, then it
// is assigned a new ID, mutating the rule which was passed in as an argument.
//
// If the rule is merged with an existing rule, then both the given rule and the
// existing rule are removed from the DB, and a new rule is constructed with the
// merged contents of the rule to be added and the existing rule, and that
// merged rule is added to the tree, and returned along with merged set to true.
//
// If the rule is not merged with an existing rule, then the given rule is
// returned, and merged is returned as false.
//
// If the rule is merged with an existing rule, then any expired permissions
// from the existing rule are pruned.
//
// If there is a conflicting rule, or if there is an error while saving the DB,
// returns an error, and the rule DB is left unchanged.
//
// The caller must ensure that the database lock is held for writing.
func (rdb *RuleDB) addOrMergeRule(rule *Rule, at prompting.At, save bool) (addedOrMergedRule *Rule, merged bool, err error) {
	// Check if rule with identical path pattern exists.
	existingRule, exists, err := rdb.lookupRuleByPathPattern(rule.User, rule.Snap, rule.Interface, rule.Constraints)
	if err != nil {
		// Database was left inconsistent, should not occur
		return nil, false, err
	}
	if !exists {
		if err := rdb.addNewRule(rule, at, save); err != nil {
			return nil, false, err
		}
		return rule, false, nil
	}

	newPermissions := make(prompting.RulePermissionMap)
	// Add any non-expired permissions from the existing rule. Each might later
	// be overridden by a permission entry in the new rule, if the latter has a
	// broader lifespan (and doesn't otherwise conflict).
	for existingPerm, existingEntry := range existingRule.Constraints.Permissions {
		if existingEntry.Expired(at) {
			continue
		}
		newPermissions[existingPerm] = existingEntry
	}
	// Check whether the new rule has outcomes/lifespans which conflict with the
	// existing rule, otherwise add them to the new set of permissions.
	var conflicts []prompting_errors.RuleConflict
	for perm, entry := range rule.Constraints.Permissions {
		existingEntry, exists := newPermissions[perm]
		if !exists {
			newPermissions[perm] = entry
			continue
		}
		if entry.Outcome != existingEntry.Outcome {
			// New entry outcome conflicts with outcome of existing entry
			conflicts = append(conflicts, prompting_errors.RuleConflict{
				Permission:    perm,
				Variant:       rule.Constraints.PathPattern().String(), // XXX: we're mis-using the full path pattern in place of the variant
				ConflictingID: existingRule.ID.String(),
			})
			continue
		}
		// Both new and existing rule have the same permission with the same
		// outcome, so preserve whichever entry has the greater lifespan.
		// Since newPermissions[perm] already has the existing entry, only
		// override it if the new rule has a greater lifespan.
		if entry.Supersedes(existingEntry, at.SessionID) {
			newPermissions[perm] = entry
		}
	}
	// If there were any conflicts with the existing rule with identical path
	// pattern, return error.
	if len(conflicts) > 0 {
		return nil, false, &prompting_errors.RuleConflictError{
			Conflicts: conflicts,
		}
	}

	// Create new rule by copying the contents of the existing rule, but copy
	// the timestamp from the new rule.
	newRule := *existingRule
	newRule.Timestamp = rule.Timestamp
	// Set constraints as well, since copying the rule just copied the pointer,
	// and we want to set the constraints to use the new permissions without
	// mutating existingRule.Constraints.
	newRule.Constraints = &prompting.RuleConstraints{
		InterfaceSpecific: existingRule.Constraints.InterfaceSpecific,
		Permissions:       newPermissions,
	}

	// Remove the existing rule from the tree. An error should not occur, since
	// we just looked up the rule and know it exists.
	rdb.removeRuleByID(existingRule.ID)

	if err := rdb.addNewRule(&newRule, at, save); err != nil {
		// Error while adding the new merged rule, likely due to a conflict
		// caused by the new permissions in the rule to be added.

		// Re-add original the original rule so all is unchanged, which should
		// succeed since addNewRule should have rolled back successfully and
		// we're now simply re-adding the existing rule which we just removed.
		// Don't save, since nothing should have changed after the rollback is
		// complete.
		if restoreErr := rdb.addNewRule(existingRule, at, false); restoreErr != nil {
			// Error should not occur, but if it does, wrap it in the other error
			err = strutil.JoinErrors(err, fmt.Errorf("cannot re-add existing rule: %w", restoreErr))
		}
		return nil, false, err
	}

	return &newRule, true, nil
}

// addNewRule adds the given rule to the rule DB without checking whether there
// are any existing rules with the same path pattern. If save is true, saves
// the DB after the new rule has been added.
//
// This method should only be called from addOrMergeRule, or when rolling back
// the removal of a removal of a rule by re-adding it after an error occurs.
//
// Returns an error if the rule conflicts with an existing rule.
//
// The caller must ensure that the database lock is held for writing.
func (rdb *RuleDB) addNewRule(rule *Rule, at prompting.At, save bool) error {
	// If the rule has no ID, assign a new one.
	if rule.ID == 0 {
		id, _ := rdb.maxIDMmap.NextID()
		rule.ID = id
	}
	if err := rdb.addRuleToRulesList(rule); err != nil {
		return err
	}
	conflictErr := rdb.addRuleToTree(rule, at)
	if conflictErr != nil {
		// remove just-added rule from rules list and IDs
		rdb.rules = rdb.rules[:len(rdb.rules)-1]
		delete(rdb.indexByID, rule.ID)
		return conflictErr
	}

	if !save {
		return nil
	}

	if err := rdb.save(); err != nil {
		// Should not occur, but if it does, roll back to the original state.
		// The following should succeeded, since we're removing the rule which
		// we just successfully added.
		rdb.removeRuleByID(rule.ID)
		return err
	}

	return nil
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
// permission which is not expired at the given point in time, returns an
// error with information about the conflicting rules.
//
// Assumes that the rule has already been internally validated. No additional
// validation is done in this function, nor is it checked whether it has
// expired.
//
// The caller must ensure that the database lock is held for writing.
func (rdb *RuleDB) addRuleToTree(rule *Rule, at prompting.At) *prompting_errors.RuleConflictError {
	addedPermissions := make([]string, 0, len(rule.Constraints.Permissions))
	var conflicts []prompting_errors.RuleConflict
	for permission, entry := range rule.Constraints.Permissions {
		permConflicts := rdb.addRulePermissionToTree(rule, permission, entry, at)
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

	// Add rule to the interfaceDB's map of path patterns to rule IDs. We know
	// the interfaceDB exists, since we just modified a rule there.
	interfaceDB := rdb.interfaceDBForUserSnapInterface(rule.User, rule.Snap, rule.Interface)
	interfaceDB.PathPatterns[rule.Constraints.PathPattern().String()] = rule.ID

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
// Rules which are expired at the given point in time, whether their outcome
// conflicts with the new rule or not, are ignored and never treated as
// conflicts. If there are no conflicts with non-expired rules, then all
// expired rules are removed from the tree entry (though not removed from the
// rule DB as a whole, nor is a notice recorded). If there is a conflict with a
// non-expired rule, then nothing about the rule DB state is changed, including
// expired rules.
//
// The caller must ensure that the database lock is held for writing, and that
// the given entry is not expired.
func (rdb *RuleDB) addRulePermissionToTree(rule *Rule, permission string, permissionEntry *prompting.RulePermissionEntry, at prompting.At) []prompting_errors.RuleConflict {
	permVariants := rdb.ensurePermissionDBForUserSnapInterfacePermission(rule.User, rule.Snap, rule.Interface, permission)

	newVariantEntries := make(map[string]variantEntry, rule.Constraints.PathPattern().NumVariants())
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
			if entry.Expired(at) {
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
	rule.Constraints.PathPattern().RenderAllVariants(addVariant)

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
		if !maybeExpired.expired(at) {
			// Previously removed the rule's permission entry from the tree for
			// this permission, now let's remove it from the rule as well.
			delete(maybeExpired.Constraints.Permissions, permission)

			// This should not occur during load since it calls rule.validate()
			// which calls RuleConstraints.ValidateForInterface, which prunes
			// any expired permissions. Thus, it should only occur when adding
			// a new rule which overlaps with another rule which has partially
			// expired.
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

	// Remove rule from the interfaceDB's map of path patterns to rule IDs. If
	// the interfaceDB doesn't exist, then there must have been a previous error,
	// and regardless, the path pattern doesn't exist in its map anymore.
	interfaceDB := rdb.interfaceDBForUserSnapInterface(rule.User, rule.Snap, rule.Interface)
	if interfaceDB != nil {
		delete(interfaceDB.PathPatterns, rule.Constraints.PathPattern().String())
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
	permVariants := rdb.permissionDBForUserSnapInterfacePermission(rule.User, rule.Snap, rule.Interface, permission)
	if permVariants == nil {
		err := fmt.Errorf("internal error: no rules in the rule tree for user %d, snap %q, interface %q, permission %q", rule.User, rule.Snap, rule.Interface, permission)
		return err
	}
	seenVariants := make(map[string]bool, rule.Constraints.PathPattern().NumVariants())
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
	rule.Constraints.PathPattern().RenderAllVariants(removeVariant)
	return nil
}

// joinInternalErrors wraps a prompting_errors.ErrRuleDBInconsistent with the given errors.
//
// If there are no non-nil errors in the given errs list, return nil.
func joinInternalErrors(errs []error) error {
	joinedErr := strutil.JoinErrors(errs...)
	if joinedErr == nil {
		return nil
	}
	// TODO:GOVERSION: wrap joinedErr as well once we're on golang v1.20+
	return fmt.Errorf("%w\n%v", prompting_errors.ErrRuleDBInconsistent, joinedErr)
}

// permissionDBForUserSnapInterfacePermission returns the permission DB for the
// given user, snap, interface, and permission, if it exists, or nil if not.
//
// The caller must ensure that the database lock is held.
func (rdb *RuleDB) permissionDBForUserSnapInterfacePermission(user uint32, snap string, iface string, permission string) *permissionDB {
	interfaceRules := rdb.interfaceDBForUserSnapInterface(user, snap, iface)
	if interfaceRules == nil {
		return nil
	}
	permRules := interfaceRules.PerPermission[permission]
	if permRules == nil {
		return nil
	}
	return permRules
}

// interfaceDBForUserSnapInterface returns the interface DB for the given user,
// snap, and interface, if it exists, or nil if not.
//
// The caller must ensure that the database lock is held.
func (rdb *RuleDB) interfaceDBForUserSnapInterface(user uint32, snap string, iface string) *interfaceDB {
	userRules := rdb.perUser[user]
	if userRules == nil {
		return nil
	}
	snapRules := userRules.PerSnap[snap]
	if snapRules == nil {
		return nil
	}
	interfaceRules := snapRules.PerInterface[iface]
	if interfaceRules == nil {
		return nil
	}
	return interfaceRules
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
			PathPatterns:  make(map[string]prompting.IDType),
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

func userSessionPath(user uint32) string {
	userIDStr := strconv.FormatUint(uint64(user), 10)
	return filepath.Join(dirs.XdgRuntimeDirBase, userIDStr)
}

func newUserSessionID() prompting.IDType {
	id := randutil.Uint64()
	return prompting.IDType(id)
}

// Allow readOrAssignUserSessionID to be mocked in tests.
var readOrAssignUserSessionID = (*RuleDB).readOrAssignUserSessionID

// readOrAssignUserSessionID returns the existing user session ID for the given
// user, if an ID exists, otherwise generates a new ID and writes it as an
// xattr on the root directory of the user session tmpfs, /run/user/$UID.
//
// Snapd defines a unique ID for the user session and stores it as an xattr on
// /run/user/$UID in order to identify when the user session has ended or been
// restarted. When the user session ends, systemd removes the tmpfs at
// /run/user/$UID. Therefore, if that directory is missing, or it does not have
// the xattr set, snapd knows the session has ended or restarted, and by
// associating the current session ID with rules which have lifespan "session",
// it can later tell whether those rules should be discarded.
//
// Returns the existing or newly-assigned ID, or an error if it occurs. If the
// user session does not exist for the given user, returns an error which wraps
// errNoUserSession.
func (rdb *RuleDB) readOrAssignUserSessionID(user uint32) (userSessionID prompting.IDType, err error) {
	rdb.userSessionIDMu.Lock()
	defer rdb.userSessionIDMu.Unlock()

	path := userSessionPath(user)

	// It's important to check for an existing session ID xattr before trying
	// to write a new session ID, as the /run/user/$UID tmpfs may be removed,
	// but snapd is the only process which should ever write a session ID xattr.

	userSessionIDXattrLen := 16 // 64-bit number as hex string
	sessionIDBuf := make([]byte, userSessionIDXattrLen)
	_, err = unix.Getxattr(path, userSessionIDXattr, sessionIDBuf)
	if err == nil {
		if e := userSessionID.UnmarshalText(sessionIDBuf); e == nil {
			return userSessionID, nil
		}
		// Xattr present, but couldn't parse it, so ignore and overwrite it
	} else if errors.Is(err, unix.ENOENT) {
		// User session tmpfs does not exist
		return 0, fmt.Errorf("%w: %d", errNoUserSession, user)
	} else if !errors.Is(err, unix.ENODATA) {
		// Something else went wrong
		return 0, fmt.Errorf("cannot get user session ID xattr: %w", err)
	}

	// No existing ID

	newID := newUserSessionID()
	data, _ := newID.MarshalText() // error is always nil
	err = unix.Setxattr(path, userSessionIDXattr, data, 0)
	if errors.Is(err, unix.ENOENT) {
		// User session tmpfs does not exist (but it existed above). This is
		// highly unlikely, and should only occur if the directory existed but
		// had no session ID xattr, then between the Getxattr call and this
		// Setxattr call, the user session was removed. But no problem, we can
		// still correctly return an error wrapping errNoUserSession.
		return 0, fmt.Errorf("%w: %d", errNoUserSession, user)
	} else if err != nil {
		return 0, fmt.Errorf("cannot set user session ID xattr: %w", err)
	}
	return newID, nil
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

	currSession, err := readOrAssignUserSessionID(rdb, user)
	if err != nil && !errors.Is(err, errNoUserSession) {
		return nil, err
	}
	at := prompting.At{
		Time:      time.Now(),
		SessionID: currSession,
	}

	newRule, err := rdb.makeNewRule(user, snap, iface, constraints, at)
	if err != nil {
		return nil, err
	}
	const save = true
	newRule, _, err = rdb.addOrMergeRule(newRule, at, save)
	if err != nil {
		// If an error occurred, all changes were rolled back.
		return nil, fmt.Errorf("cannot add rule: %w", err)
	}

	rdb.notifyRule(user, newRule.ID, nil)
	return newRule, nil
}

// makeNewRule creates a new Rule with the given contents. It does not assign
// the rule an ID, in case it can be merged with an existing rule.
//
// Constructs a new rule with the given parameters as values. The given
// constraints are converted to rule constraints at the given point in time.
//
// If any of the given parameters are invalid, returns an error.
func (rdb *RuleDB) makeNewRule(user uint32, snap string, iface string, constraints *prompting.Constraints, at prompting.At) (*Rule, error) {
	ruleConstraints, err := constraints.ToRuleConstraints(iface, at)
	if err != nil {
		return nil, err
	}

	newRule := Rule{
		Timestamp:   at.Time,
		User:        user,
		Snap:        snap,
		Interface:   iface,
		Constraints: ruleConstraints,
	}

	return &newRule, nil
}

// IsRequestAllowed checks whether a request with the given parameters is
// allowed or denied by existing rules.
//
// If any of the given permissions are allowed, they are returned as
// allowedPerms. If any permissions are denied, then returns anyDenied as true.
// If any of the given permissions were not matched by an existing rule, then
// they are returned as outstandingPerms. If an error occurred, returns it.
func (rdb *RuleDB) IsRequestAllowed(user uint32, snap string, iface string, path string, permissions []string) (allowedPerms []string, anyDenied bool, outstandingPerms []string, err error) {
	allowedPerms = make([]string, 0, len(permissions))
	outstandingPerms = make([]string, 0, len(permissions))
	currSession, err := readOrAssignUserSessionID(rdb, user)
	if err != nil && !errors.Is(err, errNoUserSession) {
		return nil, false, nil, err
	}
	at := prompting.At{
		Time:      time.Now(),
		SessionID: currSession,
	}
	var errs []error
	for _, perm := range permissions {
		allowed, err := isPathPermAllowed(rdb, user, snap, iface, path, perm, at)
		switch {
		case err == nil:
			if allowed {
				allowedPerms = append(allowedPerms, perm)
			} else {
				anyDenied = true
			}
		case errors.Is(err, prompting_errors.ErrNoMatchingRule):
			outstandingPerms = append(outstandingPerms, perm)
		default:
			errs = append(errs, err)
		}
	}
	return allowedPerms, anyDenied, outstandingPerms, strutil.JoinErrors(errs...)
}

// Allow isPathPermAllowed to be mocked in tests.
var isPathPermAllowed = (*RuleDB).isPathPermAllowed

// isPathPermAllowed checks whether the given path with the given permission is
// allowed or denied by existing rules for the given user, snap, and interface,
// at the given point in time.
//
// If no rule applies, returns prompting_errors.ErrNoMatchingRule.
func (rdb *RuleDB) isPathPermAllowed(user uint32, snap string, iface string, path string, permission string, at prompting.At) (bool, error) {
	rdb.mutex.RLock()
	defer rdb.mutex.RUnlock()
	permissionMap := rdb.permissionDBForUserSnapInterfacePermission(user, snap, iface, permission)
	if permissionMap == nil {
		return false, prompting_errors.ErrNoMatchingRule
	}
	variantMap := permissionMap.VariantEntries
	var matchingVariants []patterns.PatternVariant
	for variantStr, variantEntry := range variantMap {
		if variantEntry.expired(at) {
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
	at := prompting.At{
		Time: time.Now(),
		// SessionID is set for each rule
	}
	sessionIDCache := make(userSessionIDCache)
	var err error
	for _, rule := range rdb.rules {
		at.SessionID, err = sessionIDCache.getUserSessionID(rdb, rule.User)
		if err != nil {
			// Something unexpected went wrong reading the user session ID.
			// Treat the session ID as 0, and proceed.
			at.SessionID = 0
		}
		if rule.expired(at) {
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
// constraints for any patch field or permission which is set/non-empty.
//
// If the path pattern is nil in the patch, it is left unchanged from the
// existing rule. Any permissions which are omitted from the permissions map
// in the patch are left unchanged from the existing rule. To remove an
// existing permission from the rule, the permission in the patch should map
// to nil.
//
// Permission entries must be provided as complete units, containing both
// outcome and lifespan (and duration or session ID, if lifespan is timespan or
// session, respectively). Since neither outcome nor lifespan are omitempty,
// the unmarshaller enforces this for us.
//
// Even if the given patch contents exactly match the existing rule contents,
// the timestamp of the rule is updated to the current time. If there is any
// error while modifying the rule, the rule is rolled back to its previous
// unmodified state, leaving the database unchanged. If the database is changed,
// it is saved to disk.
func (rdb *RuleDB) PatchRule(user uint32, id prompting.IDType, constraintsPatch *prompting.RuleConstraintsPatch) (r *Rule, err error) {
	rdb.mutex.Lock()
	defer rdb.mutex.Unlock()

	if rdb.maxIDMmap.IsClosed() {
		return nil, prompting_errors.ErrRulesClosed
	}

	origRule, err := rdb.lookupRuleByIDForUser(user, id)
	if err != nil {
		return nil, err
	}

	// XXX: we don't currently check whether the rule is fully expired or not.
	// Do we want to support patching a rule for which all the permissions
	// have already expired? Or say if a rule has already expired, we don't
	// support patching it? Currently, we don't include fully expired rules
	// in the output of Rules(), should the same be done here?

	currSession, err := readOrAssignUserSessionID(rdb, user)
	if err != nil && !errors.Is(err, errNoUserSession) {
		return nil, err
	}
	// At is used to check whether existing permission entries are expired,
	// and to compute expiration/session ID for new permission entries.
	// If a new entry's lifespan is "timespan", the expiration timestamp
	// will be computed as the entry's duration after the given time. If a
	// new entry's lifespan is "session", the current session will be stored
	// as the session ID associated with the entry. Any existing non-expired
	// entries with lifespan "session" must already have a session ID matching
	// the given session ID, otherwise they would be treated as expired.
	at := prompting.At{
		Time:      time.Now(),
		SessionID: currSession,
	}

	if constraintsPatch == nil {
		constraintsPatch = &prompting.RuleConstraintsPatch{}
	}
	ruleConstraints, err := constraintsPatch.PatchRuleConstraints(origRule.Constraints, origRule.Interface, at)
	if err != nil {
		return nil, err
	}

	newRule := &Rule{
		ID:          origRule.ID,
		Timestamp:   at.Time,
		User:        origRule.User,
		Snap:        origRule.Snap,
		Interface:   origRule.Interface,
		Constraints: ruleConstraints,
	}

	// Remove the existing rule from the tree. An error should not occur, since
	// we just looked up the rule and know it exists.
	rdb.removeRuleByID(origRule.ID)

	const save = true
	newRule, _, addErr := rdb.addOrMergeRule(newRule, at, save)
	if addErr != nil {
		err := fmt.Errorf("cannot patch rule: %w", addErr)
		// Re-add the original rule so all is unchanged, which should
		// succeed since we're simply reversing what we just completed.
		// Don't save, since nothing should have changed after the rollback
		// is complete.
		if origErr := rdb.addNewRule(origRule, at, false); origErr != nil {
			// Error should not occur, but if it does, wrap it in the other error
			err = strutil.JoinErrors(err, fmt.Errorf("cannot re-add original rule: %w", origErr))
		}
		return nil, err
	}

	rdb.notifyRule(newRule.User, newRule.ID, nil)
	return newRule, nil
}

// userSessionIDCache provides an ergonomic wrapper for getting and caching
// user session IDs for many rules.
//
// A cache should not be used beyond the scope of a single method call due to
// an API request. In particular, it should not be persisted as part of a rule
// database.
type userSessionIDCache map[uint32]prompting.IDType

func (cache userSessionIDCache) getUserSessionID(rdb *RuleDB, user uint32) (prompting.IDType, error) {
	sessionID, ok := cache[user]
	if ok {
		return sessionID, nil
	}
	sessionID, err := readOrAssignUserSessionID(rdb, user)
	if err != nil && !errors.Is(err, errNoUserSession) {
		return 0, err
	}
	cache[user] = sessionID
	return sessionID, nil
}
