package accessrules

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	doublestar "github.com/bmatcuk/doublestar/v4"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
)

var ErrPathPatternMissingFromTree = errors.New("path pattern was not found in the access rule tree")
var ErrRuleIDMismatch = errors.New("the access rule ID in the tree does not match the expected rule ID")
var ErrRuleIDNotFound = errors.New("access rule ID is not found")
var ErrPathPatternConflict = errors.New("a rule with the same path pattern already exists in the tree")
var ErrPermissionNotFound = errors.New("permission not found in the permissions list for the given rule")
var ErrPermissionsEmpty = errors.New("all permissions have been removed from the permissions list of the given rule")
var ErrNoPatterns = errors.New("no patterns given, cannot establish precedence")
var ErrNoMatchingRule = errors.New("no access rules match the given path")
var ErrInvalidPathPattern = errors.New("the given path pattern is not allowed")
var ErrInvalidOutcome = errors.New(`invalid rule outcome; must be "allow" or "deny"`)
var ErrInvalidLifespan = errors.New("invalid lifespan")
var ErrInvalidDuration = errors.New("invalid duration for accompanying lifespan")
var ErrUserNotAllowed = errors.New("the given user is not allowed to access the access rule with the given ID")

type OutcomeType string

const (
	OutcomeUnset OutcomeType = ""
	OutcomeAllow OutcomeType = "allow"
	OutcomeDeny  OutcomeType = "deny"
)

type LifespanType string

const (
	LifespanUnset    LifespanType = ""
	LifespanForever  LifespanType = "forever"
	LifespanSession  LifespanType = "session"
	LifespanSingle   LifespanType = "single"
	LifespanTimespan LifespanType = "timespan"
)

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
	ID          string           `json:"id"`
	Timestamp   string           `json:"timestamp"`
	User        uint32           `json:"user"`
	Snap        string           `json:"snap"`
	App         string           `json:"app"`
	PathPattern string           `json:"path-pattern"`
	Outcome     OutcomeType      `json:"outcome"`
	Lifespan    LifespanType     `json:"lifespan"`
	Expiration  string           `json:"expiration"`
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
	ByID    map[string]*AccessRule
	PerUser map[uint32]*userDB
	mutex   sync.Mutex
}

