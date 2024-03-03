package common

import (
	"encoding/base32"
	"encoding/binary"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	doublestar "github.com/bmatcuk/doublestar/v4"

	"github.com/snapcore/snapd/sandbox/apparmor/notify"
	"github.com/snapcore/snapd/strutil"
)

var (
	ErrInvalidSnapLabel           = errors.New("the given label cannot be converted to snap")
	ErrInvalidOutcome             = errors.New(`invalid outcome; must be "allow" or "deny"`)
	ErrInvalidLifespan            = errors.New("invalid lifespan")
	ErrInvalidDurationForLifespan = fmt.Errorf(`invalid duration: duration must be empty unless lifespan is "%v"`, LifespanTimespan)
	ErrInvalidDurationEmpty       = fmt.Errorf(`invalid duration: duration must be specified if lifespan is "%v"`, LifespanTimespan)
	ErrInvalidDurationParseError  = errors.New("invalid duration: error parsing duration string")
	ErrInvalidDurationNegative    = errors.New("invalid duration: duration must be greater than zero")
	ErrNoPatterns                 = errors.New("no patterns given, cannot establish precedence")
	ErrPermissionNotInList        = errors.New("permission not found in permissions list")
	ErrPermissionsListEmpty       = errors.New("permissions list empty")
	ErrUnrecognizedFilePermission = errors.New("file permissions mask contains unrecognized permission")
)

type Constraints struct {
	PathPattern string   `json:"path-pattern"`
	Permissions []string `json:"permissions"`
}

func (constraints *Constraints) ValidateForInterface(iface string) error {
	switch iface {
	case "home", "camera":
		if err := ValidatePathPattern(constraints.PathPattern); err != nil {
			return err
		}
	default:
		return fmt.Errorf("constraints incompatible with the given interface: %s", iface)
	}
	permissions, err := AbstractPermissionsFromList(iface, constraints.Permissions)
	if err != nil {
		return err
	}
	constraints.Permissions = permissions
	return nil
}

func (constraints *Constraints) Match(path string) (bool, error) {
	return PathPatternMatches(constraints.PathPattern, path)
}

// Removes the given permission from the permissions associated with the
// constraints. Assumes that the permission occurs at most once in the list.
// If the permission does not exist in the list, returns ErrPermissionNotInList.
func (constraints *Constraints) RemovePermission(permission string) error {
	origLen := len(constraints.Permissions)
	i := 0
	for i < len(constraints.Permissions) {
		perm := constraints.Permissions[i]
		if perm != permission {
			i++
			continue
		}
		copy(constraints.Permissions[i:], constraints.Permissions[i+1:])
		constraints.Permissions = constraints.Permissions[:len(constraints.Permissions)-1]
	}
	if origLen == len(constraints.Permissions) {
		return ErrPermissionNotInList
	}
	return nil
}

func (constraints *Constraints) ContainPermissions(permissions []string) bool {
	for _, perm := range permissions {
		if !strutil.ListContains(constraints.Permissions, perm) {
			return false
		}
	}
	return true
}

type OutcomeType string

const (
	OutcomeUnset OutcomeType = ""
	OutcomeAllow OutcomeType = "allow"
	OutcomeDeny  OutcomeType = "deny"
)

func (outcome OutcomeType) AsBool() (bool, error) {
	switch outcome {
	case OutcomeAllow:
		return true, nil
	case OutcomeDeny:
		return false, nil
	default:
		return false, ErrInvalidOutcome
	}
}

type LifespanType string

const (
	LifespanUnset    LifespanType = ""
	LifespanForever  LifespanType = "forever"
	LifespanSession  LifespanType = "session"
	LifespanSingle   LifespanType = "single"
	LifespanTimespan LifespanType = "timespan"
)

// Converts the given timestamp string to a time.Time in Local time.
// The timestamp string is expected to be of the format time.RFC3999Nano.
// If it cannot be parsed as such, returns an error.
func TimestampToTime(timestamp string) (time.Time, error) {
	t, err := time.Parse(time.RFC3339Nano, timestamp)
	if err != nil {
		return t, err
	}
	return t.Local(), nil
}

