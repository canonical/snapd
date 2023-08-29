package accessrules

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/ifacestate/apparmorprompting/common"
)

var ErrPathPatternMissingFromTree = errors.New("path pattern was not found in the access rule tree")
var ErrRuleIDMismatch = errors.New("the access rule ID in the tree does not match the expected rule ID")
var ErrRuleIDNotFound = errors.New("access rule ID is not found")
var ErrPathPatternConflict = errors.New("a rule with the same path pattern already exists in the tree")
var ErrPermissionNotFound = errors.New("permission not found in the permissions list for the given rule")
var ErrPermissionsEmpty = errors.New("all permissions have been removed from the permissions list of the given rule")
var ErrNoMatchingRule = errors.New("no access rules match the given path")
var ErrUserNotAllowed = errors.New("the given user is not allowed to access the access rule with the given ID")

type AccessRule struct {
	ID          string                  `json:"id"`
	Timestamp   string                  `json:"timestamp"`
	User        int                     `json:"user"`
	Snap        string                  `json:"snap"`
	App         string                  `json:"app"`
	PathPattern string                  `json:"path-pattern"`
	Outcome     common.OutcomeType      `json:"outcome"`
	Lifespan    common.LifespanType     `json:"lifespan"`
	Expiration  string                  `json:"expiration"`
	Permissions []common.PermissionType `json:"permissions"`
}

func (rule *AccessRule) removePermissionFromPermissionsList(permission common.PermissionType) error {
	newList, err := common.RemovePermissionFromList(rule.Permissions, permission)
	if err != nil {
		return err
	}
	rule.Permissions = newList
	if len(newList) == 0 {
		return ErrPermissionsEmpty
	}
	return nil
}

type permissionDB struct {
	// permissionDB contains a map from path pattern to access rule ID
	PathRules map[string]string
}

type appDB struct {
	// appDB contains a map from permission to permissionDB for a particular app
	PerPermission map[common.PermissionType]*permissionDB
}

type snapDB struct {
	// snapDB contains a map from app to appDB for a particular snap
	PerApp map[string]*appDB
}

type userDB struct {
	// userDB contains a map from snap to snapDB for a particular user
	PerSnap map[string]*snapDB
}

type AccessRuleDB struct {
	ByID    map[string]*AccessRule
	PerUser map[int]*userDB
	mutex   sync.Mutex
}

// Creates a new access rule database, loads existing access rules from the
// path given by dbpath(), and returns the populated database.
func New() (*AccessRuleDB, error) {
	ardb := &AccessRuleDB{
		ByID:    make(map[string]*AccessRule),
		PerUser: make(map[int]*userDB),
	}
	return ardb, ardb.load()
}

func (ardb *AccessRuleDB) dbpath() string {
	return filepath.Join(dirs.SnapdStateDir(dirs.GlobalRootDir), "access-rules.json")
}

func (ardb *AccessRuleDB) permissionDBForUserSnapAppPermission(user int, snap string, app string, permission common.PermissionType) *permissionDB {
	userSnaps := ardb.PerUser[user]
	if userSnaps == nil {
		userSnaps = &userDB{
			PerSnap: make(map[string]*snapDB),
		}
		ardb.PerUser[user] = userSnaps
	}
	snapApps := userSnaps.PerSnap[snap]
	if snapApps == nil {
		snapApps = &snapDB{
			PerApp: make(map[string]*appDB),
		}
		userSnaps.PerSnap[snap] = snapApps
	}
	appPerms := snapApps.PerApp[app]
	if appPerms == nil {
		appPerms = &appDB{
			PerPermission: make(map[common.PermissionType]*permissionDB),
		}
		snapApps.PerApp[app] = appPerms
	}
	permPaths := appPerms.PerPermission[permission]
	if permPaths == nil {
		permPaths = &permissionDB{
			PathRules: make(map[string]string),
		}
		appPerms.PerPermission[permission] = permPaths
	}
	return permPaths
}

