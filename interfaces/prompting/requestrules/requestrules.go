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

// Validate verifies internal correctness of the rule
func (rule *Rule) validate() error {
	switch rule.Lifespan {
	case prompting.LifespanTimespan:
		// rules with limited timespan should have the expiration time set
		if rule.Expiration.IsZero() {
			return fmt.Errorf("rule has limited timespan but no expiration time")
		}
	}
	return nil
}

// Expired returns true if the receiving rule has a lifespan of timespan and
// the current time is after the rule's expiration time.
func (rule *Rule) Expired(currentTime time.Time) bool {
	switch rule.Lifespan {
	case prompting.LifespanTimespan:
		if currentTime.After(rule.Expiration) {
			return true
		}
		// TODO: add lifespan session
		//case prompting.LifespanSession:
		// TODO: return true if the user session has changed
	}
	return false
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
	mutex     sync.RWMutex
	maxIDMmap maxidmmap.MaxIDMmap

	// index to the rules by their rule IR
	indexByID map[prompting.IDType]int
	rules     []*Rule

	// the incoming request queries are made in the context of a user, snap,
	// snap interface, path, so this is essential a secondary compound index
	// made of all those properties for being able to identify a rule
	// matching given query
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
		logger.Noticef("cannot load rules DB: %v; using new empty rule database", err)
	}
	return rdb, nil
}

