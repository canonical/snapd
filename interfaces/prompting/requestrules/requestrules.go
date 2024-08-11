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
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/ifacestate/apparmorprompting/common"
	"github.com/snapcore/snapd/overlord/state"
)

var (
	ErrInternalInconsistency = errors.New("internal error: prompting rules database left inconsistent")
	ErrRuleIDNotFound        = errors.New("rule ID is not found")
	ErrPathPatternConflict   = errors.New("a rule with conflicting path pattern and permission already exists in the rules database")
	ErrNoMatchingRule        = errors.New("no rules match the given path")
	ErrUserNotAllowed        = errors.New("the given user is not allowed to request the rule with the given ID")
)

type Rule struct {
	ID               string              `json:"id"`
	Timestamp        time.Time           `json:"timestamp"`
	User             uint32              `json:"user"`
	Snap             string              `json:"snap"`
	Interface        string              `json:"interface"`
	Constraints      *common.Constraints `json:"constraints"`
	expandedPatterns []string            `json:"-"`
	Outcome          common.OutcomeType  `json:"outcome"`
	Lifespan         common.LifespanType `json:"lifespan"`
	Expiration       time.Time           `json:"expiration,omitempty"`
}

func (rule *Rule) removePermission(permission string) error {
	if err := rule.Constraints.RemovePermission(permission); err != nil {
		return err
	}
	if len(rule.Constraints.Permissions) == 0 {
		return common.ErrPermissionsListEmpty
	}
	return nil
}

func (rule *Rule) Expired(currentTime time.Time) (bool, error) {
	switch rule.Lifespan {
	case common.LifespanTimespan:
		if rule.Expiration.IsZero() {
			// Should not occur
			return false, fmt.Errorf("encountered rule with lifespan timespan but no expiration")
		}
		if currentTime.After(rule.Expiration) {
			return true, nil
		}
		return false, nil
	case common.LifespanSession:
		// TODO: return true if the user session has changed
	}
	return false, nil
}

type permissionDB struct {
	// permissionDB contains a map from expanded path pattern to rule ID
	PathRules map[string]string
}

type interfaceDB struct {
	// interfaceDB contains a map from permission to permissionDB for a particular interface
	PerPermission map[string]*permissionDB
}

type snapDB struct {
	// snapDB contains a map from interface to interfaceDB for a particular snap
	PerInterface map[string]*interfaceDB
}

type userDB struct {
	// userDB contains a map from snap to snapDB for a particular user
	PerSnap map[string]*snapDB
}

type RuleDB struct {
	ByID    map[string]*Rule
	PerUser map[uint32]*userDB
	mutex   sync.Mutex
	// Function to issue a notice for a change in a rule
	notifyRule func(userID uint32, ruleID string, options *state.AddNoticeOptions) error
}

// Creates a new rule database, loads existing rules from the path given by
// dbpath(), and returns the populated database.
func New(notifyRule func(userID uint32, ruleID string, options *state.AddNoticeOptions) error) (*RuleDB, error) {
	rdb := &RuleDB{
		ByID:       make(map[string]*Rule),
		PerUser:    make(map[uint32]*userDB),
		notifyRule: notifyRule,
	}
	return rdb, rdb.load()
}

func (rdb *RuleDB) dbpath() string {
	return filepath.Join(dirs.SnapdStateDir(dirs.GlobalRootDir), "request-rules.json")
}