// Returns the current time as a string in time.RFC3999Nano format.
func CurrentTimestamp() string {
	return time.Now().Format(time.RFC3339Nano)
}

// Returns a new unique ID.
// The ID is the current unix time in nanoseconds encoded as base32.
func NewID() string {
	nowUnix := uint64(time.Now().UnixNano())
	nowBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(nowBytes, nowUnix)
	id := base32.StdEncoding.EncodeToString(nowBytes)
	return id
}

// Returns a new unique ID and corresponding timestamp.
// The ID is the current unix time in nanoseconds encoded as a string in base32.
// The timestamp is the same time, encoded as a string in time.RFC3999Nano.
func NewIDAndTimestamp() (id string, timestamp string) {
	now := time.Now()
	nowUnix := uint64(now.UnixNano())
	nowBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(nowBytes, nowUnix)
	id = base32.StdEncoding.EncodeToString(nowBytes)
	timestamp = now.Format(time.RFC3339Nano)
	return id, timestamp
}

// Extracts the snap name from the given label. If the label is not of the form
// 'snap.<snap>.<app>', returns an error, and returns the label as the snap.
func LabelToSnap(label string) (string, error) {
	components := strings.Split(label, ".")
	if len(components) != 3 || components[0] != "snap" {
		return label, ErrInvalidSnapLabel
	}
	snap := components[1]
	return snap, nil
}

var (
	// If kernel request contains multiple interfaces, one must take priority.
	// Lower value is higher priority, and entries should be in priority order.
	interfacePriorities = map[string]int{
		"home":   0,
		"camera": 1,
	}

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
			"read":    notify.AA_MAY_READ,
			"write":   notify.AA_MAY_WRITE | notify.AA_MAY_APPEND | notify.AA_MAY_CREATE | notify.AA_MAY_DELETE | notify.AA_MAY_RENAME | notify.AA_MAY_CHMOD | notify.AA_MAY_LOCK | notify.AA_MAY_LINK,
			"execute": notify.AA_MAY_EXEC | notify.AA_EXEC_MMAP,
		},
		"camera": {
			"access": notify.AA_MAY_WRITE | notify.AA_MAY_READ | notify.AA_MAY_APPEND,
		},
	}
)

// Select the interface with the highest priority from the listener request to
// use with prompts and rules. If none of the given interfaces are included in
// interfacePriorities, or the list is empty, return "other".
func SelectSingleInterface(interfaces []string) string {
	bestIface := "other"
	bestPriority := len(interfacePriorities)
	for _, iface := range interfaces {
		priority, exists := interfacePriorities[iface]
		if !exists {
			continue
		}
		if priority < bestPriority {
			bestPriority = priority
			bestIface = iface
		}
	}
	return bestIface
}

// Returns the list of available permissions for the given interface.
func AvailablePermissions(iface string) ([]string, error) {
	available, exist := interfacePermissionsAvailable[iface]
	if !exist {
		return nil, fmt.Errorf("cannot get available permissions: unsupported interface: %s", iface)
	}
	return available, nil
}

// Convert AppArmor permissions to a list of abstract permissions.
func AbstractPermissionsFromAppArmorPermissions(iface string, permissions interface{}) ([]string, error) {
	switch iface {
	case "home", "camera":
		return abstractPermissionsFromAppArmorFilePermissions(iface, permissions)
	}
	return nil, fmt.Errorf("cannot parse AppArmor permissions: unsupported interface: %s", iface)
}

