package accessrules

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
)

var ErrPathPatternMissingFromTree = errors.New("path pattern was not found in the access rule tree")
var ErrRuleIdMismatch = errors.New("the access rule ID in the tree does not match the expected rule ID")
var ErrRuleIdNotFound = errors.New("access rule ID is not found")
var ErrPathPatternConflict = errors.New("a rule with the same path pattern already exists in the tree")
var ErrPermissionNotFound = errors.New("permission not found in the permissions list for the given rule")
var ErrPermissionsEmpty = errors.New("all permissions have been removed from the permissions list of the given rule")
var ErrNoPatterns = errors.New("no patterns given, cannot establish precedence")
var ErrNoMatchingRule = errors.New("no access rules match the given path")
var ErrInvalidAction = errors.New(`invalid rule action; must be "permit" or "deny"`)
var ErrUserNotAllowed = errors.New("the given user is not allowed to access the access rule with the given ID")

type LifespanType string

const (
	LifespanForever  LifespanType = "forever"
	LifespanSession  LifespanType = "session"
	LifespanSingle   LifespanType = "single"
	LifespanTimespan LifespanType = "timespan"
)

type Lifespan struct {
	Type     LifespanType `json:"type"`
	Duration uint32       `json:"duration"`
}

type PermissionType string

const (
	PermissionExecute             PermissionType = "execute"
	PermissionWrite               PermissionType = "write"
	PermissionRead                PermissionType = "read"
	PermissionAppend              PermissionType = "append"
	PermissionCreate              PermissionType = "create"
	PermissionDelete              PermissionType = "delete"
	PermissionOpen                PermissionType = "open"
	PermissionRename              PermissionType = "rename"
	PermissionSetAttr             PermissionType = "set-attr"
	PermissionGetAttr             PermissionType = "get-attr"
	PermissionSetCred             PermissionType = "set-cred"
	PermissionGetCred             PermissionType = "get-cred"
	PermissionChangeMode          PermissionType = "change-mode"
	PermissionChangeOwner         PermissionType = "change-owner"
	PermissionChangeGroup         PermissionType = "change-group"
	PermissionLock                PermissionType = "lock"
	PermissionExecuteMap          PermissionType = "execute-map"
	PermissionLink                PermissionType = "link"
	PermissionChangeProfile       PermissionType = "change-profile"
	PermissionChangeProfileOnExec PermissionType = "change-profile-on-exec"
)

type AccessRule struct {
	Id          string           `json:"id"`
	Timestamp   string           `json:"timestamp"`
	User        uint32           `json:"user"`
	Snap        string           `json:"snap"`
	App         string           `json:"app"`
	PathPattern string           `json:"path-pattern"`
	Action      string           `json:"action"`
	Lifespan    Lifespan         `json:"lifespan"`
	Permissions []PermissionType `json:"permissions"`
}

func (rule *AccessRule) removePermissionFromPermissionsList(permission PermissionType) error {
	if len(rule.Permissions) == 0 {
		return ErrPermissionsEmpty
	}
	for i, perm := range rule.Permissions {
		if perm == permission {
			rule.Permissions = append(rule.Permissions[:i], rule.Permissions[i+1:]...)
			if len(rule.Permissions) == 0 {
				return ErrPermissionsEmpty
			}
			return nil
		}
	}
	return ErrPermissionNotFound
}

func PermissionsListContains(list []PermissionType, permission PermissionType) bool {
	for _, perm := range list {
		if perm == permission {
			return true
		}
	}
	return false
}

type permissionDB struct {
	// permissionDB contains a map from path pattern to access rule ID
	PathRules map[string]string
}

type appDB struct {
	// appDB contains a map from permission to permissionDB for a particular app
	PerPermission map[PermissionType]*permissionDB
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
	ById    map[string]*AccessRule
	PerUser map[uint32]*userDB
}

func New() (*AccessRuleDB, error) {
	ardb := &AccessRuleDB{
		ById:    make(map[string]*AccessRule),
		PerUser: make(map[uint32]*userDB),
	}
	return ardb, ardb.load()
}

func (ardb *AccessRuleDB) dbpath() string {
	return filepath.Join(dirs.SnapdStateDir(dirs.GlobalRootDir), "access-rules.json")
}

