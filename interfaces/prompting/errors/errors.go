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

// The errors package defines common error types which are used across the
// prompting subsystems, along with constructors for specific errors based on
// those broader types.
package errors

import (
	"errors"
	"fmt"
	"time"
)

var (
	// NotFound errors when a prompt or rule is not found
	ErrPromptNotFound = errors.New("cannot find prompt with the given ID for the given user")
	ErrRuleNotFound   = errors.New("cannot find rule with the given ID")
	ErrRuleNotAllowed = errors.New("user not allowed to request the rule with the given ID")

	// Unsupported value errors, which are defined as functions below
	ErrInvalidOutcome       = errInvalidOutcome
	ErrInvalidLifespan      = errInvalidLifespan
	ErrRuleLifespanSingle   = errRuleLifespanSingle
	ErrInvalidInterface     = errInvalidInterface
	ErrInvalidPermissions   = errInvalidPermissions
	ErrPermissionsListEmpty = errPermissionsListEmpty
	// Parse errors, which are defined as functions below
	ErrInvalidDuration    = errInvalidDuration
	ErrInvalidExpiration  = errInvalidExpiration
	ErrInvalidPathPattern = errInvalidPathPattern
	// Validation errors, which should never be used directly apart from
	// checking errors.Is(), and should otherwise always be wrapped in
	// dedicated error types defined below.
	ErrReplyNotMatchRequestedPath        = errors.New("path pattern in reply constraints does not match originally requested path")
	ErrReplyNotMatchRequestedPermissions = errors.New("permissions in reply constraints do not include all requested permissions")
	ErrRuleConflict                      = errors.New("a rule with conflicting path pattern and permission already exists in the rule database")

	// Internal errors which are not handled specifically
	ErrPromptsClosed      = errors.New("prompts backend has already been closed")
	ErrRulesClosed        = errors.New("rules backend has already been closed")
	ErrTooManyPrompts     = errors.New("cannot add new prompts, too many outstanding")
	ErrRuleIDConflict     = errors.New("internal error: rule with conflicting ID already exists in the rule database")
	ErrRuleDBInconsistent = errors.New("internal error: interfaces requests rule database left inconsistent")

	// Errors which are used internally and should never be returned over the API
	ErrNoMatchingRule          = errors.New("no rule matches the given path")
	ErrInvalidID               = errors.New("invalid ID: format must be parsable as uint64")
	ErrRuleExpirationInThePast = errors.New("cannot have expiration time in the past")
)

// Unsupported value errors, which are built from the UnsupportedValueError struct

func errInvalidOutcome(unsupported string, supported []string) *UnsupportedValueError {
	return &UnsupportedValueError{
		Field:       "outcome",
		Msg:         fmt.Sprintf("invalid outcome: %q", unsupported),
		Unsupported: unsupported,
		Supported:   supported,
	}
}

func errInvalidLifespan(unsupported string, supported []string) *UnsupportedValueError {
	return &UnsupportedValueError{
		Field:       "lifespan",
		Msg:         fmt.Sprintf("invalid lifespan: %q", unsupported),
		Unsupported: unsupported,
		Supported:   supported,
	}
}

func errRuleLifespanSingle(supported []string) *UnsupportedValueError {
	return &UnsupportedValueError{
		Field:       "lifespan",
		Msg:         `cannot create rule with lifespan "single"`,
		Unsupported: "string",
		Supported:   supported,
	}
}

func errInvalidInterface(unsupported string, supported []string) *UnsupportedValueError {
	return &UnsupportedValueError{
		Field:       "interface",
		Msg:         fmt.Sprintf("invalid interface: %q", unsupported),
		Unsupported: unsupported,
		Supported:   supported,
	}
}

func errInvalidPermissions(iface string, unsupported []string, supported []string) *UnsupportedValueError {
	return &UnsupportedValueError{
		Field:       "permissions",
		Msg:         fmt.Sprintf("invalid permissions for %s interface: %v", iface, unsupported),
		Unsupported: unsupported,
		Supported:   supported,
	}
}