// Convert AppArmor file permissions to a list of abstract permissions.
func abstractPermissionsFromAppArmorFilePermissions(iface string, permissions interface{}) ([]string, error) {
	filePerms, ok := permissions.(notify.FilePermission)
	if !ok {
		return nil, fmt.Errorf("failed to parse the given permissions as file permissions")
	}
	abstractPermsAvailable, exists := interfacePermissionsAvailable[iface]
	if !exists {
		// This should never happen, since iface is checked in the calling function.
		return nil, fmt.Errorf("internal error: no permissions list defined for interface: %s", iface)
	}
	abstractPermsMap, exists := interfaceFilePermissionsMaps[iface]
	if !exists {
		// This should never happen, since iface is checked in the calling function.
		return nil, fmt.Errorf("internal error: no file permissions map defined for interface: %s", iface)
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
			return nil, fmt.Errorf("internal error: no permission map defined for abstract permission %s for interface %s", abstractPerm, iface)
		}
		if filePerms&aaPermMapping != 0 {
			abstractPerms = append(abstractPerms, abstractPerm)
			filePerms &= ^aaPermMapping
		}
	}
	if filePerms != notify.FilePermission(0) {
		return nil, fmt.Errorf("received unexpected permission for interface %s in AppArmor permission mask: %v", iface, filePerms)
	}
	if len(abstractPerms) == 0 {
		origMask := permissions.(notify.FilePermission)
		return nil, fmt.Errorf("no abstract permissions after parsing AppArmor permissions for interface: %s; original file permissions: %v", iface, origMask)
	}
	return abstractPerms, nil
}

func AbstractPermissionsFromList(iface string, permissions []string) ([]string, error) {
	availablePerms, ok := interfacePermissionsAvailable[iface]
	if !ok {
		return nil, fmt.Errorf("unsupported interface: %s", iface)
	}
	permsSet := make(map[string]bool, len(permissions))
	for _, perm := range permissions {
		if !strutil.ListContains(availablePerms, perm) {
			return nil, fmt.Errorf("unsupported permission for %s interface: %s", iface, perm)
		}
		permsSet[perm] = true
	}
	if len(permsSet) == 0 {
		return nil, ErrPermissionsListEmpty
	}
	permissionsList := make([]string, 0, len(permsSet))
	for _, perm := range availablePerms {
		if exists := permsSet[perm]; exists {
			permissionsList = append(permissionsList, perm)
		}
	}
	return permissionsList, nil
}

// Convert abstract permissions to AppArmor permissions.
func AbstractPermissionsToAppArmorPermissions(iface string, permissions []string) (interface{}, error) {
	switch iface {
	case "home", "camera":
		return abstractPermissionsToAppArmorFilePermissions(iface, permissions)
	}
	return nil, fmt.Errorf("cannot convert abstract permissions to AppArmor permissions: unsupported interface: %s", iface)
}

func abstractPermissionsToAppArmorFilePermissions(iface string, permissions []string) (notify.FilePermission, error) {
	if len(permissions) == 0 {
		return notify.FilePermission(0), ErrPermissionsListEmpty
	}
	filePermsMap, exists := interfaceFilePermissionsMaps[iface]
	if !exists {
		// This should never happen, since iface is checked in the calling function
		return notify.FilePermission(0), fmt.Errorf("internal error: no AppArmor file permissions map defined for interface: %s", iface)
	}
	filePerms := notify.FilePermission(0)
	for _, perm := range permissions {
		permMask, exists := filePermsMap[perm]
		if !exists {
			// Should not occur, since stored permissions list should have been validated
			return notify.FilePermission(0), fmt.Errorf("no AppArmor file permission mapping for %s interface with abstract permission: %s", iface, perm)
		}
		filePerms |= permMask
	}
	if filePerms&(notify.AA_MAY_EXEC|notify.AA_MAY_WRITE|notify.AA_MAY_READ|notify.AA_MAY_APPEND|notify.AA_MAY_CREATE) != 0 {
		filePerms |= notify.AA_MAY_OPEN
	}
	return filePerms, nil
}

var (
	// The following must be escaped if used as literals in a path pattern:
	problematicChars = `{}\[\]\?`
	// A single safe or escaped unsafe char in a path
	safePathChar = fmt.Sprintf(`([^%s]|\\[%s])`, problematicChars, problematicChars)

	// The following matches valid path patterns
	allowablePathPattern       = fmt.Sprintf(`^/%s*([^\\]?{%s*(,%s*)+})?$`, safePathChar, safePathChar, safePathChar)
	allowablePathPatternRegexp = regexp.MustCompile(allowablePathPattern)
)