func (rdb *RuleDB) permissionDBForUserSnapInterfacePermission(user uint32, snap string, iface string, permission string) *permissionDB {
	userSnaps := rdb.PerUser[user]
	if userSnaps == nil {
		userSnaps = &userDB{
			PerSnap: make(map[string]*snapDB),
		}
		rdb.PerUser[user] = userSnaps
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
	permPaths := interfacePerms.PerPermission[permission]
	if permPaths == nil {
		permPaths = &permissionDB{
			PathRules: make(map[string]string),
		}
		interfacePerms.PerPermission[permission] = permPaths
	}
	return permPaths
}

// Add all the expanded path patterns for the rule to the map for the given
// permission. If there is a conflicting path pattern from another rule, all
// patterns which were previously added during this function call are removed
// from the path map, and returns an error along with the ID of the conflicting
// rule.
func (rdb *RuleDB) addRulePermissionToTree(rule *Rule, permission string) (error, string) {
	permPaths := rdb.permissionDBForUserSnapInterfacePermission(rule.User, rule.Snap, rule.Interface, permission)
	for i, pathPattern := range rule.expandedPatterns {
		if id, exists := permPaths.PathRules[pathPattern]; exists {
			for _, prevPattern := range rule.expandedPatterns[:i] {
				delete(permPaths.PathRules, prevPattern)
			}
			return ErrPathPatternConflict, id
		}
		permPaths.PathRules[pathPattern] = rule.ID
	}
	return nil, ""
}

// Remove all the expanded path patterns for the rule to the map for the given
// permission. If an expanded pattern is not found or maps to a different rule
// ID than that of the given rule, continue to remove all other expanded paths
// from the permission map (unless they map to a different rule ID), and return
// a slice of all errors which occurred.
func (rdb *RuleDB) removeRulePermissionFromTree(rule *Rule, permission string) []error {
	permPaths := rdb.permissionDBForUserSnapInterfacePermission(rule.User, rule.Snap, rule.Interface, permission)
	var errs []error
	for _, pathPattern := range rule.expandedPatterns {
		id, exists := permPaths.PathRules[pathPattern]
		if !exists {
			// Database was left inconsistent, should not occur
			errs = append(errs, fmt.Errorf(`expanded path pattern not found in the rule tree: %q`, pathPattern))
			continue
		}
		if id != rule.ID {
			// Database was left inconsistent, should not occur
			errs = append(errs, fmt.Errorf(`expanded path pattern maps to different rule ID: %q: %s`, pathPattern, id))
			continue
		}
		delete(permPaths.PathRules, pathPattern)
	}
	return errs
}

func (rdb *RuleDB) addRuleToTree(rule *Rule) (error, string, string) {
	// If there is a conflicting path pattern from another rule, returns an
	// error along with the ID of the conflicting rule and the permission for
	// which the conflict occurred
	addedPermissions := make([]string, 0, len(rule.Constraints.Permissions))
	for _, permission := range rule.Constraints.Permissions {
		if err, conflictingID := rdb.addRulePermissionToTree(rule, permission); err != nil {
			for _, prevPerm := range addedPermissions {
				rdb.removeRulePermissionFromTree(rule, prevPerm)
			}
			return err, conflictingID, permission
		}
		addedPermissions = append(addedPermissions, permission)
	}
	return nil, "", ""
}

func (rdb *RuleDB) removeRuleFromTree(rule *Rule) error {
	// Fully removes the rule from the tree, even if an error occurs
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

// TODO: unexport (probably)
// This function is only required if database is left inconsistent (should not
// occur) or when loading, in case the stored rules on disk were corrupted.
//
// By default, issues a notice for each rule which is modified or removed as a
// result of a conflict with another rule. If notifyEveryRule is true, issues
// a notice for every rule which was in the database prior to the beginning of
// the function. In either case, at most one notice is issued for each rule.
//
// Discards the current rule tree, then iterates through the rules in rdb.ByID
// and re-populates the tree. If there are any conflicts between rules (that
// is, rules share the same path pattern and one or more of the same
// permissions), the conflicting permission is removed from the rule with the
// earlier timestamp. When the function returns, the database should be fully
// internally consistent and without conflicting rules.
func (rdb *RuleDB) RefreshTreeEnforceConsistency(notifyEveryRule bool) {
	rdb.mutex.Lock()
	defer rdb.mutex.Unlock()
	needToSave := false
	defer func() {
		if needToSave {
			rdb.save()
		}
	}()
	modifiedUserRuleIDs := make(map[uint32]map[string]bool)
	defer func() {
		for user, ruleIDs := range modifiedUserRuleIDs {
			for ruleID := range ruleIDs {
				rdb.notifyRule(user, ruleID, nil)
			}
		}
	}()
	currTime := time.Now()
	newByID := make(map[string]*Rule)
	rdb.PerUser = make(map[uint32]*userDB)
	for id, rule := range rdb.ByID {
		_, exists := modifiedUserRuleIDs[rule.User]
		if !exists {
			modifiedUserRuleIDs[rule.User] = make(map[string]bool)
		}
		if notifyEveryRule {
			modifiedUserRuleIDs[rule.User][id] = true
		}
		if err := common.ValidateConstraintsOutcomeLifespanExpiration(rule.Interface, rule.Constraints, rule.Outcome, rule.Lifespan, rule.Expiration, currTime); err != nil {
			// Invalid rule, discard it
			needToSave = true
			modifiedUserRuleIDs[rule.User][id] = true
			continue
		}
		expandedPatterns, err := common.ExpandPathPattern(rule.Constraints.PathPattern)
		if err != nil {
			// Invalid path pattern, discard this rule
			// This should not occur, since previous validation should catch invalid patterns.
			needToSave = true
			modifiedUserRuleIDs[rule.User][id] = true
			continue
		}
		rule.expandedPatterns = expandedPatterns
		for {
			err, conflictingID, conflictingPermission := rdb.addRuleToTree(rule)
			if err == nil {
				break
			}
			// Err must be ErrPathPatternConflict.
			// Prioritize newer rules by pruning permission from old rule until no conflicts remain.
			conflictingRule := rdb.ByID[conflictingID] // must exist
			if rule.Timestamp.After(conflictingRule.Timestamp) {
				rdb.removeRulePermissionFromTree(conflictingRule, conflictingPermission) // must return nil
				if conflictingRule.removePermission(conflictingPermission) == common.ErrPermissionsListEmpty {
					delete(newByID, conflictingID)
				}
				modifiedUserRuleIDs[conflictingRule.User][conflictingID] = true
			} else {
				rule.removePermission(conflictingPermission) // ignore error
				modifiedUserRuleIDs[rule.User][id] = true
			}
			needToSave = true
		}
		if len(rule.Constraints.Permissions) > 0 {
			newByID[id] = rule
		} else {
			needToSave = true
			modifiedUserRuleIDs[rule.User][id] = true
		}
	}
	rdb.ByID = newByID
}

func (rdb *RuleDB) load() error {
	target := rdb.dbpath()
	f, err := os.Open(target)
	if err != nil {
		return err
	}
	defer f.Close()

	ruleList := make([]*Rule, 0, 256)    // pre-allocate a large array to reduce memcpys
	json.NewDecoder(f).Decode(&ruleList) // TODO: handle errors

	rdb.ByID = make(map[string]*Rule) // clear out any existing rules in ByID
	for _, rule := range ruleList {
		rdb.ByID[rule.ID] = rule
	}

	notifyEveryRule := true
	rdb.RefreshTreeEnforceConsistency(notifyEveryRule)

	return nil
}

func (rdb *RuleDB) save() error {
	ruleList := make([]*Rule, 0, len(rdb.ByID))
	for _, rule := range rdb.ByID {
		ruleList = append(ruleList, rule)
	}
	b, err := json.Marshal(ruleList)
	if err != nil {
		return err
	}
	target := rdb.dbpath()
	if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
		return err
	}
	return osutil.AtomicWriteFile(target, b, 0600, 0)
}

// TODO: unexport (probably, avoid confusion with AddRule)
// Users of requestrules should probably autofill rules from JSON and never call
// this function directly.
//
// Constructs a new rule with the given parameters as values, with the
// exception of duration. Uses the given duration, in addition to the current
// time, to compute the expiration time for the rule, and stores that as part
// of the rule which is returned. If any of the given parameters are invalid,
// returns a corresponding error.
func (rdb *RuleDB) PopulateNewRule(user uint32, snap string, iface string, constraints *common.Constraints, outcome common.OutcomeType, lifespan common.LifespanType, duration string) (*Rule, error) {
	expiration, err := common.ValidateConstraintsOutcomeLifespanDuration(iface, constraints, outcome, lifespan, duration)
	if err != nil {
		return nil, err
	}
	expandedPatterns, err := common.ExpandPathPattern(constraints.PathPattern)
	if err != nil {
		return nil, err
	}
	id, timestamp := common.NewIDAndTimestamp()
	newRule := Rule{
		ID:               id,
		Timestamp:        timestamp,
		User:             user,
		Snap:             snap,
		Interface:        iface,
		Constraints:      constraints,
		expandedPatterns: expandedPatterns,
		Outcome:          outcome,
		Lifespan:         lifespan,
		Expiration:       expiration,
	}
	return &newRule, nil
}

// Checks whether the given path with the given permission is allowed or
// denied by existing rules for the given user, snap, and interface.
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
	pathMap := rdb.permissionDBForUserSnapInterfacePermission(user, snap, iface, permission).PathRules
	matchingPatterns := make([]string, 0)
	// Make sure all rules use the same expiration timestamp, so a rule with
	// an earlier expiration cannot outlive another rule with a later one.
	currTime := time.Now()
	for pathPattern, id := range pathMap {
		matchingRule, exists := rdb.ByID[id]
		if !exists {
			// Database was left inconsistent, should not occur
			delete(pathMap, id)
			// Issue a notice for the offending rule, just in case
			rdb.notifyRule(user, id, nil)
			continue
		}
		expired, err := matchingRule.Expired(currTime)
		if err != nil {
			return false, err
		}
		if expired {
			needToSave = true
			rdb.removeRuleFromTree(matchingRule)
			delete(rdb.ByID, id)
			rdb.notifyRule(user, id, nil)
			continue
		}
		// Need to compare the expanded path pattern, not the rule's path
		// pattern, so that only expanded patterns which match are included,
		// and the highest precedence expanded pattern can be computed.
		matched, err := common.PathPatternMatch(pathPattern, path)
		if err != nil {
			// Only possible error is ErrBadPattern, which should not occur
			return false, err
		}
		if matched {
			matchingPatterns = append(matchingPatterns, pathPattern)
		}
	}
	if len(matchingPatterns) == 0 {
		return false, ErrNoMatchingRule
	}
	highestPrecedencePattern, err := common.GetHighestPrecedencePattern(matchingPatterns)
	if err != nil {
		return false, err
	}
	matchingID := pathMap[highestPrecedencePattern]
	matchingRule, exists := rdb.ByID[matchingID]
	if !exists {
		// Database was left inconsistent, should not occur
		return false, ErrRuleIDNotFound
	}
	if matchingRule.Lifespan == common.LifespanSingle {
		rdb.removeRuleFromTree(matchingRule)
		delete(rdb.ByID, matchingID)
		rdb.notifyRule(user, matchingID, nil)
		needToSave = true
	}
	return matchingRule.Outcome.AsBool()
}