func errPermissionsListEmpty(iface string, supported []string) *UnsupportedValueError {
	return &UnsupportedValueError{
		Field:       "permissions",
		Msg:         fmt.Sprintf("invalid permissions for %s interface: permissions list empty", iface),
		Unsupported: []string{},
		Supported:   supported,
	}
}

// Marker for UnsupportedValueError, should never be returned as an actual
// error value.
var ErrUnsupportedValue = errors.New("unsupported value")

// UnsupportedValueError is a wrapper for errors about a field having an
// unsupported value when there is a fixed set of supported values.
type UnsupportedValueError struct {
	Field       string
	Msg         string
	Unsupported interface{} // either string or []string
	Supported   []string
}

func (e *UnsupportedValueError) Error() string {
	return e.Msg
}

func (e *UnsupportedValueError) Is(target error) bool {
	return target == ErrUnsupportedValue
}

// Parse errors, which are built from the ParseError struct

func errInvalidDuration(invalid string, reason string) *ParseError {
	return &ParseError{
		Field:   "duration",
		Msg:     fmt.Sprintf("invalid duration: %s: %q", reason, invalid),
		Invalid: invalid,
	}
}

func errInvalidExpiration(invalid time.Time, reason string) *ParseError {
	timeStr := invalid.Format(time.RFC3339Nano)
	return &ParseError{
		Field:   "expiration",
		Msg:     fmt.Sprintf("invalid expiration: %s: %q", reason, timeStr),
		Invalid: timeStr,
	}
}

func errInvalidPathPattern(invalid string, reason string) *ParseError {
	return &ParseError{
		Field:   "path-pattern",
		Msg:     fmt.Sprintf("invalid path pattern: %s: %q", reason, invalid),
		Invalid: invalid,
	}
}

// Marker for ParseError, should never be returned as an actual error value.
var ErrParseError = errors.New("parse error")

// ParseError is a wrapper for errors about a field whose value is malformed.
type ParseError struct {
	Field   string
	Msg     string
	Invalid string
}

func (e *ParseError) Error() string {
	return e.Msg
}

func (e *ParseError) Is(target error) bool {
	return target == ErrParseError
}

// Validation errors, which are all uniquely defined here

// RequestedPathNotMatchedError stores a path pattern from a reply which doesn't
// match the requested path.
type RequestedPathNotMatchedError struct {
	Received  string
	Requested string
}

func (e *RequestedPathNotMatchedError) Error() string {
	return fmt.Sprintf("%v %q: %q", ErrReplyNotMatchRequestedPath.Error(), e.Requested, e.Received)
}

func (e *RequestedPathNotMatchedError) Unwrap() error {
	return ErrReplyNotMatchRequestedPath
}

// RequestedPermissionsNotMatchedError stores the list of permissions from a
// reply which doesn't include all the requested permissions.
type RequestedPermissionsNotMatchedError struct {
	Received  []string
	Requested []string
}

func (e *RequestedPermissionsNotMatchedError) Error() string {
	return fmt.Sprintf("%v %v: %v", ErrReplyNotMatchRequestedPermissions.Error(), e.Requested, e.Received)
}

func (e *RequestedPermissionsNotMatchedError) Unwrap() error {
	return ErrReplyNotMatchRequestedPermissions
}

// RuleConflict stores the permission and rendered variant which conflicted
// with that of another rule, along with the ID of that conflicting rule.
type RuleConflict struct {
	Permission    string
	Variant       string
	ConflictingID string
}

// RuleConflictError stores a list of conflicts with existing rules which
// occurred when attempting to add or patch a rule.
type RuleConflictError struct {
	Conflicts []RuleConflict
}

func (e *RuleConflictError) Error() string {
	return ErrRuleConflict.Error()
}

func (e *RuleConflictError) Unwrap() error {
	return ErrRuleConflict
}