// rulesDBJSON is a helper type for wrapping request rules DB for serialization
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

	expiredRules := make(map[*Rule]bool)

	f, err := os.Open(rdb.dbPath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("cannot open rules database file: %w", err)
	}
	defer f.Close()

	var wrapped rulesDBJSON
	err = json.NewDecoder(f).Decode(&wrapped)
	if err != nil {
		// TODO: store rules separately per-user, so a corrupted rule for one
		// user can't impact rules for another user.
		return fmt.Errorf("cannot read stored prompting rules: %w", err)
	}

	currTime := time.Now()

	var errInvalid error
	for _, rule := range wrapped.Rules {
		if err := rule.validate(); err != nil {
			// we're loading previously saved rules, so this should not happen
			errInvalid = fmt.Errorf("internal error: %w", err)
			break
		}

		if rule.Expired(currTime) {
			expiredRules[rule] = true
			continue
		}

		if err = rdb.addRule(rule); err != nil {
			// Duplicate rules on disk, should not occur
			errInvalid = err
			break
		}
		if conflictErr := rdb.addRuleToTree(rule); conflictErr != nil {
			// Conflicting rules on disk, should not occur
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

		return errorsJoin(errInvalid, rdb.save())
	}

	expiredData := map[string]string{"removed": "expired"}
	for _, rule := range wrapped.Rules {
		var data map[string]string
		if expiredRules[rule] {
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
		logger.Noticef("cannot marshal rule DB: %v", err)
		return fmt.Errorf("cannot marshal rule DB: %w", err)
	}
	return osutil.AtomicWriteFile(rdb.dbPath, b, 0o600, 0)
}

// addRule adds the given rule to the rule DB.
//
// The caller must ensure that the database lock is held for writing.
func (rdb *RuleDB) addRule(rule *Rule) error {
	_, exists := rdb.indexByID[rule.ID]
	if exists {
		return ErrRuleIDConflict
	}
	rdb.rules = append(rdb.rules, rule)
	rdb.indexByID[rule.ID] = len(rdb.rules) - 1
	return nil
}

// lookupRuleByID returns the rule with the given ID from the rule DB.
//
// The caller must ensure that the database lock is held.
func (rdb *RuleDB) lookupRuleByID(id prompting.IDType) (*Rule, error) {
	index, exists := rdb.indexByID[id]
	if !exists {
		return nil, ErrRuleIDNotFound
	}
	if index >= len(rdb.rules) {
		return nil, ErrInternalInconsistency
	}
	return rdb.rules[index], nil
}

// removeRuleByID removes the rule with the given ID from the rule DB.
//
// The caller must ensure that the database lock is held for writing.
func (rdb *RuleDB) removeRuleByID(id prompting.IDType) (*Rule, error) {
	index, exists := rdb.indexByID[id]
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
	rdb.indexByID[movedID] = index
	delete(rdb.indexByID, id)
	return rule, nil
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

// ruleConflict stores the rendered variant which conflicted with that of
// another rule, along with the ID of that conflicting rule.
type ruleConflict struct {
	Variant       string           `json:"variant"`
	ConflictingID prompting.IDType `json:"conflicting-id"`
}

// ruleConflictError stores a list of rule conflicts that occurred for a
// particular permission.
type ruleConflictError struct {
	conflicts  []ruleConflict
	permission string
}

func (e *ruleConflictError) Error() string {
	marshalled, err := json.Marshal(e.conflicts)
	if err != nil {
		// marshalling a string and ID, error should not occur
		return fmt.Sprintf("%v: permission: '%s'", ErrPathPatternConflict, e.permission)
	}
	return fmt.Sprintf("%v: conflicts: %+v, permission: '%s'", ErrPathPatternConflict, string(marshalled), e.permission)
}

func (e *ruleConflictError) Unwrap() error {
	return ErrPathPatternConflict
}

// addRulePermissionToTree adds all the path pattern variants for the given
// rule to the map for the given permission.
//
// If there are conflicting pattern variants from other non-expired rules,
// all variants which were previously added during this function call are
// removed from the path map, and an error is returned along with a list of
// the conflicting variants and the IDs of the conflicting rules.
//
// Conflicts with expired rules, however, result in the expired rule being
// immediately removed, and the new rule can continue to be added.
//
// The caller must ensure that the database lock is held for writing.
func (rdb *RuleDB) addRulePermissionToTree(rule *Rule, permission string) *ruleConflictError {
	permVariants := rdb.ensurePermissionDBForUserSnapInterfacePermission(rule.User, rule.Snap, rule.Interface, permission)

	newVariantEntries := make(map[string]variantEntry, rule.Constraints.PathPattern.NumVariants())
	expiredRules := make(map[prompting.IDType]bool)
	var conflicts []ruleConflict

	addVariant := func(index int, variant patterns.PatternVariant) {
		newEntry := variantEntry{
			Variant: variant,
			RuleID:  rule.ID,
		}
		variantStr := variant.String()
		conflictingVariantEntry, exists := permVariants.VariantEntries[variantStr]
		switch {
		case !exists:
			newVariantEntries[variantStr] = newEntry
		case rdb.isRuleWithIDExpired(conflictingVariantEntry.RuleID, rule.Timestamp):
			expiredRules[conflictingVariantEntry.RuleID] = true
			newVariantEntries[variantStr] = newEntry
		default:
			// Exists and is not expired, so there's a conflict
			conflicts = append(conflicts, ruleConflict{
				Variant:       variantStr,
				ConflictingID: conflictingVariantEntry.RuleID,
			})
		}
	}
	rule.Constraints.PathPattern.RenderAllVariants(addVariant)

	if len(conflicts) > 0 {
		err := &ruleConflictError{
			conflicts:  conflicts,
			permission: permission,
		}
		return err
	}

	for ruleID := range expiredRules {
		removedRule, err := rdb.removeRuleByID(ruleID)
		// Error shouldn't occur. If it does, the rule was already removed.
		if err == nil {
			rdb.removeRuleFromTree(removedRule)
			rdb.notifyRule(removedRule.User, removedRule.ID,
				map[string]string{"removed": "expired"})
		}
	}

	for variantStr, entry := range newVariantEntries {
		permVariants.VariantEntries[variantStr] = entry
	}

	return nil
}

// isRuleWithIDExpired returns true if the rule with given ID is expired with respect
// to the provided timestamp, or if it otherwise no longer exists.
//
// The caller must ensure that the database lock is held for writing.
func (rdb *RuleDB) isRuleWithIDExpired(id prompting.IDType, currTime time.Time) bool {
	rule, err := rdb.lookupRuleByID(id)
	if err != nil {
		return true
	}
	return rule.Expired(currTime)
}

// removeExistingRule removes provided rule from permissions tree and
// from rules DB. As a precodition the called must have confirmed that the rule
// exists as errors are ignored.
func (rdb *RuleDB) removeExistingRule(rule *Rule) {
	rdb.removeRuleFromTree(rule) // if error occurs, rule still fully removed
	rdb.removeRuleByID(rule.ID)
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
func (rdb *RuleDB) removeRulePermissionFromTree(rule *Rule, permission string) []error {
	permVariants, ok := rdb.permissionDBForUserSnapInterfacePermission(rule.User, rule.Snap, rule.Interface, permission)
	if !ok || permVariants == nil {
		err := fmt.Errorf("internal error: no rules in the rule tree for user %d, snap %q, interface %q, permission %q", rule.User, rule.Snap, rule.Interface, permission)
		return []error{err}
	}
	var errs []error
	removeVariant := func(index int, variant patterns.PatternVariant) {
		variantEntry, exists := permVariants.VariantEntries[variant.String()]
		if !exists {
			// Database was left inconsistent, should not occur
			errs = append(errs, fmt.Errorf(`internal error: path pattern variant not found in the rule tree: %q`, variant))
		} else if variantEntry.RuleID != rule.ID {
			// Database was left inconsistent, should not occur
			errs = append(errs, fmt.Errorf(`internal error: path pattern variant maps to different rule ID: %q: %s`, variant, variantEntry.RuleID.String()))
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
// error along with the conflicting rules info and the permission for which
// the conflict occurred.
//
// The caller must ensure that the database lock is held for writing.
func (rdb *RuleDB) addRuleToTree(rule *Rule) *ruleConflictError {
	addedPermissions := make([]string, 0, len(rule.Constraints.Permissions))

	var err *ruleConflictError
	for _, permission := range rule.Constraints.Permissions {
		err = rdb.addRulePermissionToTree(rule, permission)
		if err != nil {
			break
		}
		addedPermissions = append(addedPermissions, permission)
	}

	if err != nil {
		// remove the rule permissions we just added
		for _, prevPerm := range addedPermissions {
			rdb.removeRulePermissionFromTree(rule, prevPerm)
		}
		return err
	}

	return nil
}

// removeRuleFromTree fully removes the given rule from the tree, even if an
// error occurs.
//
// The caller must ensure that the database lock is held for writing.
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

// makeNewRule creates a new Rule with the given contents.
//
// Users of requestrules should probably autofill rules from JSON and never call
// this function directly.
//
// Constructs a new rule with the given parameters as values, with the
// exception of duration. Uses the given duration, in addition to the current
// time, to compute the expiration time for the rule, and stores that as part
// of the rule which is returned. If any of the given parameters are invalid,
// returns a corresponding error.
func (rdb *RuleDB) makeNewRule(user uint32, snap string, iface string, constraints *prompting.Constraints, outcome prompting.OutcomeType, lifespan prompting.LifespanType, duration string) (*Rule, error) {
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

	if err := newRule.validate(); err != nil {
		return nil, err
	}

	return &newRule, nil
}

// IsPathAllowed checks whether the given path with the given permission is
// allowed or denied by existing rules for the given user, snap, and interface.
// If no rule applies, returns ErrNoMatchingRule.
func (rdb *RuleDB) IsPathAllowed(user uint32, snap string, iface string, path string, permission string) (bool, error) {
	rdb.mutex.RLock()
	defer rdb.mutex.RUnlock()
	permissionMap, ok := rdb.permissionDBForUserSnapInterfacePermission(user, snap, iface, permission)
	if !ok || permissionMap == nil {
		return false, ErrNoMatchingRule
	}
	variantMap := permissionMap.VariantEntries
	matchingVariants := make([]patterns.PatternVariant, 0)
	// Make sure all rules use the same expiration timestamp, so a rule with
	// an earlier expiration cannot outlive another rule with a later one.
	currTime := time.Now()
	for variantStr, variantEntry := range variantMap {
		matchingRule, err := rdb.lookupRuleByID(variantEntry.RuleID)
		if err != nil {
			logger.Noticef("internal error: inconsistent DB when fetching rule %v", variantEntry.RuleID)
			// Database was left inconsistent, should not occur
			delete(variantMap, variantStr)
			// Record a notice for the offending rule, just in case
			rdb.notifyRule(user, variantEntry.RuleID, nil)
			continue
		}

		if matchingRule.Expired(currTime) {
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
		return false, ErrNoMatchingRule
	}
	highestPrecedenceVariant, err := patterns.HighestPrecedencePattern(matchingVariants, path)
	if err != nil {
		return false, err
	}
	matchingEntry := variantMap[highestPrecedenceVariant.String()]
	matchingID := matchingEntry.RuleID
	matchingRule, err := rdb.lookupRuleByID(matchingID)
	if err != nil {
		// Database was left inconsistent, should not occur
		return false, fmt.Errorf("internal error: while looking for rule %v: %w", matchingID, ErrRuleIDNotFound)
	}
	return matchingRule.Outcome.AsBool()
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
		return nil, ErrUserNotAllowed
	}
	return rule, nil
}

// RuleWithID returns the rule with the given ID.
// If the rule is not found, returns ErrRuleNotFound.
// If the rule does not apply to the given user, returns ErrUserNotAllowed.
func (rdb *RuleDB) RuleWithID(user uint32, id prompting.IDType) (*Rule, error) {
	rdb.mutex.RLock()
	defer rdb.mutex.RUnlock()
	return rdb.lookupRuleByIDForUser(user, id)
}

// Creates a rule with the given information and adds it to the rule database.
// If any of the given parameters are invalid, returns an error. Otherwise,
// returns the newly-added rule, and saves the database to disk.
func (rdb *RuleDB) AddRule(user uint32, snap string, iface string, constraints *prompting.Constraints, outcome prompting.OutcomeType, lifespan prompting.LifespanType, duration string) (*Rule, error) {
	rdb.mutex.Lock()
	defer rdb.mutex.Unlock()
	newRule, err := rdb.makeNewRule(user, snap, iface, constraints, outcome, lifespan, duration)
	if err != nil {
		return nil, err
	}
	if err := rdb.addRuleToTree(newRule); err != nil {
		return nil, fmt.Errorf("cannot add rule: %w", err)
	}
	rdb.addRule(newRule)

	if err := rdb.save(); err != nil {
		rdb.removeExistingRule(newRule)
		return nil, err
	}

	rdb.notifyRule(user, newRule.ID, nil)
	return newRule, nil
}

// RemoveRule the rule with the given ID from the rules database. If the rule
// does not apply to the given user, returns ErrUserNotAllowed. If successful,
// saves the database to disk.
func (rdb *RuleDB) RemoveRule(user uint32, id prompting.IDType) (*Rule, error) {
	rdb.mutex.Lock()
	defer rdb.mutex.Unlock()

	rule, err := rdb.lookupRuleByIDForUser(user, id)
	if err != nil {
		// The rule doesn't exist or the user doesn't have access
		return nil, err
	}

	rdb.removeRuleByID(id) // We know the rule exists, so this should not error

	// Now that rule is removed from rules list, can try to save
	if err := rdb.save(); err != nil {
		// Roll back the change by re-adding the removed rule
		rdb.addRule(rule)
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
//
// The caller must ensure that the database lock is held for writing.
func (rdb *RuleDB) removeRulesInternal(user uint32, rules []*Rule) error {
	for _, rule := range rules {
		// Remove rule from the rules list. Caller should ensure that
		// the rule exists, and thus this should not error.
		rdb.removeRuleByID(rule.ID)
	}

	// Now that rules have been removed from rules list, attempt to save
	if err := rdb.save(); err != nil {
		// Roll back the change by re-adding all removed rules
		for _, rule := range rules {
			rdb.addRule(rule)
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

// PatchRule modifies the rule with the given ID by updating the rule's fields
// corresponding to any of the given parameters which are set/non-empty.
//
// Any of the parameters which are equal to the default/unset value for their
// types are left unchanged from the existing rule. Even if the given new rule
// contents exactly match the existing rule contents, the timestamp of the rule
// is updated to the current time. If there is any error while modifying the
// rule, the rule is rolled back to its previous unmodified state, leaving the
// database unchanged. If the database is changed, it is saved to disk.
func (rdb *RuleDB) PatchRule(user uint32, id prompting.IDType, constraints *prompting.Constraints, outcome prompting.OutcomeType, lifespan prompting.LifespanType, duration string) (r *Rule, err error) {
	rdb.mutex.Lock()
	defer rdb.mutex.Unlock()

	origRule, err := rdb.lookupRuleByIDForUser(user, id)
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

	newRule, err := rdb.makeNewRule(user, origRule.Snap, origRule.Interface, constraints, outcome, lifespan, duration)
	if err != nil {
		return nil, err
	}
	newRule.ID = origRule.ID

	// Remove the existing rule from the tree. An error should not occur, but
	// if it does, carry on, hopefully the new rule won't have a conflict
	rdb.removeRuleFromTree(origRule)

	if addErr := rdb.addRuleToTree(newRule); addErr != nil {
		err := fmt.Errorf("cannot patch rule: %w", addErr)
		// Try to re-add original rule so all is unchanged.
		if origErr := rdb.addRuleToTree(origRule); origErr != nil {
			// Error should not occur, but if it does, wrap it in the other error
			err = errorsJoin(err, fmt.Errorf("cannot re-add original rule: %w", origErr))
			// XXX: should origRule be removed from the rules list if it's no
			// longer in the tree?
		}
		return nil, err
	}

	rdb.removeRuleByID(origRule.ID) // no error should occur, we just confirmed the rule exists

	if err := rdb.addRule(newRule); err != nil {
		// Should not occur, but if so, re-add the original rule
		rdb.addRule(origRule)
		return nil, fmt.Errorf("internal error: %v", err)
	}

	if err := rdb.save(); err != nil {
		// Should not occur, but if it does, roll back to the original state.
		// All of the following should succeed, since we're reversing what we
		// just successfully completed.
		rdb.removeRuleByID(newRule.ID)
		rdb.addRule(origRule)

		rdb.removeRuleFromTree(newRule)
		rdb.addRuleToTree(origRule)
		return nil, err
	}

	rdb.notifyRule(newRule.User, newRule.ID, nil)
	return newRule, nil
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
		if rule.Expired(currTime) {
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
