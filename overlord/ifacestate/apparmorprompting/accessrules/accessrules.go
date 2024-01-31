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
	"github.com/snapcore/snapd/overlord/state"
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
	User        uint32                  `json:"user"`
	Snap        string                  `json:"snap"`
	App         string                  `json:"app"`
	Interface   string                  `json:"interface"`
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

func (rule *AccessRule) Expired(currentTime time.Time) (bool, error) {
	switch rule.Lifespan {
	case common.LifespanTimespan:
		expiration, err := time.Parse(time.RFC3339, rule.Expiration)
		if err != nil {
			// Expiration is malformed, should not occur
			return false, err
		}
		if currentTime.After(expiration) {
			return true, nil
		}
		return false, nil
	case common.LifespanSession:
		// TODO: return true if the user session has changed
	}
	return false, nil
}

type permissionDB struct {
	// permissionDB contains a map from path pattern to access rule ID
	PathRules map[string]string
}

type interfaceDB struct {
	// interfaceDB contains a map from permission to permissionDB for a particular interface
	PerPermission map[common.PermissionType]*permissionDB
}

type appDB struct {
	// appDB contains a map from interface to interfaceDB for a particular app
	PerInterface map[string]*interfaceDB
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
	PerUser map[uint32]*userDB
	mutex   sync.Mutex
	// Function to issue a notice for a change in a rule
	notifyRule func(userID uint32, ruleID string, options *state.AddNoticeOptions) error
}

// Creates a new access rule database, loads existing access rules from the
// path given by dbpath(), and returns the populated database.
func New(notifyRule func(userID uint32, ruleID string, options *state.AddNoticeOptions) error) (*AccessRuleDB, error) {
	ardb := &AccessRuleDB{
		ByID:       make(map[string]*AccessRule),
		PerUser:    make(map[uint32]*userDB),
		notifyRule: notifyRule,
	}
	return ardb, ardb.load()
}

func (ardb *AccessRuleDB) dbpath() string {
	return filepath.Join(dirs.SnapdStateDir(dirs.GlobalRootDir), "access-rules.json")
}

