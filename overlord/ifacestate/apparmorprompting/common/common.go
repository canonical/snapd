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
)

var ErrPermissionNotInList = errors.New("permission not found in permissions list")
var ErrInvalidSnapLabel = errors.New("the given label cannot be converted to snap and app")
var ErrInvalidPathPattern = errors.New("the given path pattern is not allowed")
var ErrInvalidOutcome = errors.New(`invalid rule outcome; must be "allow" or "deny"`)
var ErrInvalidLifespan = errors.New("invalid lifespan")
var ErrInvalidDurationForLifespan = fmt.Errorf(`invalid duration: duration must be empty unless lifespan is "%v"`, LifespanTimespan)
var ErrInvalidDurationEmpty = fmt.Errorf(`invalid duration: duration must be specified if lifespan is "%v"`, LifespanTimespan)
var ErrInvalidDurationParseError = errors.New("invalid duration: error parsing duration string")
var ErrInvalidDurationNegative = errors.New("invalid duration: duration must be greater than zero")
var ErrNoPatterns = errors.New("no patterns given, cannot establish precedence")
var ErrUnrecognizedFilePermission = errors.New("file permissions mask contains unrecognized permission")

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

// If kernel request contains multiple interfaces, one must take priority.
// Lower value is higher priority, and entries should be in priority order.
var interfacePriorities = map[string]int{
	"home":   0,
	"camera": 1,
}

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

// Extracts the snap and app names from the given label.
// If the label is not of the form 'snap.<snap>.<app>', returns an error, and
// returns the label as both the snap and the app.
func LabelToSnapApp(label string) (snap string, app string, err error) {
	components := strings.Split(label, ".")
	if len(components) != 3 || components[0] != "snap" {
		return label, label, ErrInvalidSnapLabel
	}
	snap = components[1]
	app = components[2]
	return snap, app, nil
}

// Select the interface with the highest priority from the listener request to
// use with prompting requests and rules. If none of the given interfaces are
// included in interfacePriorities, or the list is empty, return "other".
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

// Converts the given aparmor file permission mask into a list of permissions.
// If the mask contains an unrecognized file permission, returns an error,
// along with the list of all recognized permissions in the mask.
func PermissionMaskToPermissionsList(p notify.FilePermission) ([]PermissionType, error) {
	perms := make([]PermissionType, 0, 1)
	// Want to be memory efficient, as this list could be stored for a long time.
	// Most of the time, only one permission bit will be set anyway.
	if p&notify.AA_MAY_EXEC != 0 {
		perms = append(perms, PermissionExecute)
	}
	if p&notify.AA_MAY_WRITE != 0 {
		perms = append(perms, PermissionWrite)
	}
	if p&notify.AA_MAY_READ != 0 {
		perms = append(perms, PermissionRead)
	}
	if p&notify.AA_MAY_APPEND != 0 {
		perms = append(perms, PermissionAppend)
	}
	if p&notify.AA_MAY_CREATE != 0 {
		perms = append(perms, PermissionCreate)
	}
	if p&notify.AA_MAY_DELETE != 0 {
		perms = append(perms, PermissionDelete)
	}
	if p&notify.AA_MAY_OPEN != 0 {
		perms = append(perms, PermissionOpen)
	}
	if p&notify.AA_MAY_RENAME != 0 {
		perms = append(perms, PermissionRename)
	}
	if p&notify.AA_MAY_SETATTR != 0 {
		perms = append(perms, PermissionSetAttr)
	}
	if p&notify.AA_MAY_GETATTR != 0 {
		perms = append(perms, PermissionGetAttr)
	}
	if p&notify.AA_MAY_SETCRED != 0 {
		perms = append(perms, PermissionSetCred)
	}
	if p&notify.AA_MAY_GETCRED != 0 {
		perms = append(perms, PermissionGetCred)
	}
	if p&notify.AA_MAY_CHMOD != 0 {
		perms = append(perms, PermissionChangeMode)
	}
	if p&notify.AA_MAY_CHOWN != 0 {
		perms = append(perms, PermissionChangeOwner)
	}
	if p&notify.AA_MAY_CHGRP != 0 {
		perms = append(perms, PermissionChangeGroup)
	}
	if p&notify.AA_MAY_LOCK != 0 {
		perms = append(perms, PermissionLock)
	}
	if p&notify.AA_EXEC_MMAP != 0 {
		perms = append(perms, PermissionExecuteMap)
	}
	if p&notify.AA_MAY_LINK != 0 {
		perms = append(perms, PermissionLink)
	}
	if p&notify.AA_MAY_ONEXEC != 0 {
		perms = append(perms, PermissionChangeProfileOnExec)
	}
	if p&notify.AA_MAY_CHANGE_PROFILE != 0 {
		perms = append(perms, PermissionChangeProfile)
	}
	if !p.IsValid() {
		return perms, ErrUnrecognizedFilePermission
	}
	return perms, nil
}

// Returns true if the given permissions list contains the given permission, else false.
func PermissionsListContains(list []PermissionType, permission PermissionType) bool {
	for _, perm := range list {
		if perm == permission {
			return true
		}
	}
	return false
}

// Removes the given permission from the given list of permissions.
// Returns a new list with all instances of the given permission removed.
// If the given permission is not found in the list, returns an error, along
// with the original list.
func RemovePermissionFromList(list []PermissionType, permission PermissionType) ([]PermissionType, error) {
	if len(list) == 0 {
		return list, ErrPermissionNotInList
	}
	newList := make([]PermissionType, 0, len(list)-1)
	found := false
	for _, perm := range list {
		if perm == permission {
			found = true
			continue
		}
		newList = append(newList, perm)
	}
	if !found {
		return list, ErrPermissionNotInList
	}
	return newList, nil
}

var allowablePathPatternRegexp = regexp.MustCompile(`^(/|(/[^/*{}]+)*(/\*|(/\*\*)?(/\*\.[^/*{}]+)?)?)$`)

// Checks that the given path pattern is valid.  Returns nil if so, otherwise
// returns ErrInvalidPathPattern.
func ValidatePathPattern(pattern string) error {
	if !allowablePathPatternRegexp.MatchString(pattern) {
		return ErrInvalidPathPattern
	}
	return nil
}

// Checks that the given outcome is valid.  Returns nil if so, otherwise
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
// that the given duration is valid for that lifespan.  If the lifespan is
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
	}
	return expirationString, nil
}

// Determines which of the path patterns in the given patterns list is the
// most specific, and thus has the highest priority.  Assumes that all of the
// given patterns satisfy ValidatePathPattern(), so this is not verified as
// part of this function.
//
// Exact matches always have the highest priority.  Then, the pattern with the
// most specific file extension has priority.  If no matching patterns have
// file extensions (or if multiple share the most specific file extension),
// then the longest pattern (excluding trailing * wildcards) is the most
// specific.  Lastly, the priority order is: .../foo > .../foo/* > .../foo/**
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
		extPrefix := "*."
		if !strings.HasPrefix(finalSegment, extPrefix) {
			continue
		}
		extension := finalSegment[len(extPrefix):]
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

func StripTrailingSlashes(path string) string {
	for path[len(path)-1] == '/' {
		path = path[:len(path)-1]
	}
	return path
}

func PathPatternMatches(pathPattern string, path string) (bool, error) {
	path = StripTrailingSlashes(path)
	matched, err := doublestar.Match(pathPattern, path)
	if err != nil {
		return false, err
	}
	if matched {
		return true, nil
	}
	return doublestar.Match(pathPattern, path+"/")
}