// Expands a group, if it exists, in the path pattern, and creates a new
// string for every option in that group.
func ExpandPathPattern(pattern string) ([]string, error) {
	errPrefix := "invalid path pattern"
	var basePattern string
	groupStrings := make([]string, 0, strings.Count(pattern, ",")+1)
	sawGroup := false
	var currGroupStart int
	index := 0
	for index < len(pattern) {
		switch pattern[index] {
		case '\\':
			index += 1
			if index == len(pattern) {
				return nil, fmt.Errorf(`%s: trailing non-escaping '\' character: %q`, errPrefix, pattern)
			}
		case '{':
			if sawGroup {
				return nil, fmt.Errorf(`%s: multiple unescaped '{' characters: %q`, errPrefix, pattern)
			}
			if index == len(pattern)-1 {
				return nil, fmt.Errorf(`%s: trailing unescaped '{' character: %q`, errPrefix, pattern)
			}
			basePattern = pattern[:index]
			sawGroup = true
			currGroupStart = index + 1
		case '}':
			if !sawGroup || currGroupStart == -1 {
				return nil, fmt.Errorf(`%s: unmatched '}' character: %q`, errPrefix, pattern)
			}
			if index != len(pattern)-1 {
				return nil, fmt.Errorf(`%s: characters after group closed by '}': %s`, errPrefix, pattern)
			}
			currGroup := pattern[currGroupStart:index]
			groupStrings = append(groupStrings, currGroup)
			currGroupStart = -1
		case ',':
			currGroup := pattern[currGroupStart:index]
			groupStrings = append(groupStrings, currGroup)
			currGroupStart = index + 1
		}
		index += 1
	}
	if !sawGroup {
		return []string{trimDuplicates(pattern)}, nil
	}
	if currGroupStart != -1 {
		return nil, fmt.Errorf(`%s: unmatched '{' character: %q`, errPrefix, pattern)
	}
	expanded := make([]string, len(groupStrings))
	for i, str := range groupStrings {
		combined := basePattern + str
		expanded[i] = trimDuplicates(combined)
	}
	return expanded, nil
}

var (
	duplicateSlashes    = regexp.MustCompile(`(^|[^\\])/+`)
	charsDoublestar     = regexp.MustCompile(`([^/\\])\*\*`)
	doublestarChars     = regexp.MustCompile(`([^\\])\*\*([^/])`)
	duplicateDoublestar = regexp.MustCompile(`/\*\*(/\*\*)+`)
)

func trimDuplicates(pattern string) string {
	pattern = duplicateSlashes.ReplaceAllString(pattern, `${1}/`)
	pattern = charsDoublestar.ReplaceAllString(pattern, `${1}*`)
	pattern = doublestarChars.ReplaceAllString(pattern, `${1}*${2}`)
	pattern = duplicateDoublestar.ReplaceAllString(pattern, `/**`)
	if strings.HasSuffix(pattern, "/**/*") {
		// Strip trailing "/*" from suffix
		return pattern[:len(pattern)-2]
	}
	return pattern
}

type priorityType int

const (
	worstPriority priorityType = iota
	priorityGlobDoublestar
	priorityTerminalDoublestar
	priorityDoublestar
	priorityGlob
	prioritySinglestar
	priorityTerminated
	priorityLiteral
)

type nextPatternsContainer struct {
	currPriority    priorityType
	nextPatternsMap map[string]int
}

func (np *nextPatternsContainer) addWithPriority(priority priorityType, pattern string, e int) {
	if priority < np.currPriority {
		return
	}
	if priority > np.currPriority {
		np.nextPatternsMap = make(map[string]int)
		np.currPriority = priority
	}
	np.nextPatternsMap[pattern] = e
}

func (np *nextPatternsContainer) nextPatterns() map[string]int {
	return np.nextPatternsMap
}

