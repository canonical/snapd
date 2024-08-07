package requestrules

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/interfaces/prompting"
	"github.com/snapcore/snapd/interfaces/prompting/internal/maxidmmap"
	"github.com/snapcore/snapd/interfaces/prompting/patterns"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
)

var (
	ErrInternalInconsistency = errors.New("internal error: prompting rules database left inconsistent")
	ErrLifespanSingle        = errors.New(`cannot create rule with lifespan "single"`)
	ErrRuleIDNotFound        = errors.New("rule ID is not found")
	ErrRuleIDConflict        = errors.New("rule with matching ID already exists in rules database")
	ErrPathPatternConflict   = errors.New("a rule with conflicting path pattern and permission already exists in the rules database")
	ErrNoMatchingRule        = errors.New("no rules match the given path")
	ErrUserNotAllowed        = errors.New("the given user is not allowed to request the rule with the given ID")
)

// Rule stores the contents of a request rule.
type Rule struct {
	ID          prompting.IDType       `json:"id"`
	Timestamp   time.Time              `json:"timestamp"`
	User        uint32                 `json:"user"`
	Snap        string                 `json:"snap"`
	Interface   string                 `json:"interface"`
	Constraints *prompting.Constraints `json:"constraints"`
	Outcome     prompting.OutcomeType  `json:"outcome"`
	Lifespan    prompting.LifespanType `json:"lifespan"`
	Expiration  time.Time              `json:"expiration,omitempty"`
}

// removePermission removes the given permission from the rule's list of
// permissions.
func (rule *Rule) removePermission(permission string) error {
	if err := rule.Constraints.RemovePermission(permission); err != nil {
		return err
	}
	if len(rule.Constraints.Permissions) == 0 {
		return prompting.ErrPermissionsListEmpty
	}
	return nil
}

// Expired returns true if the receiving rule has a lifespan of timespan and
// the current time is after the rule's expiration time.
//
// Returns an error if the rule's expiration time is invalid.
func (rule *Rule) Expired(currentTime time.Time) (bool, error) {
	switch rule.Lifespan {
	case prompting.LifespanTimespan:
		if rule.Expiration.IsZero() {
			// Should not occur
			return false, fmt.Errorf("encountered rule with lifespan timespan but no expiration")
		}
		if currentTime.After(rule.Expiration) {
			return true, nil
		}
		return false, nil
		// TODO: add lifespan session
		//case prompting.LifespanSession:
		// TODO: return true if the user session has changed
	}
	return false, nil
}

// variantEntry stores the variant struct and the ID of its corresponding rule.
//
// This is necessary since multiple variants might render to the same string,
// and it would be necessary to make a deep comparison of two variants to tell
// that they are the same. Since we want to map from variant to rule ID, we need
// to use the variant string as the key.
type variantEntry struct {
	Variant patterns.PatternVariant
	RuleID  prompting.IDType
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
	mutex     sync.Mutex
	maxIDMmap maxidmmap.MaxIDMmap
	ids       map[prompting.IDType]int
	rules     []*Rule
	perUser   map[uint32]*userDB
	// notifyRule is a closure which will be called to record a notice when a
	// rule is added, patched, or removed.
	notifyRule func(userID uint32, ruleID prompting.IDType, data map[string]string) error
}

// New creates a new rule database, loads existing rules from the database file,
// and returns the populated database.
//
// The given notifyRule closure will be called when a rule is added, modified,
// expired, or removed. In order to guarantee the order of notices, notifyRule
// is called with the prompt DB lock held, so it should not block for a
// substantial amount of time (such as to lock and modify snapd state).
func New(notifyRule func(userID uint32, ruleID prompting.IDType, data map[string]string) error) (*RuleDB, error) {
	maxIDFilepath := filepath.Join(dirs.SnapdStateDir(dirs.GlobalRootDir), "request-rule-max-id")
	maxIDMmap, err := maxidmmap.OpenMaxIDMmap(maxIDFilepath)
	if err != nil {
		return nil, err
	}
	rdb := &RuleDB{
		maxIDMmap:  maxIDMmap,
		ids:        make(map[prompting.IDType]int),
		rules:      make([]*Rule, 0),
		perUser:    make(map[uint32]*userDB),
		notifyRule: notifyRule,
	}
	if err = rdb.load(); err != nil {
		logger.Noticef("%v; using new empty rule database", err)
	}
	return rdb, nil
}