func (ardb *AccessRuleDB) permissionDBForUserSnapAppInterfacePermission(user uint32, snap string, app string, iface string, permission common.PermissionType) *permissionDB {
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
	appInterfaces := snapApps.PerApp[app]
	if appInterfaces == nil {
		appInterfaces = &appDB{
			PerInterface: make(map[string]*interfaceDB),
		}
		snapApps.PerApp[app] = appInterfaces
	}
	interfacePerms := appInterfaces.PerInterface[iface]
	if interfacePerms == nil {
		interfacePerms = &interfaceDB{
			PerPermission: make(map[common.PermissionType]*permissionDB),
		}
		appInterfaces.PerInterface[iface] = interfacePerms
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

func (ardb *AccessRuleDB) addRulePermissionToTree(rule *AccessRule, permission common.PermissionType) (error, string) {
	// If there is a conflicting path pattern from another rule, returns an
	// error along with the ID of the conflicting rule.
	permPaths := ardb.permissionDBForUserSnapAppInterfacePermission(rule.User, rule.Snap, rule.App, rule.Interface, permission)
	pathPattern := rule.PathPattern
	if id, exists := permPaths.PathRules[pathPattern]; exists {
		return ErrPathPatternConflict, id
	}
	permPaths.PathRules[pathPattern] = rule.ID
	return nil, ""
}

func (ardb *AccessRuleDB) removeRulePermissionFromTree(rule *AccessRule, permission common.PermissionType) error {
	permPaths := ardb.permissionDBForUserSnapAppInterfacePermission(rule.User, rule.Snap, rule.App, rule.Interface, permission)
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
// By default, issues a notice for each rule which is modified or deleted as a
// result of a conflict with another rule. If notifyEveryRule is true, issues
// a notice for every rule which was in the database prior to the beginning of
// the function. In either case, at most one notice is issued for each rule.
//
// Discards the current access rule tree, then iterates through the rules in
// ardb.ByID and re-populates the tree.  If there are any conflicts between
// rules (that is, rules share the same path pattern and one or more of the
// same permissions), the conflicting permission is removed from the rule with
// the earlier timestamp.  When the function returns, the database should be
// fully internally consistent and without conflicting rules.
func (ardb *AccessRuleDB) RefreshTreeEnforceConsistency(notifyEveryRule bool) {
	ardb.mutex.Lock()
	defer ardb.mutex.Unlock()
	needToSave := false
	defer func() {
		if needToSave {
			ardb.save()
		}
	}()
	modifiedUserRuleIDs := make(map[uint32]map[string]bool)
	defer func() {
		for user, ruleIDs := range modifiedUserRuleIDs {
			for ruleID := range ruleIDs {
				ardb.notifyRule(user, ruleID, nil)
			}
		}
	}()
	newByID := make(map[string]*AccessRule)
	ardb.PerUser = make(map[uint32]*userDB)
	for id, rule := range ardb.ByID {
		_, exists := modifiedUserRuleIDs[rule.User]
		if !exists {
			modifiedUserRuleIDs[rule.User] = make(map[string]bool)
		}
		if notifyEveryRule {
			modifiedUserRuleIDs[rule.User][id] = true
		}
		for {
			err, conflictingID, conflictingPermission := ardb.addRuleToTree(rule)
			if err == nil {
				break
			}
			// Err must be ErrPathPatternConflict.
			// Prioritize newer rules by pruning permission from old rule until no conflicts remain.
			conflictingRule := ardb.ByID[conflictingID] // must exist
			if getNewerRule(id, rule.Timestamp, conflictingID, conflictingRule.Timestamp) == id {
				ardb.removeRulePermissionFromTree(conflictingRule, conflictingPermission) // must return nil
				if conflictingRule.removePermissionFromPermissionsList(conflictingPermission) == ErrPermissionsEmpty {
					delete(newByID, conflictingID)
				}
				modifiedUserRuleIDs[conflictingRule.User][conflictingID] = true
			} else {
				rule.removePermissionFromPermissionsList(conflictingPermission) // ignore error
				modifiedUserRuleIDs[rule.User][id] = true
			}
			needToSave = true
		}
		if len(rule.Permissions) > 0 {
			newByID[id] = rule
		} else {
			needToSave = true
			modifiedUserRuleIDs[rule.User][id] = true
		}
	}
	ardb.ByID = newByID
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

	notifyEveryRule := true
	ardb.RefreshTreeEnforceConsistency(notifyEveryRule)

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

func validatePatternOutcomeLifespanDuration(pathPattern string, outcome common.OutcomeType, lifespan common.LifespanType, duration string) (string, error) {
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
func (ardb *AccessRuleDB) PopulateNewAccessRule(user uint32, snap string, app string, iface string, pathPattern string, outcome common.OutcomeType, lifespan common.LifespanType, duration string, permissions []common.PermissionType) (*AccessRule, error) {
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
		Interface:   iface,
		PathPattern: pathPattern,
		Outcome:     outcome,
		Lifespan:    lifespan,
		Expiration:  expiration,
		Permissions: newPermissions,
	}
	return &newRule, nil
}

// Checks whether the given path with the given permission is allowed or
// denied by existing access rules for the given user, snap, app, and interface.
// If no access rule applies, returns ErrNoMatchingRule.
func (ardb *AccessRuleDB) IsPathAllowed(user uint32, snap string, app string, iface string, path string, permission common.PermissionType) (bool, error) {
	ardb.mutex.Lock()
	defer ardb.mutex.Unlock()
	needToSave := false
	defer func() {
		if needToSave {
			ardb.save()
		}
	}()
	pathMap := ardb.permissionDBForUserSnapAppInterfacePermission(user, snap, app, iface, permission).PathRules
	matchingPatterns := make([]string, 0)
	// Make sure all rules use the same expiration timestamp, so a rule with
	// an earlier expiration cannot outlive another rule with a later one.
	currTime := time.Now()
	for pathPattern, id := range pathMap {
		matchingRule, exists := ardb.ByID[id]
		if !exists {
			// Database was left inconsistent, should not occur
			delete(pathMap, id)
			// Issue a notice for the offending rule, just in case
			ardb.notifyRule(user, id, nil)
			continue
		}
		expired, err := matchingRule.Expired(currTime)
		if err != nil {
			return false, err
		}
		if expired {
			needToSave = true
			ardb.removeRuleFromTree(matchingRule)
			delete(ardb.ByID, id)
			ardb.notifyRule(user, id, nil)
			continue
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
		ardb.notifyRule(user, matchingID, nil)
		needToSave = true
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

func (ardb *AccessRuleDB) ruleWithIDInternal(user uint32, id string) (*AccessRule, error) {
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
func (ardb *AccessRuleDB) RuleWithID(user uint32, id string) (*AccessRule, error) {
	ardb.mutex.Lock()
	defer ardb.mutex.Unlock()
	return ardb.ruleWithIDInternal(user, id)
}

// Creates an access rule with the given information and adds it to the rule
// database.  If any of the given parameters are invalid, returns an error.
// Otherwise, returns the newly-created access rule, and saves the database to
// disk.
func (ardb *AccessRuleDB) CreateAccessRule(user uint32, snap string, app string, iface string, pathPattern string, outcome common.OutcomeType, lifespan common.LifespanType, duration string, permissions []common.PermissionType) (*AccessRule, error) {
	ardb.mutex.Lock()
	defer ardb.mutex.Unlock()
	pathPattern = common.StripTrailingSlashes(pathPattern)
	newRule, err := ardb.PopulateNewAccessRule(user, snap, app, iface, pathPattern, outcome, lifespan, duration, permissions)
	if err != nil {
		return nil, err
	}
	if err, conflictingID, conflictingPermission := ardb.addRuleToTree(newRule); err != nil {
		return nil, fmt.Errorf("%s: ID: '%s', Permission: '%s'", err, conflictingID, conflictingPermission)
	}
	ardb.ByID[newRule.ID] = newRule
	ardb.save()
	ardb.notifyRule(user, newRule.ID, nil)
	return newRule, nil
}

// Removes the access rule with the given ID from the rules database.  If the
// rule does not apply to the given user, returns ErrUserNotAllowed.  If
// successful, saves the database to disk.
func (ardb *AccessRuleDB) DeleteAccessRule(user uint32, id string) (*AccessRule, error) {
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
	ardb.notifyRule(user, id, nil)
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
func (ardb *AccessRuleDB) ModifyAccessRule(user uint32, id string, pathPattern string, outcome common.OutcomeType, lifespan common.LifespanType, duration string, permissions []common.PermissionType) (*AccessRule, error) {
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
		ardb.notifyRule(user, id, nil)
		return nil, err
	}
	newRule, err := ardb.PopulateNewAccessRule(user, origRule.Snap, origRule.App, origRule.Interface, pathPattern, outcome, lifespan, duration, permissions)
	if err != nil {
		ardb.addRuleToTree(origRule) // ignore any new error
		// origRule was successfully removed before, so it should now be able
		// to be successfully re-added without error, and all is unchanged.
		return nil, err
	}
	newRule.ID = origRule.ID
	err, conflictingID, conflictingPermission := ardb.addRuleToTree(newRule)
	if err != nil {
		ardb.addRuleToTree(origRule) // ignore any new error
		// origRule was successfully removed before, so it should now be able
		// to be successfully re-added without error, and all is unchanged.
		return nil, fmt.Errorf("%s: ID: '%s', Permission: '%s'", err, conflictingID, conflictingPermission)
	}
	ardb.ByID[newRule.ID] = newRule
	ardb.save()
	ardb.notifyRule(user, id, nil)
	return newRule, nil
}

// Returns all access rules which apply to the given user.
func (ardb *AccessRuleDB) Rules(user uint32) []*AccessRule {
	ardb.mutex.Lock()
	defer ardb.mutex.Unlock()
	ruleFilter := func(rule *AccessRule) bool {
		return rule.User == user
	}
	return ardb.rulesInternal(ruleFilter)
}

func (ardb *AccessRuleDB) rulesInternal(ruleFilter func(rule *AccessRule) bool) []*AccessRule {
	rules := make([]*AccessRule, 0)
	currTime := time.Now()
	needToSave := false
	defer func() {
		if needToSave {
			ardb.save()
		}
	}()
	for id, rule := range ardb.ByID {
		expired, err := rule.Expired(currTime)
		if err != nil {
			// Issue with expiration, this should not occur
		}
		if expired {
			needToSave = true
			ardb.removeRuleFromTree(rule)
			delete(ardb.ByID, id)
			ardb.notifyRule(rule.User, id, nil)
			continue
		}
		if ruleFilter(rule) {
			rules = append(rules, rule)
		}
	}
	return rules
}

// Returns all access rules which apply to the given user and the given snap.
func (ardb *AccessRuleDB) RulesForSnap(user uint32, snap string) []*AccessRule {
	ardb.mutex.Lock()
	defer ardb.mutex.Unlock()
	ruleFilter := func(rule *AccessRule) bool {
		return rule.User == user && rule.Snap == snap
	}
	return ardb.rulesInternal(ruleFilter)
}

// Returns all access rules which apply to the given user, snap, and app.
func (ardb *AccessRuleDB) RulesForSnapApp(user uint32, snap string, app string) []*AccessRule {
	ardb.mutex.Lock()
	defer ardb.mutex.Unlock()
	ruleFilter := func(rule *AccessRule) bool {
		return rule.User == user && rule.Snap == snap && rule.App == app
	}
	return ardb.rulesInternal(ruleFilter)
}

// Returns all access rules which apply to the given user, snap, and interface.
func (ardb *AccessRuleDB) RulesForSnapInterface(user uint32, snap string, iface string) []*AccessRule {
	ardb.mutex.Lock()
	defer ardb.mutex.Unlock()
	ruleFilter := func(rule *AccessRule) bool {
		return rule.User == user && rule.Snap == snap && rule.Interface == iface
	}
	return ardb.rulesInternal(ruleFilter)
}

// Returns all access rules which apply to the given user, snap, app, and interface.
func (ardb *AccessRuleDB) RulesForSnapAppInterface(user uint32, snap string, app string, iface string) []*AccessRule {
	ardb.mutex.Lock()
	defer ardb.mutex.Unlock()
	ruleFilter := func(rule *AccessRule) bool {
		return rule.User == user && rule.Snap == snap && rule.App == app && rule.Interface == iface
	}
	return ardb.rulesInternal(ruleFilter)
}
