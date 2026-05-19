// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2026 Canonical Ltd
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

// This file contains the Go types that represent the data carried by security
// audit events, across all event categories. It is intentionally separate
// from seclog.go, which owns the emission machinery (SecurityLogger, Setup,
// LogEvent wrappers).
//
// Design goals:
//
//  1. Spec alignment: field names and JSON tags match the security audit
//     specification directly. These types are the single source of truth for
//     what appears in the audit log.
//
//  2. Independence from internal types: no type in this file may reference
//     types from other snapd packages. This means that renaming or
//     restructuring internal types (e.g. auth.UserState) cannot silently
//     change the audit format. The translation from an internal type to an
//     audit event type is the explicit responsibility of the caller.
//
// When adding a new event category, define its types here.

package seclog

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"
)

// unknown is the placeholder for empty fields in descriptions.
const unknown = "<unknown>"

// Reason codes are stable identifiers for security audit events.
const (
	ReasonInvalidCredentials = "invalid-credentials"
	ReasonTwoFactorRequired  = "two-factor-required"
	ReasonTwoFactorFailed    = "two-factor-failed"
	ReasonInvalidAuthData    = "invalid-auth-data"
	ReasonPasswordPolicy     = "password-policy"
	ReasonInternal           = "internal"
)

// Reason describes why a security event happened. The JSON tags match
// the security audit specification field names.
type Reason struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// String returns a colon-separated representation in the form
// "<Code>:<Message>". Fields that are unset use "<unknown>" as a
// placeholder.
func (r Reason) String() string {
	code := unknown
	if r.Code != "" {
		code = r.Code
	}

	message := unknown
	if r.Message != "" {
		message = r.Message
	}

	return code + ":" + message
}

// SnapdUser represents the identity of a user for security log events.
type SnapdUser struct {
	ID             int64     `json:"snapd-user-id"`
	StoreUserName  string    `json:"store-user-name"`
	StoreUserEmail string    `json:"store-user-email"`
	Expiration     time.Time `json:"expiration"`
}

// String returns a colon-separated description of the user in the form
// "<ID>:<StoreUserEmail>:<StoreUserName>". Fields that are unset use
// "<unknown>" as a placeholder; a zero ID is considered unset.
func (u SnapdUser) String() string {
	id := unknown
	if u.ID != 0 {
		id = fmt.Sprintf("%d", u.ID)
	}

	email := unknown
	if u.StoreUserEmail != "" {
		email = u.StoreUserEmail
	}

	name := unknown
	if u.StoreUserName != "" {
		name = u.StoreUserName
	}

	return id + ":" + email + ":" + name
}

// SnapdUserState represents the full set of auditable fields for a
// snapd user. It embeds [SnapdUser] for the identity subset and adds
// the credential and discharge fields. The JSON tags are the canonical
// field names for the security logging specification (SD236, "User
// Updated") and serve as the single source of truth for field naming
// in the "changed_fields" audit attribute.
type SnapdUserState struct {
	SnapdUser
	LocalMacaroon   string   `json:"local-macaroon"`
	LocalDischarges []string `json:"local-discharges"`
	StoreMacaroon   string   `json:"store-macaroon"`
	StoreDischarges []string `json:"store-discharges"`
}

// ChangedFields returns a sorted list of field names whose values
// differ between s and other. The names are derived from JSON struct
// tags. String slice fields are compared order-independently.
// time.Time fields are compared by instant, ignoring location and any
// monotonic clock reading.
func (s SnapdUserState) ChangedFields(other SnapdUserState) []string {
	var changed []string
	sv := reflect.ValueOf(s)
	ov := reflect.ValueOf(other)
	for _, sf := range reflect.VisibleFields(sv.Type()) {
		if sf.Anonymous {
			continue
		}
		tag := strings.SplitN(sf.Tag.Get("json"), ",", 2)[0]
		if tag == "" || tag == "-" {
			continue
		}
		sval := sv.FieldByIndex(sf.Index).Interface()
		oval := ov.FieldByIndex(sf.Index).Interface()
		var equal bool
		switch sv := sval.(type) {
		case []string:
			equal = stringSlicesEqual(sv, oval.([]string))
		case time.Time:
			equal = sv.Equal(oval.(time.Time))
		default:
			equal = reflect.DeepEqual(sval, oval)
		}
		if !equal {
			changed = append(changed, tag)
		}
	}
	sort.Strings(changed)
	return changed
}

// stringSlicesEqual reports whether two string slices contain the same
// elements regardless of order. Both nil and empty slices are treated
// as equal.
func stringSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	if len(a) == 0 {
		return true
	}
	ac := make([]string, len(a))
	bc := make([]string, len(b))
	copy(ac, a)
	copy(bc, b)
	sort.Strings(ac)
	sort.Strings(bc)
	for i := range ac {
		if ac[i] != bc[i] {
			return false
		}
	}
	return true
}