func (ardb *AccessRuleDB) addRulePermissionToTree(rule *AccessRule, permission common.PermissionType) (error, string) {
	// If there is a conflicting path pattern from another rule, returns an
	// error along with the ID of the conflicting rule.
	permPaths := ardb.permissionDBForUserSnapAppPermission(rule.User, rule.Snap, rule.App, permission)
	pathPattern := rule.PathPattern
	if id, exists := permPaths.PathRules[pathPattern]; exists {
		return ErrPathPatternConflict, id
	}
	permPaths.PathRules[pathPattern] = rule.ID
	return nil, ""
}

func (ardb *AccessRuleDB) removeRulePermissionFromTree(rule *AccessRule, permission common.PermissionType) error {
	permPaths := ardb.permissionDBForUserSnapAppPermission(rule.User, rule.Snap, rule.App, permission)
	pathPattern := rule.PathPattern
	id, exists := permPaths.PathRules[pathPattern]
	if !exists {
		// Database was left inconsistent, should not occur
		return ErrPathPatternMissingFromTree
	}
	if id != rule.ID {
		// Database was left inconsistent, should not occur
		return ErrRuleIDMismatch
	}
	delete(permPaths.PathRules, pathPattern)
	return nil
}

func (ardb *AccessRuleDB) addRuleToTree(rule *AccessRule) (error, string, common.PermissionType) {
	// If there is a conflicting path pattern from another rule, returns an
	// error along with the ID of the conflicting rule and the permission for
	// which the conflict occurred
	addedPermissions := make([]common.PermissionType, 0, len(rule.Permissions))
	for _, permission := range rule.Permissions {
		if err, conflictingID := ardb.addRulePermissionToTree(rule, permission); err != nil {
			for _, prevPerm := range addedPermissions {
				ardb.removeRulePermissionFromTree(rule, prevPerm)
			}
			return err, conflictingID, permission
		}
		addedPermissions = append(addedPermissions, permission)
	}
	return nil, "", ""
}

func (ardb *AccessRuleDB) removeRuleFromTree(rule *AccessRule) error {
	// Fully removes the rule from the tree, even if an error occurs
	var err error
	for _, permission := range rule.Permissions {
		if e := ardb.removeRulePermissionFromTree(rule, permission); e != nil {
			// Database was left inconsistent, should not occur.
			// Store the most recent non-nil error, but keep removing.
			err = e
		}
	}
	return err
}

func getNewerRule(id1 string, ts1 string, id2 string, ts2 string) string {
	// Returns the id with the newest timestamp. If the timestamp for one id
	// cannot be parsed, return the other id. If both cannot be parsed, return
	// the id corresponding to the timestamp which is larger lexicographically.
	// If there is a tie, return id1.
	time1, err1 := common.TimestampToTime(ts1)
	time2, err2 := common.TimestampToTime(ts2)
	if err1 != nil {
		if err2 != nil {
			if strings.Compare(ts1, ts2) == -1 {
				return id2
			}
			return id1
		}
		return id2
	}
	if time1.Before(time2) {
		return id2
	}
	return id1
}