// Determines which of the given path patterns is the most specific (top priority).
//
// Assumes that all of the given patterns satisfy ValidatePathPattern(), so this
// is not verified as part of this function. Additionally, also assumes that the
// patterns have been previously expanded using ExpandPathPattern(), so there
// are no groups in any of the patterns.
//
// Below are some sample patterns, in order of precedence, though precedence is
// only guaranteed between two patterns which may match the same path:
//
//	# literals
//	- /foo/bar/baz
//	- /foo/bar/
//	# terminated
//	- /foo/bar
//	# singlestars
//	- /foo/bar/*/baz
//	- /foo/bar/*/
//	- /foo/bar/*/*baz
//	- /foo/bar/*/*
//	- /foo/bar/*
//	- /foo/bar/*/**
//	# glob
//	- /foo/bar*baz
//	- /foo/bar*/baz
//	- /foo/bar*/baz/**
//	- /foo/bar*/
//	- /foo/bar*/*baz
//	- /foo/bar*/*/baz
//	- /foo/bar*/*/
//	- /foo/bar*/*
//	- /foo/bar*
//	# doublestars
//	- /foo/bar/**/baz
//	- /foo/bar/**/*baz/
//	- /foo/bar/**/*baz
//	# terminal doublestar
//	- /foo/bar/**/        # These are tough... usually, /foo/bar/**/ would have precedence over
//	- /foo/bar/**/*       # precedence over /foo/bar/**/*baz, but in this case,
//	- /foo/bar/**         # the trailing *baz adds more specificity.
//	# glob with immediate doublestar
//	- /foo/bar*/**/baz
//	- /foo/bar*/**/
//	- /foo/bar*/**
func GetHighestPrecedencePattern(patterns []string) (string, error) {
	if len(patterns) == 0 {
		return "", ErrNoPatterns
	}
	// Map pattern to number of escaped characters which have been seen
	remainingPatterns := make(map[string]int, len(patterns))
	for _, pattern := range patterns {
		remainingPatterns[pattern] = 0
	}
	// Loop over index into each pattern until only one pattern left
	for i := 0; len(remainingPatterns) > 1; i++ {
		nextPatterns := nextPatternsContainer{}
		for pattern, e := range remainingPatterns {
			// For each pattern, e is number of escaped chars, thus the number
			// which should be added to the index to compare equivalent indices
			// into all patterns
			patternLen := len(pattern)
			if i+e == patternLen {
				nextPatterns.addWithPriority(priorityTerminated, pattern, e)
				continue
			}
			// Check for '*' before '\\', since "\\*" is literal '*'
			if pattern[i+e] == '*' {
				if patternLen >= i+e+4 && pattern[i+e+1:i+e+4] == "/**" {
					// Next parts of pattern are "*/**"
					nextPatterns.addWithPriority(priorityGlobDoublestar, pattern, e)
					continue
				}
				nextPatterns.addWithPriority(priorityGlob, pattern, e)
				continue
			}
			if pattern[i+e] == '\\' {
				e += 1
				if patternLen == i+e {
					return "", fmt.Errorf(`invalid path pattern: trailing '\' character: %q`, pattern)
				}
			}
			// Can safely check for '/' after '\\', since it is '/' either way
			if pattern[i+e] != '/' || patternLen < i+e+2 || pattern[i+e+1] != '*' {
				// Next parts of pattern are not "/*" or "/**"
				nextPatterns.addWithPriority(priorityLiteral, pattern, e)
				continue
			}
			// pattern[i+e:i+e+2] must be "/*"
			if patternLen < i+e+3 || pattern[i+e+2] != '*' {
				// pattern[i+e:i+e+3] must not be "/**"
				nextPatterns.addWithPriority(prioritySinglestar, pattern, e)
				continue
			}
			if patternLen == i+e+3 || (pattern[i+e+3] == '/' && (patternLen == i+e+4 || (patternLen == i+e+5 && pattern[i+e+4] == '*'))) {
				// pattern[1+e:i+e+3 must terminate with "/**" or "/**/".
				// Doesn't matter if pattern ends in "/**/*", that will be
				// caught in the usual case later.
				nextPatterns.addWithPriority(priorityTerminalDoublestar, pattern, e)
				continue
			}
			// pattern has non-terminal "/**" next
			nextPatterns.addWithPriority(priorityDoublestar, pattern, e)
		}
		remainingPatterns = nextPatterns.nextPatterns()
	}
	p := ""
	for pattern := range remainingPatterns {
		p = pattern
	}
	return p, nil
}