// load reads the stored rules from the database file and populates the
// receiving rule database.
//
// If an error occurs, does not modify the rule database.
func (rdb *RuleDB) load() error {
	target := rdb.dbpath()
	f, err := os.Open(target)
	if err != nil {
		return fmt.Errorf("cannot open rules database file: %w", err)
	}
	defer f.Close()

	var ruleList []*Rule
	err = json.NewDecoder(f).Decode(&ruleList)
	if err != nil {
		// TODO: store rules separately per-user, so a corrupted rule for one
		// user can't impact rules for another user.
		return fmt.Errorf("cannot read stored prompting rules: %w", err)
	}

	rdb.rules = ruleList
	rdb.ids = make(map[prompting.IDType]int) // clear out any existing rules in ids
	for index, rule := range rdb.rules {
		rdb.ids[rule.ID] = index
	}

	notifyEveryRule := true
	rdb.refreshTreeEnforceConsistency(notifyEveryRule)

	return nil
}

// save writes the current state of the rule database to the database file.
func (rdb *RuleDB) save() error {
	b, err := json.Marshal(rdb.rules)
	if err != nil {
		logger.Noticef("cannot marshal rule DB: %v", err)
		return fmt.Errorf("cannot marshal rule DB: %w", err)
	}
	target := rdb.dbpath()
	return osutil.AtomicWriteFile(target, b, 0600, 0)
}

// dbpath returns the path of the database file.
func (rdb *RuleDB) dbpath() string {
	return filepath.Join(dirs.SnapdStateDir(dirs.GlobalRootDir), "request-rules.json")
}

// addRule adds the given rule to the rule DB.
func (rdb *RuleDB) addRule(rule *Rule) error {
	_, exists := rdb.ids[rule.ID]
	if exists {
		return ErrRuleIDConflict
	}
	rdb.rules = append(rdb.rules, rule)
	rdb.ids[rule.ID] = len(rdb.rules) - 1
	return nil
}

// ruleWithID returns the rule with the given ID from the rule DB.
func (rdb *RuleDB) ruleWithID(id prompting.IDType) (*Rule, error) {
	index, exists := rdb.ids[id]
	if !exists {
		return nil, ErrRuleIDNotFound
	}
	if index >= len(rdb.rules) {
		return nil, ErrInternalInconsistency
	}
	return rdb.rules[index], nil
}