// TODO: unexport (probably)
// This function is only required if database is left inconsistent (should not
// occur) or when loading, in case the stored rules on disk were corrupted.
//
// Discards the current access rule tree, then iterates through the rules in
// ardb.ByID and re-populates the tree.  If there are any conflicts between
// rules (that is, rules share the same path pattern and one or more of the
// same permissions), the conflicting permission is removed from the rule with
// the earlier timestamp.  When the function returns, the database should be
// fully internally consistent and without conflicting rules.
func (ardb *AccessRuleDB) RefreshTreeEnforceConsistency() {
	ardb.mutex.Lock()
	defer ardb.mutex.Unlock()
	needToSave := false
	newByID := make(map[string]*AccessRule)
	ardb.PerUser = make(map[int]*userDB)
	for id, rule := range ardb.ByID {
		err, conflictingID, conflictingPermission := ardb.addRuleToTree(rule)
		for err != nil {
			// Err must be ErrPathPatternConflict.
			// Prioritize newer rules by pruning permission from old rule until no conflicts remain.
			conflictingRule := ardb.ByID[conflictingID] // must exist
			if getNewerRule(id, rule.Timestamp, conflictingID, conflictingRule.Timestamp) == id {
				ardb.removeRulePermissionFromTree(conflictingRule, conflictingPermission) // must return nil
				if conflictingRule.removePermissionFromPermissionsList(conflictingPermission) == ErrPermissionsEmpty {
					delete(newByID, conflictingID)
				}
			} else {
				rule.removePermissionFromPermissionsList(conflictingPermission) // ignore error
			}
			err, conflictingID, conflictingPermission = ardb.addRuleToTree(rule)
			needToSave = true
		}
		if len(rule.Permissions) > 0 {
			newByID[id] = rule
		} else {
			needToSave = true
		}
	}
	ardb.ByID = newByID
	if needToSave {
		ardb.save()
	}
}

func (ardb *AccessRuleDB) load() error {
	target := ardb.dbpath()
	f, err := os.Open(target)
	if err != nil {
		return err
	}
	defer f.Close()

	accessRuleList := make([]*AccessRule, 0, 256) // pre-allocate a large array to reduce memcpys
	json.NewDecoder(f).Decode(&accessRuleList)    // TODO: handle errors

	ardb.ByID = make(map[string]*AccessRule) // clear out any existing rules in ByID
	for _, rule := range accessRuleList {
		ardb.ByID[rule.ID] = rule
	}

	ardb.RefreshTreeEnforceConsistency()

	return nil
}

func (ardb *AccessRuleDB) save() error {
	ruleList := make([]*AccessRule, 0, len(ardb.ByID))
	for _, rule := range ardb.ByID {
		ruleList = append(ruleList, rule)
	}
	b, err := json.Marshal(ruleList)
	if err != nil {
		return err
	}
	target := ardb.dbpath()
	if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
		return err
	}
	return osutil.AtomicWriteFile(target, b, 0600, 0)
}

func validatePatternOutcomeLifespanDuration(pathPattern string, outcome common.OutcomeType, lifespan common.LifespanType, duration int) (string, error) {
	if err := common.ValidatePathPattern(pathPattern); err != nil {
		return "", err
	}
	if err := common.ValidateOutcome(outcome); err != nil {
		return "", err
	}
	return common.ValidateLifespanParseDuration(lifespan, duration)
}

// TODO: unexport (probably, avoid confusion with CreateAccessRule)
// Users of accessrules should probably autofill AccessRules from JSON and
// never call this function directly.
//
// Constructs a new access rule with the given parameters as values, with the
// exception of duration.  Uses the given duration, in addition to the current
// time, to compute the expiration time for the rule, and stores that as part
// of the access rule which is returned.  If any of the given parameters are
// invalid, returns a corresponding error.
func (ardb *AccessRuleDB) PopulateNewAccessRule(user int, snap string, app string, pathPattern string, outcome common.OutcomeType, lifespan common.LifespanType, duration int, permissions []common.PermissionType) (*AccessRule, error) {
	pathPattern = common.StripTrailingSlashes(pathPattern)
	expiration, err := validatePatternOutcomeLifespanDuration(pathPattern, outcome, lifespan, duration)
	if err != nil {
		return nil, err
	}
	newPermissions := make([]common.PermissionType, len(permissions))
	copy(newPermissions, permissions)
	id, timestamp := common.NewIDAndTimestamp()
	newRule := AccessRule{
		ID:          id,
		Timestamp:   timestamp,
		User:        user,
		Snap:        snap,
		App:         app,
		PathPattern: pathPattern,
		Outcome:     outcome,
		Lifespan:    lifespan,
		Expiration:  expiration,
		Permissions: newPermissions,
	}
	return &newRule, nil
}