// Checks that the given path pattern is valid. Returns nil if so, otherwise
// returns an error.
func ValidatePathPattern(pattern string) error {
	if !doublestar.ValidatePattern(pattern) {
		return fmt.Errorf("invalid path pattern: %q", pattern)
	}
	if pattern == "" || pattern[0] != '/' {
		return fmt.Errorf("invalid path pattern: must start with '/': %q", pattern)
	}
	if !allowablePathPatternRegexp.MatchString(pattern) {
		return fmt.Errorf("invalid path pattern: cannot contain unescaped '[', ']', or '?',  or >1 unescaped '{' or '}': %q", pattern)
	}
	return nil
}

// Checks that the given outcome is valid. Returns nil if so, otherwise
// returns ErrInvalidOutcome.
func ValidateOutcome(outcome OutcomeType) error {
	switch outcome {
	case OutcomeAllow, OutcomeDeny:
		return nil
	default:
		return ErrInvalidOutcome
	}
}

// ValidateLifespanParseDuration checks that the given lifespan is valid and
// that the given duration is valid for that lifespan. If the lifespan is
// LifespanTimespan, then duration must be a string parsable by
// time.ParseDuration(), representing the duration of time for which the rule
// should be valid. Otherwise, it must be empty. Returns an error if any of the
// above are invalid, otherwise computes the expiration time of the rule based
// on the current time and the given duration and returns it.
func ValidateLifespanParseDuration(lifespan LifespanType, duration string) (string, error) {
	expirationString := ""
	switch lifespan {
	case LifespanForever, LifespanSession, LifespanSingle:
		if duration != "" {
			return "", ErrInvalidDurationForLifespan
		}
	case LifespanTimespan:
		if duration == "" {
			return "", ErrInvalidDurationEmpty
		}
		parsedDuration, err := time.ParseDuration(duration)
		if err != nil {
			return "", ErrInvalidDurationParseError
		}
		if parsedDuration <= 0 {
			return "", ErrInvalidDurationNegative
		}
		expirationString = time.Now().Add(parsedDuration).Format(time.RFC3339)
	default:
		return "", ErrInvalidLifespan
	}
	return expirationString, nil
}

// Ensures that the given constraints, outcome, lifespan, and duration are valid
// for the given interface. If not, returns an error. Additionally, converts the
// given duration to an expiration timestamp.
func ValidateConstraintsOutcomeLifespanDuration(iface string, constraints *Constraints, outcome OutcomeType, lifespan LifespanType, duration string) (string, error) {
	if err := constraints.ValidateForInterface(iface); err != nil {
		return "", err
	}
	if err := ValidateOutcome(outcome); err != nil {
		return "", err
	}
	return ValidateLifespanParseDuration(lifespan, duration)
}

func StripTrailingSlashes(path string) string {
	for path[len(path)-1] == '/' {
		path = path[:len(path)-1]
	}
	return path
}

// Returns true if the given pattern matches the given path. The pattern should
// not contain groups, and should likely have been an output of ExpandPathPattern.
//
// The doublestar package has special cases for patterns ending in `/**`, `**/`,
// and `/**/`: `/foo/**`, and `/foo/**/` both match `/foo`, but not `/foo/`.
//
// Since paths to directories are received with trailing slashes, we want to
// ensure that patterns without trailing slashes match paths with trailing
// slashes. However, patterns with trailing slashes should not match paths
// without trailing slashes.
func PathPatternMatches(pattern string, path string) (bool, error) {
	matched, err := doublestar.Match(pattern, path)
	if err != nil {
		return false, err
	}
	if matched {
		return true, nil
	}
	patternSlash := doublestarSuffix.ReplaceAllString(pattern, `/`)
	return doublestar.Match(patternSlash, path)
}

var doublestarSuffix = regexp.MustCompile(`(/\*\*)?/?$`)