func (ardb *AccessRuleDB) permissionDBForUserSnapAppPermission(user uint32, snap string, app string, permission PermissionType) *permissionDB {
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
			PerPermission: make(map[PermissionType]*permissionDB),
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

func (ardb *AccessRuleDB) addRulePermissionToTree(rule *AccessRule, permission PermissionType) (error, string) {
	// If there is a conflicting path pattern from another rule, returns an
	// error along with the ID of the conflicting rule.
	permPaths := ardb.permissionDBForUserSnapAppPermission(rule.User, rule.Snap, rule.App, permission)
	pathPattern := rule.PathPattern
	if id, exists := permPaths.PathRules[pathPattern]; exists {
		return ErrPathPatternConflict, id
	}
	permPaths.PathRules[pathPattern] = rule.Id
	return nil, ""
}

func (ardb *AccessRuleDB) removeRulePermissionFromTree(rule *AccessRule, permission PermissionType) error {
	permPaths := ardb.permissionDBForUserSnapAppPermission(rule.User, rule.Snap, rule.App, permission)
	pathPattern := rule.PathPattern
	id, exists := permPaths.PathRules[pathPattern]
	if !exists {
		return ErrPathPatternMissingFromTree
	}
	if id != rule.Id {
		return ErrRuleIdMismatch
	}
	delete(permPaths.PathRules, pathPattern)
	return nil
}

func (ardb *AccessRuleDB) addRuleToTree(rule *AccessRule) (error, string, PermissionType) {
	// If there is a conflicting path pattern from another rule, returns an
	// error along with the ID of the conflicting rule and the permission for
	// which the conflict occurred
	addedPermissions := make([]PermissionType, 0, len(rule.Permissions))
	for _, permission := range rule.Permissions {
		if err, conflictingId := ardb.addRulePermissionToTree(rule, permission); err != nil {
			for _, prevPerm := range addedPermissions {
				ardb.removeRulePermissionFromTree(rule, prevPerm)
			}
			return err, conflictingId, permission
		}
		addedPermissions = append(addedPermissions, permission)
	}
	return nil, "", ""
}

func (ardb *AccessRuleDB) removeRuleFromTree(rule *AccessRule) error {
	// fully removes the rule from the tree, even if an error occurs
	var err error
	for _, permission := range rule.Permissions {
		if e := ardb.removeRulePermissionFromTree(rule, permission); e != nil {
			// store the most recent non-nil error, but keep removing
			err = e
		}
	}
	return err
}

func getNewerRule(id1 string, ts1 string, id2 string, ts2 string) string {
	// returns the id with the newest timestamp. If the timestamp for one id
	// cannot be parsed, return the other id. If both cannot be parsed, return
	// the id corresponding to the timestamp which is larger lexicographically.
	// If there is a tie, return id1.
	time1, err1 := time.Parse(time.RFC3339Nano, ts1)
	time2, err2 := time.Parse(time.RFC3339Nano, ts2)
	if err1 != nil {
		if err2 != nil {
			if strings.Compare(ts1, ts2) == -1 {
				return id2
			}
			return id1
		}
		return id2
	}
	if time1.Compare(time2) == -1 {
		return id2
	}
	return id1
}

// TODO: unexport (probably)
func (ardb *AccessRuleDB) RefreshTreeEnforceConsistency() {
	newById := make(map[string]*AccessRule)
	ardb.PerUser = make(map[uint32]*userDB)
	for id, rule := range ardb.ById {
		err, conflictingId, conflictingPermission := ardb.addRuleToTree(rule)
		for err != nil {
			// err must be ErrPathPatternConflict
			// prioritize newer rules by pruning permission from old rule until no conflicts remain
			conflictingRule := ardb.ById[conflictingId] // must exist
			if getNewerRule(id, rule.Timestamp, conflictingId, conflictingRule.Timestamp) == id {
				ardb.removeRulePermissionFromTree(conflictingRule, conflictingPermission) // must return nil
				if conflictingRule.removePermissionFromPermissionsList(conflictingPermission) == ErrPermissionsEmpty {
					delete(newById, conflictingId)
				}
			} else {
				rule.removePermissionFromPermissionsList(conflictingPermission) // ignore error
			}
			err, conflictingId, conflictingPermission = ardb.addRuleToTree(rule)
		}
		if len(rule.Permissions) > 0 {
			newById[id] = rule
		}
	}
	ardb.ById = newById
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

	ardb.ById = make(map[string]*AccessRule) // clear out any existing rules in ById
	for _, rule := range accessRuleList {
		ardb.ById[rule.Id] = rule
	}

	ardb.RefreshTreeEnforceConsistency()

	return nil
}

func (ardb *AccessRuleDB) save() error {
	ruleList := make([]*AccessRule, 0, len(ardb.ById))
	for _, rule := range ardb.ById {
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

func CurrentTimestamp() string {
	return time.Now().Format(time.RFC3339Nano)
}

// TODO: unexport (probably, avoid confusion with CreateAccessRule)
// Users of accessrules should probably autofill AccessRules from JSON and
// never call this function directly.
func (ardb *AccessRuleDB) PopulateNewAccessRule(user uint32, snap string, app string, pathPattern string, action string, lifespan Lifespan, permissions []PermissionType) *AccessRule {
	timestamp := CurrentTimestamp()
	newRule := AccessRule{
		Id:          timestamp,
		Timestamp:   timestamp,
		User:        user,
		Snap:        snap,
		App:         app,
		PathPattern: pathPattern,
		Action:      action,
		Lifespan:    lifespan,
		Permissions: permissions,
	}
	return &newRule
}

var allowablePathPatternRegexp = regexp.MustCompile(`^(/|(/[^/*{}]+)*(/\*|(/\*\*)?(/\*\.[^/*{}]+)?)?)$`)

func PathPatternAllowed(pattern string) bool {
	return allowablePathPatternRegexp.MatchString(pattern)
}

func GetHighestPrecedencePattern(patterns []string) (string, error) {
	if len(patterns) == 0 {
		return "", ErrNoPatterns
	}
	// first find rules with extensions, if any exist -- these are most specific
	// longer file extensions are more specific than longer paths, so
	// /foo/bar/**/*.tar.gz is more specific than /foo/bar/baz/**/*.gz
	extensions := make(map[string][]string)
	for _, pattern := range patterns {
		if strings.Index(pattern, "*") == -1 {
			// exact match, has highest precedence
			return pattern, nil
		}
		segments := strings.Split(pattern, "/")
		finalSegment := segments[len(segments)-1]
		extension, exists := strings.CutPrefix(finalSegment, "*.")
		if !exists {
			continue
		}
		extensions[extension] = append(extensions[extension], pattern)
	}
	longestExtension := ""
	for extension, extPatterns := range extensions {
		if len(extension) > len(longestExtension) {
			longestExtension = extension
			patterns = extPatterns
		}
	}
	// either patterns all have same extension, or patterns have no extension (but possibly trailing /* or /**)
	// prioritize longest patterns (excluding /** or /*)
	longestCleanedLength := 0
	longestCleanedPatterns := make([]string, 0)
	for _, pattern := range patterns {
		cleanedPattern := strings.ReplaceAll(pattern, "/**", "")
		cleanedPattern = strings.ReplaceAll(cleanedPattern, "/*", "")
		length := len(cleanedPattern)
		if length < longestCleanedLength {
			continue
		}
		if length > longestCleanedLength {
			longestCleanedLength = length
			longestCleanedPatterns = longestCleanedPatterns[:0] // clear but preserve allocated memory
		}
		longestCleanedPatterns = append(longestCleanedPatterns, pattern)
	}
	// longestCleanedPatterns is all the most-specific patterns that match
	// Now, want to prioritize .../foo over .../foo/* over .../foo/**, so take shortest of these
	shortestPattern := longestCleanedPatterns[0]
	for _, pattern := range longestCleanedPatterns {
		if len(pattern) < len(shortestPattern) {
			shortestPattern = pattern
		}
	}
	return shortestPattern, nil
}

func (ardb *AccessRuleDB) IsPathPermitted(user uint32, snap string, app string, path string, permission PermissionType) (bool, error) {
	pathMap := ardb.permissionDBForUserSnapAppPermission(user, snap, app, permission).PathRules
	matchingPatterns := make([]string, 0)
	for pathPattern := range pathMap {
		matched, err := filepath.Match(pathPattern, path)
		if err != nil {
			// only possible error is ErrBadPattern, which should not occur
			return false, err
		}
		if matched {
			matchingPatterns = append(matchingPatterns, pathPattern)
		}
	}
	if len(matchingPatterns) == 0 {
		return false, ErrNoMatchingRule
	}
	highestPrecedencePattern, err := GetHighestPrecedencePattern(matchingPatterns)
	if err != nil {
		return false, err
	}
	matchingId := pathMap[highestPrecedencePattern]
	matchingRule, exists := ardb.ById[matchingId]
	if !exists {
		return false, ErrRuleIdNotFound
	}
	switch matchingRule.Action {
	case "permit":
		return true, nil
	case "deny":
		return false, nil
	default:
		return false, ErrInvalidAction
	}
}

func (ardb *AccessRuleDB) RuleWithId(user uint32, id string) (*AccessRule, error) {
	rule, exists := ardb.ById[id]
	if !exists {
		return nil, ErrRuleIdNotFound
	}
	if rule.User != user {
		return nil, ErrUserNotAllowed
	}
	return rule, nil
}

func (ardb *AccessRuleDB) CreateAccessRule(user uint32, snap string, app string, pathPattern string, action string, lifespan Lifespan, permissions []PermissionType) (*AccessRule, error) {
	newRule := ardb.PopulateNewAccessRule(user, snap, app, pathPattern, action, lifespan, permissions)
	if err, _, _ := ardb.addRuleToTree(newRule); err != nil {
		// TODO: could return the conflicting rule ID and permission
		return nil, err
	}
	ardb.ById[newRule.Id] = newRule
	return newRule, nil
}

func (ardb *AccessRuleDB) DeleteAccessRule(user uint32, id string) (*AccessRule, error) {
	rule, err := ardb.RuleWithId(user, id)
	if err != nil {
		return nil, err
	}
	err = ardb.removeRuleFromTree(rule)
	// if error occurs, rule was still fully removed from tree
	delete(ardb.ById, id)
	return rule, err
}

func (ardb *AccessRuleDB) ModifyAccessRule(user uint32, id string, pathPattern string, action string, lifespan *Lifespan, permissions []PermissionType) (*AccessRule, error) {
	rule, err := ardb.RuleWithId(user, id)
	if err != nil {
		return nil, err
	}
	if permissions == nil || len(permissions) == 0 {
		// treat empty permissions list as leave permissions unchanged
		// since go has little distinction between nil and empty list
		permissions = rule.Permissions
	}
	if pathPattern != "" && pathPattern != rule.PathPattern {
		// remove and re-add all permissions, since path must be changed
		if err = ardb.removeRuleFromTree(rule); err != nil {
			// if error occurs, rule is fully removed from tree
			return nil, err
		}
		rule.Permissions = rule.Permissions[:0]
		rule.PathPattern = pathPattern
	}
	preservedPermissions := make([]PermissionType, 0, len(rule.Permissions))
	for _, permission := range rule.Permissions {
		// remove permissions which are no longer included
		if PermissionsListContains(permissions, permission) {
			preservedPermissions = append(preservedPermissions, permission)
		} else {
			if err = ardb.removeRulePermissionFromTree(rule, permission); err != nil {
				// if error occurs, rule and tree are not synchronized
				// TODO: decide whether to remove rule entirely or revert to previous (broken) state
				return nil, err
			}
		}
	}
	rule.Permissions = preservedPermissions
	for _, permission := range permissions {
		// add new permissions which were not previously included
		if !PermissionsListContains(rule.Permissions, permission) {
			if addingErr, _ := ardb.addRulePermissionToTree(rule, permission); err == nil {
				// if error occurs, rule and tree are synchronized
				err = addingErr
			} else {
				rule.Permissions = append(rule.Permissions, permission)
			}
		}
	}
	if action != "" {
		rule.Action = action
	}
	if lifespan != nil {
		rule.Lifespan = *lifespan
	}
	rule.Timestamp = CurrentTimestamp()
	return rule, err
}

func (ardb *AccessRuleDB) Rules(user uint32) []*AccessRule {
	rules := make([]*AccessRule, 0)
	for _, rule := range ardb.ById {
		if rule.User == user {
			rules = append(rules, rule)
		}
	}
	return rules
}

func (ardb *AccessRuleDB) RulesForSnap(user uint32, snap string) []*AccessRule {
	rules := make([]*AccessRule, 0)
	for _, rule := range ardb.ById {
		if rule.User == user && rule.Snap == snap {
			rules = append(rules, rule)
		}
	}
	return rules
}

func (ardb *AccessRuleDB) RulesForSnapApp(user uint32, snap string, app string) []*AccessRule {
	rules := make([]*AccessRule, 0)
	for _, rule := range ardb.ById {
		if rule.User == user && rule.Snap == snap && rule.App == app {
			rules = append(rules, rule)
		}
	}
	return rules
}