// Checks whether the given path with the given permission is allowed or
// denied by existing access rules for the given user, snap, and app.  If no
// access rule applies, returns ErrNoMatchingRule.
func (ardb *AccessRuleDB) IsPathAllowed(user int, snap string, app string, path string, permission common.PermissionType) (bool, error) {
	ardb.mutex.Lock()
	defer ardb.mutex.Unlock()
	needToSave := false
	pathMap := ardb.permissionDBForUserSnapAppPermission(user, snap, app, permission).PathRules
	matchingPatterns := make([]string, 0)
	currTime := time.Now()
	for pathPattern, id := range pathMap {
		matchingRule, exists := ardb.ByID[id]
		if !exists {
			// Database was left inconsistent, should not occur
			delete(pathMap, id)
			continue
		}
		if matchingRule.Lifespan == common.LifespanTimespan {
			expiration, err := time.Parse(time.RFC3339, matchingRule.Expiration)
			if err != nil {
				// Expiration is malformed, should not occur
				return false, err
			}
			if currTime.After(expiration) {
				needToSave = true
				ardb.removeRuleFromTree(matchingRule)
				delete(ardb.ByID, id)
				continue
			}
		}
		matched, err := common.PathPatternMatches(pathPattern, path)
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
	matchingRule, exists := ardb.ByID[matchingID]
	if !exists {
		// Database was left inconsistent, should not occur
		return false, ErrRuleIDNotFound
	}
	if matchingRule.Lifespan == common.LifespanSingle {
		ardb.removeRuleFromTree(matchingRule)
		delete(ardb.ByID, matchingID)
		needToSave = true
	}
	if needToSave {
		ardb.save()
	}
	switch matchingRule.Outcome {
	case "allow":
		return true, nil
	case "deny":
		return false, nil
	default:
		// Outcome should have been validated, so this should not occur
		return false, common.ErrInvalidOutcome
	}
}

func (ardb *AccessRuleDB) ruleWithIDInternal(user int, id string) (*AccessRule, error) {
	rule, exists := ardb.ByID[id]
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
func (ardb *AccessRuleDB) RuleWithID(user int, id string) (*AccessRule, error) {
	ardb.mutex.Lock()
	defer ardb.mutex.Unlock()
	return ardb.ruleWithIDInternal(user, id)
}

// Creates an access rule with the given information and adds it to the rule
// database.  If any of the given parameters are invalid, returns an error.
// Otherwise, returns the newly-created access rule, and saves the database to
// disk.
func (ardb *AccessRuleDB) CreateAccessRule(user int, snap string, app string, pathPattern string, outcome common.OutcomeType, lifespan common.LifespanType, duration int, permissions []common.PermissionType) (*AccessRule, error) {
	ardb.mutex.Lock()
	defer ardb.mutex.Unlock()
	pathPattern = common.StripTrailingSlashes(pathPattern)
	newRule, err := ardb.PopulateNewAccessRule(user, snap, app, pathPattern, outcome, lifespan, duration, permissions)
	if err != nil {
		return nil, err
	}
	if err, conflictingID, conflictingPermission := ardb.addRuleToTree(newRule); err != nil {
		return nil, fmt.Errorf("%s: ID: '%s', Permission: '%s'", err, conflictingID, conflictingPermission)
	}
	ardb.ByID[newRule.ID] = newRule
	ardb.save()
	return newRule, nil
}

// Removes the access rule with the given ID from the rules database.  If the
// rule does not apply to the given user, returns ErrUserNotAllowed.  If
// successful, saves the database to disk.
func (ardb *AccessRuleDB) DeleteAccessRule(user int, id string) (*AccessRule, error) {
	ardb.mutex.Lock()
	defer ardb.mutex.Unlock()
	rule, err := ardb.ruleWithIDInternal(user, id)
	if err != nil {
		return nil, err
	}
	err = ardb.removeRuleFromTree(rule)
	// If error occurs, rule was still fully removed from tree
	delete(ardb.ByID, id)
	ardb.save()
	return rule, err
}

// Modifies the access rule with the given ID.  The rule is modified by
// constructing a new rule based on the given parameters, and then replacing
// the old rule with the same ID with the new rule.  Any of the parameters
// which are equal to the default/unset value for their types are replaced by
// the corresponding values in the existing rule.  Even if the given new rule
// contents exactly match the existing rule contents, the timestamp of the rule
// is updated to the current time.  If there is any error while adding the
// modified rule to the database, rolls back to the previous unmodified rule,
// leaving the database unchanged.  If the database is changed, it is saved to
// disk.
func (ardb *AccessRuleDB) ModifyAccessRule(user int, id string, pathPattern string, outcome common.OutcomeType, lifespan common.LifespanType, duration int, permissions []common.PermissionType) (*AccessRule, error) {
	ardb.mutex.Lock()
	defer ardb.mutex.Unlock()
	origRule, err := ardb.ruleWithIDInternal(user, id)
	if err != nil {
		return nil, err
	}
	if pathPattern == "" {
		pathPattern = origRule.PathPattern
	}
	pathPattern = common.StripTrailingSlashes(pathPattern)
	if outcome == common.OutcomeUnset {
		outcome = origRule.Outcome
	}
	if lifespan == common.LifespanUnset {
		lifespan = origRule.Lifespan
	}
	if permissions == nil || len(permissions) == 0 {
		// Treat empty permissions list as leave permissions unchanged
		// since go has little distinction between nil and empty list.
		permissions = origRule.Permissions
	}
	if err = ardb.removeRuleFromTree(origRule); err != nil {
		// If error occurs, rule is fully removed from tree
		return nil, err
	}
	newRule, err := ardb.PopulateNewAccessRule(user, origRule.Snap, origRule.App, pathPattern, outcome, lifespan, duration, permissions)
	if err != nil {
		ardb.addRuleToTree(origRule) // ignore any new error
		// origRule was successfully removed, it should be successfully added
		return nil, err
	}
	newRule.ID = origRule.ID
	err, conflictingID, conflictingPermission := ardb.addRuleToTree(newRule)
	if err != nil {
		ardb.addRuleToTree(origRule) // ignore any new error
		// origRule was successfully removed, it should be successfully added
		return nil, fmt.Errorf("%s: ID: '%s', Permission: '%s'", err, conflictingID, conflictingPermission)
	}
	ardb.ByID[newRule.ID] = newRule
	ardb.save()
	return newRule, nil
}

// Returns all access rules which apply to the given user.
func (ardb *AccessRuleDB) Rules(user int) []*AccessRule {
	ardb.mutex.Lock()
	defer ardb.mutex.Unlock()
	rules := make([]*AccessRule, 0)
	for _, rule := range ardb.ByID {
		if rule.User == user {
			rules = append(rules, rule)
		}
	}
	return rules
}

// Returns all access rules which apply to the given user and the given snap.
func (ardb *AccessRuleDB) RulesForSnap(user int, snap string) []*AccessRule {
	ardb.mutex.Lock()
	defer ardb.mutex.Unlock()
	rules := make([]*AccessRule, 0)
	for _, rule := range ardb.ByID {
		if rule.User == user && rule.Snap == snap {
			rules = append(rules, rule)
		}
	}
	return rules
}

// Returns all access rules which apply to the given user, snap, and app.
func (ardb *AccessRuleDB) RulesForSnapApp(user int, snap string, app string) []*AccessRule {
	ardb.mutex.Lock()
	defer ardb.mutex.Unlock()
	rules := make([]*AccessRule, 0)
	for _, rule := range ardb.ByID {
		if rule.User == user && rule.Snap == snap && rule.App == app {
			rules = append(rules, rule)
		}
	}
	return rules
}