func New() (*AccessRuleDB, error) {
	ardb := &AccessRuleDB{
		ByID:    make(map[string]*AccessRule),
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
	permPaths.PathRules[pathPattern] = rule.ID
	return nil, ""
}

func (ardb *AccessRuleDB) removeRulePermissionFromTree(rule *AccessRule, permission PermissionType) error {
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

func (ardb *AccessRuleDB) addRuleToTree(rule *AccessRule) (error, string, PermissionType) {
	// If there is a conflicting path pattern from another rule, returns an
	// error along with the ID of the conflicting rule and the permission for
	// which the conflict occurred
	addedPermissions := make([]PermissionType, 0, len(rule.Permissions))
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
	needToSave := false
	newByID := make(map[string]*AccessRule)
	ardb.PerUser = make(map[uint32]*userDB)
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

func CurrentTimestamp() string {
	return time.Now().Format(time.RFC3339Nano)
}

var allowablePathPatternRegexp = regexp.MustCompile(`^(/|(/[^/*{}]+)*(/\*|(/\*\*)?(/\*\.[^/*{}]+)?)?)$`)

func ValidatePathPattern(pattern string) error {
	if !allowablePathPatternRegexp.MatchString(pattern) {
		return ErrInvalidPathPattern
	}
	return nil
}

func ValidateOutcome(outcome OutcomeType) error {
	switch outcome {
	case OutcomeAllow, OutcomeDeny:
		return nil
	default:
		return ErrInvalidOutcome
	}
}

// ValidateLifespanParseDuration checks that the given lifespan is valid and
// that the given duration is valid for that lifespan.  If the lifespan is
// LifespanTimespan, the duration must be parsable via time.ParseDuration and
// yield a positive duration. Otherwise, it must be the empty string.
// Returns an error if any of the above are invalid, otherwise computes the
// expiration time of the rule based on the current time and the given duration
// and returns it.
func ValidateLifespanParseDuration(lifespan LifespanType, duration string) (string, error) {
	expirationString := ""
	switch lifespan {
	case LifespanForever, LifespanSession, LifespanSingle:
		if duration != "" {
			return "", ErrInvalidDuration
		}
	case LifespanTimespan:
		parsedDuration, err := time.ParseDuration(duration)
		if err != nil {
			return "", fmt.Errorf("%w: %w", ErrInvalidDuration, err)
		}
		if parsedDuration <= time.Duration(0) {
			return "", ErrInvalidDuration
		}
		expirationString = time.Now().Add(parsedDuration).Format(time.RFC3339)
	}
	return expirationString, nil
}

func validatePatternOutcomeLifespanDuration(pathPattern string, outcome OutcomeType, lifespan LifespanType, duration string) (string, error) {
	if err := ValidatePathPattern(pathPattern); err != nil {
		return "", err
	}
	if err := ValidateOutcome(outcome); err != nil {
		return "", err
	}
	return ValidateLifespanParseDuration(lifespan, duration)
}

// TODO: unexport (probably, avoid confusion with CreateAccessRule)
// Users of accessrules should probably autofill AccessRules from JSON and
// never call this function directly.
func (ardb *AccessRuleDB) PopulateNewAccessRule(user uint32, snap string, app string, pathPattern string, outcome OutcomeType, lifespan LifespanType, duration string, permissions []PermissionType) (*AccessRule, error) {
	expiration, err := validatePatternOutcomeLifespanDuration(pathPattern, outcome, lifespan, duration)
	if err != nil {
		return nil, err
	}
	newPermissions := make([]PermissionType, len(permissions))
	copy(newPermissions, permissions)
	timestamp := CurrentTimestamp()
	newRule := AccessRule{
		ID:          timestamp,
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

func GetHighestPrecedencePattern(patterns []string) (string, error) {
	if len(patterns) == 0 {
		return "", ErrNoPatterns
	}
	// First find rules with extensions, if any exist -- these are most specific
	// longer file extensions are more specific than longer paths, so
	// /foo/bar/**/*.tar.gz is more specific than /foo/bar/baz/**/*.gz
	extensions := make(map[string][]string)
	for _, pattern := range patterns {
		if strings.Index(pattern, "*") == -1 {
			// Exact match, has highest precedence
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
	// Either patterns all have same extension, or patterns have no extension
	// (but possibly trailing /* or /**).
	// Prioritize longest patterns (excluding /** or /*).
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
	// longestCleanedPatterns is all the most-specific patterns that match.
	// Now, want to prioritize .../foo over .../foo/* over .../foo/**, so take shortest of these
	shortestPattern := longestCleanedPatterns[0]
	for _, pattern := range longestCleanedPatterns {
		if len(pattern) < len(shortestPattern) {
			shortestPattern = pattern
		}
	}
	return shortestPattern, nil
}

func (ardb *AccessRuleDB) IsPathAllowed(user uint32, snap string, app string, path string, permission PermissionType) (bool, error) {
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
		if matchingRule.Lifespan == LifespanTimespan {
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
		matched, err := doublestar.Match(pathPattern, path)
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
	highestPrecedencePattern, err := GetHighestPrecedencePattern(matchingPatterns)
	if err != nil {
		return false, err
	}
	matchingID := pathMap[highestPrecedencePattern]
	matchingRule, exists := ardb.ByID[matchingID]
	if !exists {
		// Database was left inconsistent, should not occur
		return false, ErrRuleIDNotFound
	}
	if matchingRule.Lifespan == LifespanSingle {
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
		return false, ErrInvalidOutcome
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

func (ardb *AccessRuleDB) RuleWithID(user uint32, id string) (*AccessRule, error) {
	ardb.mutex.Lock()
	defer ardb.mutex.Unlock()
	return ardb.ruleWithIDInternal(user, id)
}

func (ardb *AccessRuleDB) CreateAccessRule(user uint32, snap string, app string, pathPattern string, outcome OutcomeType, lifespan LifespanType, duration string, permissions []PermissionType) (*AccessRule, error) {
	ardb.mutex.Lock()
	defer ardb.mutex.Unlock()
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
	return rule, err
}

func (ardb *AccessRuleDB) ModifyAccessRule(user uint32, id string, pathPattern string, outcome OutcomeType, lifespan LifespanType, duration string, permissions []PermissionType) (*AccessRule, error) {
	ardb.mutex.Lock()
	defer ardb.mutex.Unlock()
	origRule, err := ardb.ruleWithIDInternal(user, id)
	if err != nil {
		return nil, err
	}
	if pathPattern == "" {
		pathPattern = origRule.PathPattern
	}
	if outcome == OutcomeUnset {
		outcome = origRule.Outcome
	}
	if lifespan == LifespanUnset {
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

func (ardb *AccessRuleDB) Rules(user uint32) []*AccessRule {
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

func (ardb *AccessRuleDB) RulesForSnap(user uint32, snap string) []*AccessRule {
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

func (ardb *AccessRuleDB) RulesForSnapApp(user uint32, snap string, app string) []*AccessRule {
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
