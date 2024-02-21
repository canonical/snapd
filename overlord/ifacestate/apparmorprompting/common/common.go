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

package common

import (
	"encoding/base32"
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/snapcore/snapd/sandbox/apparmor/notify"
	"github.com/snapcore/snapd/strutil"
)

var (
	ErrInvalidSnapLabel           = errors.New("the given label cannot be converted to snap and app")
	ErrInvalidOutcome             = errors.New(`invalid outcome; must be "allow" or "deny"`)
	ErrInvalidLifespan            = errors.New("invalid lifespan")
	ErrInvalidDurationForLifespan = fmt.Errorf(`invalid duration: duration must be empty unless lifespan is "%v"`, LifespanTimespan)
	ErrInvalidDurationEmpty       = fmt.Errorf(`invalid duration: duration must be specified if lifespan is "%v"`, LifespanTimespan)
	ErrInvalidDurationParseError  = errors.New("invalid duration: error parsing duration string")
	ErrInvalidDurationNegative    = errors.New("invalid duration: duration must be greater than zero")
	ErrPermissionNotInList        = errors.New("permission not found in permissions list")
	ErrPermissionsListEmpty       = errors.New("permissions list empty")
	ErrUnrecognizedFilePermission = errors.New("file permissions mask contains unrecognized permission")
)

type Constraints struct {
	PathPattern string   `json:"path-pattern"`
	Permissions []string `json:"permissions"`
}

// ValidateForInterface returns nil if the constraints are valid for the given
// interface, otherwise returns an error.
func (constraints *Constraints) ValidateForInterface(iface string) error {
	switch iface {
	case "home", "camera":
		// TODO: change to this once PR #13730 is merged:
		// if err := ValidatePathPattern(constraints.PathPattern); err != nil {
		//	return err
		// }
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

// Match returns true if the constraints match the given path, otherwise false.
//
// If the constraints or path are invalid, returns an error.
func (constraints *Constraints) Match(path string) (bool, error) {
	// TODO: change to this once PR #13730 is merged:
	// return PathPatternMatch(constraints.PathPattern, path)
	return true, nil
}

// RemovePermission removes every instance of the given permission from the
// permissions list associated with the constraints. If the permission does
// not exist in the list, returns ErrPermissionNotInList.
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

// ContainPermissions returns true if the constraints include every one of the
// givne permissions.
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

// AsBool returns the outcome as a boolean, or an error if it cannot be parsed.
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

// TimestampToTime converts the given timestamp string to a time.Time in Local
// time. The timestamp string is expected to be of the format time.RFC3339Nano.
// If it cannot be parsed as such, returns an error.
func TimestampToTime(timestamp string) (time.Time, error) {
	t, err := time.Parse(time.RFC3339Nano, timestamp)
	if err != nil {
		return t, err
	}
	return t.Local(), nil
}

// CurrentTimestamp returns the current time as a time.RFC3339Nano string.
func CurrentTimestamp() string {
	return time.Now().Format(time.RFC3339Nano)
}

// NewID returns a new unique ID.
//
// The ID is the current unix time in nanoseconds encoded as base32.
func NewID() string {
	nowUnix := uint64(time.Now().UnixNano())
	nowBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(nowBytes, nowUnix)
	id := base32.StdEncoding.EncodeToString(nowBytes)
	return id
}

// NewIDAndTimestamp returns a new unique ID and corresponding timestamp.
//
// The ID is the current unix time in nanoseconds encoded as a string in base32.
// The timestamp is the same time, encoded as a string in time.RFC3339Nano.
func NewIDAndTimestamp() (id string, timestamp string) {
	now := time.Now()
	nowUnix := uint64(now.UnixNano())
	nowBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(nowBytes, nowUnix)
	id = base32.StdEncoding.EncodeToString(nowBytes)
	timestamp = now.Format(time.RFC3339Nano)
	return id, timestamp
}

// LabelToSnapApp extracts the snap and app names from the given label.
//
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

// SelectSingleInterface selects the interface with the highest priority from
// the given list. If none of the given interfaces are included in
// interfacePriorities, or if the list is empty, return "other".
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

// AvailablePermissions returns the list of available permissions for the given
// interface.
func AvailablePermissions(iface string) ([]string, error) {
	available, exist := interfacePermissionsAvailable[iface]
	if !exist {
		return nil, fmt.Errorf("cannot get available permissions: unsupported interface: %s", iface)
	}
	return available, nil
}

// AbstractPermissionsFromAppArmorPermissions returns the list of permissions
// corresponding to the given AppArmor permissions for the given interface.
func AbstractPermissionsFromAppArmorPermissions(iface string, permissions interface{}) ([]string, error) {
	switch iface {
	case "home", "camera":
		return abstractPermissionsFromAppArmorFilePermissions(iface, permissions)
	}
	return nil, fmt.Errorf("cannot parse AppArmor permissions: unsupported interface: %s", iface)
}

// abstractPermissionsFromAppArmorFilePermissions returns the list of permissions
// corresponding to the given AppArmor file permissions for the given interface.
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

// AbstractPermissionsFromList validates the given permissions list for the
// given interface and returns a list containing those permissions in the order
// in which they occur in the list of available permissions for that interface.
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

// AbstractPermissionsToAppArmorPermissions returns AppArmor permissions
// corresponding to the given permissions for the given interface.
func AbstractPermissionsToAppArmorPermissions(iface string, permissions []string) (interface{}, error) {
	switch iface {
	case "home", "camera":
		return abstractPermissionsToAppArmorFilePermissions(iface, permissions)
	}
	return nil, fmt.Errorf("cannot convert abstract permissions to AppArmor permissions: unsupported interface: %s", iface)
}

// AbstractPermissionsToAppArmorFilePermissions returns AppArmor file
// permissions corresponding to the given permissions for the given interface.
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

// ValidateOutcome returns nil if the given outcome is valid, otherwise an error.
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

// ValidateConstraintsOutcomeLifespanDuration returns an error if the given
// constraints, outcome, lifespan, or duration are invalid. Otherwise, converts
// the given duration to an expiration timestamp and returns it and nil error.
func ValidateConstraintsOutcomeLifespanDuration(iface string, constraints *Constraints, outcome OutcomeType, lifespan LifespanType, duration string) (string, error) {
	if err := constraints.ValidateForInterface(iface); err != nil {
		return "", err
	}
	if err := ValidateOutcome(outcome); err != nil {
		return "", err
	}
	return ValidateLifespanParseDuration(lifespan, duration)
}