// removeRuleWithID removes the rule with the given ID from the rule DB.
func (rdb *RuleDB) removeRuleWithID(id prompting.IDType) (*Rule, error) {
	index, exists := rdb.ids[id]
	if !exists {
		return nil, ErrRuleIDNotFound
	}
	if index >= len(rdb.rules) {
		return nil, ErrInternalInconsistency
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
	rdb.ids[movedID] = index
	delete(rdb.ids, id)
	return rule, nil
}

// permissionDBForUserSnapInterfacePermission returns the permission DB for the
// given user, snap, interface, and permission.
func (rdb *RuleDB) permissionDBForUserSnapInterfacePermission(user uint32, snap string, iface string, permission string) *permissionDB {
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

// RuleConflict stores the rendered variant which conflicted with that of
// another rule, along with the ID of that conflicting rule.
type RuleConflict struct {
	Variant       string           `json:"pattern-variant"`
	ConflictingID prompting.IDType `json:"conflicting-rule-id"`
}

// addRulePermissionToTree adds all the path pattern variants for the given
// rule to the map for the given permission.
//
// If there are conflicting pattern variant froms other rules, all variants
// which were previously added during this function call are removed
// from the path map, and an error is returned along with a list of the
// conflicting variants and the IDs of the conflicting rules.
func (rdb *RuleDB) addRulePermissionToTree(rule *Rule, permission string) (error, []RuleConflict) {
	permVariants := rdb.permissionDBForUserSnapInterfacePermission(rule.User, rule.Snap, rule.Interface, permission)
	conflicts := make([]RuleConflict, 0, rule.Constraints.PathPattern.NumVariants())
	addVariant := func(index int, variant patterns.PatternVariant) {
		if conflictingVariantEntry, exists := permVariants.VariantEntries[variant.String()]; exists {
			conflicts = append(conflicts, RuleConflict{
				Variant:       variant.String(),
				ConflictingID: conflictingVariantEntry.RuleID,
			})
		} else {
			permVariants.VariantEntries[variant.String()] = variantEntry{
				Variant: variant,
				RuleID:  rule.ID,
			}
		}
	}
	rule.Constraints.PathPattern.RenderAllVariants(addVariant)
	if len(conflicts) == 0 {
		return nil, nil
	}
	// There were conflicts, so remove any variants which were added to the tree
	nextMatchIndex := 0
	removeVariant := func(index int, variant patterns.PatternVariant) {
		if nextMatchIndex < len(conflicts) && conflicts[nextMatchIndex].Variant == variant.String() {
			nextMatchIndex++
		} else {
			delete(permVariants.VariantEntries, variant.String())
		}
	}
	rule.Constraints.PathPattern.RenderAllVariants(removeVariant)
	return ErrPathPatternConflict, conflicts
}

// removeRulePermissionFromTree removes all the path patterns variants for the
// given rule from the map for the given permission.
//
// If a pattern variant is not found or maps to a different rule ID than that
// of the given rule, continue to remove all other variants from the permission
// map (unless they map to a different rule ID), and return a slice of all
// errors which occurred.
func (rdb *RuleDB) removeRulePermissionFromTree(rule *Rule, permission string) []error {
	permVariants := rdb.permissionDBForUserSnapInterfacePermission(rule.User, rule.Snap, rule.Interface, permission)
	var errs []error
	removeVariant := func(index int, variant patterns.PatternVariant) {
		variantEntry, exists := permVariants.VariantEntries[variant.String()]
		if !exists {
			// Database was left inconsistent, should not occur
			errs = append(errs, fmt.Errorf(`path pattern variant not found in the rule tree: %q`, variant))
		} else if variantEntry.RuleID != rule.ID {
			// Database was left inconsistent, should not occur
			errs = append(errs, fmt.Errorf(`path pattern variant maps to different rule ID: %q: %s`, variant, variantEntry.RuleID.String()))
		} else {
			delete(permVariants.VariantEntries, variant.String())
		}
	}
	rule.Constraints.PathPattern.RenderAllVariants(removeVariant)
	return errs
}

// addRuleToTree adds the given rule to the rule tree.
//
// If there is a conflicting path pattern from another rule, returns an
// error along with the ID of the conflicting rule and the permission for
// which the conflict occurred
func (rdb *RuleDB) addRuleToTree(rule *Rule) (error, []RuleConflict, string) {
	addedPermissions := make([]string, 0, len(rule.Constraints.Permissions))
	for _, permission := range rule.Constraints.Permissions {
		if err, conflicts := rdb.addRulePermissionToTree(rule, permission); err != nil {
			for _, prevPerm := range addedPermissions {
				rdb.removeRulePermissionFromTree(rule, prevPerm)
			}
			return err, conflicts, permission
		}
		addedPermissions = append(addedPermissions, permission)
	}
	return nil, nil, ""
}

// removeRuleFromTree fully removes the given rule from the tree, even if an
// error occurs.
func (rdb *RuleDB) removeRuleFromTree(rule *Rule) error {
	var errs []error
	for _, permission := range rule.Constraints.Permissions {
		if es := rdb.removeRulePermissionFromTree(rule, permission); len(es) > 0 {
			// Database was left inconsistent, should not occur.
			// Store the errors, but keep removing.
			errs = append(errs, es...)
		}
	}
	return joinInternalErrors(errs)
}

// joinInternalErrors wraps an ErrInternalInconsistency with the given errors.
//
// If there are no non-nil errors in the given errs list, return nil.
func joinInternalErrors(errs []error) error {
	joinedErr := errorsJoin(errs...)
	if joinedErr == nil {
		return nil
	}
	// TODO: wrap joinedErr as well once we're on golang v1.20+
	return fmt.Errorf("%w\n%v", ErrInternalInconsistency, joinedErr)
}

// errorsJoin returns an error that wraps the given errors.
// Any nil error values are discarded.
// errorsJoin returns nil if every value in errs is nil.
//
// TODO: replace with errors.Join() once we're on golang v1.20+
func errorsJoin(errs ...error) error {
	var nonNilErrs []error
	for _, e := range errs {
		if e != nil {
			nonNilErrs = append(nonNilErrs, e)
		}
	}
	if len(nonNilErrs) == 0 {
		return nil
	}
	err := nonNilErrs[0]
	for _, e := range nonNilErrs[1:] {
		err = fmt.Errorf("%w\n%v", err, e)
	}
	return err
}

// refreshTreeEnforceConsistency rebuilds the rule tree, resolving any
// conflicting pattern variants and permissions by pruning the offending
// permission from the older of any two conflicting rules.
//
// This function is only required if database is left inconsistent (should not
// occur) or when loading, in case the stored rules on disk were corrupted.
//
// By default, issues a notice for each rule which is modified or removed as a
// result of a conflict with another rule. If notifyEveryRule is true, issues
// a notice for every rule which was in the database prior to the beginning of
// the function. In either case, at most one notice is issued for each rule.
//
// Discards the current rule tree, then iterates through the rules in rdb.rules
// and re-populates the tree. If there are any conflicts between rules (that
// is, rules share the same path pattern and one or more of the same
// permissions), the conflicting permission is removed from the rule with the
// earlier timestamp. When the function returns, the database should be fully
// internally consistent and without conflicting rules.
func (rdb *RuleDB) refreshTreeEnforceConsistency(notifyEveryRule bool) {
	rdb.mutex.Lock()
	defer rdb.mutex.Unlock()
	needToSave := false
	defer func() {
		if needToSave {
			rdb.save()
		}
	}()
	modifiedUserRuleIDs := make(map[uint32]map[prompting.IDType]map[string]string)
	defer func() {
		for user, ruleIDs := range modifiedUserRuleIDs {
			for ruleID, data := range ruleIDs {
				rdb.notifyRule(user, ruleID, data)
			}
		}
	}()
	currTime := time.Now()
	origRules := rdb.rules
	rdb.ids = make(map[prompting.IDType]int)
	rdb.rules = make([]*Rule, 0, len(rdb.rules))
	rdb.perUser = make(map[uint32]*userDB)
	for _, rule := range origRules {
		_, exists := modifiedUserRuleIDs[rule.User]
		if !exists {
			modifiedUserRuleIDs[rule.User] = make(map[prompting.IDType]map[string]string)
		}
		if notifyEveryRule {
			modifiedUserRuleIDs[rule.User][rule.ID] = nil
		}
		if err := rule.Lifespan.ValidateExpiration(rule.Expiration, currTime); err != nil || rule.Constraints.ValidateForInterface(rule.Interface) != nil {
			// Invalid rule, discard it
			needToSave = true
			data := map[string]string{"removed": "invalid"}
			if errors.Is(err, prompting.ErrExpirationInThePast) {
				// Not actually invalid, just expired
				data["removed"] = "expired"
			}
			modifiedUserRuleIDs[rule.User][rule.ID] = data
			continue
		}
		for {
			err, conflicts, conflictingPermission := rdb.addRuleToTree(rule)
			if err == nil {
				break
			}
			// Err must be ErrPathPatternConflict.
			// Prioritize newer rules by pruning permission from old rule until
			// no conflicts remain.
			// XXX: this results in the permission being dropped for all other
			// variants of the older rule.
			// TODO: split older rule into two rules, preserving all except the
			// directly conflicting variant/permission combination.
			for _, conflict := range conflicts {
				conflictingID := conflict.ConflictingID
				conflictingRule, _ := rdb.ruleWithID(conflictingID) // must exist
				if rule.Timestamp.After(conflictingRule.Timestamp) {
					rdb.removeRulePermissionFromTree(conflictingRule, conflictingPermission) // must return nil
					var data map[string]string
					if conflictingRule.removePermission(conflictingPermission) == prompting.ErrPermissionsListEmpty {
						rdb.removeRuleWithID(conflictingID)
						data = map[string]string{"removed": "conflict"}
					}
					modifiedUserRuleIDs[conflictingRule.User][conflictingID] = data
				} else {
					rule.removePermission(conflictingPermission) // ignore error
					var data map[string]string
					modifiedUserRuleIDs[rule.User][rule.ID] = data
				}
				needToSave = true
			}
		}
		if len(rule.Constraints.Permissions) > 0 {
			rdb.addRule(rule)
		} else {
			// TODO: record status of the rule ("removed") in modifiedUserRuleIDs
			needToSave = true
			data := map[string]string{"removed": "conflict"}
			modifiedUserRuleIDs[rule.User][rule.ID] = data
		}
	}
}

// populateNewRule creates a new Rule with the given contents.
//
// Users of requestrules should probably autofill rules from JSON and never call
// this function directly.
//
// Constructs a new rule with the given parameters as values, with the
// exception of duration. Uses the given duration, in addition to the current
// time, to compute the expiration time for the rule, and stores that as part
// of the rule which is returned. If any of the given parameters are invalid,
// returns a corresponding error.
func (rdb *RuleDB) populateNewRule(user uint32, snap string, iface string, constraints *prompting.Constraints, outcome prompting.OutcomeType, lifespan prompting.LifespanType, duration string) (*Rule, error) {
	if err := constraints.ValidateForInterface(iface); err != nil {
		return nil, err
	}
	if _, err := outcome.AsBool(); err != nil {
		// This should not occur, since populateNewRule should only be called
		// on values which were validated while unmarshalling
		return nil, err
	}
	if lifespan == prompting.LifespanSingle {
		// We don't allow creating rules with a lifespan of "single"
		return nil, ErrLifespanSingle
	}
	id, _ := rdb.maxIDMmap.NextID()
	currTime := time.Now()
	expiration, err := lifespan.ParseDuration(duration, currTime)
	if err != nil {
		return nil, err
	}
	newRule := Rule{
		ID:          id,
		Timestamp:   currTime,
		User:        user,
		Snap:        snap,
		Interface:   iface,
		Constraints: constraints,
		Outcome:     outcome,
		Lifespan:    lifespan,
		Expiration:  expiration,
	}
	return &newRule, nil
}

// IsPathAllowed checks whether the given path with the given permission is
// allowed or denied by existing rules for the given user, snap, and interface.
// If no rule applies, returns ErrNoMatchingRule.
func (rdb *RuleDB) IsPathAllowed(user uint32, snap string, iface string, path string, permission string) (bool, error) {
	rdb.mutex.Lock()
	defer rdb.mutex.Unlock()
	needToSave := false
	defer func() {
		if needToSave {
			rdb.save()
		}
	}()
	variantMap := rdb.permissionDBForUserSnapInterfacePermission(user, snap, iface, permission).VariantEntries
	matchingVariants := make([]patterns.PatternVariant, 0)
	// Make sure all rules use the same expiration timestamp, so a rule with
	// an earlier expiration cannot outlive another rule with a later one.
	currTime := time.Now()
	for variantStr, variantEntry := range variantMap {
		matchingRule, err := rdb.ruleWithID(variantEntry.RuleID)
		if err != nil {
			// Database was left inconsistent, should not occur
			delete(variantMap, variantStr)
			// Record a notice for the offending rule, just in case
			rdb.notifyRule(user, variantEntry.RuleID, nil)
			continue
		}
		expired, err := matchingRule.Expired(currTime)
		switch {
		case err != nil:
			// Should not occur
			logger.Noticef("error while checking whether rule had expired: %v", err)
			fallthrough
		case expired:
			needToSave = true
			rdb.removeRuleFromTree(matchingRule)
			rdb.removeRuleWithID(variantEntry.RuleID)
			data := map[string]string{"removed": "expired"}
			rdb.notifyRule(user, variantEntry.RuleID, data)
			continue
		}
		// Need to compare the path pattern variant, not the rule's path
		// pattern, so that only variants which match are included,
		// and the highest precedence variant can be computed.
		matched, err := patterns.PathPatternMatches(variantStr, path)
		if err != nil {
			// Only possible error is ErrBadPattern, which should not occur
			return false, err
		}
		if matched {
			matchingVariants = append(matchingVariants, variantEntry.Variant)
		}
	}
	if len(matchingVariants) == 0 {
		return false, ErrNoMatchingRule
	}
	highestPrecedenceVariant, err := patterns.HighestPrecedencePattern(matchingVariants, path)
	if err != nil {
		return false, err
	}
	matchingEntry := variantMap[highestPrecedenceVariant.String()]
	matchingID := matchingEntry.RuleID
	matchingRule, err := rdb.ruleWithID(matchingID)
	if err != nil {
		// Database was left inconsistent, should not occur
		return false, ErrRuleIDNotFound
	}
	return matchingRule.Outcome.AsBool()
}

// ruleWithIDForUser returns the rule with the given ID, if it exists, for the
// given user. Otherwise, returns an error.
func (rdb *RuleDB) ruleWithIDForUser(user uint32, id prompting.IDType) (*Rule, error) {
	rule, err := rdb.ruleWithID(id)
	if err != nil {
		return nil, err
	}
	if rule.User != user {
		return nil, ErrUserNotAllowed
	}
	return rule, nil
}

// RuleWithID returns the rule with the given ID.
// If the rule is not found, returns ErrRuleNotFound.
// If the rule does not apply to the given user, returns ErrUserNotAllowed.
func (rdb *RuleDB) RuleWithID(user uint32, id prompting.IDType) (*Rule, error) {
	rdb.mutex.Lock()
	defer rdb.mutex.Unlock()
	return rdb.ruleWithIDForUser(user, id)
}

// Creates a rule with the given information and adds it to the rule database.
// If any of the given parameters are invalid, returns an error. Otherwise,
// returns the newly-added rule, and saves the database to disk.
func (rdb *RuleDB) AddRule(user uint32, snap string, iface string, constraints *prompting.Constraints, outcome prompting.OutcomeType, lifespan prompting.LifespanType, duration string) (*Rule, error) {
	rdb.mutex.Lock()
	defer rdb.mutex.Unlock()
	newRule, err := rdb.populateNewRule(user, snap, iface, constraints, outcome, lifespan, duration)
	if err != nil {
		return nil, err
	}
	if err, conflicts, conflictingPermission := rdb.addRuleToTree(newRule); err != nil {
		return nil, fmt.Errorf("%w: conflicts: %+v, Permission: '%s'", err, conflicts, conflictingPermission)
	}
	rdb.addRule(newRule)
	rdb.save()
	rdb.notifyRule(user, newRule.ID, nil)
	return newRule, nil
}

// RemoveRule the rule with the given ID from the rules database. If the rule
// does not apply to the given user, returns ErrUserNotAllowed. If successful,
// saves the database to disk.
func (rdb *RuleDB) RemoveRule(user uint32, id prompting.IDType) (*Rule, error) {
	rdb.mutex.Lock()
	defer rdb.mutex.Unlock()
	rule, err := rdb.ruleWithIDForUser(user, id)
	if err != nil {
		return nil, err
	}
	err = rdb.removeRuleFromTree(rule)
	// If error occurs, rule was still fully removed from tree
	rdb.removeRuleWithID(id)
	rdb.save()
	data := map[string]string{"removed": "removed"}
	rdb.notifyRule(user, id, data)
	return rule, err
}

// RemoveRulesForSnap removes all rules pertaining to the given snap for the
// user with the given user ID.
func (rdb *RuleDB) RemoveRulesForSnap(user uint32, snap string) []*Rule {
	rdb.mutex.Lock()
	defer rdb.mutex.Unlock()
	ruleFilter := func(rule *Rule) bool {
		return rule.User == user && rule.Snap == snap
	}
	rules := rdb.rulesInternal(ruleFilter)
	rdb.removeRulesInternal(user, rules)
	return rules
}

// removeRulesInternal removes all of the given rules from the rule DB and
// records a notice for each one.
func (rdb *RuleDB) removeRulesInternal(user uint32, rules []*Rule) {
	for _, rule := range rules {
		rdb.removeRuleFromTree(rule)
		// If error occurs, rule was still fully removed from tree
		rdb.removeRuleWithID(rule.ID)
		data := map[string]string{"removed": "removed"}
		rdb.notifyRule(user, rule.ID, data)
	}
	rdb.save()
}

// RemoveRulesForSnapInterface removes all rules pertaining to the given snap
// and interface for the user with the given user ID.
func (rdb *RuleDB) RemoveRulesForSnapInterface(user uint32, snap string, iface string) []*Rule {
	rdb.mutex.Lock()
	defer rdb.mutex.Unlock()
	ruleFilter := func(rule *Rule) bool {
		return rule.User == user && rule.Snap == snap && rule.Interface == iface
	}
	rules := rdb.rulesInternal(ruleFilter)
	rdb.removeRulesInternal(user, rules)
	return rules
}

// PatchRule modifies the rule with the given ID by updating the rule's fields
// corresponding to any of the given parameters which are set/non-empty.
//
// Any of the parameters which are equal to the default/unset value for their
// types are left unchanged from the existing rule. Even if the given new rule
// contents exactly match the existing rule contents, the timestamp of the rule
// is updated to the current time. If there is any error while modifying the
// rule, the rule is rolled back to its previous unmodified state, leaving the
// database unchanged. If the database is changed, it is saved to disk.
func (rdb *RuleDB) PatchRule(user uint32, id prompting.IDType, constraints *prompting.Constraints, outcome prompting.OutcomeType, lifespan prompting.LifespanType, duration string) (*Rule, error) {
	rdb.mutex.Lock()
	defer rdb.mutex.Unlock()
	changeOccurred := false
	defer func() {
		if changeOccurred {
			rdb.save()
			rdb.notifyRule(user, id, nil)
		}
	}()
	origRule, err := rdb.ruleWithIDForUser(user, id)
	if err != nil {
		return nil, err
	}
	if constraints == nil {
		constraints = origRule.Constraints
	}
	if outcome == prompting.OutcomeUnset {
		outcome = origRule.Outcome
	}
	if lifespan == prompting.LifespanUnset {
		lifespan = origRule.Lifespan
	}
	if err = rdb.removeRuleFromTree(origRule); err != nil {
		// If error occurs, rule is fully removed from tree
		changeOccurred = true
		return nil, err
	}
	newRule, err := rdb.populateNewRule(user, origRule.Snap, origRule.Interface, constraints, outcome, lifespan, duration)
	if err != nil {
		rdb.addRuleToTree(origRule) // ignore any new error, should not occur
		// origRule was successfully removed before, so it should now be able
		// to be successfully re-added without error, and all is unchanged.
		changeOccurred = false
		return nil, err
	}
	newRule.ID = origRule.ID
	err, conflicts, conflictingPermission := rdb.addRuleToTree(newRule)
	if err != nil {
		rdb.addRuleToTree(origRule) // ignore any new error
		// origRule was successfully removed before, so it should now be able
		// to be successfully re-added without error, and all is unchanged.
		changeOccurred = false
		return nil, fmt.Errorf("%w: conflicts: %+v, Permission: '%s'", err, conflicts, conflictingPermission)
	}
	rdb.removeRuleWithID(origRule.ID) // no error can occur, we just confirmed the rule exists
	if err := rdb.addRule(newRule); err != nil {
		// Should not occur
		return nil, fmt.Errorf("internal error: %v", err)
	}
	changeOccurred = true
	return newRule, nil
}

// Rules returns all rules which apply to the given user.
func (rdb *RuleDB) Rules(user uint32) []*Rule {
	rdb.mutex.Lock()
	defer rdb.mutex.Unlock()
	ruleFilter := func(rule *Rule) bool {
		return rule.User == user
	}
	return rdb.rulesInternal(ruleFilter)
}

// rulesInternal returns all rules matching the given filter.
//
// TODO: store rules separately per user, snap, and interface, so actions which
// look up or delete all rules for a given user/snap/interface are much faster.
// This is safe, since rules must each apply to exactly one user, snap and
// interface, but may apply to multiple permissions.
func (rdb *RuleDB) rulesInternal(ruleFilter func(rule *Rule) bool) []*Rule {
	rules := make([]*Rule, 0)
	currTime := time.Now()
	needToSave := false
	defer func() {
		if needToSave {
			rdb.save()
		}
	}()
	for _, rule := range rdb.rules {
		expired, err := rule.Expired(currTime)
		if err != nil {
			// Issue with expiration, this should not occur
		}
		if expired {
			needToSave = true
			rdb.removeRuleFromTree(rule)
			rdb.removeRuleWithID(rule.ID)
			data := map[string]string{"removed": "expired"}
			rdb.notifyRule(rule.User, rule.ID, data)
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
	rdb.mutex.Lock()
	defer rdb.mutex.Unlock()
	ruleFilter := func(rule *Rule) bool {
		return rule.User == user && rule.Snap == snap
	}
	return rdb.rulesInternal(ruleFilter)
}

// RulesForInterface returns all rules which apply to the given user and
// interface.
func (rdb *RuleDB) RulesForInterface(user uint32, iface string) []*Rule {
	rdb.mutex.Lock()
	defer rdb.mutex.Unlock()
	ruleFilter := func(rule *Rule) bool {
		return rule.User == user && rule.Interface == iface
	}
	return rdb.rulesInternal(ruleFilter)
}

// RulesForSnapInterface returns all rules which apply to the given user, snap,
// and interface.
func (rdb *RuleDB) RulesForSnapInterface(user uint32, snap string, iface string) []*Rule {
	rdb.mutex.Lock()
	defer rdb.mutex.Unlock()
	ruleFilter := func(rule *Rule) bool {
		return rule.User == user && rule.Snap == snap && rule.Interface == iface
	}
	return rdb.rulesInternal(ruleFilter)
}
