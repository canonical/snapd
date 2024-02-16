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
)

var ErrPermissionNotInList = errors.New("permission not found in permissions list")
var ErrInvalidSnapLabel = errors.New("the given label cannot be converted to snap and app")
var ErrInvalidOutcome = errors.New(`invalid rule outcome; must be "allow" or "deny"`)
var ErrInvalidLifespan = errors.New("invalid lifespan")
var ErrInvalidDurationForLifespan = fmt.Errorf(`invalid duration: duration must be empty unless lifespan is "%v"`, LifespanTimespan)
var ErrInvalidDurationEmpty = fmt.Errorf(`invalid duration: duration must be specified if lifespan is "%v"`, LifespanTimespan)
var ErrInvalidDurationParseError = errors.New("invalid duration: error parsing duration string")
var ErrInvalidDurationNegative = errors.New("invalid duration: duration must be greater than zero")
var ErrUnrecognizedFilePermission = errors.New("file permissions mask contains unrecognized permission")

type Constraints struct {
	PathPattern string           `json:"path-pattern"`
	Permissions []PermissionType `json:"permissions"`
}

// ValidateForInterface returns nil if the constraints are valid for the given
// interface, otherwise returns an error.
func (constraints *Constraints) ValidateForInterface(iface string) error {
	switch iface {
	case "home", "camera":
	default:
		return fmt.Errorf("constraints incompatible with the given interface: %s", iface)
	}
	// TODO: change to this once PR #13730 is merged:
	// return ValidatePathPattern(constraints.PathPattern)
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
func (constraints *Constraints) RemovePermission(permission PermissionType) error {
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

// PermissionMaskToPermissionsList converts the given aparmor file permission
// mask into a list of permissions. If the mask contains an unrecognized file
// permission, returns an error, along with the list of all recognized
// permissions in the mask.
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

// PermissionsListContains returns true if the given permissions list contains
// the given permission, else false.
func PermissionsListContains(list []PermissionType, permission PermissionType) bool {
	for _, perm := range list {
		if perm == permission {
			return true
		}
	}
	return false
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