func (rdb *RuleDB) ruleWithIDInternal(user uint32, id string) (*Rule, error) {
	rule, exists := rdb.ByID[id]
	if !exists {
		return nil, ErrRuleIDNotFound
	}
	if rule.User != user {
		return nil, ErrUserNotAllowed
	}
	return rule, nil
}

// Returns the rule with the given ID.
// If the rule is not found, returns ErrRuleNotFound.
// If the rule does not apply to the given user, returns ErrUserNotAllowed.
func (rdb *RuleDB) RuleWithID(user uint32, id string) (*Rule, error) {
	rdb.mutex.Lock()
	defer rdb.mutex.Unlock()
	return rdb.ruleWithIDInternal(user, id)
}

// Creates a rule with the given information and adds it to the rule database.
// If any of the given parameters are invalid, returns an error. Otherwise,
// returns the newly-added rule, and saves the database to disk.
func (rdb *RuleDB) AddRule(user uint32, snap string, iface string, constraints *common.Constraints, outcome common.OutcomeType, lifespan common.LifespanType, duration string) (*Rule, error) {
	rdb.mutex.Lock()
	defer rdb.mutex.Unlock()
	newRule, err := rdb.PopulateNewRule(user, snap, iface, constraints, outcome, lifespan, duration)
	if err != nil {
		return nil, err
	}
	if err, conflictingID, conflictingPermission := rdb.addRuleToTree(newRule); err != nil {
		return nil, fmt.Errorf("%w: ID: '%s', Permission: '%s'", err, conflictingID, conflictingPermission)
	}
	rdb.ByID[newRule.ID] = newRule
	rdb.save()
	rdb.notifyRule(user, newRule.ID, nil)
	return newRule, nil
}

// RemoveRule the rule with the given ID from the rules database. If the rule
// does not apply to the given user, returns ErrUserNotAllowed. If successful,
// saves the database to disk.
func (rdb *RuleDB) RemoveRule(user uint32, id string) (*Rule, error) {
	rdb.mutex.Lock()
	defer rdb.mutex.Unlock()
	rule, err := rdb.ruleWithIDInternal(user, id)
	if err != nil {
		return nil, err
	}
	err = rdb.removeRuleFromTree(rule)
	// If error occurs, rule was still fully removed from tree
	delete(rdb.ByID, id)
	rdb.save()
	rdb.notifyRule(user, id, nil)
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

func (rdb *RuleDB) removeRulesInternal(user uint32, rules []*Rule) {
	for _, rule := range rules {
		rdb.removeRuleFromTree(rule)
		// If error occurs, rule was still fully removed from tree
		delete(rdb.ByID, rule.ID)
		rdb.notifyRule(user, rule.ID, nil)
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

// Patches the rule with the given ID. The rule is modified by constructing a
// new rule based on the given parameters, and then replacing the old rule with
// the same ID with the new rule. Any of the parameters which are equal to the
// default/unset value for their types are replaced by the corresponding values
// in the existing rule. Even if the given new rule contents exactly match the
// existing rule contents, the timestamp of the rule is updated to the current
// time. If there is any error while adding the patched rule to the database,
// rolls back to the previous unmodified rule, leaving the databse unchanged.
// If the database is changed, it is saved to disk.
func (rdb *RuleDB) PatchRule(user uint32, id string, constraints *common.Constraints, outcome common.OutcomeType, lifespan common.LifespanType, duration string) (*Rule, error) {
	rdb.mutex.Lock()
	defer rdb.mutex.Unlock()
	changeOccurred := false
	defer func() {
		if changeOccurred {
			rdb.save()
			rdb.notifyRule(user, id, nil)
		}
	}()
	origRule, err := rdb.ruleWithIDInternal(user, id)
	if err != nil {
		return nil, err
	}
	if constraints == nil {
		constraints = origRule.Constraints
	}
	if outcome == common.OutcomeUnset {
		outcome = origRule.Outcome
	}
	if lifespan == common.LifespanUnset {
		lifespan = origRule.Lifespan
	}
	if err = rdb.removeRuleFromTree(origRule); err != nil {
		// If error occurs, rule is fully removed from tree
		changeOccurred = true
		return nil, err
	}
	newRule, err := rdb.PopulateNewRule(user, origRule.Snap, origRule.Interface, constraints, outcome, lifespan, duration)
	if err != nil {
		rdb.addRuleToTree(origRule) // ignore any new error, should not occur
		// origRule was successfully removed before, so it should now be able
		// to be successfully re-added without error, and all is unchanged.
		changeOccurred = false
		return nil, err
	}
	newRule.ID = origRule.ID
	err, conflictingID, conflictingPermission := rdb.addRuleToTree(newRule)
	if err != nil {
		rdb.addRuleToTree(origRule) // ignore any new error
		// origRule was successfully removed before, so it should now be able
		// to be successfully re-added without error, and all is unchanged.
		changeOccurred = false
		return nil, fmt.Errorf("%w: ID: '%s', Permission: '%s'", err, conflictingID, conflictingPermission)
	}
	rdb.ByID[newRule.ID] = newRule
	changeOccurred = true
	return newRule, nil
}

// Returns all rules which apply to the given user.
func (rdb *RuleDB) Rules(user uint32) []*Rule {
	rdb.mutex.Lock()
	defer rdb.mutex.Unlock()
	ruleFilter := func(rule *Rule) bool {
		return rule.User == user
	}
	return rdb.rulesInternal(ruleFilter)
}

func (rdb *RuleDB) rulesInternal(ruleFilter func(rule *Rule) bool) []*Rule {
	rules := make([]*Rule, 0)
	currTime := time.Now()
	needToSave := false
	defer func() {
		if needToSave {
			rdb.save()
		}
	}()
	for id, rule := range rdb.ByID {
		expired, err := rule.Expired(currTime)
		if err != nil {
			// Issue with expiration, this should not occur
		}
		if expired {
			needToSave = true
			rdb.removeRuleFromTree(rule)
			delete(rdb.ByID, id)
			rdb.notifyRule(rule.User, id, nil)
			continue
		}
		if ruleFilter(rule) {
			rules = append(rules, rule)
		}
	}
	return rules
}

// Returns all rules which apply to the given user and snap.
func (rdb *RuleDB) RulesForSnap(user uint32, snap string) []*Rule {
	rdb.mutex.Lock()
	defer rdb.mutex.Unlock()
	ruleFilter := func(rule *Rule) bool {
		return rule.User == user && rule.Snap == snap
	}
	return rdb.rulesInternal(ruleFilter)
}

// Returns all rules which apply to the given user and interface.
func (rdb *RuleDB) RulesForInterface(user uint32, iface string) []*Rule {
	rdb.mutex.Lock()
	defer rdb.mutex.Unlock()
	ruleFilter := func(rule *Rule) bool {
		return rule.User == user && rule.Interface == iface
	}
	return rdb.rulesInternal(ruleFilter)
}

// Returns all rules which apply to the given user, snap, and interface.
func (rdb *RuleDB) RulesForSnapInterface(user uint32, snap string, iface string) []*Rule {
	rdb.mutex.Lock()
	defer rdb.mutex.Unlock()
	ruleFilter := func(rule *Rule) bool {
		return rule.User == user && rule.Snap == snap && rule.Interface == iface
	}
	return rdb.rulesInternal(ruleFilter)
}
